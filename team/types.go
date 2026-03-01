package team

import "time"

// Team represents a group of coordinating agents.
type Team struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	AgentType   string       `json:"agent_type,omitempty"`
	Members     []TeamMember `json:"members"`
	CreatedAt   time.Time    `json:"created_at"`
}

// TeamMember represents an agent registered in a team.
type TeamMember struct {
	Name      string `json:"name"`
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type,omitempty"`
}

// TeamMessage is routed between agents via the MessageBus.
type TeamMessage struct {
	Type      string `json:"type"`                // "message", "broadcast", "shutdown_request", "shutdown_response"
	From      string `json:"from"`
	Recipient string `json:"recipient,omitempty"`
	Content   string `json:"content"`
	Summary   string `json:"summary,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Approve   *bool  `json:"approve,omitempty"`
}
