package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chzyer/readline"
)

func interactiveLoop(base, session, outputFormat string) {
	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/quit"),
		readline.PcItem("/exit"),
		readline.PcItem("/session"),
		readline.PcItem("/model"),
		readline.PcItem("/permissions"),
		readline.PcItem("/history"),
		readline.PcItem("/compact"),
		readline.PcItem("/clear"),
		readline.PcItem("/cost"),
		readline.PcItem("/usage"),
		readline.PcItem("/tasks"),
		readline.PcItem("/skills"),
		readline.PcItem("/plan"),
	)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "\033[1m> \033[0m",
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		Stderr:          stderr.(io.Writer),
		Stdout:          stderr.(io.Writer),
	})
	if err != nil {
		fmt.Fprintf(stderr, "%serror initializing readline: %v%s\n", colorRed, err, colorReset)
		return
	}
	defer rl.Close()

	inMultiline := false
	var multilineLines []string

	for {
		if inMultiline {
			rl.SetPrompt("\033[2m... \033[0m")
		} else {
			rl.SetPrompt("\n\033[1m> \033[0m")
		}

		line, err := rl.Readline()
		if err != nil {
			// EOF (ctrl-d) or interrupt
			fmt.Fprintln(stderr)
			return
		}

		// Multiline mode toggle
		if strings.TrimSpace(line) == `"""` {
			if inMultiline {
				// End multiline — join and send
				inMultiline = false
				message := strings.Join(multilineLines, "\n")
				multilineLines = nil
				if strings.TrimSpace(message) == "" {
					continue
				}
				streamMessage(base, session, message, outputFormat)
				continue
			}
			// Start multiline
			inMultiline = true
			multilineLines = nil
			continue
		}

		if inMultiline {
			multilineLines = append(multilineLines, line)
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Handle slash commands
		switch {
		case trimmed == "/quit" || trimmed == "/exit":
			return
		case trimmed == "/history":
			printSessionHistory(base, session)
			continue
		case trimmed == "/session":
			printSessionInfo(base, session)
			continue
		case trimmed == "/clear":
			fmt.Print("\033[2J\033[H")
			continue
		case trimmed == "/cost":
			printUsageInfo(base, session)
			continue
		case trimmed == "/usage" || strings.HasPrefix(trimmed, "/usage "):
			printAccountUsage(base, trimmed)
			continue
		case trimmed == "/tasks":
			printTasks(base, session)
			continue
		case trimmed == "/permissions" || strings.HasPrefix(trimmed, "/permissions "):
			handlePermissions(base, session, trimmed)
			continue
		case trimmed == "/model" || strings.HasPrefix(trimmed, "/model "):
			handleModel(base, session, trimmed)
			continue
		case trimmed == "/plan":
			fmt.Fprintf(stderr, "%s  ◇ Send a message asking to enter plan mode, or use the galacta_enter_plan_mode tool.%s\n", colorDim, colorReset)
			continue
		case trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact "):
			handleCompact(base, session, trimmed)
			continue
		case trimmed == "/skills":
			printSkills(base, session)
			continue
		case trimmed == "/help":
			printHelp()
			continue
		case strings.HasPrefix(trimmed, "/"):
			fmt.Fprintf(stderr, "%s  unknown command: %s%s\n", colorDim, trimmed, colorReset)
			continue
		}

		streamMessage(base, session, trimmed, outputFormat)
	}
}

func printSessionHistory(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session + "/messages")
	if err != nil {
		fmt.Fprintf(stderr, "%serror loading history: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) == 0 {
		return
	}

	fmt.Fprintf(stderr, "%s--- conversation history (%d messages) ---%s\n", colorDim, len(messages), colorReset)

	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["Role"].(string)
		contentStr, _ := msg["Content"].(string)

		var content []map[string]any
		if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
			continue
		}

		switch role {
		case "user":
			for _, block := range content {
				blockType, _ := block["type"].(string)
				if blockType == "text" {
					text, _ := block["text"].(string)
					fmt.Fprintf(stderr, "%s> %s%s\n", colorBold, text, colorReset)
				} else if blockType == "tool_result" {
					toolID, _ := block["tool_use_id"].(string)
					text, _ := block["content"].(string)
					isErr, _ := block["is_error"].(bool)
					if isErr {
						fmt.Fprintf(stderr, "%s  [result %s] error: %s%s\n", colorDim, shortID(toolID), truncate(text, 100), colorReset)
					} else {
						fmt.Fprintf(stderr, "%s  [result %s] %s%s\n", colorDim, shortID(toolID), truncate(text, 100), colorReset)
					}
				}
			}
		case "assistant":
			for _, block := range content {
				blockType, _ := block["type"].(string)
				if blockType == "text" {
					text, _ := block["text"].(string)
					fmt.Fprintf(stderr, "%s\n", text)
				} else if blockType == "tool_use" {
					name, _ := block["name"].(string)
					fmt.Fprintf(stderr, "%s  [%s]%s\n", colorCyan, name, colorReset)
				}
			}
		}
	}
	fmt.Fprintf(stderr, "%s--- end history ---%s\n\n", colorDim, colorReset)
}

func printSessionInfo(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}

	model, _ := data["model"].(string)
	dir, _ := data["working_dir"].(string)
	mode, _ := data["permission_mode"].(string)
	status, _ := data["status"].(string)

	width := 40
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop("Session", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("ID:     %s", shortID(session)), width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Model:  %s", model), width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Dir:    %s", dir), width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Mode:   %s", mode), width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Status: %s", status), width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
}

func printUsageInfo(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}
	model, _ := data["model"].(string)

	width := 36
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop("Usage", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Model:    %s", model), width), colorReset)

	if usage, ok := data["usage"].(map[string]any); ok {
		inTok, _ := usage["TotalInputTokens"].(float64)
		outTok, _ := usage["TotalOutputTokens"].(float64)
		cacheRead, _ := usage["TotalCacheReadTokens"].(float64)
		cacheWrite, _ := usage["TotalCacheWriteTokens"].(float64)
		msgs, _ := usage["MessageCount"].(float64)

		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Input:    %s tokens", fmtTokens(int(inTok))), width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Output:   %s tokens", fmtTokens(int(outTok))), width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Cache:    %s read · %s wr", fmtTokens(int(cacheRead)), fmtTokens(int(cacheWrite))), width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Messages: %d", int(msgs)), width), colorReset)

		// Estimate cost
		var costUSD float64
		switch {
		case strings.Contains(model, "opus"):
			costUSD = (inTok/1_000_000)*15.0 + (outTok/1_000_000)*75.0
		case strings.Contains(model, "sonnet"):
			costUSD = (inTok/1_000_000)*3.0 + (outTok/1_000_000)*15.0
		case strings.Contains(model, "haiku"):
			costUSD = (inTok/1_000_000)*0.80 + (outTok/1_000_000)*4.0
		}
		if costUSD > 0 {
			fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("Cost:     %s", fmtCost(costUSD)), width), colorReset)
		}
	}
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
}

func printTasks(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session + "/tasks")
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) == 0 {
		fmt.Fprintf(stderr, "%s  No tasks.%s\n", colorDim, colorReset)
		return
	}
	for _, item := range data {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := t["id"].(float64)
		subject, _ := t["subject"].(string)
		status, _ := t["status"].(string)
		owner, _ := t["owner"].(string)

		icon := "●"
		statusColor := colorDim
		switch status {
		case "in_progress":
			icon = "◐"
			statusColor = colorYellow
		case "completed":
			icon = "✓"
			statusColor = colorGreen
		}
		ownerStr := ""
		if owner != "" {
			ownerStr = fmt.Sprintf(" (%s)", owner)
		}
		fmt.Fprintf(stderr, "  %s%s%s %d. %-30s %s%s%s%s\n",
			statusColor, icon, colorReset, int(id), subject+ownerStr, statusColor, status, colorReset, "")
	}
}

func handlePermissions(base, session, line string) {
	parts := strings.Fields(line)
	if len(parts) == 1 {
		resp, err := getJSON(base + "/sessions/" + session)
		if err != nil {
			fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
			return
		}
		data, ok := resp["data"].(map[string]any)
		if !ok {
			return
		}
		mode, _ := data["permission_mode"].(string)
		fmt.Fprintf(stderr, "  Permission mode: %s%s%s\n", colorBold, mode, colorReset)
		fmt.Fprintf(stderr, "%s  Available: default, acceptEdits, bypassPermissions, plan, dontAsk%s\n", colorDim, colorReset)
		return
	}
	newMode := parts[1]
	_, err := patchJSON(base+"/sessions/"+session, map[string]string{"permission_mode": newMode})
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	fmt.Fprintf(stderr, "  Permission mode set to: %s%s%s\n", colorBold, newMode, colorReset)
}

func handleModel(base, session, line string) {
	parts := strings.Fields(line)
	if len(parts) == 1 {
		resp, err := getJSON(base + "/sessions/" + session)
		if err != nil {
			fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
			return
		}
		data, ok := resp["data"].(map[string]any)
		if !ok {
			return
		}
		model, _ := data["model"].(string)
		fmt.Fprintf(stderr, "  Model: %s%s%s\n", colorBold, model, colorReset)
		return
	}
	newModel := parts[1]
	_, err := patchJSON(base+"/sessions/"+session, map[string]string{"model": newModel})
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	fmt.Fprintf(stderr, "  Model set to: %s%s%s\n", colorBold, newModel, colorReset)
}

func handleCompact(base, session, line string) {
	keep := 10
	parts := strings.Fields(line)
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &keep)
	}

	body := map[string]int{"keep_messages": keep}
	resp, err := postJSON(base+"/sessions/"+session+"/compact", body)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}
	compacted, _ := data["compacted"].(bool)
	remaining, _ := data["message_count"].(float64)
	removed, _ := data["removed_messages"].(float64)
	if compacted {
		fmt.Fprintf(stderr, "  Compacted: removed %d messages, %d remaining\n", int(removed), int(remaining))
	} else {
		fmt.Fprintf(stderr, "  Nothing to compact (%d messages)\n", int(remaining))
	}
}

func printSkills(base, session string) {
	// Get session info to find working_dir
	resp, err := getJSON(base + "/sessions/" + session)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}
	dir, _ := data["working_dir"].(string)

	skillResp, err := getJSON(base + "/skills?working_dir=" + dir)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	skills, ok := skillResp["data"].([]any)
	if !ok || len(skills) == 0 {
		fmt.Fprintf(stderr, "%s  No skills available.%s\n", colorDim, colorReset)
		return
	}

	width := 44
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop("Skills", width), colorReset)
	for _, s := range skills {
		sk, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := sk["name"].(string)
		desc, _ := sk["description"].(string)
		if desc != "" {
			fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("/%s — %s", name, desc), width), colorReset)
		} else {
			fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("/%s", name), width), colorReset)
		}
	}
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
}

func printAccountUsage(base, line string) {
	period := "today"
	parts := strings.Fields(line)
	if len(parts) > 1 {
		period = parts[1]
	}

	resp, err := getJSON(base + "/usage?period=" + period)
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		errMsg, _ := resp["error"].(string)
		if errMsg != "" {
			fmt.Fprintf(stderr, "%s  %s%s\n", colorRed, errMsg, colorReset)
		} else {
			fmt.Fprintf(stderr, "%s  No usage data.%s\n", colorDim, colorReset)
		}
		return
	}

	buckets, _ := data["data"].([]any)
	if len(buckets) == 0 {
		width := 40
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop(fmt.Sprintf("Account Usage (%s)", period), width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("No usage data for this period.", width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
		return
	}

	// Aggregate by model
	type modelUsage struct {
		Input  int
		Output int
		Cached int
	}
	models := make(map[string]*modelUsage)
	var modelOrder []string
	for _, b := range buckets {
		bucket, ok := b.(map[string]any)
		if !ok {
			continue
		}
		model, _ := bucket["model"].(string)
		inTok, _ := bucket["input_tokens"].(float64)
		outTok, _ := bucket["output_tokens"].(float64)
		cached, _ := bucket["input_cached_tokens"].(float64)

		if _, exists := models[model]; !exists {
			models[model] = &modelUsage{}
			modelOrder = append(modelOrder, model)
		}
		m := models[model]
		m.Input += int(inTok)
		m.Output += int(outTok)
		m.Cached += int(cached)
	}

	width := 40
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop(fmt.Sprintf("Account Usage (%s)", period), width), colorReset)
	for _, model := range modelOrder {
		m := models[model]
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorBold, boxLine(model, width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("  Input:   %s tokens", fmtTokens(m.Input)), width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("  Output:  %s tokens", fmtTokens(m.Output)), width), colorReset)
		if m.Cached > 0 {
			fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine(fmt.Sprintf("  Cached:  %s tokens", fmtTokens(m.Cached)), width), colorReset)
		}
	}
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
}

func printHelp() {
	width := 47
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxTop("Commands", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorBold, boxLine("Session", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /session       Show session info", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /model [name]  Show or change model", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /permissions   Show or change perms", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /history       Conversation history", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /compact [N]   Compact conversation", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /clear         Clear screen", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorBold, boxLine("Tasks & Tools", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /tasks         List tasks", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /skills        List available skills", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /cost          Session token usage", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /usage [period] Account usage", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorBold, boxLine("Other", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /plan          Plan mode info", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /help          This help", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("  /quit          End session", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxLine("", width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorDim, boxLine(`Tip: Use """ for multiline input`, width), colorReset)
	fmt.Fprintf(stderr, "%s%s%s\n", colorCyan, boxBottom(width), colorReset)
}
