# Herald — Roadmap

## Phase 0 — Skeleton (now)
- [ ] Package structure: `agent/`, `tools/`, `stream/`, `permissions/`, `context/`, `mcp/`
- [ ] Core interfaces: `ToolExecutor`, `EventEmitter`, `PermissionGate`, `ContextStrategy`
- [ ] Anthropic streaming client: SSE parsing, `content_block_delta`, `input_json_delta`
- [ ] Basic event types and NDJSON serialization

## Phase 1 — Core Agent Loop + P0 Tools
- [ ] `AgentLoop.Run()`: send → receive → tool_use check → execute → append tool_result → repeat
- [ ] Parallel tool call support: detect multiple `tool_use` blocks, run concurrently, collect results
- [ ] Tool: `Bash` — exec with cwd, env, timeout, cancellation (pipe mode first, PTY later)
- [ ] Tool: `Read` — file read with offset/limit
- [ ] Tool: `Write` — create or overwrite file
- [ ] Tool: `Edit` — exact old_string/new_string replacement with clear error on mismatch
- [ ] Tool: `Glob` — filepath pattern matching sorted by mtime
- [ ] Tool: `Grep` — regex search with type filter, context lines
- [ ] Tool: `LS` — directory listing
- [ ] SSE event stream: `text_delta`, `tool_start`, `tool_result`, `turn_complete`, `usage`, `error`
- [ ] Basic token tracking per turn (from API response metadata)

## Phase 2 — Permission System
- [ ] `PermissionGate` interface
- [ ] Permission modes: `default`, `acceptEdits`, `bypassPermissions`, `plan`, `dontAsk`
- [ ] Loop suspension on `permission_request`: emit event, wait on channel
- [ ] Resume endpoint: POST to approve/reject a pending permission
- [ ] `plan` mode: dry-run — tool calls return a "would execute" result, no side effects

## Phase 3 — Context & Token Management
- [ ] Conversation history stored in SQLite (reuse gnz's DB)
- [ ] Token usage accumulated per session (input, output, cache_read, cache_write)
- [ ] Cost estimation (model pricing table, updated per model)
- [ ] Strategy interface: `TruncationStrategy` (implementations: sliding window, summarize)
- [ ] Sliding window: drop oldest user/assistant pairs when approaching limit
- [ ] Summarization: inject a summary message when history is pruned

## Phase 4 — MCP Client
- [ ] MCP client over SSE transport (JSON-RPC 2.0)
- [ ] Auto-connect to gnz's own MCP server per session (replaces temp config file approach)
- [ ] Dynamic tool registration: `mcp__{server}__{tool}` → `ToolExecutor` at runtime
- [ ] MCP tool call / result cycle wired into `ToolCaller`
- [ ] Support multiple MCP servers per session

## Phase 5 — gnz Integration
- [ ] Replace `manager.go` subprocess model with `herald.Run()`
- [ ] Session resume: load history from SQLite, pass to `AgentLoop`
- [ ] SSE handler in `handler.go` consumes Herald event channel
- [ ] Frontend event adapter: map Herald events to gnz UI event types
- [ ] Remove `resolveClaudeBinary()` and all `exec.CommandContext` for claude
- [ ] Keep `--permission-prompt-tool stdio` path as fallback (while in transition)

## Phase 6 — P1/P2 Tools
- [ ] Tool: `MultiEdit` — batch file edits in a single call
- [ ] Tool: `TodoRead` / `TodoWrite` — per-session todo list in SQLite
- [ ] Tool: `Task` — sub-agent spawning (nested `AgentLoop` with shared session store)
- [ ] Tool: `WebFetch` — HTTP GET, HTML→markdown via goldmark or similar
- [ ] Tool: `WebSearch` — pluggable search backend (Brave, Serper, etc.)

## Phase 7 — Bash PTY
- [ ] PTY allocation for interactive bash commands (vim, less, etc.)
- [ ] Terminal size negotiation
- [ ] Input forwarding from gnz frontend to PTY

## Phase 8 — Notebooks (P3, optional)
- [ ] Tool: `NotebookRead` — parse .ipynb, return cells with outputs
- [ ] Tool: `NotebookEdit` — modify cell source by index

---

## Decisions Log

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Native Go, no subprocess | Eliminate V8/Node overhead; ~10x lower RAM |
| 2 | Herald-native SSE event format | Don't carry Claude Code schema debt; rebuild gnz frontend adapter cleanly |
| 3 | SQLite for history | Reuse gnz's existing DB; sessions survive restarts |
| 4 | SSE over gRPC | gnz already uses SSE; single-consumer desktop app doesn't need gRPC |
| 5 | Parallel tool calls from Phase 1 | Claude regularly emits multiple tool_use blocks; serial execution would be a regression |
| 6 | ToolExecutor interface | Allows MCP tools and built-in tools to be treated identically by AgentLoop |
| 7 | PermissionGate as interface | Lets plan mode, bypass mode, and interactive mode share the same loop code |
