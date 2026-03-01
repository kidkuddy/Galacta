# Herald vs Claude Code: Compatibility Matrix

**Baseline:** Claude Code CLI v2.1.63
**Date:** 2026-03-01

---

## Tools

Herald implements its own tool definitions (prefixed `herald_*`) rather than using Claude Code's tool names and prompts. Claude Code's tool system includes detailed system prompts with usage instructions baked into each tool description — Herald does not replicate any of this.

### Implemented

| Claude Code Tool | Herald Equivalent | Notes |
|-----------------|-------------------|-------|
| Read | `herald_read` | Line numbers, offset/limit support |
| Write | `herald_write` | Creates parent dirs |
| Edit | `herald_edit` | old_string/new_string with replace_all |
| Glob | `herald_glob` | Pattern matching, newest-first |
| Grep | `herald_grep` | Regex search, context lines, output modes |
| Bash | `herald_bash` | Timeout support (120s default, 600s max) |
| WebFetch | `herald_web_fetch` | URL fetch with max_bytes, strips HTML |

Herald also has `herald_ls` (directory listing) which has no direct Claude Code equivalent — Claude Code uses Bash for `ls`.

### NOT Implemented

| Claude Code Tool | Status | Priority | Notes |
|-----------------|--------|----------|-------|
| WebSearch | Missing | Medium | Web search via API. Herald only has fetch, not search |
| NotebookEdit | Missing | Low | Jupyter notebook cell editing. Niche use case |
| Agent | Missing | High | Sub-agent spawning. Core to Claude Code's delegation model |
| TaskCreate/Get/Update/List | Missing | Medium | Task tracking for multi-step work |
| TeamCreate/Delete | Missing | Low | Multi-agent team coordination |
| SendMessage | Missing | Low | Inter-agent messaging |
| EnterPlanMode/ExitPlanMode | Missing | Medium | Plan-then-execute workflow |
| AskUserQuestion | Missing | Medium | Structured user prompts with options |
| EnterWorktree | Missing | Low | Git worktree isolation |
| Skill | Missing | Low | Slash command / skill invocation |

### Key Gap: Agent Tool

The Agent tool is Claude Code's most architecturally significant capability — it spawns sub-agents with their own tool access, context, and specializations (Explore, Plan, general-purpose). Herald has no equivalent. This means Herald sessions can't:
- Delegate research to a background agent
- Run parallel investigations
- Use specialized agent types (code explorer, planner)

## System Prompts

**Claude Code** injects a substantial system prompt that includes:
- Detailed per-tool usage instructions (when to use Read vs Bash, Edit vs Write, etc.)
- Git commit conventions and safety protocols
- PR creation workflow
- Code style guidelines (no over-engineering, no sycophancy, etc.)
- Environment context (OS, shell, git status, model info)
- CLAUDE.md file contents (project-specific instructions)
- Tone and formatting rules

**Herald** sends no default system prompt. The `system_prompt` field is optional and user-provided per session. This means:
- The model gets no guidance on tool usage patterns
- No safety rails for destructive operations
- No conventions for git, PRs, or code style
- No CLAUDE.md integration

This is a significant behavioral difference. Claude Code's system prompt is what makes the model reliably use the right tool for the job (e.g., "use Read instead of cat", "use Edit instead of sed"). Without it, the model may make suboptimal tool choices or miss safety checks.

## Slash Commands

Claude Code supports ~55 built-in slash commands. Herald has none. These fall into several categories:

### Session Management
| Command | Description |
|---------|-------------|
| `/clear` `/reset` `/new` | Clear conversation history |
| `/compact [instructions]` | Compact conversation with optional focus |
| `/resume` `/continue` | Resume previous conversation |
| `/fork [name]` | Fork conversation at current point |
| `/rename [name]` | Rename current session |
| `/rewind` `/checkpoint` | Rewind to previous point |
| `/export [filename]` | Export conversation as text |
| `/copy` | Copy last response to clipboard |

### Context & Usage
| Command | Description |
|---------|-------------|
| `/context` | Visualize context usage |
| `/cost` | Show token usage |
| `/usage` | Plan usage limits and rate limits |
| `/stats` | Daily usage, session history, streaks |
| `/model [model]` | Change AI model |
| `/fast [on\|off]` | Toggle fast mode |

### Development Workflow
| Command | Description |
|---------|-------------|
| `/diff` | Interactive diff viewer for uncommitted changes |
| `/pr-comments [PR]` | Fetch GitHub PR comments |
| `/review` | Review PR for quality and security |
| `/security-review` | Analyze changes for vulnerabilities |
| `/plan` | Enter plan mode |
| `/init` | Initialize CLAUDE.md |
| `/memory` | Edit CLAUDE.md files |

### Configuration
| Command | Description |
|---------|-------------|
| `/config` `/settings` | Open settings |
| `/permissions` `/allowed-tools` | View/update permissions |
| `/hooks` | Manage hook configurations |
| `/keybindings` | Keybindings config |
| `/terminal-setup` | Terminal keybindings |
| `/statusline` | Configure status line |
| `/theme` | Change color theme |
| `/vim` | Toggle vim mode |
| `/sandbox` | Toggle sandbox mode |
| `/output-style [style]` | Switch output styles |

### Extensions
| Command | Description |
|---------|-------------|
| `/mcp` | Manage MCP servers |
| `/plugin` | Manage plugins |
| `/skills` | List available skills |
| `/agents` | Manage agent configurations |
| `/add-dir <path>` | Add working directory |

### Account & System
| Command | Description |
|---------|-------------|
| `/login` `/logout` | Auth management |
| `/doctor` | Diagnose installation |
| `/help` | Show help |
| `/status` | Version, model, account info |
| `/release-notes` | View changelog |
| `/feedback` `/bug` | Submit feedback |
| `/upgrade` | Upgrade plan |
| `/exit` `/quit` | Exit CLI |

### Which Matter for Herald

Not all of these are relevant. Herald's daemon architecture means some are handled differently:

**Should implement (high value):**
- Session management (clear, compact, resume) — partially exists via API
- Context visualization — useful for monitoring token usage
- Model switching — per-session model override

**Could implement (medium value):**
- Diff viewer — useful but can be done via tools
- Plan mode — structured workflow

**Not applicable:**
- Account management (login/logout) — Herald uses API keys directly
- IDE integrations, plugins, themes — Herald is headless
- GitHub app installs, Slack integration — out of scope

## CLI Flags

Claude Code supports several flags that Herald doesn't:

| Flag | Herald Status | Notes |
|------|--------------|-------|
| `--model` | Partial | Set at session creation, not per-message |
| `--effort` | Missing | Reasoning effort level (low/medium/high) |
| `--permission-mode` | Implemented | Same modes: default, acceptEdits, bypassPermissions, plan, dontAsk |
| `--worktree` | Missing | Git worktree auto-creation |
| `--tmux` | Missing | Tmux session for worktree |
| `--from-pr` | Missing | Resume from GitHub PR |
| `--json-schema` | Missing | Structured output validation |
| `--output-format` | Partial | Herald streams SSE, no json/text toggle |
| `--max-budget-usd` | Missing | Spending cap |
| `--continue` | Partial | Session resume via `-s` flag |
| `--mcp-config` | Implemented | External MCP server config |
| `--tools` | Missing | Tool allow/deny lists |
| `--allowedTools` | Missing | Glob-pattern tool filtering |
| `--fallback-model` | Missing | Auto-fallback on overload |
| `--chrome` | Missing | Browser integration |

## Version Tracking

| Component | Version | Date |
|-----------|---------|------|
| Claude Code CLI | 2.1.63 | 2026-03-01 |
| Herald | 0.1.0 (pre-release) | 2026-03-01 |
| Anthropic API | 2023-06-01 | (API version header) |
| Go | 1.24+ | Herald runtime |
