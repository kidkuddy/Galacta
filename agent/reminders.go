package agent

import (
	"fmt"
	"strings"

	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/tools/plan"
)

// ReminderConfig holds state needed to generate per-turn reminders.
type ReminderConfig struct {
	PlanState    *plan.PlanState
	PlanFilePath string // path to the plan file, if any
	TurnCount    int    // current turn number
}

// BuildReminders generates system reminder messages to inject into the conversation
// before sending to the API. These are "meta" messages that provide context to the
// model without being part of the actual conversation.
func BuildReminders(cfg ReminderConfig) []anthropic.ContentBlock {
	var blocks []anthropic.ContentBlock

	// Plan mode reminders
	if cfg.PlanState != nil && cfg.PlanState.IsActive() {
		reminder := buildPlanModeReminder(cfg)
		if reminder != "" {
			blocks = append(blocks, anthropic.ContentBlock{
				Type: "text",
				Text: wrapReminder("plan_mode", reminder),
			})
		}
	}

	return blocks
}

// buildPlanModeReminder returns the plan mode prompt for the current turn.
// Every 5th turn gets the full prompt; others get the sparse version.
func buildPlanModeReminder(cfg ReminderConfig) string {
	var b strings.Builder

	// Plan file status prefix
	if cfg.PlanFilePath != "" {
		b.WriteString(fmt.Sprintf("Your plan file is at %s. Continue developing your plan by writing to or editing this file.\n\n", cfg.PlanFilePath))
	} else {
		defaultPath := "plan.md"
		b.WriteString(fmt.Sprintf("Create a plan file at %s to capture your planning. NOTE: this is the only file you may edit — all other actions must be READ-ONLY.\n\n", defaultPath))
	}

	isFull := cfg.TurnCount%5 == 0

	if isFull {
		b.WriteString(planModeFullPrompt)
	} else {
		b.WriteString(planModeSparsePrompt)
	}

	return b.String()
}

const planModeFullPrompt = `## Iterative Planning Workflow
You are pair-planning with the user. Explore code to build context, ask questions when you hit decisions you can't resolve alone, and write findings into the plan file as you go. The plan file is the ONLY file you may edit — it starts as a rough skeleton and evolves into the final plan.

Repeat this cycle until the plan is complete:
1. **Explore** — Use galacta_read, galacta_glob, galacta_grep, and galacta_bash to read code. Hunt for existing functions, utilities, and patterns to reuse. The galacta_agent with subagent_type=Explore can parallelize complex searches without filling your context, but direct tools are simpler for straightforward queries.
2. **Update the plan file** — After each discovery, capture it immediately. Don't wait until the end.
3. **Ask the user** — When you hit an ambiguity or decision you can't resolve from code, use galacta_ask_user. Then return to step 1.

Begin by scanning a few key files for an initial understanding of scope. Write a skeleton plan (headers and rough notes) and ask your first questions. Don't exhaustively explore before engaging the user.

### Asking Good Questions
- Never ask what you could learn by reading code
- Batch related questions (multi-question galacta_ask_user calls)
- Focus on things only the user can answer: requirements, preferences, trade-offs, edge case priorities
- Scale depth to the task — a vague feature request needs many rounds; a focused bug fix may need one or none

### Plan File Structure
Divide the plan into clear markdown sections based on the request. Fill them in progressively.
- Open with a **Context** section: the problem, what prompted the change, and the intended outcome
- Present only the recommended approach
- Keep it scannable yet detailed enough to execute
- Include critical file paths
- Reference existing functions/utilities to reuse, with paths
- End with a verification section: how to test end-to-end

### When to Converge
The plan is ready when all ambiguities are resolved and it covers: what to change, which files to modify, what existing code to reuse (with paths), and how to verify. Call galacta_exit_plan_mode.

### Ending Your Turn
Your turn should ONLY end by either:
- Using galacta_ask_user to gather more information
- Calling galacta_exit_plan_mode when the plan is ready for approval

**Important:** galacta_ask_user is for clarifying requirements or choosing between approaches. galacta_exit_plan_mode is for requesting plan approval. Do NOT ask about plan approval any other way — no text questions, no galacta_ask_user. Phrases like "Is this plan okay?", "Should I proceed?", "How does this look?" MUST use galacta_exit_plan_mode.`

const planModeSparsePrompt = `You are in plan mode. Only read-only tools are available.
- Explore code with galacta_read, galacta_glob, galacta_grep
- Write findings to the plan file (the only editable file)
- Use galacta_ask_user for questions only the user can answer
- Call galacta_exit_plan_mode when the plan is complete`

// wrapReminder wraps reminder content in the system-reminder XML tag format.
func wrapReminder(reminderType, content string) string {
	return fmt.Sprintf("<system-reminder>\n%s\n\n      IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.\n</system-reminder>", content)
}
