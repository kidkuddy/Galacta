# Galacta/Jeff vs Claude Code: Compatibility Matrix

**Baseline:** Claude Code CLI v2.1.63
**Date:** 2026-03-01

> **Galacta** = daemon (handles conversations, tool execution, agent loop)
> **Jeff** = CLI client (interacts with Galacta via HTTP/SSE API)

---

## Tools

Galacta implements its own tool definitions (prefixed `galacta_*`) rather than using Claude Code's tool names and prompts. Claude Code's tool system includes detailed system prompts with usage instructions baked into each tool description — Galacta does not replicate any of this.

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

### NOT Implemented

| Claude Code Tool | Status | Priority | Notes |
|-----------------|--------|----------|-------|
| WebSearch | Missing | Medium | Web search via API. Galacta only has fetch, not search |
| NotebookEdit | Missing | Low | Jupyter notebook cell editing. Niche use case |
| Agent | Missing | **High** | Sub-agent spawning. Core to Claude Code's delegation model |
| TaskCreate/Get/Update/List | Missing | Medium | Task tracking for multi-step work |
| TeamCreate/Delete | Missing | Low | Multi-agent team coordination |
| SendMessage | Missing | Low | Inter-agent messaging |
| EnterPlanMode/ExitPlanMode | Missing | Medium | Plan-then-execute workflow |
| AskUserQuestion | Missing | Medium | Structured user prompts with options |
| EnterWorktree | Missing | Low | Git worktree isolation |
| Skill | Missing | Low | Slash command / skill invocation |

### Key Gap: Agent Tool

The Agent tool is Claude Code's most architecturally significant capability — it spawns sub-agents with their own tool access, context, and specializations (Explore, Plan, general-purpose). Galacta has no equivalent. This means Galacta sessions can't:
- Delegate research to a background agent
- Run parallel investigations
- Use specialized agent types (code explorer, planner)

## System Prompt

**Claude Code** injects a substantial default system prompt that includes:
- Detailed per-tool usage instructions (when to use Read vs Bash, Edit vs Write, etc.)
- Git commit conventions and safety protocols
- PR creation workflow
- Code style guidelines (no over-engineering, no sycophancy, etc.)
- Environment context (OS, shell, git status, model info)
- CLAUDE.md file contents (project-specific instructions)
- Tone and formatting rules

**Galacta** now builds a default system prompt via the `systemprompt` package:
- [x] Default system prompt with tool usage guidance
- [x] CLAUDE.md discovery and injection
- [x] Environment context injection (OS, shell, git status, model)
- [x] Safety rails for destructive operations
- [x] Git/PR conventions

User-provided system prompts (via `--system-prompt` flag or session creation) are appended to the built default.

## Slash Commands

### Currently Implemented in Jeff

| Command | Notes |
|---------|-------|
| `/quit` `/exit` | End session |
| `/history` | Show conversation history |
| `/session` | Show session info |
| `/clear` | Clear screen (visual only, not conversation) |

### Priority 1 — Must Have

Core session and context management. These directly impact usability.

| Command | Description | Implementation |
|---------|-------------|----------------|
| `/compact [instructions]` | Compact conversation with optional focus | Galacta API — summarize + truncate history |
| `/cost` | Show token usage | Jeff client — aggregate from `usage` events |
| `/context` | Visualize context window usage | Jeff client — show tokens used vs limit |
| `/model [model]` | Change model mid-session | Galacta API — update session metadata |
| `/diff` | Show uncommitted git changes | Jeff client — run `git diff` locally |
| `/help` | Show available commands | Jeff client — local |
| `/status` | Version, model, session info | Jeff client — combine local + session info |

### Priority 2 — Should Have

Workflow features that make the tool competitive for real development use.

| Command | Description | Implementation |
|---------|-------------|----------------|
| `/new` `/reset` | Start fresh conversation (keep session) | Galacta API — clear history |
| `/resume` `/continue` | Resume previous session | Jeff client — already has `-s` flag, add as slash cmd |
| `/rename [name]` | Rename current session | Galacta API — update session name |
| `/export [filename]` | Export conversation as text/markdown | Jeff client — format history to file |
| `/copy` | Copy last response to clipboard | Jeff client — local |
| `/plan` | Enter plan mode | Galacta API — switch permission mode to `plan` |
| `/mode [mode]` | Switch permission mode | Galacta API — update session mode |
| `/pr-comments [PR]` | Fetch GitHub PR comments | Jeff client — `gh` CLI wrapper |

### Priority 3 — Nice to Have

Power-user features. Not blocking for CC parity.

| Command | Description | Implementation |
|---------|-------------|----------------|
| `/fork [name]` | Fork conversation at current point | Galacta API — clone session with history |
| `/rewind` `/checkpoint` | Rewind to previous turn | Galacta API — truncate history |
| `/review` | Review PR for quality/security | Jeff client — `gh` wrapper + prompt |
| `/security-review` | Analyze changes for vulnerabilities | Jeff client — prompt-based |
| `/fast [on\|off]` | Toggle fast mode | Jeff client — model switch shorthand |
| `/stats` | Usage stats, session history | Jeff client — aggregate from DB |
| `/mcp` | Manage MCP servers | Jeff client — config management |
| `/add-dir <path>` | Add working directory | Galacta API — multi-root support |

### Not Applicable

These don't apply to Galacta's headless daemon architecture:

| Command | Reason |
|---------|--------|
| `/login` `/logout` | Galacta uses API keys directly |
| `/config` `/settings` | No TUI settings editor |
| `/permissions` `/allowed-tools` | Managed via `--mode` flag |
| `/hooks` | Not implemented (different extension model) |
| `/keybindings` `/terminal-setup` | Jeff is a simple CLI, not a TUI |
| `/statusline` `/theme` `/vim` | No TUI chrome |
| `/sandbox` | Galacta runs tools in-process, sandboxing is different |
| `/output-style` | SSE event stream, not configurable output styles |
| `/plugin` `/skills` `/agents` | No plugin/skill system |
| `/init` `/memory` | Depends on CLAUDE.md integration (see System Prompt) |
| `/doctor` | Different install model |
| `/release-notes` `/feedback` `/bug` `/upgrade` | Product lifecycle — later |

## CLI Flags (Jeff)

### Currently Implemented

| Flag | Notes |
|------|-------|
| `--session, -s` | Resume existing session by UUID |
| `--model, -m` | Model override at session creation |
| `--dir, -d` | Working directory |
| `--mode` | Permission mode (default, acceptEdits, bypassPermissions, plan, dontAsk) |
| `--galacta` | Daemon URL (default: localhost:9090) |
| `--name` | Session name (on `session create`) |
| `--id` | Custom session ID (on `session create`) |

### Implemented (New)

| Flag | CC Equivalent | Notes |
|------|--------------|-------|
| `--effort` | `--effort` | Reasoning effort (low/medium/high). Maps to thinking budget_tokens |
| `--system-prompt` | (built-in) | Override/append to default system prompt from CLI |
| `--max-budget-usd` | `--max-budget-usd` | Spending cap. Tracked via usage totals per session |
| `--continue` | `--continue` | Resume most recent session (by updated_at) |
| `--output-format` | `--output-format` | `stream` (default SSE), `json` (raw events), `text` (text only) |
| `--tools` | `--tools` | Tool allow list |
| `--allowedTools` | `--allowedTools` | Glob-pattern tool filtering |
| `--fallback-model` | `--fallback-model` | Auto-fallback on 529 overload |

### Not Applicable

| Flag | Reason |
|------|--------|
| `--worktree` | Git worktree — later, depends on EnterWorktree tool |
| `--tmux` | Tmux integration — out of scope |
| `--from-pr` | GitHub PR resume — depends on PR workflow |
| `--json-schema` | Structured output — niche |
| `--chrome` | Browser integration — out of scope |

## Version Tracking

| Component | Version | Date |
|-----------|---------|------|
| Claude Code CLI | 2.1.63 | 2026-03-01 |
| Galacta (daemon) | 0.1.0 (pre-release) | 2026-03-01 |
| Jeff (CLI) | 0.1.0 (pre-release) | 2026-03-01 |
| Anthropic API | 2023-06-01 | (API version header) |
| Go | 1.24+ | Runtime |
