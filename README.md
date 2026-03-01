<p align="center">
  <img src="assets/logo.png" alt="Galacta" width="200" />
</p>

<h1 align="center">Galacta</h1>

<p align="center">
  A native Go agent daemon with <strong>Jeff</strong>, its terminal CLI.
</p>

---

## What It Is

Galacta is a self-contained Go implementation of a Claude agent loop that runs as a local HTTP daemon. **Jeff** is the interactive CLI that talks to it — spinners, box-drawn UI, multiline input, and all.

No Node.js. No Bun. No V8. No subprocess. ~10–15 MB idle.

---

## Quick Start

```bash
# Start the daemon
galacta serve

# Start an interactive session
jeff run

# One-shot message
jeff run "explain this codebase"

# Resume last session
jeff run --continue
```

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                         Galacta Daemon                       │
│                                                              │
│  HTTP API ──► AgentLoop ──► Anthropic Client (SSE)          │
│                  │                                           │
│                  ▼                                           │
│             ToolCaller ──► [Bash, Read, Write, Edit, Glob,  │
│                             Grep, WebFetch, WebSearch,       │
│                             Agent, Skill, Task, Team, ...]   │
│                  │                                           │
│                  ▼                                           │
│          PermissionGate ──► EventStream (SSE → Jeff CLI)    │
│                                                              │
│  SessionDB (SQLite) ──► history, tokens, tasks, metadata    │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                          Jeff CLI                            │
│                                                              │
│  main.go      Entry point, flags, session CRUD, HTTP helpers│
│  ui.go        Spinner, box drawing, banner, colors          │
│  events.go    SSE streaming with spinner + styled output    │
│  commands.go  Interactive loop, /slash commands, multiline  │
└──────────────────────────────────────────────────────────────┘
```

### Core Components

| Component | Responsibility |
|-----------|---------------|
| `AgentLoop` | The `tool_use` → execute → `tool_result` → repeat cycle |
| `Anthropic Client` | Streaming Claude API client (SSE, content block deltas) |
| `ToolCaller` | Dispatches tool calls; serial and concurrent execution |
| `PermissionGate` | Intercepts tool calls requiring user confirmation |
| `EventEmitter` | Structured SSE events to connected clients |
| `SessionDB` | SQLite-backed session history, token tracking, tasks |
| `Team Manager` | Multi-agent teams with messaging and task coordination |

---

## Jeff CLI

Jeff is the terminal interface to Galacta. It provides:

- **Spinner indicators** — animated braille spinner during thinking and tool execution
- **Box-drawn tool output** — tool inputs, outputs, and durations in styled boxes
- **Session banner** — model, directory, session ID, and permission mode on start
- **Multiline input** — type `"""` to enter/exit multiline mode
- **Styled permission prompts** — double-line box for approval requests
- **Question UI** — numbered options in a box for interactive questions

### Interactive Commands

| Command | Description |
|---------|-------------|
| `/session` | Show session info |
| `/model [name]` | Show or change model |
| `/permissions [mode]` | Show or change permission mode |
| `/history` | Conversation history |
| `/compact [N]` | Compact conversation (keep last N messages) |
| `/clear` | Clear screen |
| `/tasks` | List tasks with status icons |
| `/skills` | List available skills |
| `/cost` | Token usage and estimated cost |
| `/plan` | Plan mode info |
| `/help` | Show all commands |
| `/quit` | End session |

### CLI Flags

```
jeff run [flags] ["message"]

  --session, -s        Resume session by UUID
  --model, -m          Model override
  --dir, -d            Working directory (default: cwd)
  --mode               Permission mode
  --continue           Resume most recent session
  --output-format      stream | json | text
  --effort             low | medium | high
  --max-budget-usd     Spending cap in USD
  --tools              Tool allow list (repeatable)
  --allowedTools       Tool glob patterns (repeatable)
  --fallback-model     Fallback model on overload
  --galacta            Daemon URL (default: http://localhost:9090)
  --system-prompt      Override/append system prompt
```

---

## Tools

### File System

| Tool | Description |
|------|-------------|
| `galacta_read` | Read file with optional line offset/limit |
| `galacta_write` | Create or overwrite file |
| `galacta_edit` | Exact string replacement |
| `galacta_glob` | File pattern matching |
| `galacta_grep` | Regex search with context lines |
| `galacta_ls` | Directory listing |

### Execution

| Tool | Description |
|------|-------------|
| `galacta_bash` | Shell execution with timeout and cancellation |

### Web

| Tool | Description |
|------|-------------|
| `galacta_web_fetch` | HTTP fetch with HTML-to-markdown |
| `galacta_web_search` | Web search (Anthropic server tool) |

### Agent & Orchestration

| Tool | Description |
|------|-------------|
| `galacta_agent` | Spawn sub-agents for parallel work |
| `galacta_skill` | Execute named skills (built-in or user-defined) |
| `galacta_task_*` | Task creation, listing, and updates |
| `galacta_team_*` | Team creation, messaging, and coordination |
| `galacta_ask_user` | Interactive questions with options |
| `galacta_enter_plan_mode` | Enter plan mode for implementation planning |
| `galacta_worktree` | Git worktree isolation |

### MCP (Dynamic)

External MCP tools are registered at runtime as `mcp__{server}__{tool}` based on connected MCP servers.

---

## Permission Modes

| Mode | Behavior |
|------|----------|
| `default` | Ask before bash commands and writes outside cwd |
| `acceptEdits` | Auto-approve file edits; ask before bash |
| `bypassPermissions` | No prompts — execute everything |
| `plan` | Read-only; no writes or bash |
| `dontAsk` | Auto-approve everything |

---

## API

Galacta exposes an HTTP API on port 9090 (default).

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Daemon status |
| `POST` | `/sessions` | Create session |
| `GET` | `/sessions` | List sessions |
| `GET` | `/sessions/{id}` | Get session info |
| `PATCH` | `/sessions/{id}` | Update session (model, mode) |
| `DELETE` | `/sessions/{id}` | Delete session |
| `POST` | `/sessions/{id}/message` | Send message (SSE stream) |
| `GET` | `/sessions/{id}/messages` | List message history |
| `POST` | `/sessions/{id}/compact` | Compact conversation |
| `GET` | `/sessions/{id}/tasks` | List tasks |
| `POST` | `/sessions/{id}/permission/{rid}` | Respond to permission |
| `POST` | `/sessions/{id}/question/{rid}` | Respond to question |
| `GET` | `/skills?working_dir=...` | List available skills |

### SSE Event Types

```jsonc
{ "type": "text_delta", "text": "..." }
{ "type": "thinking_delta", "text": "..." }
{ "type": "tool_start", "tool": "galacta_bash", "input": { "command": "ls" } }
{ "type": "tool_result", "tool": "galacta_bash", "output": "...", "duration_ms": 23 }
{ "type": "permission_request", "request_id": "...", "tool": "...", "input": { ... } }
{ "type": "question_request", "request_id": "...", "question": "...", "options": [...] }
{ "type": "usage", "input_tokens": 1234, "output_tokens": 456 }
{ "type": "turn_complete", "stop_reason": "end_turn" }
{ "type": "plan_mode_changed", "active": true }
{ "type": "subagent_start", "agent_type": "general-purpose", "description": "..." }
{ "type": "team_created", "team_name": "..." }
{ "type": "team_message", "from": "...", "recipient": "...", "summary": "..." }
{ "type": "error", "message": "..." }
```

---

## Skills

Skills are reusable prompt templates. Define custom skills in `.claude/skills/*.md`:

```markdown
---
name: my-skill
description: Does something useful
---
Your prompt template here. Use {{.Args}} for arguments.
```

Built-in skills: `commit`, `review-pr`. List all with `/skills` in Jeff or `GET /skills`.

---

## Non-Goals

- Terminal UI / TUI rendering (Jeff is intentionally simple ANSI)
- Claude Code CLI compatibility mode
- Multi-user / networked deployment
- Sandboxed bash execution
