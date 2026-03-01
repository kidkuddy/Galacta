package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kidkuddy/galacta/events"
	"github.com/google/uuid"
)

// Decision represents a permission check result.
type Decision int

const (
	Allow Decision = iota
	Deny
	Ask
)

// Gate checks whether a tool call is permitted under the current mode.
type Gate interface {
	Check(tool string, input json.RawMessage, workingDir string) Decision
}

// InteractiveGate wraps a Gate and handles the "Ask" flow.
type InteractiveGate struct {
	gate    Gate
	emitter *events.Emitter
	pending map[string]chan bool
	mu      sync.Mutex
}

func NewInteractiveGate(gate Gate, emitter *events.Emitter) *InteractiveGate {
	return &InteractiveGate{
		gate:    gate,
		emitter: emitter,
		pending: make(map[string]chan bool),
	}
}

// CheckAndWait checks permissions. If auto-allowed, returns (true, nil) immediately.
// If denied, returns (false, nil). If Ask, emits permission_request and blocks
// until Respond is called with the matching request_id.
func (ig *InteractiveGate) CheckAndWait(ctx context.Context, tool string, input json.RawMessage, workingDir string) (bool, error) {
	decision := ig.gate.Check(tool, input, workingDir)

	switch decision {
	case Allow:
		return true, nil
	case Deny:
		return false, nil
	case Ask:
		requestID := uuid.NewString()
		ch := make(chan bool, 1)

		ig.mu.Lock()
		ig.pending[requestID] = ch
		ig.mu.Unlock()

		ig.emitter.EmitPermissionRequest(requestID, tool, input)

		select {
		case approved := <-ch:
			return approved, nil
		case <-ctx.Done():
			ig.mu.Lock()
			delete(ig.pending, requestID)
			ig.mu.Unlock()
			return false, ctx.Err()
		}
	}

	return false, fmt.Errorf("unknown permission decision: %d", decision)
}

// Respond unblocks a pending permission request.
func (ig *InteractiveGate) Respond(requestID string, approved bool) error {
	ig.mu.Lock()
	ch, ok := ig.pending[requestID]
	if ok {
		delete(ig.pending, requestID)
	}
	ig.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending permission request with id %q", requestID)
	}

	ch <- approved
	return nil
}
