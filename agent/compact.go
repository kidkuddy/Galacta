package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/db"
)

const (
	// autoCompactBuffer is the token budget reserved for the compact response itself.
	// Mirrors Claude Code's LFA constant (13000 tokens).
	autoCompactBuffer = 13000

	// maxOutputReserve is subtracted from context window for max_output_tokens.
	// Mirrors Claude Code's wl7 constant (20000 tokens).
	maxOutputReserve = 20000

	// CompactSystemPrompt is the system prompt used for conversation summarization.
	CompactSystemPrompt = "You are a helpful AI assistant tasked with summarizing conversations."

	// CompactUserPrompt is the user prompt used to request conversation summarization.
	CompactUserPrompt = `Your task is to produce a detailed summary of the conversation so far. Pay close
attention to explicit user requests and the actions you took in response.

The summary must capture technical details, code patterns, and architectural decisions
thoroughly enough that development work can continue without context loss.

Before writing your final summary, use <analysis> tags to structure your reasoning
and verify completeness. During analysis:
1. Walk through each message chronologically. For each segment, identify:
   - The user's explicit requests and underlying intent
   - How you addressed those requests
   - Key decisions, technical concepts, and code patterns
   - Concrete details:
     - Complete code snippets
     - Function signatures
   - Errors encountered and how they were resolved
   - Specific user feedback, especially corrections or redirections
2. Verify technical accuracy and completeness against each required section below.

Structure your summary with these sections:
1. Primary Request and Intent: All explicit user requests and their underlying goals, in detail
2. Key Technical Concepts: All significant technologies, frameworks, and concepts discussed
3. Files and Code Sections: Every file examined, modified, or created — with full code snippets where relevant and a note on why each file matters
4. Errors and Fixes: Every error encountered, how it was resolved, and any user feedback on the resolution
5. Problem Solving: Problems solved and any ongoing troubleshooting
6. All User Messages: Every non-tool-result user message — these are critical for tracking feedback and shifting intent
7. Pending Tasks: Any tasks explicitly assigned but not yet completed
8. Current Work: Precisely what was being worked on immediately before this summary — file names, code snippets, and context from the most recent messages
9. Optional Next Step: The immediate next action, but ONLY if it directly continues the user's most recent explicit request. If the last task was finished, only list next steps that are explicitly aligned with user requests. If a next step exists, include verbatim quotes from the most recent messages showing exactly what was being worked on and where it left off — prevent any drift in task interpretation.

IMPORTANT: Do NOT use any tools. You MUST respond with ONLY the <summary>...</summary> block as your text output.`

	// CompactRecentOnlyPrompt is the user prompt for compacting conversations that
	// already have prior retained context (summarizes only the recent portion).
	CompactRecentOnlyPrompt = `Your task is to summarize ONLY the recent portion of the conversation — the messages
following the earlier retained context. The earlier messages are preserved and do NOT
need summarizing. Focus exclusively on what was discussed, learned, and accomplished
in the recent messages.

Before writing your final summary, use <analysis> tags to structure your reasoning.
During analysis:
1. Walk through the recent messages chronologically. For each segment, identify:
   - The user's explicit requests and intent
   - How you addressed those requests
   - Key decisions, technical concepts, and code patterns
   - Concrete details: full code snippets, function signatures
   - Errors encountered and resolutions
   - Specific user feedback, especially corrections
2. Verify technical accuracy and completeness.

Structure your summary with these sections:
1. Primary Request and Intent: User requests and intent from the recent messages
2. Key Technical Concepts: Technologies, frameworks, and concepts discussed recently
3. Files and Code Sections: Files examined, modified, or created — with code snippets and notes on importance
4. Errors and Fixes: Errors encountered and their resolutions
5. Problem Solving: Problems solved and ongoing troubleshooting
6. All User Messages: Every non-tool-result user message from the recent portion
7. Pending Tasks: Tasks from recent messages still outstanding
8. Current Work: What was being worked on immediately before this summary
9. Optional Next Step: Immediate next action from the most recent work, with verbatim quotes from recent conversation

Summarize ONLY the recent messages (after retained context). Be precise and thorough.

IMPORTANT: Do NOT use any tools. You MUST respond with ONLY the <summary>...</summary> block as your text output.`
)

var summaryRegex = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

// ExtractSummary extracts content between <summary> tags from text.
func ExtractSummary(text string) string {
	matches := summaryRegex.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

// autoCompactThreshold returns the token count above which auto-compaction triggers.
func autoCompactThreshold(model string) int {
	contextWindow := ContextWindowSize(model)
	maxOutput := maxOutputReserve
	if maxOutput > contextWindow {
		maxOutput = contextWindow / 10
	}
	return contextWindow - maxOutput - autoCompactBuffer
}

// shouldAutoCompact returns true if the actual input token count from the API
// response has exceeded the auto-compact threshold.
func shouldAutoCompact(inputTokens int, model string) bool {
	return inputTokens >= autoCompactThreshold(model)
}

// estimateTokens gives a rough char/4 estimate for logging purposes only.
func estimateTokens(messages []anthropic.Message) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "text", "thinking":
				total += len(block.Text) / 4
			case "tool_use":
				total += len(block.Input) / 4
				total += len(block.Name)
			case "tool_result":
				total += len(block.Content) / 4
			}
		}
	}
	return total
}

// compactConversation summarizes the conversation using Claude and replaces old messages
// with the summary. Returns the new compacted history.
func (l *AgentLoop) compactConversation(ctx context.Context, history []anthropic.Message) ([]anthropic.Message, error) {
	log.Printf("galacta: auto-compacting conversation (%d messages, ~%d tokens)", len(history), estimateTokens(history))

	l.emitter.EmitTextDelta("\n[Compacting conversation...]\n")

	// Strip server tool blocks (e.g. server_tool_use, web_search_tool_result)
	// to avoid API validation errors from orphaned server tool blocks.
	cleanedHistory := stripServerToolBlocks(history)

	// Build the compact request with conversation as context
	compactMessages := make([]anthropic.Message, 0, len(cleanedHistory)+1)
	compactMessages = append(compactMessages, cleanedHistory...)
	compactMessages = append(compactMessages, anthropic.NewUserMessage(CompactUserPrompt))

	resp, err := l.client.SendMessage(ctx, anthropic.MessageRequest{
		Model:    l.model,
		System:   []anthropic.SystemBlock{anthropic.NewSystemBlock(CompactSystemPrompt)},
		Messages: compactMessages,
	})
	if err != nil {
		return history, fmt.Errorf("compact API call: %w", err)
	}

	// Extract summary from response
	var summaryText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summaryText += block.Text
		}
	}

	// Extract content within <summary> tags
	summary := ExtractSummary(summaryText)
	if summary == "" {
		return history, fmt.Errorf("compact response missing <summary> tags")
	}

	// Build the continuation message (same format as Claude Code's XcT)
	continuationText := fmt.Sprintf(`This session continues from a previous conversation that exhausted its context. The summary below covers the earlier portion.

%s

Continue from where you left off without asking the user further questions. Resume the last task you were working on.`, summary)

	// Replace history with single summary message
	compacted := []anthropic.Message{
		anthropic.NewUserMessage(continuationText),
	}

	// Persist: delete all old messages, save the summary as a new user message
	if l.store != nil {
		// Accumulate token counts into lifetime metadata before wiping messages.
		if err := l.store.AccumulateUsage(); err != nil {
			log.Printf("galacta: failed to accumulate usage before compact: %v", err)
		}
		rows, err := l.store.ListMessages()
		if err == nil {
			for _, row := range rows {
				l.store.DeleteMessage(row.ID)
			}
		}
		contentJSON, _ := json.Marshal(compacted[0].Content)
		l.store.SaveMessage(&db.MessageRow{
			ID:      uuid.New().String(),
			Role:    "user",
			Content: string(contentJSON),
			Model:   l.model,
		})
	}

	log.Printf("galacta: compacted to ~%d tokens", estimateTokens(compacted))
	l.emitter.EmitTextDelta("[Compaction complete]\n\n")

	return compacted, nil
}
