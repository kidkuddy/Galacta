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

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "run":
		cmdRun(os.Args[2:])
	case "session":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: jeff session [create|list|messages|delete] ...")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "create":
			cmdSessionCreate(os.Args[3:])
		case "list":
			cmdSessionList()
		case "messages":
			if len(os.Args) < 4 {
				fmt.Fprintln(os.Stderr, "usage: jeff session messages <session-id>")
				os.Exit(1)
			}
			cmdSessionMessages(os.Args[3])
		case "delete":
			if len(os.Args) < 4 {
				fmt.Fprintln(os.Stderr, "usage: jeff session delete <session-id>")
				os.Exit(1)
			}
			cmdSessionDelete(os.Args[3])
		default:
			fmt.Fprintf(os.Stderr, "unknown session subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "health":
		cmdHealth()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  jeff run [flags] ["message"]       Start interactive session (or one-shot with message)
  jeff session create [flags]        Create a new session
  jeff session list                  List all sessions
  jeff session messages <id>         Show message history
  jeff session delete <id>           Delete a session
  jeff health                        Check Galacta daemon health

Flags for run:
  --session, -s        Resume existing session by UUID
  --model, -m          Model override
  --dir, -d            Working directory (default: cwd)
  --mode               Permission mode (default: default)
  --galacta            Galacta daemon URL (default: http://localhost:9090)
  --system-prompt      Override/append system prompt
  --continue           Resume most recent session
  --output-format      Output format: stream (default), json, text
  --effort             Thinking effort: low, medium, high
  --max-budget-usd     Maximum spending cap in USD
  --tools              Tool allow list (repeatable)
  --allowedTools       Tool glob patterns (repeatable)
  --fallback-model     Fallback model on overload (529)

Interactive commands (during session):
  /quit, /exit     End the session
  /history         Show conversation history
  /session         Show session info
  /clear           Clear screen`)
}

func galactaURL() string {
	if v := os.Getenv("GALACTA_URL"); v != "" {
		return v
	}
	return "http://localhost:9090"
}

// runFlags holds parsed flags for the run command.
type runFlags struct {
	session       string
	model         string
	dir           string
	mode          string
	base          string
	message       string
	systemPrompt  string
	continueFlag  bool
	outputFormat  string // "stream" (default), "json", "text"
	effort        string // "low", "medium", "high"
	maxBudgetUSD  float64
	tools         []string
	allowedTools  []string
	fallbackModel string
}

func parseRunFlags(args []string) runFlags {
	f := runFlags{base: galactaURL(), outputFormat: "stream"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session", "-s":
			i++
			if i < len(args) {
				f.session = args[i]
			}
		case "--model", "-m":
			i++
			if i < len(args) {
				f.model = args[i]
			}
		case "--dir", "-d":
			i++
			if i < len(args) {
				f.dir = args[i]
			}
		case "--mode":
			i++
			if i < len(args) {
				f.mode = args[i]
			}
		case "--galacta":
			i++
			if i < len(args) {
				f.base = args[i]
			}
		case "--system-prompt":
			i++
			if i < len(args) {
				f.systemPrompt = args[i]
			}
		case "--continue":
			f.continueFlag = true
		case "--output-format":
			i++
			if i < len(args) {
				f.outputFormat = args[i]
			}
		case "--effort":
			i++
			if i < len(args) {
				f.effort = args[i]
			}
		case "--max-budget-usd":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%f", &f.maxBudgetUSD)
			}
		case "--tools":
			i++
			if i < len(args) {
				f.tools = append(f.tools, args[i])
			}
		case "--allowedTools":
			i++
			if i < len(args) {
				f.allowedTools = append(f.allowedTools, args[i])
			}
		case "--fallback-model":
			i++
			if i < len(args) {
				f.fallbackModel = args[i]
			}
		default:
			f.message = args[i]
		}
	}
	if f.dir == "" {
		f.dir, _ = os.Getwd()
	}
	return f
}

func cmdRun(args []string) {
	flags := parseRunFlags(args)

	// Ensure or create session
	session := flags.session

	// Handle --continue: find the most recent session
	if session == "" && flags.continueFlag {
		session = findMostRecentSession(flags.base)
		if session == "" {
			fatal("no previous sessions found to continue")
		}
	}

	if session == "" {
		session = createSession(flags)
	} else {
		// Resuming — print old messages
		printSessionHistory(flags.base, session)
	}

	fmt.Fprintf(os.Stderr, "%sSession: %s%s\n", colorDim, session, colorReset)

	// If a message was given on the command line, send it
	if flags.message != "" {
		streamMessage(flags.base, session, flags.message, flags.outputFormat)
	}

	// Enter interactive loop
	interactiveLoop(flags.base, session, flags.outputFormat)
}

func createSession(flags runFlags) string {
	body := map[string]any{"working_dir": flags.dir}
	if flags.model != "" {
		body["model"] = flags.model
	}
	if flags.mode != "" {
		body["permission_mode"] = flags.mode
	}
	if flags.systemPrompt != "" {
		body["system_prompt"] = flags.systemPrompt
	}
	if flags.effort != "" {
		body["effort"] = flags.effort
	}
	if flags.maxBudgetUSD > 0 {
		body["max_budget_usd"] = flags.maxBudgetUSD
	}
	if flags.fallbackModel != "" {
		body["fallback_model"] = flags.fallbackModel
	}
	if len(flags.tools) > 0 {
		body["tools"] = flags.tools
	}
	if len(flags.allowedTools) > 0 {
		body["allowed_tools"] = flags.allowedTools
	}
	resp, err := postJSON(flags.base+"/sessions", body)
	if err != nil {
		fatal("creating session: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		fatal("unexpected response creating session")
	}
	return data["id"].(string)
}

func findMostRecentSession(base string) string {
	resp, err := getJSON(base + "/sessions")
	if err != nil {
		return ""
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) == 0 {
		return ""
	}

	// Find session with most recent updated_at
	var bestID string
	var bestTime string
	for _, s := range data {
		sess, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := sess["id"].(string)
		updatedAt, _ := sess["updated_at"].(string)
		if updatedAt > bestTime || bestID == "" {
			bestTime = updatedAt
			bestID = id
		}
	}
	return bestID
}

func interactiveLoop(base, session, outputFormat string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stderr, "\n%s> %s", colorBold, colorReset)
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF (ctrl-d)
			fmt.Fprintln(os.Stderr)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle slash commands
		switch {
		case line == "/quit" || line == "/exit":
			return
		case line == "/history":
			printSessionHistory(base, session)
			continue
		case line == "/session":
			printSessionInfo(base, session)
			continue
		case line == "/clear":
			fmt.Print("\033[2J\033[H")
			continue
		case strings.HasPrefix(line, "/"):
			fmt.Fprintf(os.Stderr, "%sunknown command: %s%s\n", colorDim, line, colorReset)
			continue
		}

		streamMessage(base, session, line, outputFormat)
	}
}

func streamMessage(base, session, message, outputFormat string) {
	reqBody, _ := json.Marshal(map[string]string{"message": message})
	req, err := http.NewRequest("POST", base+"/sessions/"+session+"/message", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%serror connecting to Galacta: %v%s\n", colorRed, err, colorReset)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "%serror: HTTP %d: %s%s\n", colorRed, resp.StatusCode, string(body), colorReset)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

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
				handleSSEEvent(data, base, session)
			}
		}
	}
	if outputFormat != "json" {
		fmt.Println()
	}
}

// handleSSEEventText handles SSE events in "text" output mode — only prints text content.
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

func handleSSEEvent(data string, base string, session string) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	eventType, _ := event["type"].(string)

	switch eventType {
	case "text_delta":
		text, _ := event["text"].(string)
		fmt.Print(text)

	case "thinking_delta":
		text, _ := event["text"].(string)
		fmt.Fprintf(os.Stderr, "%s%s%s", colorDim, text, colorReset)

	case "tool_start":
		tool, _ := event["tool"].(string)
		input, _ := json.Marshal(event["input"])
		fmt.Fprintf(os.Stderr, "\n%s[%s]%s %s\n", colorCyan, tool, colorReset, string(input))

	case "tool_result":
		tool, _ := event["tool"].(string)
		output, _ := event["output"].(string)
		isError, _ := event["is_error"].(bool)
		dur, _ := event["duration_ms"].(float64)
		if isError {
			fmt.Fprintf(os.Stderr, "%s[%s error] %s%s\n", colorRed, tool, output, colorReset)
		} else {
			lines := strings.Split(output, "\n")
			maxLines := 20
			if len(lines) > maxLines {
				for _, l := range lines[:maxLines] {
					fmt.Fprintf(os.Stderr, "  %s\n", l)
				}
				fmt.Fprintf(os.Stderr, "%s  ... (%d more lines)%s\n", colorDim, len(lines)-maxLines, colorReset)
			} else {
				for _, l := range lines {
					fmt.Fprintf(os.Stderr, "  %s\n", l)
				}
			}
			fmt.Fprintf(os.Stderr, "%s  (%dms)%s\n", colorDim, int(dur), colorReset)
		}

	case "permission_request":
		requestID, _ := event["request_id"].(string)
		tool, _ := event["tool"].(string)
		input, _ := json.Marshal(event["input"])
		fmt.Fprintf(os.Stderr, "\n%s[permission] %s%s %s\n", colorYellow, tool, colorReset, string(input))
		fmt.Fprintf(os.Stderr, "Allow? (y/n): ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		approved := answer == "y" || answer == "yes"

		postJSON(base+"/sessions/"+session+"/permission/"+requestID, map[string]bool{"approved": approved})

	case "usage":
		inputTok, _ := event["input_tokens"].(float64)
		outputTok, _ := event["output_tokens"].(float64)
		fmt.Fprintf(os.Stderr, "%s[tokens: %d in, %d out]%s\n", colorDim, int(inputTok), int(outputTok), colorReset)

	case "turn_complete":
		reason, _ := event["stop_reason"].(string)
		fmt.Fprintf(os.Stderr, "%s[done: %s]%s\n", colorDim, reason, colorReset)

	case "error":
		msg, _ := event["message"].(string)
		fmt.Fprintf(os.Stderr, "%s[error] %s%s\n", colorRed, msg, colorReset)
	}
}

func printSessionHistory(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session + "/messages")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%serror loading history: %v%s\n", colorRed, err, colorReset)
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

	fmt.Fprintf(os.Stderr, "%s--- conversation history (%d messages) ---%s\n", colorDim, len(messages), colorReset)

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
					fmt.Fprintf(os.Stderr, "%s> %s%s\n", colorBold, text, colorReset)
				} else if blockType == "tool_result" {
					toolID, _ := block["tool_use_id"].(string)
					text, _ := block["content"].(string)
					isErr, _ := block["is_error"].(bool)
					if isErr {
						fmt.Fprintf(os.Stderr, "%s  [result %s] error: %s%s\n", colorDim, shortID(toolID), truncate(text, 100), colorReset)
					} else {
						fmt.Fprintf(os.Stderr, "%s  [result %s] %s%s\n", colorDim, shortID(toolID), truncate(text, 100), colorReset)
					}
				}
			}
		case "assistant":
			for _, block := range content {
				blockType, _ := block["type"].(string)
				if blockType == "text" {
					text, _ := block["text"].(string)
					fmt.Fprintf(os.Stderr, "%s\n", text)
				} else if blockType == "tool_use" {
					name, _ := block["name"].(string)
					fmt.Fprintf(os.Stderr, "%s[%s]%s\n", colorCyan, name, colorReset)
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "%s--- end history ---%s\n\n", colorDim, colorReset)
}

func printSessionInfo(base, session string) {
	resp, err := getJSON(base + "/sessions/" + session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%serror: %v%s\n", colorRed, err, colorReset)
		return
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		return
	}
	fmt.Fprintf(os.Stderr, "  ID:     %s\n", data["id"])
	fmt.Fprintf(os.Stderr, "  Model:  %s\n", data["model"])
	fmt.Fprintf(os.Stderr, "  Dir:    %s\n", data["working_dir"])
	fmt.Fprintf(os.Stderr, "  Mode:   %s\n", data["permission_mode"])
	fmt.Fprintf(os.Stderr, "  Status: %s\n", data["status"])
	if usage, ok := data["usage"].(map[string]any); ok {
		in, _ := usage["TotalInputTokens"].(float64)
		out, _ := usage["TotalOutputTokens"].(float64)
		msgs, _ := usage["MessageCount"].(float64)
		fmt.Fprintf(os.Stderr, "  Tokens: %d in, %d out (%d messages)\n", int(in), int(out), int(msgs))
	}
}

func cmdSessionCreate(args []string) {
	var dir, model, mode, id, name string
	base := galactaURL()

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir", "-d":
			i++
			if i < len(args) {
				dir = args[i]
			}
		case "--model", "-m":
			i++
			if i < len(args) {
				model = args[i]
			}
		case "--mode":
			i++
			if i < len(args) {
				mode = args[i]
			}
		case "--id":
			i++
			if i < len(args) {
				id = args[i]
			}
		case "--name":
			i++
			if i < len(args) {
				name = args[i]
			}
		case "--galacta":
			i++
			if i < len(args) {
				base = args[i]
			}
		}
	}

	if dir == "" {
		dir, _ = os.Getwd()
	}

	body := map[string]string{"working_dir": dir}
	if model != "" {
		body["model"] = model
	}
	if mode != "" {
		body["permission_mode"] = mode
	}
	if id != "" {
		body["id"] = id
	}
	if name != "" {
		body["name"] = name
	}

	resp, err := postJSON(base+"/sessions", body)
	if err != nil {
		fatal("creating session: %v", err)
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
}

func cmdSessionList() {
	base := galactaURL()
	resp, err := getJSON(base + "/sessions")
	if err != nil {
		fatal("listing sessions: %v", err)
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) == 0 {
		fmt.Println("No sessions.")
		return
	}
	for _, s := range data {
		sess, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := sess["id"].(string)
		model, _ := sess["model"].(string)
		dir, _ := sess["working_dir"].(string)
		status, _ := sess["status"].(string)
		mode, _ := sess["permission_mode"].(string)

		var tokenInfo string
		if usage, ok := sess["usage"].(map[string]any); ok {
			in, _ := usage["TotalInputTokens"].(float64)
			out, _ := usage["TotalOutputTokens"].(float64)
			msgs, _ := usage["MessageCount"].(float64)
			tokenInfo = fmt.Sprintf("%din/%dout %dmsgs", int(in), int(out), int(msgs))
		}
		fmt.Fprintf(os.Stderr, "  %s  %s%-7s%s  %s  %s  %s  %s\n",
			shortID(id), colorGreen, status, colorReset, mode, model, dir, tokenInfo)
	}
}

func cmdSessionMessages(id string) {
	base := galactaURL()
	printSessionHistory(base, id)
}

func cmdSessionDelete(id string) {
	base := galactaURL()
	req, err := http.NewRequest("DELETE", base+"/sessions/"+id, nil)
	if err != nil {
		fatal("deleting session: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("deleting session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		fatal("HTTP %d: %s", resp.StatusCode, string(body))
	}
	fmt.Fprintf(os.Stderr, "Deleted session %s\n", shortID(id))
}

func cmdHealth() {
	base := galactaURL()
	resp, err := getJSON(base + "/health")
	if err != nil {
		fatal("health check: %v", err)
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
}

func postJSON(url string, body any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode >= 400 {
		errMsg, _ := result["error"].(string)
		return result, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errMsg)
	}
	return result, nil
}

func getJSON(url string) (map[string]any, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
