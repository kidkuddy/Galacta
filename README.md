# Herald

A lightweight, native Go agent loop — gnz's replacement for the Claude Code CLI dependency.

Named after the Silver Surfer: the herald that goes ahead, does the work, and reports back.

---

## What It Is

Herald is a self-contained Go package that implements the Claude agent loop directly against
the Anthropic API. It replaces the current pattern of shelling out to the `claude` binary
(`manager.go` → `exec.CommandContext`) with a native in-process implementation.

No Node.js. No Bun. No V8. No Ink. No subprocess.

Memory footprint: ~10–15 MB idle vs ~200–400 MB for the Claude Code CLI.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          Herald                                  │
│                                                                  │
│  Session ──► AgentLoop ──► AnthropicClient (SSE streaming)      │
│                │                                                 │
│                ▼                                                 │
│           ToolCaller ──► [Bash, Read, Write, Edit, Glob, Grep,  │
│                           LS, WebFetch, MCP...]                  │
│                │                                                 │
│                ▼                                                 │
│        PermissionGate ──► EventStream (SSE → gnz frontend)      │
│                                                                  │
│  ContextManager ──► SQLite (session history, token tracking)    │
└─────────────────────────────────────────────────────────────────┘
```

### Core Components

| Component | Responsibility |
|-----------|---------------|
| `AgentLoop` | The main `tool_use` → execute → `tool_result` → repeat cycle |
| `AnthropicClient` | Streaming Claude API client (SSE, handles `content_block_delta`) |
| `ToolCaller` | Dispatches tool calls; supports serial and concurrent execution |
| `ToolExecutor` | Interface each tool implements |
| `PermissionGate` | Intercepts tool calls requiring user confirmation; suspends loop |
| `ContextManager` | Tracks conversation history, token usage, applies truncation strategy |
| `EventStream` | Emits structured SSE events to the gnz frontend |
| `SessionStore` | Persists session state and history to SQLite (gnz's existing DB) |

---

## Tool Inventory

These are all tools Claude Code supports. Herald implements the ones marked ✓ in Phase 1.

### File System

| Tool | Claude Code name | Priority | Notes |
|------|-----------------|----------|-------|
| Read | `Read` | P0 | Read file, with line offset/limit |
| Write | `Write` | P0 | Overwrite or create file |
| Edit | `Edit` | P0 | Exact string replace (`old_string` / `new_string`); hardest to get right |
| MultiEdit | `MultiEdit` | P1 | Batch edits in one call |
| Glob | `Glob` | P0 | File pattern matching, sorted by mtime |
| Grep | `Grep` | P0 | ripgrep-equivalent; regex, file type filter, context lines |
| LS | `LS` | P0 | Directory listing with ignore patterns |

### Execution

| Tool | Claude Code name | Priority | Notes |
|------|-----------------|----------|-------|
| Bash | `Bash` | P0 | Shell exec; cwd, env, timeout, cancellation. PTY for interactive |

### Web

| Tool | Claude Code name | Priority | Notes |
|------|-----------------|----------|-------|
| WebFetch | `WebFetch` | P2 | HTTP fetch, HTML→markdown |
| WebSearch | `WebSearch` | P2 | Web search (requires search API key) |

### Notebooks

| Tool | Claude Code name | Priority | Notes |
|------|-----------------|----------|-------|
| NotebookRead | `NotebookRead` | P3 | Read Jupyter .ipynb |
| NotebookEdit | `NotebookEdit` | P3 | Edit notebook cells |

### Agent / Meta

| Tool | Claude Code name | Priority | Notes |
|------|-----------------|----------|-------|
| Task | `Task` | P2 | Spawn sub-agent with its own loop |
| TodoRead | `TodoRead` | P1 | Read session todo list |
| TodoWrite | `TodoWrite` | P1 | Write session todo list |

### MCP (dynamic)

MCP tools are registered at runtime as `mcp__{server}__{tool}` based on connected servers.
Herald acts as an MCP client; gnz's own MCP server is auto-connected per session.

---

## Permission Modes

Inherited from Claude Code. Controls which tool calls require user confirmation.

| Mode | Behavior |
|------|----------|
| `default` | Ask before bash commands and writes outside cwd |
| `acceptEdits` | Auto-approve file edits; ask before bash |
| `bypassPermissions` | No prompts — execute everything automatically |
| `plan` | Read-only; no writes or bash execution |
| `dontAsk` | Auto-approve everything (alias for bypassPermissions) |

Permission prompts suspend the agent loop and emit a `permission_request` SSE event.
The gnz frontend responds via a POST to resume or reject.

---

## Event Format

Herald emits NDJSON events over SSE. The schema is Herald-native (not Claude Code compat):

```jsonc
// Text streaming
{ "type": "text_delta", "session_id": "...", "text": "..." }

// Tool call starting
{ "type": "tool_start", "session_id": "...", "tool": "Bash", "input": { "command": "ls" } }

// Tool result
{ "type": "tool_result", "session_id": "...", "tool": "Bash", "output": "...", "error": null }

// Permission request (loop suspended)
{ "type": "permission_request", "session_id": "...", "tool": "Bash", "input": { "command": "rm -rf /tmp/x" } }

// Turn complete
{ "type": "turn_complete", "session_id": "...", "stop_reason": "end_turn" }

// Usage / cost tracking
{ "type": "usage", "session_id": "...", "input_tokens": 1234, "output_tokens": 456, "cache_read_tokens": 0, "cache_write_tokens": 0 }

// Error
{ "type": "error", "session_id": "...", "message": "..." }
```

---

## Integration with gnz

Herald replaces the subprocess model in `backend/internal/modules/claude/manager.go`.

The `Manager` struct stays; `spawnProcess` is replaced with a call into `herald.Run(session, eventsCh)`.
Session state (history, token counts, working directory) moves from in-memory to SQLite.

The gnz MCP server is auto-wired as an MCP client connection per session — same as today,
minus the temp JSON config file.

---

## Non-Goals

- Terminal UI / TUI rendering
- Claude Code CLI compatibility mode
- Multi-user / networked deployment (single operator desktop tool)
- Sandboxing / containerized bash execution (operator tool, trust the user)
