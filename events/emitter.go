package events

import (
	"encoding/json"
	"log"
)

type Emitter struct {
	ch        chan []byte
	sessionID string
}

func NewEmitter(sessionID string, bufSize int) *Emitter {
	return &Emitter{
		ch:        make(chan []byte, bufSize),
		sessionID: sessionID,
	}
}

func (e *Emitter) Emit(event any) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("galacta: failed to marshal event: %v", err)
		return
	}
	select {
	case e.ch <- data:
	default:
		log.Printf("galacta: event channel full for session %s, dropping event", e.sessionID)
	}
}

func (e *Emitter) Channel() <-chan []byte {
	return e.ch
}

func (e *Emitter) Close() {
	close(e.ch)
}

func (e *Emitter) EmitTextDelta(text string) {
	e.Emit(&TextDelta{
		Event: Event{Type: "text_delta", SessionID: e.sessionID},
		Text:  text,
	})
}

func (e *Emitter) EmitThinkingDelta(text string) {
	e.Emit(&ThinkingDelta{
		Event: Event{Type: "thinking_delta", SessionID: e.sessionID},
		Text:  text,
	})
}

func (e *Emitter) EmitToolStart(callID, tool string, input json.RawMessage) {
	e.Emit(&ToolStart{
		Event:  Event{Type: "tool_start", SessionID: e.sessionID},
		CallID: callID,
		Tool:   tool,
		Input:  input,
	})
}

func (e *Emitter) EmitToolResult(callID, tool, output string, isError bool, durationMs int64) {
	e.Emit(&ToolResult{
		Event:      Event{Type: "tool_result", SessionID: e.sessionID},
		CallID:     callID,
		Tool:       tool,
		Output:     output,
		IsError:    isError,
		DurationMs: durationMs,
	})
}

func (e *Emitter) EmitPermissionRequest(requestID, tool string, input json.RawMessage) {
	e.Emit(&PermissionRequest{
		Event:     Event{Type: "permission_request", SessionID: e.sessionID},
		RequestID: requestID,
		Tool:      tool,
		Input:     input,
	})
}

func (e *Emitter) EmitUsage(inputTokens, outputTokens, cacheRead, cacheWrite int, costUSD float64, sessionUsage *SessionUsage) {
	e.Emit(&UsageEvent{
		Event:            Event{Type: "usage", SessionID: e.sessionID},
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		CostUSD:          costUSD,
		SessionUsage:     sessionUsage,
	})
}

func (e *Emitter) EmitTurnComplete(stopReason string) {
	e.Emit(&TurnComplete{
		Event:      Event{Type: "turn_complete", SessionID: e.sessionID},
		StopReason: stopReason,
	})
}

func (e *Emitter) EmitError(message string) {
	e.Emit(&ErrorEvent{
		Event:   Event{Type: "error", SessionID: e.sessionID},
		Message: message,
	})
}

func (e *Emitter) EmitSubAgentStart(agentType, description string) {
	e.Emit(&SubAgentStart{
		Event:       Event{Type: "subagent_start", SessionID: e.sessionID},
		AgentType:   agentType,
		Description: description,
	})
}

func (e *Emitter) EmitSubAgentEnd(agentType string) {
	e.Emit(&SubAgentEnd{
		Event:     Event{Type: "subagent_end", SessionID: e.sessionID},
		AgentType: agentType,
	})
}

func (e *Emitter) EmitQuestionRequest(requestID, question, header string, options []QuestionOption) {
	e.Emit(&QuestionRequest{
		Event:     Event{Type: "question_request", SessionID: e.sessionID},
		RequestID: requestID,
		Question:  question,
		Header:    header,
		Options:   options,
	})
}

func (e *Emitter) EmitPlanModeChanged(active bool) {
	e.Emit(&PlanModeChanged{
		Event:  Event{Type: "plan_mode_changed", SessionID: e.sessionID},
		Active: active,
	})
}

func (e *Emitter) EmitTeamCreated(teamName string) {
	e.Emit(&TeamCreated{
		Event:    Event{Type: "team_created", SessionID: e.sessionID},
		TeamName: teamName,
	})
}

func (e *Emitter) EmitTeamDeleted(teamName string) {
	e.Emit(&TeamDeleted{
		Event:    Event{Type: "team_deleted", SessionID: e.sessionID},
		TeamName: teamName,
	})
}

func (e *Emitter) EmitTeamMessage(from, recipient, summary, msgType string) {
	e.Emit(&TeamMessageEvent{
		Event:     Event{Type: "team_message", SessionID: e.sessionID},
		From:      from,
		Recipient: recipient,
		Summary:   summary,
		MsgType:   msgType,
	})
}
