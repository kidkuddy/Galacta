package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// stderr is the standard error writer used for all UI output.
// Defined here so ui.go can also reference it.
var stderr io.Writer = os.Stderr

func streamMessage(base, session, message, outputFormat string) {
	reqBody, _ := json.Marshal(map[string]string{"message": message})
	req, err := http.NewRequest("POST", base+"/sessions/"+session+"/message", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	spinner := NewSpinner()
	if outputFormat == "stream" || outputFormat == "" {
		spinner.Start("Thinking...")
	}

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		spinner.Stop()
		fmt.Fprintf(stderr, "%serror connecting to Galacta: %v%s\n", colorRed, err, colorReset)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		spinner.Stop()
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(stderr, "%serror: HTTP %d: %s%s\n", colorRed, resp.StatusCode, string(body), colorReset)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	ctx := &streamContext{spinner: spinner}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			switch outputFormat {
			case "json":
				fmt.Println(data)
			case "text":
				handleSSEEventText(data)
			default:
				handleSSEEvent(data, base, session, ctx)
			}
		}
	}
	spinner.Stop()
	if outputFormat != "json" {
		fmt.Println()
	}
}

// streamContext carries state across SSE events within a single stream.
type streamContext struct {
	spinner     *Spinner
	firstText   bool // true after first text_delta
	currentTool string
}

func handleSSEEventText(data string) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	eventType, _ := event["type"].(string)
	if eventType == "text_delta" {
		text, _ := event["text"].(string)
		fmt.Print(text)
	}
}

func handleSSEEvent(data string, base string, session string, ctx *streamContext) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	eventType, _ := event["type"].(string)

	switch eventType {
	case "text_delta":
		ctx.spinner.Stop()
		text, _ := event["text"].(string)
		fmt.Print(text)

	case "thinking_delta":
		// Keep spinner running during extended thinking — don't stop it.
		// The spinner already shows "Thinking..." which is the right signal.
		// Printing raw thinking text conflicts with the spinner's line-clearing.

	case "tool_start":
		ctx.spinner.Stop()
		tool, _ := event["tool"].(string)
		ctx.currentTool = tool

		// Print tool box header
		inputRaw, _ := json.Marshal(event["input"])
		var inputMap map[string]any
		json.Unmarshal(inputRaw, &inputMap)

		fmt.Fprintf(stderr, "\n%s%s%s\n", colorCyan, toolBoxTop(tool, 40), colorReset)

		// Print input params
		for k, v := range inputMap {
			val := fmt.Sprintf("%v", v)
			if len(val) > 80 {
				val = val[:80] + "..."
			}
			fmt.Fprintf(stderr, "%s%s%s\n", colorDim, toolBoxLine(fmt.Sprintf("%s: %s", k, val)), colorReset)
		}

		ctx.spinner.Start(fmt.Sprintf("Running %s...", tool))

	case "tool_result":
		ctx.spinner.Stop()
		tool, _ := event["tool"].(string)
		output, _ := event["output"].(string)
		isError, _ := event["is_error"].(bool)
		dur, _ := event["duration_ms"].(float64)

		if isError {
			fmt.Fprintf(stderr, "%s%s%s\n", colorRed, toolBoxLine("✗ "+output), colorReset)
		} else {
			lines := strings.Split(output, "\n")
			maxLines := 20
			if len(lines) > maxLines {
				for _, l := range lines[:maxLines] {
					fmt.Fprintf(stderr, "%s\n", toolBoxLine(l))
				}
				fmt.Fprintf(stderr, "%s%s%s\n", colorDim, toolBoxLine(fmt.Sprintf("... (%d more lines)", len(lines)-maxLines)), colorReset)
			} else {
				for _, l := range lines {
					fmt.Fprintf(stderr, "%s\n", toolBoxLine(l))
				}
			}
		}

		durTag := fmt.Sprintf("%dms", int(dur))
		_ = tool
		fmt.Fprintf(stderr, "%s%s%s\n", colorDim, toolBoxBottom(durTag, 40), colorReset)

	case "permission_request":
		ctx.spinner.Stop()
		requestID, _ := event["request_id"].(string)
		tool, _ := event["tool"].(string)
		inputRaw, _ := json.Marshal(event["input"])
		var inputMap map[string]any
		json.Unmarshal(inputRaw, &inputMap)

		width := 42
		fmt.Fprintf(stderr, "\n%s%s%s\n", colorYellow, dboxTop("Permission", width), colorReset)
		fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, dboxLine(tool, width), colorReset)
		for k, v := range inputMap {
			val := fmt.Sprintf("%v", v)
			if len(val) > 34 {
				val = val[:34] + "..."
			}
			fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, dboxLine(fmt.Sprintf("%s: %s", k, val), width), colorReset)
		}
		fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, dboxBottom(width), colorReset)
		fmt.Fprintf(stderr, "  Allow? [y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		approved := answer == "y" || answer == "yes"

		postJSON(base+"/sessions/"+session+"/permission/"+requestID, map[string]bool{"approved": approved})

	case "usage":
		ctx.spinner.Stop()
		inputTok, _ := event["input_tokens"].(float64)
		outputTok, _ := event["output_tokens"].(float64)
		costUSD, _ := event["cost_usd"].(float64)
		costStr := ""
		if costUSD > 0 {
			costStr = fmt.Sprintf(" · %s", fmtCost(costUSD))
		}
		fmt.Fprintf(stderr, "%s  ── %s in · %s out%s ──%s\n",
			colorDim, fmtTokens(int(inputTok)), fmtTokens(int(outputTok)), costStr, colorReset)

	case "turn_complete":
		ctx.spinner.Stop()

	case "subagent_start":
		ctx.spinner.Stop()
		agentType, _ := event["agent_type"].(string)
		description, _ := event["description"].(string)
		msg := fmt.Sprintf("Agent: %s", agentType)
		if description != "" {
			msg += " — " + description
		}
		ctx.spinner.Start(msg)

	case "subagent_end":
		ctx.spinner.Stop()

	case "question_request":
		ctx.spinner.Stop()
		requestID, _ := event["request_id"].(string)
		question, _ := event["question"].(string)
		header, _ := event["header"].(string)

		title := "Question"
		if header != "" {
			title = header
		}

		width := 42
		fmt.Fprintf(stderr, "\n%s%s%s\n", colorYellow, boxTop(title, width), colorReset)

		// Wrap question text
		for _, qline := range strings.Split(question, "\n") {
			fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, boxLine(qline, width), colorReset)
		}

		// Display options
		if opts, ok := event["options"].([]any); ok && len(opts) > 0 {
			fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, boxLine("", width), colorReset)
			for i, opt := range opts {
				if o, ok := opt.(map[string]any); ok {
					label, _ := o["label"].(string)
					desc, _ := o["description"].(string)
					optLine := fmt.Sprintf(" %d) %s", i+1, label)
					if desc != "" {
						optLine += " — " + desc
					}
					fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, boxLine(optLine, width), colorReset)
				}
			}
		}
		fmt.Fprintf(stderr, "%s%s%s\n", colorYellow, boxBottom(width), colorReset)
		fmt.Fprintf(stderr, "  Answer: ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)

		// Resolve numeric answers to option labels
		if opts, ok := event["options"].([]any); ok && len(opts) > 0 {
			var idx int
			if _, err := fmt.Sscanf(answer, "%d", &idx); err == nil && idx >= 1 && idx <= len(opts) {
				if o, ok := opts[idx-1].(map[string]any); ok {
					if label, ok := o["label"].(string); ok {
						answer = label
					}
				}
			}
		}

		postJSON(base+"/sessions/"+session+"/question/"+requestID, map[string]string{"answer": answer})

	case "plan_mode_changed":
		ctx.spinner.Stop()
		active, _ := event["active"].(bool)
		if active {
			fmt.Fprintf(stderr, "%s  ◆ Plan mode: ON%s\n", colorBoldYellow, colorReset)
		} else {
			fmt.Fprintf(stderr, "%s  ◇ Plan mode: OFF%s\n", colorDim, colorReset)
		}

	case "team_created":
		teamName, _ := event["team_name"].(string)
		fmt.Fprintf(stderr, "%s  ┌─ Team: %s ─── created ─┐%s\n", colorCyan, teamName, colorReset)

	case "team_deleted":
		teamName, _ := event["team_name"].(string)
		fmt.Fprintf(stderr, "%s  └─ Team: %s ─── deleted ─┘%s\n", colorDim, teamName, colorReset)

	case "team_message":
		from, _ := event["from"].(string)
		recipient, _ := event["recipient"].(string)
		summary, _ := event["summary"].(string)
		if recipient != "" {
			fmt.Fprintf(stderr, "%s  ┃ %s → %s: %s%s\n", colorCyan, from, recipient, summary, colorReset)
		} else {
			fmt.Fprintf(stderr, "%s  ┃ %s → all: %s%s\n", colorCyan, from, summary, colorReset)
		}

	case "error":
		ctx.spinner.Stop()
		msg, _ := event["message"].(string)
		fmt.Fprintf(stderr, "%s  ✗ error: %s%s\n", colorRed, msg, colorReset)
	}
}
