package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/db"
	"github.com/kidkuddy/galacta/events"
	"github.com/kidkuddy/galacta/permissions"
	"github.com/kidkuddy/galacta/team"
	"github.com/kidkuddy/galacta/toolcaller"
	"github.com/kidkuddy/galacta/tools/plan"
	"github.com/google/uuid"
)

const defaultMaxTurns = 100

// AgentLoop runs the core send → tool_use → execute → tool_result cycle.
type AgentLoop struct {
	client        *anthropic.Client
	caller        *toolcaller.Caller
	gate          *permissions.InteractiveGate
	emitter       *events.Emitter
	store         *db.SessionDB
	model         string
	systemPrompt  string
	maxTurns      int
	maxBudgetUSD  float64
	fallbackModel string
	thinking      *anthropic.ThinkingConfig
	serverTools   []anthropic.ServerTool
	planState     *plan.PlanState
	inboxCh       <-chan team.TeamMessage
}

// AgentLoopOptions holds optional configuration for AgentLoop.
type AgentLoopOptions struct {
	MaxBudgetUSD  float64
	FallbackModel string
	Thinking      *anthropic.ThinkingConfig
	ServerTools   []anthropic.ServerTool
	PlanState     *plan.PlanState
	InboxCh       <-chan team.TeamMessage
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
	opts *AgentLoopOptions,
) *AgentLoop {
	l := &AgentLoop{
		client:       client,
		caller:       caller,
		gate:         gate,
		emitter:      emitter,
		store:        store,
		model:        model,
		systemPrompt: systemPrompt,
		maxTurns:     defaultMaxTurns,
	}
	if opts != nil {
		l.maxBudgetUSD = opts.MaxBudgetUSD
		l.fallbackModel = opts.FallbackModel
		l.thinking = opts.Thinking
		l.serverTools = opts.ServerTools
		l.planState = opts.PlanState
		l.inboxCh = opts.InboxCh
	}
	return l
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

	_, _, err = l.iterate(ctx, sessionID, history, tools, l.maxTurns, true)
	return err
}

// RunSubAgent runs a sub-agent with a fresh history and no DB persistence.
// Returns the final assistant text output.
func (l *AgentLoop) RunSubAgent(ctx context.Context, prompt string, maxTurns int, filter *toolcaller.ToolFilter) (string, error) {
	history := []anthropic.Message{anthropic.NewUserMessage(prompt)}

	var tools []anthropic.Tool
	if filter != nil {
		tools = l.caller.FilteredListTools(filter)
	} else {
		tools = l.caller.ListTools()
	}

	history, finalText, err := l.iterate(ctx, "subagent", history, tools, maxTurns, false)
	if err != nil {
		return finalText, err
	}
	return finalText, nil
}

// iterate runs the core send -> tool_use -> execute -> tool_result cycle.
// persist=true saves messages to DB, persist=false keeps them in-memory only.
// Returns the final history, the last assistant text, and any error.
func (l *AgentLoop) iterate(ctx context.Context, sessionID string,
	history []anthropic.Message, tools []anthropic.Tool,
	maxTurns int, persist bool) ([]anthropic.Message, string, error) {

	var lastText string

	for turn := 0; turn < maxTurns; turn++ {
		select {
		case <-ctx.Done():
			l.emitter.EmitTurnComplete("aborted")
			return history, lastText, ctx.Err()
		default:
		}

		// Drain inbox messages from team bus (non-blocking)
		if l.inboxCh != nil {
			for {
				select {
				case msg := <-l.inboxCh:
					injected := fmt.Sprintf("[Message from %s]: %s", msg.From, msg.Content)
					history = append(history, anthropic.NewUserMessage(injected))
				default:
					goto doneInbox
				}
			}
		doneInbox:
		}

		// Filter tools if plan mode is active
		currentTools := tools
		if l.planState != nil && l.planState.IsActive() {
			currentTools = filterReadOnly(tools)
		}

		// Call Anthropic API
		currentModel := l.model
		req := anthropic.MessageRequest{
			Model:       currentModel,
			System:      l.systemPrompt,
			Messages:    history,
			Tools:       currentTools,
			ServerTools: l.serverTools,
			Thinking:    l.thinking,
		}

		assistantMsg, usage, stopReason, err := l.streamTurn(ctx, sessionID, req)
		if err != nil {
			// Check for overloaded (529) and try fallback model
			var apiErr *anthropic.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 529 && l.fallbackModel != "" {
				log.Printf("galacta: model %s overloaded, falling back to %s", currentModel, l.fallbackModel)
				currentModel = l.fallbackModel
				req.Model = currentModel
				assistantMsg, usage, stopReason, err = l.streamTurn(ctx, sessionID, req)
			}
			if err != nil {
				l.emitter.EmitError(err.Error())
				l.emitter.EmitTurnComplete("error")
				return history, lastText, fmt.Errorf("streaming turn %d: %w", turn, err)
			}
		}

		// Extract text from assistant message
		for _, block := range assistantMsg.Content {
			if block.Type == "text" {
				lastText = block.Text
			}
		}

		// Save assistant message to DB (only when persisting)
		if persist && l.store != nil {
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
		}

		// Emit usage with actual cost and session totals
		costUSD := CalculateCost(currentModel, usage.InputTokens, usage.OutputTokens)
		var sessionUsage *events.SessionUsage
		if persist && l.store != nil {
			if totals, err := l.store.GetUsageTotals(); err == nil && totals != nil {
				sessionUsage = &events.SessionUsage{
					TotalInputTokens:      totals.TotalInputTokens,
					TotalOutputTokens:     totals.TotalOutputTokens,
					TotalCacheReadTokens:  totals.TotalCacheReadTokens,
					TotalCacheWriteTokens: totals.TotalCacheWriteTokens,
					TotalCostUSD:          CalculateCost(currentModel, totals.TotalInputTokens, totals.TotalOutputTokens),
					MessageCount:          totals.MessageCount,
				}
			}
		}
		l.emitter.EmitUsage(usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, costUSD, sessionUsage)

		// Check budget (only when persisting with a store)
		if l.maxBudgetUSD > 0 && persist && l.store != nil {
			totals, _ := l.store.GetUsageTotals()
			if totals != nil {
				totalCost := CalculateCost(currentModel, totals.TotalInputTokens, totals.TotalOutputTokens)
				if totalCost >= l.maxBudgetUSD {
					l.emitter.EmitTurnComplete("budget_exceeded")
					return history, lastText, fmt.Errorf("budget exceeded: $%.4f >= $%.2f", totalCost, l.maxBudgetUSD)
				}
			}
		}

		// Append assistant message to history
		history = append(history, *assistantMsg)

		// Auto-compact if approaching context window limit (use actual token count from API)
		if persist && shouldAutoCompact(usage.InputTokens, currentModel) {
			compacted, compactErr := l.compactConversation(ctx, history)
			if compactErr != nil {
				log.Printf("galacta: auto-compact failed: %v", compactErr)
			} else {
				history = compacted
			}
		}

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
			return history, lastText, nil
		}

		// Check permissions and execute tool calls
		toolResults, err := l.executeTools(ctx, sessionID, toolUses)
		if err != nil {
			l.emitter.EmitError(err.Error())
			l.emitter.EmitTurnComplete("error")
			return history, lastText, fmt.Errorf("executing tools on turn %d: %w", turn, err)
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

		// Save tool results to DB (only when persisting)
		if persist && l.store != nil {
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
	}

	l.emitter.EmitTurnComplete("max_turns")
	return history, lastText, fmt.Errorf("reached max turns (%d)", maxTurns)
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
				// Server tool blocks (web_search etc.) — preserve as-is
				if block.Type == "server_tool_use" || block.Type == "web_search_tool_result" {
					// These are fully formed from the API, just accumulate
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
					if currentBlock.Type == "tool_use" {
						if pj, ok := partialJSON[stopData.Index]; ok && pj != "" {
							currentBlock.Input = json.RawMessage(pj)
						} else if len(currentBlock.Input) == 0 {
							currentBlock.Input = json.RawMessage("{}")
						}
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

	var workingDir string
	if l.store != nil {
		workingDir, _ = l.store.GetMeta("working_dir")
	}

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

// filterReadOnly returns only tools whose names match read-only patterns.
func filterReadOnly(tools []anthropic.Tool) []anthropic.Tool {
	readOnlyPatterns := []string{"read", "glob", "grep", "search", "list", "fetch", "task", "skill", "ask", "plan"}
	var filtered []anthropic.Tool
	for _, t := range tools {
		lower := strings.ToLower(t.Name)
		for _, p := range readOnlyPatterns {
			if strings.Contains(lower, p) {
				filtered = append(filtered, t)
				break
			}
		}
	}
	return filtered
}

// loadHistory reconstructs the conversation history from the DB.
// It detects and removes orphaned tool_use blocks at the end of the history
// (assistant messages with tool_use that lack a following tool_result).
func (l *AgentLoop) loadHistory() ([]anthropic.Message, error) {
	rows, err := l.store.ListMessages()
	if err != nil {
		return nil, err
	}

	type indexedMessage struct {
		msg   anthropic.Message
		rowID string
	}

	var indexed []indexedMessage
	for _, row := range rows {
		var content []anthropic.ContentBlock
		if err := json.Unmarshal([]byte(row.Content), &content); err != nil {
			log.Printf("galacta: skipping message %s: invalid content JSON: %v", row.ID, err)
			continue
		}
		indexed = append(indexed, indexedMessage{
			msg: anthropic.Message{
				Role:    row.Role,
				Content: content,
			},
			rowID: row.ID,
		})
	}

	// Detect orphaned tool_use at the end of history: the last message is an
	// assistant message containing tool_use blocks with no following tool_result.
	if len(indexed) > 0 {
		last := indexed[len(indexed)-1]
		if last.msg.Role == "assistant" && hasToolUse(last.msg.Content) {
			log.Printf("galacta: removing orphaned tool_use message %s (no tool_result follows)", last.rowID)
			if err := l.store.DeleteMessage(last.rowID); err != nil {
				log.Printf("galacta: failed to delete orphaned message: %v", err)
			}
			indexed = indexed[:len(indexed)-1]
		}
	}

	messages := make([]anthropic.Message, len(indexed))
	for i, im := range indexed {
		messages[i] = im.msg
	}

	return messages, nil
}

// hasToolUse returns true if any content block is a tool_use block.
func hasToolUse(blocks []anthropic.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}
