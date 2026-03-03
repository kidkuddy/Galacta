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
	CompactUserPrompt = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirmation from the user.

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
		System:   CompactSystemPrompt,
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
	continuationText := fmt.Sprintf(`This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.

%s

Please continue the conversation from where we left off without asking the user any further questions. Continue with the last task that you were asked to work on.`, summary)

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
