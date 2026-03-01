package permissions

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

const (
	ModeDefault           = "default"
	ModeAcceptEdits       = "acceptEdits"
	ModeBypassPermissions = "bypassPermissions"
	ModePlan              = "plan"
	ModeDontAsk           = "dontAsk"
)

// ModeGate implements Gate for a specific permission mode.
type ModeGate struct {
	mode string
}

func NewModeGate(mode string) *ModeGate {
	return &ModeGate{mode: mode}
}

// Check implements Gate.
func (m *ModeGate) Check(tool string, input json.RawMessage, workingDir string) Decision {
	lower := strings.ToLower(tool)

	switch m.mode {
	case ModeBypassPermissions, ModeDontAsk:
		return Allow

	case ModePlan:
		if isReadOnly(lower) {
			return Allow
		}
		return Deny

	case ModeAcceptEdits:
		if isReadOnly(lower) {
			return Allow
		}
		if isBash(lower) {
			return Ask
		}
		if isWriteEdit(lower) {
			if isInsideCwd(input, workingDir) {
				return Allow
			}
			return Ask
		}
		return Ask

	default: // ModeDefault
		if isReadOnly(lower) {
			return Allow
		}
		return Ask
	}
}

func isReadOnly(tool string) bool {
	readOnlyPatterns := []string{"read", "glob", "grep", "search", "list", "fetch"}
	for _, p := range readOnlyPatterns {
		if strings.Contains(tool, p) {
			return true
		}
	}
	return false
}

func isBash(tool string) bool {
	return strings.Contains(tool, "bash") || strings.Contains(tool, "exec")
}

func isWriteEdit(tool string) bool {
	return strings.Contains(tool, "write") || strings.Contains(tool, "edit")
}

func isInsideCwd(input json.RawMessage, workingDir string) bool {
	if len(input) == 0 || workingDir == "" {
		return false
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return false
	}
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return false
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	absCwd, err := filepath.Abs(workingDir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absCwd+string(filepath.Separator)) || absPath == absCwd
}
