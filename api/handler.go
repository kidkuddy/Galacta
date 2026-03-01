package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kidkuddy/galacta/agent"
	"github.com/kidkuddy/galacta/anthropic"
	"github.com/kidkuddy/galacta/db"
	"github.com/kidkuddy/galacta/events"
	"github.com/kidkuddy/galacta/permissions"
	"github.com/kidkuddy/galacta/systemprompt"
	"github.com/kidkuddy/galacta/toolcaller"
	"github.com/kidkuddy/galacta/team"
	agenttools "github.com/kidkuddy/galacta/tools/agent"
	"github.com/kidkuddy/galacta/tools/ask"
	exectools "github.com/kidkuddy/galacta/tools/exec"
	"github.com/kidkuddy/galacta/tools/fs"
	"github.com/kidkuddy/galacta/tools/plan"
	"github.com/kidkuddy/galacta/tools/skill"
	"github.com/kidkuddy/galacta/tools/task"
	teamtools "github.com/kidkuddy/galacta/tools/team"
	"github.com/kidkuddy/galacta/tools/web"
	"github.com/kidkuddy/galacta/tools/worktree"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	dataDir        string
	apiClient      *anthropic.Client
	globalCaller   *toolcaller.Caller // holds external MCP tools (shared across sessions)
	defaultModel   string
	maxConcurrency int
	teamManager    *team.Manager

	mu       sync.RWMutex
	active   map[string]*activeRun
}

type activeRun struct {
	session      *agent.Session
	gate         *permissions.InteractiveGate
	cancel       context.CancelFunc
	emitter      *events.Emitter
	store        *db.SessionDB
	questionGate *ask.QuestionGate
}

// MCPServerInfo is returned by the health endpoint.
type MCPServerInfo struct {
	Name   string `json:"name"`
	Tools  int    `json:"tools"`
	Status string `json:"status"`
}

// NewHandler creates a new Handler.
func NewHandler(dataDir string, apiClient *anthropic.Client, globalCaller *toolcaller.Caller, defaultModel string, maxConcurrency int) *Handler {
	return &Handler{
		dataDir:        dataDir,
		apiClient:      apiClient,
		globalCaller:   globalCaller,
		defaultModel:   defaultModel,
		maxConcurrency: maxConcurrency,
		teamManager:    team.NewManager(dataDir),
		active:         make(map[string]*activeRun),
	}
}

// Health returns daemon status.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	activeSessions := len(h.active)
	h.mu.RUnlock()

	tools := h.globalCaller.ListTools()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":         "0.1.0",
		"active_sessions": activeSessions,
		"total_tools":     len(tools),
		"status":          "ok",
	})
}

// CreateSessionRequest is the request body for creating a session.
type CreateSessionRequest struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	WorkingDir     string   `json:"working_dir"`
	Model          string   `json:"model"`
	PermissionMode string   `json:"permission_mode"`
	SystemPrompt   string   `json:"system_prompt"`
	Effort         string   `json:"effort"`          // low, medium, high
	MaxBudgetUSD   float64  `json:"max_budget_usd"`
	FallbackModel  string   `json:"fallback_model"`
	Tools          []string `json:"tools"`            // tool allow list
	AllowedTools   []string `json:"allowed_tools"`    // glob patterns
}

// CreateSession creates a new session.
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.WorkingDir == "" {
		writeError(w, http.StatusBadRequest, "working_dir is required")
		return
	}

	// Validate working directory
	if info, err := os.Stat(req.WorkingDir); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid working_dir: %s", req.WorkingDir))
		return
	}

	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	if req.Model == "" {
		req.Model = h.defaultModel
	}
	if req.PermissionMode == "" {
		req.PermissionMode = "default"
	}
	if !isValidPermissionMode(req.PermissionMode) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid permission_mode: %s", req.PermissionMode))
		return
	}

	// Create session DB
	store, err := db.Open(h.dataDir, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating session db: %v", err))
		return
	}

	// Store session metadata
	store.SetMeta("name", req.Name)
	store.SetMeta("working_dir", req.WorkingDir)
	store.SetMeta("model", req.Model)
	store.SetMeta("permission_mode", req.PermissionMode)
	store.SetMeta("system_prompt", req.SystemPrompt)
	store.SetMeta("updated_at", time.Now().Format(time.RFC3339))
	if req.Effort != "" {
		store.SetMeta("effort", req.Effort)
	}
	if req.MaxBudgetUSD > 0 {
		store.SetMeta("max_budget_usd", fmt.Sprintf("%f", req.MaxBudgetUSD))
	}
	if req.FallbackModel != "" {
		store.SetMeta("fallback_model", req.FallbackModel)
	}
	if len(req.Tools) > 0 {
		toolsJSON, _ := json.Marshal(req.Tools)
		store.SetMeta("tools", string(toolsJSON))
	}
	if len(req.AllowedTools) > 0 {
		allowedJSON, _ := json.Marshal(req.AllowedTools)
		store.SetMeta("allowed_tools", string(allowedJSON))
	}
	store.Close()

	now := time.Now()
	sess := &agent.Session{
		ID:             req.ID,
		Name:           req.Name,
		WorkingDir:     req.WorkingDir,
		Model:          req.Model,
		PermissionMode: req.PermissionMode,
		SystemPrompt:   req.SystemPrompt,
		Status:         agent.StatusIdle,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	writeJSON(w, http.StatusCreated, sess)
}

// ListSessions lists all sessions by scanning the sessions directory.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	filterDir := r.URL.Query().Get("working_dir")

	sessDir := filepath.Join(h.dataDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []interface{}{})
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("reading sessions dir: %v", err))
		return
	}

	var sessions []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".db")
		info := h.sessionInfo(sessionID)
		if info == nil {
			continue
		}
		if filterDir != "" {
			wd, _ := info["working_dir"].(string)
			if wd != filterDir {
				continue
			}
		}
		sessions = append(sessions, info)
	}

	if sessions == nil {
		sessions = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

// GetSession returns session info.
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	info := h.sessionInfo(id)
	if info == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// DeleteSession aborts a running session and deletes its DB.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Abort if running
	h.mu.Lock()
	if run, ok := h.active[id]; ok {
		run.cancel()
		delete(h.active, id)
	}
	h.mu.Unlock()

	if err := db.DeleteSessionDB(h.dataDir, id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("deleting session: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// RunMessageRequest is the request body for running a message.
type RunMessageRequest struct {
	Message string `json:"message"`
}

// RunMessage runs a user message in a session and streams SSE events.
func (h *Handler) RunMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req RunMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Check not already running
	h.mu.RLock()
	if _, running := h.active[id]; running {
		h.mu.RUnlock()
		writeError(w, http.StatusConflict, "session is already running")
		return
	}
	h.mu.RUnlock()

	// Open session DB
	store, err := db.Open(h.dataDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}

	// Load session metadata
	model, _ := store.GetMeta("model")
	permMode, _ := store.GetMeta("permission_mode")
	userSystemPrompt, _ := store.GetMeta("system_prompt")
	workingDir, _ := store.GetMeta("working_dir")
	effort, _ := store.GetMeta("effort")
	maxBudgetStr, _ := store.GetMeta("max_budget_usd")
	fallbackModel, _ := store.GetMeta("fallback_model")
	toolsStr, _ := store.GetMeta("tools")
	allowedToolsStr, _ := store.GetMeta("allowed_tools")

	if model == "" {
		model = h.defaultModel
	}
	if permMode == "" {
		permMode = "default"
	}

	// Update the session's updated_at timestamp
	store.SetMeta("updated_at", time.Now().Format(time.RFC3339))

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		store.Close()
		return
	}

	ctx, cancel := context.WithCancel(r.Context())

	emitter := events.NewEmitter(id, 256)
	gate := permissions.NewInteractiveGate(
		permissions.NewModeGate(permMode),
		emitter,
	)

	// Create per-session MCP tool servers (they need the session's working dir)
	questionGate := ask.NewQuestionGate(emitter)
	planState := &plan.PlanState{}
	sessionCaller, sessionClients := h.buildSessionCaller(workingDir, store, emitter, questionGate, planState)

	// Build tool filter from session metadata
	var toolFilter *toolcaller.ToolFilter
	if toolsStr != "" || allowedToolsStr != "" {
		toolFilter = &toolcaller.ToolFilter{}
		if toolsStr != "" {
			json.Unmarshal([]byte(toolsStr), &toolFilter.Allow)
		}
		if allowedToolsStr != "" {
			json.Unmarshal([]byte(allowedToolsStr), &toolFilter.Globs)
		}
	}

	// Get tool names for system prompt
	var tools []anthropic.Tool
	if toolFilter != nil {
		tools = sessionCaller.FilteredListTools(toolFilter)
	} else {
		tools = sessionCaller.ListTools()
	}
	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name
	}

	// Build system prompt with CLAUDE.md discovery and environment context
	builtPrompt, err := systemprompt.Build(systemprompt.BuildOptions{
		WorkingDir:   workingDir,
		Model:        model,
		ToolNames:    toolNames,
		UserOverride: userSystemPrompt,
	})
	if err != nil {
		log.Printf("galacta: failed to build system prompt: %v", err)
		builtPrompt = userSystemPrompt // fallback to user-provided prompt
	}

	// Build agent loop options
	loopOpts := &agent.AgentLoopOptions{
		FallbackModel: fallbackModel,
		ServerTools: []anthropic.ServerTool{
			{Type: "web_search_20250305", Name: "web_search", MaxUses: 5},
		},
		PlanState: planState,
	}
	if maxBudgetStr != "" {
		fmt.Sscanf(maxBudgetStr, "%f", &loopOpts.MaxBudgetUSD)
	}

	// Map effort to thinking config
	if effort != "" {
		loopOpts.Thinking = effortToThinking(effort)
	}

	// Register agent tool (done post-build to avoid circular dep — agent needs the caller)
	agentSrv := agenttools.NewServer(&agenttools.Deps{
		Client:      h.apiClient,
		Caller:      sessionCaller,
		Emitter:     emitter,
		Model:       model,
		WorkingDir:  workingDir,
		TeamManager: h.teamManager,
	})
	agentMC, err := client.NewInProcessClient(agentSrv)
	if err == nil {
		if err := agentMC.Start(ctx); err == nil {
			initReq := mcp.InitializeRequest{}
			initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initReq.Params.ClientInfo = mcp.Implementation{Name: "galacta", Version: "0.1.0"}
			if _, err := agentMC.Initialize(ctx, initReq); err == nil {
				if err := sessionCaller.AddClient(ctx, agentMC); err == nil {
					sessionClients = append(sessionClients, agentMC)
				}
			}
		}
	}

	loop := agent.NewAgentLoop(h.apiClient, sessionCaller, gate, emitter, store, model, builtPrompt, loopOpts)

	run := &activeRun{
		session: &agent.Session{
			ID:             id,
			WorkingDir:     workingDir,
			Model:          model,
			PermissionMode: permMode,
		},
		gate:         gate,
		cancel:       cancel,
		emitter:      emitter,
		store:        store,
		questionGate: questionGate,
	}

	h.mu.Lock()
	h.active[id] = run
	h.mu.Unlock()

	// Run agent loop in a goroutine
	done := make(chan error, 1)
	go func() {
		defer func() {
			emitter.Close()
			store.Close()
			for _, mc := range sessionClients {
				mc.Close()
			}
			h.mu.Lock()
			delete(h.active, id)
			h.mu.Unlock()
		}()
		done <- loop.Run(ctx, id, req.Message)
	}()

	// Stream events to the HTTP response
	for data := range emitter.Channel() {
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		flusher.Flush()
	}

	// Final done event
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()

	// Wait for loop to finish (should already be done since emitter is closed)
	if err := <-done; err != nil {
		log.Printf("galacta: session %s run error: %v", id, err)
	}

	cancel()
}

// PermissionResponse is the request body for responding to a permission request.
type PermissionResponse struct {
	Approved bool `json:"approved"`
}

// RespondPermission responds to a pending permission request.
func (h *Handler) RespondPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	requestID := chi.URLParam(r, "requestID")

	var req PermissionResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.RLock()
	run, ok := h.active[id]
	h.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "session not running")
		return
	}

	if err := run.gate.Respond(requestID, req.Approved); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("permission request not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"approved": req.Approved})
}

// QuestionResponse is the request body for responding to a question.
type QuestionResponse struct {
	Answer string `json:"answer"`
}

// RespondQuestion responds to a pending question from the ask_user tool.
func (h *Handler) RespondQuestion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	requestID := chi.URLParam(r, "requestID")

	var req QuestionResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.RLock()
	run, ok := h.active[id]
	h.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "session not running")
		return
	}

	if run.questionGate == nil {
		writeError(w, http.StatusNotFound, "no question gate for session")
		return
	}

	if err := run.questionGate.Respond(requestID, req.Answer); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("question not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"answered": requestID})
}

// ListMessages returns all messages in a session.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	store, err := db.Open(h.dataDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	defer store.Close()

	messages, err := store.ListMessages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("listing messages: %v", err))
		return
	}

	usage, _ := store.GetUsageTotals()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"usage":    usage,
	})
}

// sessionInfo loads session info from its DB file.
func (h *Handler) sessionInfo(sessionID string) map[string]interface{} {
	store, err := db.Open(h.dataDir, sessionID)
	if err != nil {
		return nil
	}
	defer store.Close()

	name, _ := store.GetMeta("name")
	model, _ := store.GetMeta("model")
	workingDir, _ := store.GetMeta("working_dir")
	permMode, _ := store.GetMeta("permission_mode")
	updatedAt, _ := store.GetMeta("updated_at")
	usage, _ := store.GetUsageTotals()

	h.mu.RLock()
	_, running := h.active[sessionID]
	h.mu.RUnlock()

	status := agent.StatusIdle
	if running {
		status = agent.StatusRunning
	}

	info := map[string]interface{}{
		"id":              sessionID,
		"name":            name,
		"model":           model,
		"working_dir":     workingDir,
		"permission_mode": permMode,
		"status":          status,
		"updated_at":      updatedAt,
	}
	if usage != nil {
		info["usage"] = usage
	}
	return info
}

// buildSessionCaller creates a per-session ToolCaller with built-in MCP tools
// scoped to the session's working directory, plus any global external tools.
func (h *Handler) buildSessionCaller(workingDir string, store *db.SessionDB, emitter *events.Emitter, questionGate *ask.QuestionGate, planState *plan.PlanState) (*toolcaller.Caller, []client.MCPClient) {
	registry := toolcaller.NewRegistry()
	caller := toolcaller.NewCaller(registry, h.maxConcurrency)

	var clients []client.MCPClient

	ctx := context.Background()

	// Built-in tools scoped to this session's working dir
	servers := []struct {
		name string
		srv  *server.MCPServer
	}{
		{"fs", fs.NewServer(workingDir)},
		{"exec", exectools.NewServer(workingDir)},
		{"web", web.NewServer()},
		{"task", task.NewServer(store)},
		{"skill", skill.NewServer(workingDir)},
		{"ask", ask.NewServer(questionGate)},
		{"plan", plan.NewServer(planState, emitter)},
		{"team", teamtools.NewServer(&teamtools.Deps{Manager: h.teamManager, Emitter: emitter})},
		{"worktree", worktree.NewServer(&worktree.Deps{WorkingDir: workingDir, Store: store})},
	}

	for _, s := range servers {
		mc, err := client.NewInProcessClient(s.srv)
		if err != nil {
			log.Printf("galacta: failed to create %s client: %v", s.name, err)
			continue
		}
		if err := mc.Start(ctx); err != nil {
			log.Printf("galacta: failed to start %s client: %v", s.name, err)
			continue
		}
		initReq := mcp.InitializeRequest{}
		initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initReq.Params.ClientInfo = mcp.Implementation{Name: "galacta", Version: "0.1.0"}
		if _, err := mc.Initialize(ctx, initReq); err != nil {
			log.Printf("galacta: failed to initialize %s client: %v", s.name, err)
			mc.Close()
			continue
		}
		if err := caller.AddClient(ctx, mc); err != nil {
			log.Printf("galacta: failed to discover %s tools: %v", s.name, err)
			mc.Close()
			continue
		}
		clients = append(clients, mc)
	}

	// Also register any global (external) tools
	for _, ref := range h.globalCaller.ListToolRefs() {
		registry.Add(ref.Name, ref.ToolRef)
	}

	return caller, clients
}

// UpdateSessionRequest is the request body for patching a session.
type UpdateSessionRequest struct {
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// UpdateSession patches session metadata (model, permission_mode).
func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	store, err := db.Open(h.dataDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	defer store.Close()

	if req.Model != "" {
		store.SetMeta("model", req.Model)
	}
	if req.PermissionMode != "" {
		if !isValidPermissionMode(req.PermissionMode) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid permission_mode: %s", req.PermissionMode))
			return
		}
		store.SetMeta("permission_mode", req.PermissionMode)
	}

	store.SetMeta("updated_at", time.Now().Format(time.RFC3339))

	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

// ListTasks returns all tasks in a session.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	store, err := db.Open(h.dataDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	defer store.Close()

	tasks, err := store.ListTasks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("listing tasks: %v", err))
		return
	}
	if tasks == nil {
		tasks = []*db.Task{}
	}

	writeJSON(w, http.StatusOK, tasks)
}

// CompactSessionRequest is the request body for compacting a session.
type CompactSessionRequest struct {
	Instructions string `json:"instructions,omitempty"` // optional extra instructions for summarization
}

// CompactSession summarizes the conversation and replaces old messages with the summary.
// Uses the same summarization approach as Claude Code's /compact command.
func (h *Handler) CompactSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req CompactSessionRequest
	json.NewDecoder(r.Body).Decode(&req)

	store, err := db.Open(h.dataDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	defer store.Close()

	messages, err := store.ListMessages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("listing messages: %v", err))
		return
	}

	if len(messages) <= 2 {
		writeJSON(w, http.StatusOK, map[string]any{
			"compacted":     false,
			"message_count": len(messages),
		})
		return
	}

	// Reconstruct conversation history
	var history []anthropic.Message
	for _, row := range messages {
		var content []anthropic.ContentBlock
		if err := json.Unmarshal([]byte(row.Content), &content); err != nil {
			continue
		}
		history = append(history, anthropic.Message{
			Role:    row.Role,
			Content: content,
		})
	}

	// Build compact request
	model, _ := store.GetMeta("model")
	if model == "" {
		model = h.defaultModel
	}

	compactPrompt := agent.CompactUserPrompt
	if req.Instructions != "" {
		compactPrompt += "\n\nAdditional instructions: " + req.Instructions
	}

	compactMessages := make([]anthropic.Message, 0, len(history)+1)
	compactMessages = append(compactMessages, history...)
	compactMessages = append(compactMessages, anthropic.NewUserMessage(compactPrompt))

	resp, err := h.apiClient.SendMessage(r.Context(), anthropic.MessageRequest{
		Model:    model,
		System:   agent.CompactSystemPrompt,
		Messages: compactMessages,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("compact API call: %v", err))
		return
	}

	// Extract summary
	var summaryText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summaryText += block.Text
		}
	}

	summary := agent.ExtractSummary(summaryText)
	if summary == "" {
		writeError(w, http.StatusInternalServerError, "compact response missing <summary> tags")
		return
	}

	// Replace all messages with the summary
	continuationText := fmt.Sprintf("This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n%s\n\nPlease continue the conversation from where we left off without asking the user any further questions. Continue with the last task that you were asked to work on.", summary)

	preCount := len(messages)
	for _, row := range messages {
		store.DeleteMessage(row.ID)
	}

	summaryContent, _ := json.Marshal([]anthropic.ContentBlock{
		{Type: "text", Text: continuationText},
	})
	store.SaveMessage(&db.MessageRow{
		ID:      fmt.Sprintf("compact-%s", id[:8]),
		Role:    "user",
		Content: string(summaryContent),
		Model:   model,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"compacted":          true,
		"pre_message_count":  preCount,
		"post_message_count": 1,
		"compact_usage": map[string]int{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	})
}

// ListSkills returns available skills for a given working directory.
func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workingDir := r.URL.Query().Get("working_dir")
	if workingDir == "" {
		writeError(w, http.StatusBadRequest, "working_dir query parameter is required")
		return
	}
	registry := skill.NewRegistry(workingDir)
	writeJSON(w, http.StatusOK, registry.ListInfo())
}

// GetAccountUsage returns rate limit utilization captured from API response headers.
func (h *Handler) GetAccountUsage(w http.ResponseWriter, r *http.Request) {
	info := h.apiClient.GetRateLimits()
	if info == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "no_data",
			"message": "No rate limit data yet. Send a message first.",
		})
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func isValidPermissionMode(mode string) bool {
	switch mode {
	case "default", "acceptEdits", "bypassPermissions", "plan", "dontAsk":
		return true
	}
	return false
}

// effortToThinking maps effort level to Anthropic thinking configuration.
func effortToThinking(effort string) *anthropic.ThinkingConfig {
	switch effort {
	case "low":
		return &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 1024}
	case "medium":
		return &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	case "high":
		return &anthropic.ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	default:
		return nil
	}
}
