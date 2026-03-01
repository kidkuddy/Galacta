package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/db"
	"github.com/kidkuddy/galacta/events"
	"github.com/kidkuddy/galacta/permissions"
	"github.com/kidkuddy/galacta/toolcaller"
	"github.com/google/uuid"
)

const defaultMaxTurns = 100

// AgentLoop runs the core send → tool_use → execute → tool_result cycle.
type AgentLoop struct {
	client      *anthropic.Client
	caller      *toolcaller.Caller
	gate        *permissions.InteractiveGate
	emitter     *events.Emitter
	store       *db.SessionDB
	model       string
	systemPrompt string
	maxTurns    int
}

// NewAgentLoop creates a new agent loop.
func NewAgentLoop(
	client *anthropic.Client,
	caller *toolcaller.Caller,
	gate *permissions.InteractiveGate,
	emitter *events.Emitter,
	store *db.SessionDB,
	model string,
	systemPrompt string,
) *AgentLoop {
	return &AgentLoop{
		client:       client,
		caller:       caller,
		gate:         gate,
		emitter:      emitter,
		store:        store,
		model:        model,
		systemPrompt: systemPrompt,
		maxTurns:     defaultMaxTurns,
	}
}

// Run executes the agent loop for a single user message.
// It blocks until the turn completes (end_turn), max turns reached, or ctx is cancelled.
func (l *AgentLoop) Run(ctx context.Context, sessionID, message string) error {
	// Load existing history from DB
	history, err := l.loadHistory()
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	// Append user message
	userMsg := anthropic.NewUserMessage(message)
	history = append(history, userMsg)

	// Save user message to DB
	contentJSON, _ := json.Marshal(userMsg.Content)
	if err := l.store.SaveMessage(&db.MessageRow{
		ID:      uuid.New().String(),
		Role:    "user",
		Content: string(contentJSON),
		Model:   l.model,
	}); err != nil {
		log.Printf("galacta: failed to save user message: %v", err)
	}

	// Get available tools
	tools := l.caller.ListTools()

	for turn := 0; turn < l.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			l.emitter.EmitTurnComplete("aborted")
			return ctx.Err()
		default:
		}

		// Call Anthropic API
		req := anthropic.MessageRequest{
			Model:    l.model,
			System:   l.systemPrompt,
			Messages: history,
			Tools:    tools,
		}

		assistantMsg, usage, stopReason, err := l.streamTurn(ctx, sessionID, req)
		if err != nil {
			l.emitter.EmitError(err.Error())
			l.emitter.EmitTurnComplete("error")
			return fmt.Errorf("streaming turn %d: %w", turn, err)
		}

		// Save assistant message to DB
		assistantContentJSON, _ := json.Marshal(assistantMsg.Content)
		if err := l.store.SaveMessage(&db.MessageRow{
			ID:               uuid.New().String(),
			Role:             "assistant",
			Content:          string(assistantContentJSON),
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			Model:            l.model,
			StopReason:       stopReason,
		}); err != nil {
			log.Printf("galacta: failed to save assistant message: %v", err)
		}

		// Emit usage
		l.emitter.EmitUsage(usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, 0)

		// Append assistant message to history
		history = append(history, *assistantMsg)

		// Check for tool_use blocks
		var toolUses []toolcaller.ToolCall
		for _, block := range assistantMsg.Content {
			if block.Type == "tool_use" {
				toolUses = append(toolUses, toolcaller.ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				})
			}
		}

		// No tool calls — we're done
		if len(toolUses) == 0 {
			l.emitter.EmitTurnComplete(stopReason)
			return nil
		}

		// Check permissions and execute tool calls
		toolResults, err := l.executeTools(ctx, sessionID, toolUses)
		if err != nil {
			l.emitter.EmitError(err.Error())
			l.emitter.EmitTurnComplete("error")
			return fmt.Errorf("executing tools on turn %d: %w", turn, err)
		}

		// Build tool_result content blocks
		var resultBlocks []anthropic.ContentBlock
		for _, tr := range toolResults {
			resultBlocks = append(resultBlocks, anthropic.NewToolResultMessage(tr.ID, tr.Output, tr.IsError))
		}

		// Append tool results as a user message
		toolResultMsg := anthropic.Message{
			Role:    "user",
			Content: resultBlocks,
		}
		history = append(history, toolResultMsg)

		// Save tool results to DB
		toolResultJSON, _ := json.Marshal(resultBlocks)
		if err := l.store.SaveMessage(&db.MessageRow{
			ID:      uuid.New().String(),
			Role:    "user",
			Content: string(toolResultJSON),
			Model:   l.model,
		}); err != nil {
			log.Printf("galacta: failed to save tool result message: %v", err)
		}
	}

	l.emitter.EmitTurnComplete("max_turns")
	return fmt.Errorf("reached max turns (%d)", l.maxTurns)
}

// streamTurn sends a request and processes the SSE stream for one API call.
// Returns the assembled assistant message, usage, stop_reason, and any error.
func (l *AgentLoop) streamTurn(ctx context.Context, sessionID string, req anthropic.MessageRequest) (*anthropic.Message, *anthropic.Usage, string, error) {
	eventsCh, errCh := l.client.Stream(ctx, req)

	var (
		contentBlocks []anthropic.ContentBlock
		currentBlock  *anthropic.ContentBlock
		usage         anthropic.Usage
		stopReason    string
		// For accumulating partial input_json_delta
		partialJSON map[int]string
	)
	partialJSON = make(map[int]string)

	for event := range eventsCh {
		switch event.Type {
		case "message_start":
			var data anthropic.MessageStartData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				usage = data.Message.Usage
			}

		case "content_block_start":
			var data anthropic.ContentBlockStartData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				block := data.ContentBlock
				currentBlock = &block
				// Initialize partial JSON accumulator for tool_use blocks
				if block.Type == "tool_use" {
					partialJSON[data.Index] = ""
				}
			}

		case "content_block_delta":
			var data anthropic.ContentBlockDeltaData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				switch data.Delta.Type {
				case "text_delta":
					if currentBlock != nil {
						currentBlock.Text += data.Delta.Text
					}
					l.emitter.EmitTextDelta(data.Delta.Text)

				case "thinking_delta":
					l.emitter.EmitThinkingDelta(data.Delta.Thinking)

				case "input_json_delta":
					partialJSON[data.Index] += data.Delta.PartialJSON
				}
			}

		case "content_block_stop":
			if currentBlock != nil {
				var stopData struct {
					Index int `json:"index"`
				}
				if err := json.Unmarshal(event.Data, &stopData); err == nil {
					// If we accumulated partial JSON for this block, set it
					if pj, ok := partialJSON[stopData.Index]; ok && currentBlock.Type == "tool_use" {
						currentBlock.Input = json.RawMessage(pj)
						// Emit tool_start now that we have full input
						l.emitter.EmitToolStart(currentBlock.ID, currentBlock.Name, currentBlock.Input)
					}
				}
				contentBlocks = append(contentBlocks, *currentBlock)
				currentBlock = nil
			}

		case "message_delta":
			var data anthropic.MessageDeltaData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				stopReason = data.Delta.StopReason
				if data.Usage != nil {
					usage.OutputTokens = data.Usage.OutputTokens
				}
			}

		case "message_stop":
			// Stream complete
		}
	}

	// Wait for the stream goroutine to finish and check for errors.
	// errCh is closed after the goroutine exits, so this will not block forever.
	if err := <-errCh; err != nil {
		return nil, nil, "", err
	}

	msg := &anthropic.Message{
		Role:    "assistant",
		Content: contentBlocks,
	}

	return msg, &usage, stopReason, nil
}

// executeTools checks permissions and runs tool calls.
func (l *AgentLoop) executeTools(ctx context.Context, sessionID string, toolUses []toolcaller.ToolCall) ([]toolcaller.ToolCallResult, error) {
	// Check permissions for each tool call (sequentially to avoid UX chaos)
	var approved []toolcaller.ToolCall
	var results []toolcaller.ToolCallResult

	workingDir, _ := l.store.GetMeta("working_dir")

	for _, tc := range toolUses {
		allowed, err := l.gate.CheckAndWait(ctx, tc.Name, tc.Input, workingDir)
		if err != nil {
			return nil, fmt.Errorf("checking permission for %s: %w", tc.Name, err)
		}
		if allowed {
			approved = append(approved, tc)
		} else {
			// Denied — return a tool_result with error
			results = append(results, toolcaller.ToolCallResult{
				ID:      tc.ID,
				Name:    tc.Name,
				Output:  "Permission denied by user",
				IsError: true,
			})
		}
	}

	if len(approved) > 0 {
		// Emit tool_start for each (already emitted during stream for tool_use blocks)
		startTime := time.Now()
		callResults := l.caller.CallMany(ctx, approved)
		_ = startTime // duration tracked inside CallMany

		// Emit tool_result events
		for _, cr := range callResults {
			l.emitter.EmitToolResult(cr.ID, cr.Name, cr.Output, cr.IsError, cr.DurationMs)
		}

		results = append(results, callResults...)
	}

	return results, nil
}

// loadHistory reconstructs the conversation history from the DB.
func (l *AgentLoop) loadHistory() ([]anthropic.Message, error) {
	rows, err := l.store.ListMessages()
	if err != nil {
		return nil, err
	}

	var messages []anthropic.Message
	for _, row := range rows {
		var content []anthropic.ContentBlock
		if err := json.Unmarshal([]byte(row.Content), &content); err != nil {
			log.Printf("galacta: skipping message %s: invalid content JSON: %v", row.ID, err)
			continue
		}
		messages = append(messages, anthropic.Message{
			Role:    row.Role,
			Content: content,
		})
	}

	return messages, nil
}
