# galacta vs Claude Code -- Feature Parity Analysis

_Audited: 2026-03-06_

## Overall Parity Score: ~72%

Galacta covers the core agent loop, tool system, permission model, and session management well. The main gaps are in developer UX polish (markdown rendering, image/PDF support, hooks) and ecosystem features (auto-updater, init command, IDE integrations).

---

## Feature Parity Matrix

### Legend

| Symbol | Meaning |
|--------|---------|
| **Y** | Fully implemented, feature-complete |
| **P** | Partially implemented, core works but gaps exist |
| **N** | Not implemented |

---

### Core Agent Loop

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| Multi-turn conversation loop | Y | Y | **Y** | `agent/loop.go` — iterate until end_turn |
| Streaming responses (SSE) | Y | Y | **Y** | `anthropic/stream.go` + `events/emitter.go` |
| Extended thinking | Y | Y | **Y** | `anthropic/types.go:9-12`, effort levels: low/medium/high |
| Auto-compact on context pressure | Y | Y | **Y** | `agent/compact.go` — mirrors Claude Code's LFA (13k) and wl7 (20k) constants |
| Budget enforcement (max spend) | Y | Y | **Y** | `agent/loop.go` — `maxBudgetUSD` check per turn |
| Fallback model on overload (529) | Y | Y | **Y** | `agent/loop.go` — retries with `fallback_model` |
| Sub-agent spawning | Y | Y | **Y** | `tools/agent/server.go` — fresh history, filtered tools, no DB persistence |
| Turn limit enforcement | Y | Y | **Y** | `agent/loop.go` — `defaultMaxTurns=100` |
| Context window awareness | Y | Y | **Y** | `agent/pricing.go` — per-model context windows |

**Parity: 9/9 (100%)**

---

### Tool System

| Tool | Claude Code | Galacta | Status | Notes |
|------|-------------|---------|--------|-------|
| Read (file) | Y | Y | **Y** | `galacta_read` — offset/limit, line numbers, 2000 line default |
| Write (file) | Y | Y | **Y** | `galacta_write` — create or overwrite |
| Edit (file) | Y | Y | **Y** | `galacta_edit` — diff-based line edits |
| Glob (file search) | Y | Y | **Y** | `galacta_glob` — pattern matching, mtime sorted |
| Grep (content search) | Y | Y | **Y** | `galacta_grep` — regex, context lines, file type filters |
| Bash (command exec) | Y | Y | **Y** | `galacta_bash` — timeout 120s default, max 600s |
| Agent (sub-agent) | Y | Y | **Y** | `galacta_agent` — with subagent_type, team support |
| AskUserQuestion | Y | Y | **Y** | `galacta_ask_user` — with options support |
| WebFetch | Y | Y | **Y** | `galacta_web_fetch` — HTML stripping |
| Skill | Y | Y | **Y** | `galacta_skill` + `galacta_register_skill` |
| EnterPlanMode / ExitPlanMode | Y | Y | **Y** | `galacta_enter_plan_mode` / `galacta_exit_plan_mode` |
| EnterWorktree | Y | Y | **Y** | `galacta_enter_worktree` — git worktree isolation |
| TaskCreate/Get/Update/List | Y | Y | **Y** | `galacta_task_*` — full CRUD with status + relationships |
| TeamCreate / TeamDelete | Y | Y | **Y** | `galacta_team_create` / `galacta_team_delete` |
| SendMessage | Y | Y | **Y** | `galacta_send_message` — message + broadcast |
| WebSearch | Y | N | **N** | Removed in commit `f7128aa` |
| NotebookEdit | Y | N | **N** | No Jupyter notebook support |
| LSP (language server) | Y | N | **N** | No language server protocol integration |
| TodoRead / TodoWrite | Y | N | **N** | Tasks exist but no todo-list equivalent |

**Parity: 15/19 tools (79%)**

---

### Permission System

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| default mode | Y | Y | **Y** | Read auto-allow, others ask |
| acceptEdits mode | Y | Y | **Y** | In-cwd writes auto-allow |
| bypassPermissions mode | Y | Y | **Y** | All auto-allow |
| plan mode | Y | Y | **Y** | Read-only, writes denied |
| dontAsk mode | Y | Y | **Y** | All auto-allow, silent |
| Interactive approval (Ask) | Y | Y | **Y** | `permissions/gate.go` — channel-based wait |
| Path validation (cwd check) | Y | Y | **Y** | `permissions/modes.go` — filepath.Abs resolution |
| Live mode switching | Y | Y | **Y** | `ModeGate.SetMode()` — atomic store |
| Tool categorization | Y | Y | **P** | Hardcoded string patterns vs Claude Code's rule engine |
| Hooks (pre/post tool) | Y | N | **N** | No hook system |

**Parity: 8/10 (80%)**

---

### Session Management

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| Create session | Y | Y | **Y** | POST `/sessions` |
| Resume session | Y | Y | **Y** | `--session` or `--continue` flag |
| List sessions | Y | Y | **Y** | GET `/sessions` with `working_dir` filter |
| Delete session | Y | Y | **Y** | DELETE `/sessions/{id}` |
| Archive session | Y | Y | **Y** | POST `/sessions/{id}/archive` |
| Session history | Y | Y | **Y** | SQLite persistence, full replay |
| Manual compact | Y | Y | **Y** | POST `/sessions/{id}/compact` + `/compact` command |
| Clear history | Y | Y | **Y** | POST `/sessions/{id}/clear` + `/clear` command |
| Per-session model | Y | Y | **Y** | Stored in metadata |
| Per-session permission mode | Y | Y | **Y** | Stored in metadata, live update |
| Conversation forking | Y | N | **N** | No fork/branch support |

**Parity: 10/11 (91%)**

---

### System Prompt & Context

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| CLAUDE.md loading | Y | Y | **Y** | Global + ancestor walk + project-level |
| Environment detection | Y | Y | **Y** | OS, shell, git repo, branch, recent commits |
| Git context injection | Y | Y | **Y** | Branch, status, recent commits |
| Tool list injection | Y | Y | **Y** | Available tools listed in prompt |
| Template-based prompt | Y | Y | **Y** | `systemprompt/prompt.tmpl` |
| User override (system_prompt) | Y | Y | **Y** | Appended as "# User Instructions" |
| Framework detection | Y | Y | **P** | Basic detection (Rails, Django, etc.) vs Claude Code's deeper analysis |
| Memory system (auto-memory) | Y | N | **N** | No persistent memory across sessions |

**Parity: 6.5/8 (81%)**

---

### CLI / UX

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| Interactive REPL | Y | Y | **Y** | readline-based with history |
| Multiline input | Y | Y | **Y** | Triple-quote (`"""`) toggle |
| `/help` | Y | Y | **Y** | Available commands listing |
| `/compact` | Y | Y | **Y** | Manual compaction with optional threshold |
| `/clear` | Y | Y | **Y** | Clear message history |
| `/cost` | Y | Y | **Y** | Accumulated cost display |
| `/model` | Y | Y | **Y** | Show/change model |
| `/permissions` | Y | Y | **Y** | Show/change permission mode |
| `/history` | Y | Y | **Y** | Show message history |
| `/tasks` | Y | Y | **Y** | List tasks |
| `/skills` | Y | Y | **Y** | List available skills |
| `/plan` | Y | Y | **Y** | Plan mode status |
| `/quit`, `/exit` | Y | Y | **Y** | Exit session |
| `/session` | Y | Y | **Y** | Session info |
| `/usage` | Y | Y | **Y** | Token usage stats |
| Spinner / progress | Y | Y | **Y** | Braille spinner with messages |
| Output formats | Y | Y | **Y** | stream, json, text |
| `--continue` (resume last) | Y | Y | **Y** | Resumes most recent session |
| Markdown rendering | Y | P | **P** | Bold/dim/color only — no syntax highlighting, no code block rendering |
| Image display | Y | N | **N** | No image file reading/display |
| PDF reading | Y | N | **N** | No PDF parsing |
| Jupyter rendering | Y | N | **N** | No .ipynb display |
| `/init` command | Y | N | **N** | No CLAUDE.md scaffolding |
| `/bug` feedback | Y | N | **N** | No feedback mechanism |
| `/login` / `/logout` | Y | N | **N** | Auth managed externally (env var / keychain) |
| Keyboard shortcut config | Y | N | **N** | No keybindings.json |
| Status line (bottom bar) | Y | N | **N** | No persistent status bar |
| Vim/Emacs mode toggle | Y | N | **N** | No input mode switching |

**Parity: 17/28 (61%)**

---

### Infrastructure & API

| Feature | Claude Code | Galacta | Status | Notes |
|---------|-------------|---------|--------|-------|
| Anthropic Messages API | Y | Y | **Y** | Streaming + non-streaming |
| OAuth token refresh | Y | Y | **Y** | Keychain-based, 401 retry |
| API key from env var | Y | Y | **Y** | `ANTHROPIC_API_KEY` |
| External MCP servers | Y | Y | **Y** | SSE-based MCP client |
| Rate limit tracking | Y | Y | **Y** | Header parsing, display |
| Per-model pricing | Y | Y | **Y** | Sonnet, Opus, Haiku |
| Server-side tools (thinking) | Y | Y | **Y** | ThinkingConfig with budget |
| Caching (prompt caching) | Y | P | **P** | Cache tokens tracked but no explicit cache control headers sent |
| Auto-updater | Y | N | **N** | No self-update mechanism |
| Telemetry / analytics | Y | N | **N** | No usage reporting |

**Parity: 7.5/10 (75%)**

---

### Architecture (Daemon vs Inline)

| Aspect | Claude Code | Galacta | Notes |
|--------|-------------|---------|-------|
| Execution model | Single process (Node.js) | Client-server (daemon + CLI) | Galacta's architecture is fundamentally different — more scalable, allows multiple CLI clients |
| Language | TypeScript | Go | Go gives better concurrency, lower memory, faster startup |
| State persistence | File-based JSON | SQLite per-session | SQLite is more robust for concurrent access |
| Tool execution | In-process | In-process MCP servers | Same effective model, MCP adds extensibility |
| Streaming | Direct stdout | HTTP SSE | Galacta's SSE is network-transparent — supports remote daemon |

---

### Hooks System (Claude Code feature — fully missing)

| Hook | Claude Code | Galacta | Status |
|------|-------------|---------|--------|
| PreToolUse | Y | N | **N** |
| PostToolUse | Y | N | **N** |
| Notification | Y | N | **N** |
| Stop | Y | N | **N** |
| UserPromptSubmit | Y | N | **N** |
| Custom shell commands | Y | N | **N** |

**Parity: 0/6 (0%)**

---

## Summary Scorecard

| Category | Implemented | Total | Parity |
|----------|------------|-------|--------|
| Core Agent Loop | 9 | 9 | **100%** |
| Session Management | 10 | 11 | **91%** |
| Permission System | 8 | 10 | **80%** |
| System Prompt & Context | 6.5 | 8 | **81%** |
| Tool System | 15 | 19 | **79%** |
| Infrastructure & API | 7.5 | 10 | **75%** |
| CLI / UX | 17 | 28 | **61%** |
| Hooks System | 0 | 6 | **0%** |
| **Overall** | **73** | **101** | **~72%** |

---

## Gap Analysis: What's Missing (Priority Order)

### High Impact Gaps (would significantly close the gap)

| # | Feature | Effort | Impact | Notes |
|---|---------|--------|--------|-------|
| 1 | **Hooks system** | L | High | Pre/post tool execution hooks enable user automation, linting, CI integration. Core differentiator for power users. |
| 2 | **Markdown rendering** | M | High | Syntax-highlighted code blocks, proper heading rendering, link formatting. Terminal UX gap is very visible. |
| 3 | **Memory system** | M | High | Auto-memory for persistent preferences across sessions. Stored in `~/.claude/projects/*/memory/`. |
| 4 | **Status line** | M | Medium | Persistent bottom bar showing model, mode, cost, tokens. Standard in modern CLI tools. |

### Medium Impact Gaps

| # | Feature | Effort | Impact | Notes |
|---|---------|--------|--------|-------|
| 5 | **LSP integration** | L | Medium | Language server for go-to-definition, symbol search. Useful but not critical. |
| 6 | **NotebookEdit** | M | Medium | Jupyter notebook cell editing. Niche but expected. |
| 7 | **Image/PDF reading** | M | Medium | Multimodal support. Pass images to API, parse PDFs. |
| 8 | **`/init` command** | S | Medium | Scaffold CLAUDE.md in project. Easy win. |
| 9 | **WebSearch tool** | S | Medium | Was implemented, then removed. Re-add with API tool. |
| 10 | **Prompt caching** | S | Medium | Send cache control headers to reduce costs. |

### Low Impact Gaps (nice-to-have)

| # | Feature | Effort | Impact | Notes |
|---|---------|--------|--------|-------|
| 11 | Auto-updater | M | Low | Self-update from GitHub releases |
| 12 | `/login` / `/logout` | S | Low | Auth UX (currently relies on env var or Claude Code keychain) |
| 13 | Keybindings config | S | Low | `~/.claude/keybindings.json` |
| 14 | Conversation forking | M | Low | Branch sessions — power user feature |
| 15 | Telemetry | M | Low | Usage analytics — enterprise feature |
| 16 | Vim/Emacs mode | S | Low | readline already supports this with config |
| 17 | `/bug` feedback | S | Low | Open GitHub issue from CLI |

---

## Architectural Advantages Over Claude Code

Galacta's daemon architecture provides capabilities Claude Code doesn't have:

1. **Multi-client sessions** — Multiple CLI instances can observe the same session via SSE. Claude Code is single-process.
2. **Remote daemon** — Jeff CLI can connect to a daemon on another machine. Claude Code is local-only.
3. **Team/multi-agent orchestration** — First-class message bus for agent coordination. Claude Code uses sub-agents but no message bus.
4. **Language performance** — Go's goroutine model handles concurrent tool execution more efficiently than Node.js.
5. **SQLite persistence** — More robust than file-based JSON for session state, with proper transactions and migrations.
6. **Decoupled UI** — Any HTTP client can be a frontend. Claude Code is tightly coupled to its terminal UI.

---

## Recommended Implementation Roadmap

To reach **~85% parity** (the practical ceiling before diminishing returns):

### Phase 1: Quick Wins (1-2 days each) → +5%
- `/init` command (scaffold CLAUDE.md)
- Prompt caching headers
- Re-add WebSearch as API server tool

### Phase 2: Core UX (3-5 days each) → +8%
- Markdown rendering (glamour or termenv library)
- Status line (persistent bottom bar)
- Image support (pass to API as base64)

### Phase 3: Power Features (1-2 weeks each) → +5%
- Hooks system (PreToolUse, PostToolUse, Notification)
- Memory system (auto-memory directory per project)

**Total projected parity after roadmap: ~90%**
