package permissions

import "encoding/json"

// BypassGate auto-approves all tool calls. Used for sub-agents where
// the parent has already authorized spawning.
type BypassGate struct{}

func (b *BypassGate) Check(tool string, input json.RawMessage, workingDir string) Decision {
	return Allow
}
