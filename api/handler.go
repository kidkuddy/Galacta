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
	"github.com/kidkuddy/galacta/toolcaller"
	exectools "github.com/kidkuddy/galacta/tools/exec"
	"github.com/kidkuddy/galacta/tools/fs"
	"github.com/kidkuddy/galacta/tools/web"
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

	mu       sync.RWMutex
	active   map[string]*activeRun
}

type activeRun struct {
	session *agent.Session
	gate    *permissions.InteractiveGate
	cancel  context.CancelFunc
	emitter *events.Emitter
	store   *db.SessionDB
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
	ID             string `json:"id"`
	Name           string `json:"name"`
	WorkingDir     string `json:"working_dir"`
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	SystemPrompt   string `json:"system_prompt"`
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
		if info != nil {
			sessions = append(sessions, info)
		}
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
	systemPrompt, _ := store.GetMeta("system_prompt")
	workingDir, _ := store.GetMeta("working_dir")

	if model == "" {
		model = h.defaultModel
	}
	if permMode == "" {
		permMode = "default"
	}

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
	sessionCaller, sessionClients := h.buildSessionCaller(workingDir)

	loop := agent.NewAgentLoop(h.apiClient, sessionCaller, gate, emitter, store, model, systemPrompt)

	run := &activeRun{
		session: &agent.Session{
			ID:             id,
			WorkingDir:     workingDir,
			Model:          model,
			PermissionMode: permMode,
		},
		gate:    gate,
		cancel:  cancel,
		emitter: emitter,
		store:   store,
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
	}
	if usage != nil {
		info["usage"] = usage
	}
	return info
}

// buildSessionCaller creates a per-session ToolCaller with built-in MCP tools
// scoped to the session's working directory, plus any global external tools.
func (h *Handler) buildSessionCaller(workingDir string) (*toolcaller.Caller, []client.MCPClient) {
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

func isValidPermissionMode(mode string) bool {
	switch mode {
	case "default", "acceptEdits", "bypassPermissions", "plan", "dontAsk":
		return true
	}
	return false
}
