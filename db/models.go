package db

import "time"

type MessageRow struct {
	ID               string
	Role             string // "user", "assistant"
	Content          string // JSON array of content blocks
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	Model            string
	StopReason       string
	CreatedAt        time.Time
}

type UsageTotals struct {
	TotalInputTokens      int
	TotalOutputTokens     int
	TotalCacheReadTokens  int
	TotalCacheWriteTokens int
	MessageCount          int
}
