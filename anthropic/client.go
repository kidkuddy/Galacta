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
	apiKey     string
	baseURL    string
	httpClient *http.Client

	rateLimitMu sync.RWMutex
	rateLimits  *RateLimitInfo
}

// NewClient creates a Client with the default base URL.
func NewClient(apiKey string) *Client {
	return NewClientWithBase(apiKey, defaultBaseURL)
}

// NewClientWithBase creates a Client with a custom base URL.
func NewClientWithBase(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
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

// SendMessage sends a non-streaming MessageRequest and returns the full response.
func (c *Client) SendMessage(ctx context.Context, req MessageRequest) (*MessageResponse, error) {
	req.Stream = false
	if req.MaxTokens == 0 {
		req.MaxTokens = defaultMaxTokens
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if isOAuthToken(c.apiKey) {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	c.parseRateLimitHeaders(resp.Header)

	if resp.StatusCode != http.StatusOK {
		var rawErr struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if decErr := json.NewDecoder(resp.Body).Decode(&rawErr); decErr == nil && rawErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Type:       rawErr.Error.Type,
				Message:    rawErr.Error.Message,
			}
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Type:       "unknown",
			Message:    fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
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

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			close(events)
			errCh <- fmt.Errorf("create request: %w", err)
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		if isOAuthToken(c.apiKey) {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
			httpReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
		} else {
			httpReq.Header.Set("x-api-key", c.apiKey)
		}
		httpReq.Header.Set("anthropic-version", apiVersion)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			close(events)
			errCh <- fmt.Errorf("send request: %w", err)
			return
		}
		defer resp.Body.Close()

		// Capture rate limit headers from every response (even errors)
		c.parseRateLimitHeaders(resp.Header)

		if resp.StatusCode != http.StatusOK {
			close(events)
			var rawErr struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if decErr := json.NewDecoder(resp.Body).Decode(&rawErr); decErr == nil && rawErr.Error.Message != "" {
				errCh <- &APIError{
					StatusCode: resp.StatusCode,
					Type:       rawErr.Error.Type,
					Message:    rawErr.Error.Message,
				}
			} else {
				errCh <- &APIError{
					StatusCode: resp.StatusCode,
					Type:       "unknown",
					Message:    fmt.Sprintf("HTTP %d", resp.StatusCode),
				}
			}
			return
		}

		// ParseSSEStream closes the events channel when done
		ParseSSEStream(ctx, resp.Body, events)
	}()

	return events, errCh
}
