package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// isOAuthToken returns true if the key is an OAuth access token (vs a standard API key).
// OAuth tokens use prefix "sk-ant-oat01-", standard keys use "sk-ant-api03-".
func isOAuthToken(key string) bool {
	return strings.HasPrefix(key, "sk-ant-oat01-")
}

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultMaxTokens = 16384
	apiVersion       = "2023-06-01"
)

// Client is an HTTP client for the Anthropic Messages API.
type Client struct {
	keyFunc    func() string
	cachedKey  string
	keyMu      sync.RWMutex
	baseURL    string
	httpClient *http.Client

	rateLimitMu sync.RWMutex
	rateLimits  *RateLimitInfo
}

// NewClient creates a Client with the default base URL.
// The keyFunc is called to obtain the initial key and again on 401 to refresh.
func NewClient(keyFunc func() string) *Client {
	return NewClientWithBase(keyFunc, defaultBaseURL)
}

// NewClientWithBase creates a Client with a custom base URL.
func NewClientWithBase(keyFunc func() string, baseURL string) *Client {
	return &Client{
		keyFunc:   keyFunc,
		cachedKey: keyFunc(),
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// apiKey returns the cached API key.
func (c *Client) apiKey() string {
	c.keyMu.RLock()
	defer c.keyMu.RUnlock()
	return c.cachedKey
}

// refreshKey calls keyFunc to get a fresh key and caches it.
// Returns the new key and whether it differs from the old one.
func (c *Client) refreshKey() (string, bool) {
	newKey := c.keyFunc()
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	changed := newKey != c.cachedKey
	c.cachedKey = newKey
	return newKey, changed
}

// GetRateLimits returns the latest rate limit info captured from API response headers.
func (c *Client) GetRateLimits() *RateLimitInfo {
	c.rateLimitMu.RLock()
	defer c.rateLimitMu.RUnlock()
	return c.rateLimits
}

// parseRateLimitHeaders extracts rate limit utilization and reset from response headers.
// Headers follow the pattern: anthropic-ratelimit-unified-{abbrev}-utilization / -reset
// where abbrev is "5h" (five_hour) or "7d" (seven_day).
func (c *Client) parseRateLimitHeaders(h http.Header) {
	abbrevs := []struct {
		abbrev string
		name   string
	}{
		{"5h", "five_hour"},
		{"7d", "seven_day"},
	}

	var windows []RateLimitWindow
	for _, a := range abbrevs {
		utilStr := h.Get("anthropic-ratelimit-unified-" + a.abbrev + "-utilization")
		if utilStr == "" {
			continue
		}
		util, err := strconv.ParseFloat(utilStr, 64)
		if err != nil {
			continue
		}
		w := RateLimitWindow{
			Type:        a.name,
			Utilization: util,
		}
		if resetStr := h.Get("anthropic-ratelimit-unified-" + a.abbrev + "-reset"); resetStr != "" {
			if v, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				w.ResetsAt = v
			}
		}
		windows = append(windows, w)
	}

	if len(windows) > 0 {
		c.rateLimitMu.Lock()
		c.rateLimits = &RateLimitInfo{Windows: windows}
		c.rateLimitMu.Unlock()
	}
}

// setAuthHeaders sets the appropriate auth headers for the given API key.
func setAuthHeaders(h http.Header, key string) {
	if isOAuthToken(key) {
		h.Set("Authorization", "Bearer "+key)
		h.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		h.Set("x-api-key", key)
	}
}

// decodeAPIError reads an error response body into an *APIError.
func decodeAPIError(resp *http.Response) *APIError {
	var rawErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawErr); err == nil && rawErr.Error.Message != "" {
		return &APIError{
			StatusCode: resp.StatusCode,
			Type:       rawErr.Error.Type,
			Message:    rawErr.Error.Message,
		}
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Type:       "unknown",
		Message:    fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}

// SendMessage sends a non-streaming MessageRequest and returns the full response.
// On a 401, it refreshes the API key from the keychain and retries once.
func (c *Client) SendMessage(ctx context.Context, req MessageRequest) (*MessageResponse, error) {
	req.Stream = false
	if req.MaxTokens == 0 {
		req.MaxTokens = defaultMaxTokens
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	doRequest := func(key string) (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		setAuthHeaders(httpReq.Header, key)
		httpReq.Header.Set("anthropic-version", apiVersion)
		return c.httpClient.Do(httpReq)
	}

	resp, err := doRequest(c.apiKey())
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	c.parseRateLimitHeaders(resp.Header)

	// On 401, refresh key and retry once if the key actually changed.
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if newKey, changed := c.refreshKey(); changed {
			resp, err = doRequest(newKey)
			if err != nil {
				return nil, fmt.Errorf("send request (retry): %w", err)
			}
			defer resp.Body.Close()
			c.parseRateLimitHeaders(resp.Header)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, decodeAPIError(resp)
	}

	var msgResp MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &msgResp, nil
}

// Stream sends a MessageRequest with stream=true and returns a channel of StreamEvents.
// The events channel is closed when the stream ends or ctx is cancelled.
// Any error encountered is sent on the error channel before both channels are closed.
// On a 401, it refreshes the API key from the keychain and retries once.
func (c *Client) Stream(ctx context.Context, req MessageRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		req.Stream = true
		if req.MaxTokens == 0 {
			req.MaxTokens = defaultMaxTokens
		}

		body, err := json.Marshal(req)
		if err != nil {
			close(events)
			errCh <- fmt.Errorf("marshal request: %w", err)
			return
		}

		doRequest := func(key string) (*http.Response, error) {
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("create request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")
			setAuthHeaders(httpReq.Header, key)
			httpReq.Header.Set("anthropic-version", apiVersion)
			return c.httpClient.Do(httpReq)
		}

		resp, err := doRequest(c.apiKey())
		if err != nil {
			close(events)
			errCh <- fmt.Errorf("send request: %w", err)
			return
		}

		c.parseRateLimitHeaders(resp.Header)

		// On 401, refresh key and retry once if the key actually changed.
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			if newKey, changed := c.refreshKey(); changed {
				resp, err = doRequest(newKey)
				if err != nil {
					close(events)
					errCh <- fmt.Errorf("send request (retry): %w", err)
					return
				}
				c.parseRateLimitHeaders(resp.Header)
			}
		}

		if resp.StatusCode != http.StatusOK {
			close(events)
			errCh <- decodeAPIError(resp)
			resp.Body.Close()
			return
		}

		// ParseSSEStream closes the events channel when done.
		// resp.Body is closed when the stream parser finishes.
		ParseSSEStream(ctx, resp.Body, events)
		resp.Body.Close()
	}()

	return events, errCh
}
