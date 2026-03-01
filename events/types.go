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
	SessionUsage     *SessionUsage `json:"session_usage,omitempty"`
}

type SessionUsage struct {
	TotalInputTokens      int     `json:"total_input_tokens"`
	TotalOutputTokens     int     `json:"total_output_tokens"`
	TotalCacheReadTokens  int     `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int     `json:"total_cache_write_tokens"`
	TotalCostUSD          float64 `json:"total_cost_usd"`
	MessageCount          int     `json:"message_count"`
}

type TurnComplete struct {
	Event
	StopReason string `json:"stop_reason"`
}

type ErrorEvent struct {
	Event
	Message string `json:"message"`
}

type SubAgentStart struct {
	Event
	AgentType   string `json:"agent_type"`
	Description string `json:"description,omitempty"`
}

type SubAgentEnd struct {
	Event
	AgentType string `json:"agent_type"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionRequest struct {
	Event
	RequestID string           `json:"request_id"`
	Question  string           `json:"question"`
	Header    string           `json:"header,omitempty"`
	Options   []QuestionOption `json:"options,omitempty"`
}

type PlanModeChanged struct {
	Event
	Active bool `json:"active"`
}

type TeamCreated struct {
	Event
	TeamName string `json:"team_name"`
}

type TeamDeleted struct {
	Event
	TeamName string `json:"team_name"`
}

type TeamMessageEvent struct {
	Event
	From      string `json:"from"`
	Recipient string `json:"recipient,omitempty"`
	Summary   string `json:"summary"`
	MsgType   string `json:"msg_type"`
}
