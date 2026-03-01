# Galacta/Jeff vs Claude Code: Compatibility Matrix

**Baseline:** Claude Code CLI v2.1.63
**Date:** 2026-03-01

> **Galacta** = daemon (handles conversations, tool execution, agent loop)
> **Jeff** = CLI client (interacts with Galacta via HTTP/SSE API)

---

## Tools

Galacta implements its own tool definitions (prefixed `galacta_*`) rather than using Claude Code's tool names and prompts. Claude Code's tool system includes detailed system prompts with usage instructions baked into each tool description — Galacta replicates the key guidance via its `systemprompt` package.

### Implemented

| Claude Code Tool | Galacta Equivalent | Notes |
|-----------------|-------------------|-------|
| Read | `galacta_read` | Line numbers, offset/limit support |
| Write | `galacta_write` | Creates parent dirs |
| Edit | `galacta_edit` | old_string/new_string with replace_all |
| Glob | `galacta_glob` | Pattern matching, newest-first |
| Grep | `galacta_grep` | Regex search, context lines, output modes |
| Bash | `galacta_bash` | Timeout support (120s default, 600s max) |
| WebFetch | `galacta_web_fetch` | URL fetch with max_bytes, strips HTML |
| WebSearch | Server tool | Anthropic web_search server tool |
| Agent | `galacta_agent` | Sub-agent spawning (general-purpose, explore, plan) |
| Skill | `galacta_skill` | Built-in + user-defined skill execution |
| TaskCreate | `galacta_task_create` | Task creation with dependencies |
| TaskGet | `galacta_task_get` | Get task by ID |
| TaskUpdate | `galacta_task_update` | Update status, owner, blocks |
| TaskList | `galacta_task_list` | List all tasks |
| AskUserQuestion | `galacta_ask_user` | Structured prompts with options |
| EnterPlanMode | `galacta_enter_plan_mode` | Enter plan mode |
| ExitPlanMode | `galacta_exit_plan_mode` | Exit plan mode with approval |
| TeamCreate | `galacta_team_create` | Multi-agent team creation |
| TeamDelete | `galacta_team_delete` | Team cleanup |
| SendMessage | `galacta_send_message` | Inter-agent messaging |
| EnterWorktree | `galacta_worktree` | Git worktree isolation |

### Not Implemented

| Claude Code Tool | Priority | Notes |
|-----------------|----------|-------|
| NotebookEdit | Skipped | Jupyter notebook cell editing — not needed |

## System Prompt

**Claude Code** injects a substantial default system prompt that includes:
- Detailed per-tool usage instructions (when to use Read vs Bash, Edit vs Write, etc.)
- Git commit conventions and safety protocols
- PR creation workflow
- Code style guidelines (no over-engineering, no sycophancy, etc.)
- Environment context (OS, shell, git status, model info)
- CLAUDE.md file contents (project-specific instructions)
- Tone and formatting rules

**Galacta** builds a default system prompt via the `systemprompt` package:
- [x] Default system prompt with tool usage guidance
- [x] CLAUDE.md discovery and injection
- [x] Environment context injection (OS, shell, git status, model)
- [x] Safety rails for destructive operations
- [x] Git/PR conventions

User-provided system prompts (via `--system-prompt` flag or session creation) are appended to the built default.

## Slash Commands

### Implemented in Jeff

| Command | Notes |
|---------|-------|
| `/quit` `/exit` | End session |
| `/history` | Show conversation history |
| `/session` | Show session info (box-drawn) |
| `/clear` | Clear screen |
| `/cost` `/usage` | Token usage and estimated cost (box-drawn) |
| `/model [name]` | Show or change model |
| `/permissions [mode]` | Show or change permission mode |
| `/compact [N]` | Compact conversation (keep last N messages) |
| `/tasks` | List tasks with status icons (● ◐ ✓) |
| `/skills` | List available skills with descriptions |
| `/plan` | Plan mode info |
| `/help` | Box-drawn categorized help |

### Jeff UI Features

| Feature | Status |
|---------|--------|
| Spinner (braille animation) | Implemented |
| Box-drawn tool output | Implemented |
| Session banner | Implemented |
| Multiline input (`"""`) | Implemented |
| Double-line permission boxes | Implemented |
| Question boxes with options | Implemented |
| Plan mode indicators (◆/◇) | Implemented |
| Team event styling | Implemented |
| Token/cost formatters | Implemented |

### Not Applicable

These don't apply to Galacta's headless daemon architecture:

| Command | Reason |
|---------|--------|
| `/login` `/logout` | Galacta uses API keys directly |
| `/config` `/settings` | No TUI settings editor |
| `/hooks` | Not implemented (different extension model) |
| `/keybindings` `/terminal-setup` | Jeff is a simple CLI, not a TUI |
| `/statusline` `/theme` `/vim` | No TUI chrome |
| `/sandbox` | Galacta runs tools in-process |
| `/output-style` | SSE event stream, not configurable output styles |
| `/doctor` | Different install model |
| `/release-notes` `/feedback` `/bug` `/upgrade` | Product lifecycle — later |

## CLI Flags (Jeff)

### Implemented

| Flag | Notes |
|------|-------|
| `--session, -s` | Resume existing session by UUID |
| `--model, -m` | Model override |
| `--dir, -d` | Working directory |
| `--mode` | Permission mode (default, acceptEdits, bypassPermissions, plan, dontAsk) |
| `--galacta` | Daemon URL (default: localhost:9090) |
| `--system-prompt` | Override/append system prompt |
| `--continue` | Resume most recent session |
| `--output-format` | `stream` (default), `json`, `text` |
| `--effort` | Reasoning effort (low/medium/high) |
| `--max-budget-usd` | Spending cap |
| `--tools` | Tool allow list (repeatable) |
| `--allowedTools` | Glob-pattern tool filtering (repeatable) |
| `--fallback-model` | Fallback on 529 overload |

### Not Applicable

| Flag | Reason |
|------|--------|
| `--worktree` | Worktree is a tool, not a CLI flag |
| `--tmux` | Tmux integration — out of scope |
| `--from-pr` | GitHub PR resume — depends on PR workflow |
| `--json-schema` | Structured output — niche |
| `--chrome` | Browser integration — out of scope |

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Daemon status |
| `POST` | `/sessions` | Create session |
| `GET` | `/sessions` | List sessions |
| `GET` | `/sessions/{id}` | Get session info |
| `PATCH` | `/sessions/{id}` | Update session |
| `DELETE` | `/sessions/{id}` | Delete session |
| `POST` | `/sessions/{id}/message` | Send message (SSE) |
| `GET` | `/sessions/{id}/messages` | Message history |
| `POST` | `/sessions/{id}/compact` | Compact conversation |
| `GET` | `/sessions/{id}/tasks` | List tasks |
| `POST` | `/sessions/{id}/permission/{rid}` | Permission response |
| `POST` | `/sessions/{id}/question/{rid}` | Question response |
| `GET` | `/skills?working_dir=...` | List skills |

## Version Tracking

| Component | Version | Date |
|-----------|---------|------|
| Claude Code CLI | 2.1.63 | 2026-03-01 |
| Galacta (daemon) | 0.1.0 (pre-release) | 2026-03-01 |
| Jeff (CLI) | 0.1.0 (pre-release) | 2026-03-01 |
| Anthropic API | 2023-06-01 | (API version header) |
| Go | 1.24+ | Runtime |
