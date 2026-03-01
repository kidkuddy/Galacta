package events

import "encoding/json"

type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

type TextDelta struct {
	Event
	Text string `json:"text"`
}

type ThinkingDelta struct {
	Event
	Text string `json:"text"`
}

type ToolStart struct {
	Event
	CallID string          `json:"call_id"`
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
}

type ToolResult struct {
	Event
	CallID     string `json:"call_id"`
	Tool       string `json:"tool"`
	Output     string `json:"output"`
	IsError    bool   `json:"is_error"`
	DurationMs int64  `json:"duration_ms"`
}

type PermissionRequest struct {
	Event
	RequestID string          `json:"request_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
}

type UsageEvent struct {
	Event
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

type TurnComplete struct {
	Event
	StopReason string `json:"stop_reason"`
}

type ErrorEvent struct {
	Event
	Message string `json:"message"`
}
