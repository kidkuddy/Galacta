package agent

import "time"

// Session holds the configuration for a Galacta session.
type Session struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	WorkingDir     string `json:"working_dir"`
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	Status         string `json:"status"` // idle, running, error
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusError   = "error"
)
