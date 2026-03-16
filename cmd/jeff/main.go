package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
		// Default to "run" with no args
		cmdRun(nil)
		return
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
	case "help", "--help", "-h":
		printUsage()
	default:
		// Unknown subcommand — treat entire args as "run" args
		// so `jeff "hello"` works as `jeff run "hello"`
		cmdRun(os.Args[1:])
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
  --mcp-config         Path to MCP servers config JSON

Interactive commands (during session):
  /help          Show available commands
  /quit, /exit   End the session`)
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
	mcpConfigPath string
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
		case "--mcp-config":
			i++
			if i < len(args) {
				f.mcpConfigPath = args[i]
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

	// Handle --continue: find the most recent session for this working dir
	if session == "" && flags.continueFlag {
		session = findMostRecentSession(flags.base, flags.dir)
		if session == "" {
			fatal("no previous sessions found to continue")
		}
	}

	if session == "" {
		session = createSession(flags)
	} else {
		// Resuming — print old messages
		printSessionHistory(flags.base, session)
		// If --mcp-config was provided on resume, update the session's stored MCP servers.
		if flags.mcpConfigPath != "" {
			updateSessionMCP(flags.base, session, flags.mcpConfigPath)
		}
	}

	// Fetch session info for banner
	model := flags.model
	mode := flags.mode
	if model == "" || mode == "" {
		if resp, err := getJSON(flags.base + "/sessions/" + session); err == nil {
			if data, ok := resp["data"].(map[string]any); ok {
				if model == "" {
					model, _ = data["model"].(string)
				}
				if mode == "" {
					mode, _ = data["permission_mode"].(string)
				}
			}
		}
	}
	if mode == "" {
		mode = "default"
	}

	printBanner(model, flags.dir, session, mode)

	// If a message was given on the command line, send it
	if flags.message != "" {
		streamMessage(flags.base, session, flags.message, flags.outputFormat)
	}

	// Enter interactive loop
	interactiveLoop(flags.base, session, flags.outputFormat)
	printResumeHint(session)
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
	if flags.mcpConfigPath != "" {
		data, err := os.ReadFile(flags.mcpConfigPath)
		if err != nil {
			fatal("reading MCP config: %v", err)
		}
		var cfg struct {
			MCPServers map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			fatal("parsing MCP config: %v", err)
		}
		if len(cfg.MCPServers) > 0 {
			body["mcp_servers"] = cfg.MCPServers
		}
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

func updateSessionMCP(base, session, mcpConfigPath string) {
	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%swarning: reading MCP config: %v%s\n", colorYellow, err, colorReset)
		return
	}
	var cfg struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%swarning: parsing MCP config: %v%s\n", colorYellow, err, colorReset)
		return
	}
	if len(cfg.MCPServers) == 0 {
		return
	}
	body := map[string]any{"mcp_servers": cfg.MCPServers}
	if _, err := patchJSON(base+"/sessions/"+session, body); err != nil {
		fmt.Fprintf(os.Stderr, "%swarning: updating session MCP config: %v%s\n", colorYellow, err, colorReset)
	}
}

func findMostRecentSession(base, workingDir string) string {
	url := base + "/sessions"
	if workingDir != "" {
		url += "?working_dir=" + workingDir
	}
	resp, err := getJSON(url)
	if err != nil {
		return ""
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) == 0 {
		return ""
	}

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
	cwd, _ := os.Getwd()
	url := base + "/sessions"
	if cwd != "" {
		url += "?working_dir=" + cwd
	}
	resp, err := getJSON(url)
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

// HTTP helpers

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

func patchJSON(url string, body any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
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

// Utilities

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
