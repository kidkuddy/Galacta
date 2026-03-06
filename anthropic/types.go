package anthropic

import (
	"encoding/json"
	"fmt"
)

// ThinkingConfig controls extended thinking behavior.
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled" | "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // only when type="enabled"
}

// APIError represents a structured error from the Anthropic API.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error %d: %s: %s", e.StatusCode, e.Type, e.Message)
}

// SystemBlock is a single block within the system prompt array.
// The Anthropic API accepts system as either a string or an array of these blocks.
// Using the array form enables prompt caching via cache_control markers.
type SystemBlock struct {
	Type         string        `json:"type"`                    // always "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"` // set to enable caching on this block
}

// CacheControl marks a system block as a cache breakpoint.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// NewSystemBlock creates a plain text system block.
func NewSystemBlock(text string) SystemBlock {
	return SystemBlock{Type: "text", Text: text}
}

// NewCachedSystemBlock creates a text system block with cache_control set.
func NewCachedSystemBlock(text string) SystemBlock {
	return SystemBlock{
		Type:         "text",
		Text:         text,
		CacheControl: &CacheControl{Type: "ephemeral"},
	}
}

// MessageRequest is the request body for the Anthropic Messages API.
type MessageRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	System      []SystemBlock   `json:"system,omitempty"`
	Messages    []Message       `json:"messages"`
	Tools       []Tool          `json:"tools,omitempty"`
	ServerTools []ServerTool    `json:"-"` // merged into tools via custom MarshalJSON
	Stream      bool            `json:"stream"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`
}

// SetSystemString is a convenience method to set the system prompt as a single string.
func (r *MessageRequest) SetSystemString(s string) {
	if s == "" {
		r.System = nil
		return
	}
	r.System = []SystemBlock{NewSystemBlock(s)}
}

// Message represents a single message in the conversation.
type Message struct {
	Role    string         `json:"role"` // "user" | "assistant"
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a block of content within a message.
type ContentBlock struct {
	Type      string          `json:"type"`                   // "text" | "tool_use" | "tool_result" | "thinking"
	Text      string          `json:"text,omitempty"`         // text content
	ID        string          `json:"id,omitempty"`           // tool_use id
	Name      string          `json:"name,omitempty"`         // tool name
	Input     json.RawMessage `json:"input,omitempty"`        // tool_use input
	ToolUseID string          `json:"tool_use_id,omitempty"`  // for tool_result
	Content   string          `json:"content,omitempty"`      // for tool_result text
	IsError   bool            `json:"is_error,omitempty"`     // for tool_result error flag
}

// Tool describes a tool available to the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ServerTool describes a server-side tool (e.g. web_search) included in the API request.
type ServerTool struct {
	Type           string   `json:"type"`                      // e.g. "web_search_20250305"
	Name           string   `json:"name"`                      // e.g. "web_search"
	MaxUses        int      `json:"max_uses,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

// MessageResponse is the full response from the Messages API.
type MessageResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_input_tokens"`
	CacheWriteTokens int `json:"cache_creation_input_tokens"`
}

// StreamEvent is a parsed SSE event from the Anthropic streaming API.
type StreamEvent struct {
	Type string          // event type: message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
	Data json.RawMessage // raw JSON data
}

// MessageStartData is the parsed data for a message_start event.
type MessageStartData struct {
	Type    string          `json:"type"`
	Message MessageResponse `json:"message"`
}

// ContentBlockStartData is the parsed data for a content_block_start event.
type ContentBlockStartData struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDeltaData is the parsed data for a content_block_delta event.
type ContentBlockDeltaData struct {
	Type  string     `json:"type"`
	Index int        `json:"index"`
	Delta DeltaBlock `json:"delta"`
}

// DeltaBlock represents an incremental update within a content block.
type DeltaBlock struct {
	Type        string `json:"type"` // "text_delta" | "input_json_delta" | "thinking_delta"
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
}

// MessageDeltaData is the parsed data for a message_delta event.
type MessageDeltaData struct {
	Type  string       `json:"type"`
	Delta MessageDelta `json:"delta"`
	Usage *Usage       `json:"usage,omitempty"`
}

// MessageDelta carries stop reason information.
type MessageDelta struct {
	StopReason string `json:"stop_reason"`
}

// RateLimitWindow holds utilization data for a single rate limit window (e.g. 5h, 7d).
type RateLimitWindow struct {
	Type        string  `json:"type"`                   // "five_hour" or "seven_day"
	Utilization float64 `json:"utilization"`             // 0.0–1.0
	ResetsAt    int64   `json:"resets_at,omitempty"`     // unix timestamp
}

// RateLimitInfo holds the latest rate limit data captured from API response headers.
type RateLimitInfo struct {
	Windows []RateLimitWindow `json:"windows"`
}

// NewUserMessage creates a Message with role "user" containing a single text block.
func NewUserMessage(text string) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

// NewToolResultMessage creates a ContentBlock for a tool_result response.
func NewToolResultMessage(toolUseID, content string, isError bool) ContentBlock {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
		IsError:   isError,
	}
}
