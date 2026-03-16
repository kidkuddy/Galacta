# galacta -- Project Map

_Audited: 2026-03-06 | Mode: standard_

## Purpose

Galacta is a Go-based AI coding assistant daemon that orchestrates Claude model interactions through a tool-equipped agent loop. It provides an HTTP API with SSE streaming for real-time feedback, per-session SQLite persistence, and an extensible MCP (Model Context Protocol) tool system. The companion CLI "Jeff" provides an interactive terminal interface.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.23 |
| HTTP Router | chi/v5 |
| Database | SQLite (modernc.org/sqlite, WAL mode) |
| Tool Protocol | MCP (mark3labs/mcp-go v0.28.0) |
| Streaming | Server-Sent Events (SSE) |
| CLI Input | readline |
| Build | Makefile + `go build` |
| Tests | None |
| CI/CD | None |

## Architecture

**Pattern**: Layered + Event-Driven Hybrid

```
                        ┌─────────────┐
                        │  Jeff CLI    │ (cmd/jeff)
                        │  (consumer)  │
                        └──────┬───────┘
                               │ HTTP + SSE
                        ┌──────▼───────┐
                        │  HTTP API    │ (api/)
                        │  + Handler   │
                        └──────┬───────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
      ┌───────▼──────┐ ┌──────▼──────┐ ┌───────▼──────┐
      │ Agent Loop   │ │ Permissions │ │ Events       │
      │ (agent/)     │ │ (perms/)    │ │ (events/)    │
      └───────┬──────┘ └─────────────┘ └──────────────┘
              │
      ┌───────▼──────┐
      │ Tool Caller  │ (toolcaller/)
      └───────┬──────┘
              │
    ┌─────────┼──────────┐
    │         │          │
┌───▼───┐ ┌──▼───┐ ┌────▼────┐
│Built-in│ │Extern│ │ Team    │
│ Tools  │ │ MCP  │ │(team/)  │
│(tools/)│ │Servers│ └─────────┘
└────────┘ └──────┘
```

## Scopes

| Scope | Path | Type | Entry Point | Tech Stack | State |
|-------|------|------|-------------|------------|-------|
| Galacta Daemon | `/` | HTTP daemon | `cmd/galacta/main.go` | Go, chi, SQLite, mcp-go | Ready |
| Jeff CLI | `cmd/jeff/` | Terminal CLI | `cmd/jeff/main.go` | Go, readline | Ready |
| Benchmark Suite | `bench/prompteval/` | Standalone tool | `bench/prompteval/main.go` | Go | Building |

## Contracts

### Cross-Scope Contracts

| Contract | Defined In | Used By | Stability |
|----------|-----------|---------|-----------|
| `agent.Session` | `agent/session.go` | Daemon (API responses), Jeff CLI (session display) | Stable |
| `anthropic.Message` | `anthropic/types.go` | Daemon (core loop), Jeff CLI (display), Benchmark | Stable |
| `anthropic.Tool` | `anthropic/types.go` | Daemon (tool registry), Benchmark (judgment) | Stable |
| `events.Event` variants | `events/types.go` | Daemon (SSE streaming), Jeff CLI (parsing) | Stable |
| `db.MessageRow`, `db.UsageTotals` | `db/models.go` | Daemon (session persistence) | Stable |

### External Contracts -- Galacta Daemon

| Contract | Direction | Protocol | Schema/Shape | Status | Files |
|----------|-----------|----------|--------------|--------|-------|
| Health | Provides | GET `/health` | `{version, active_sessions, total_tools, status}` | Live | `api/server.go:26` |
| Create Session | Provides | POST `/sessions` | Req: `CreateSessionRequest`; Res: `agent.Session` | Live | `api/handler.go:115-194` |
| List Sessions | Provides | GET `/sessions` | Query: `working_dir?`, `include_archived?` | Live | `api/handler.go:198-241` |
| Get Session | Provides | GET `/sessions/{id}` | Res: session metadata | Live | `api/handler.go:244-250` |
| Delete Session | Provides | DELETE `/sessions/{id}` | Cancels + removes | Live | `api/handler.go` |
| Run Message (SSE) | Provides | POST `/sessions/{id}/message` | Req: `{message}`; Res: SSE stream | Live | `api/handler.go:295-497` |
| Permission Response | Provides | POST `/sessions/{id}/permission/{requestID}` | Req: `{approved: bool}` | Live | `api/handler.go:499+` |
| Question Response | Provides | POST `/sessions/{id}/question/{requestID}` | Req: `{answer: string}` | Live | `api/handler.go:499+` |
| List Messages | Provides | GET `/sessions/{id}/messages` | Res: `[]db.MessageRow` | Live | `api/handler.go` |
| Update Session | Provides | PATCH `/sessions/{id}` | Req: partial session updates | Live | `api/handler.go` |
| Compact Session | Provides | POST `/sessions/{id}/compact` | Triggers history compaction | Live | `api/handler.go` |
| Clear Session | Provides | POST `/sessions/{id}/clear` | Clears history | Live | `api/handler.go` |
| Archive Session | Provides | POST `/sessions/{id}/archive` | Archives session | Live | `api/handler.go` |
| List Tasks | Provides | GET `/sessions/{id}/tasks` | Res: task objects | Live | `api/handler.go` |
| List Skills | Provides | GET `/skills` | Res: skill list | Live | `api/handler.go` |
| Account Usage | Provides | GET `/usage` | Res: API usage info | Live | `api/handler.go` |
| Anthropic Messages API | Consumes | HTTPS POST | `/v1/messages` with streaming | Live | `anthropic/client.go` |
| macOS Keychain | Consumes | CLI exec | OAuth token from "Claude Code-credentials" | Live | `config.go:87-137` |
| External MCP Servers | Consumes | SSE + MCP | Dynamic tool discovery | Live | `galacta.go:46-59` |

### External Contracts -- Jeff CLI

| Contract | Direction | Protocol | Status | Files |
|----------|-----------|----------|--------|-------|
| Galacta HTTP API | Consumes | HTTP/REST + SSE | Live | `cmd/jeff/main.go:455-507` |
| Terminal I/O | Provides | stdin/stdout readline | Live | `cmd/jeff/main.go`, `cmd/jeff/ui.go` |

### Intra-Scope Contracts (Plugin Systems)

**MCP Tool Registry**: `toolcaller.Registry` maps tool names to MCP client refs + schemas. Consumers: `toolcaller.Caller`, agent loop, session handler.

**Permission Gate System**: `permissions.Gate` interface (Allow/Deny/Ask). Implementations: `ModeGate` (5 modes), `InteractiveGate` (Ask flow with channels), `BypassGate` (sub-agents).

**Event Bus**: `events.Emitter` -- buffered channel broadcaster. Event types: TextDelta, ThinkingDelta, ToolStart, ToolResult, PermissionRequest, QuestionRequest, UsageEvent, TurnComplete, ErrorEvent, SubAgentStart/End, TeamCreated/Deleted, TeamMessageEvent, PlanModeChanged.

**Team Message Bus**: `team.EventBus` -- in-memory pub/sub for inter-agent messaging. 64-buffer channels per agent.

## Shared Package Policy

| Package | Primary Owner | Consumed By | Change Policy |
|---------|---------------|-------------|---------------|
| `agent/` | Daemon | Handler, sub-agents | Core logic; breaking changes require handler+CLI updates |
| `anthropic/` | Daemon | Daemon, benchmark, agent loop | Wraps Anthropic API; versioned on API changes |
| `db/` | Daemon | Handler, agent loop | Schema changes require migrations |
| `events/` | Daemon | Handler (streaming), agent loop (emission) | Append-only preferred; schema changes affect CLI |
| `permissions/` | Daemon | Handler, agent loop, tool servers | Stable semantics |
| `systemprompt/` | Daemon | Agent loop | Prompt changes; no interface breaking |
| `team/` | Daemon | Agent loop, agent tool server | New; may change rapidly |
| `toolcaller/` | Daemon | Handler, agent loop | Stable registry interface |
| `tools/*` | Daemon | Handler (registration), agent loop (invocation) | MCP servers; schema changes affect agent behavior |

## Build Matrix

| Scope | Build Command | Test Command | Artifact | Deploy Target |
|-------|--------------|-------------|----------|---------------|
| Galacta Daemon | `make galacta` | N/A (no tests) | `bin/galacta` | Local PATH |
| Jeff CLI | `make jeff` | N/A (no tests) | `bin/jeff` | Local PATH |
| Benchmark | `go run ./bench/prompteval/` | N/A | N/A (direct run) | Local dev only |

---

## Bounded Contexts

### SHARED Contexts

#### Context 1: Daemon & API Infrastructure

Core daemon wiring, HTTP routing, session lifecycle management, and SSE streaming.

**Files**: `galacta.go`, `config.go`, `api/server.go`, `api/handler.go` (1000 lines)

##### Session Lifecycle
- **state**: ready
- **why**: Persistent conversation contexts across CLI runs
- **acceptance**:
  - [ ] when POST /sessions, then session DB created with migrations
  - [ ] when GET /sessions, then returns filtered list
  - [ ] when DELETE /sessions/{id}, then running session cancelled and DB removed
  - [ ] when resuming, then message history restored
- **depends-on**: Database, Agent Loop
- **files**: `api/handler.go:115-250`, `db/store.go`

##### Message Streaming (SSE)
- **state**: ready
- **why**: Real-time feedback during model execution
- **acceptance**:
  - [ ] when POST /sessions/{id}/message, then SSE stream of events
  - [ ] when client disconnects, then active run cancelled
  - [ ] when concurrent message to same session, then 409 Conflict
- **depends-on**: Agent Loop, Events Emitter
- **files**: `api/handler.go:295-497`

##### Daemon Bootstrap
- **state**: ready
- **why**: Initialize all subsystems with config
- **acceptance**:
  - [ ] when galacta.New(), then Anthropic client + tool registry + MCP servers connected
  - [ ] when Start(), then server listens and prints "READY"
- **files**: `galacta.go`, `config.go`, `cmd/galacta/main.go`

---

#### Context 2: Agent Loop & Execution

Core turn-based agent: message -> API call -> tool execution -> response loop.

**Files**: `agent/loop.go` (588 lines), `agent/session.go`, `agent/compact.go`, `agent/pricing.go`

##### Main Agent Loop
- **state**: ready
- **why**: Multi-turn Claude interactions with tool execution
- **acceptance**:
  - [ ] when Run() called, then iterate() sends to API, executes tools, repeats until end_turn
  - [ ] when max turns reached, then loop exits with turn limit message
  - [ ] when context cancelled, then loop exits cleanly
- **depends-on**: Anthropic Client, Tool Caller, Permissions, Events, Database
- **files**: `agent/loop.go:60-310`

##### Budget Enforcement
- **state**: ready
- **why**: Stop execution if cost exceeds limit
- **acceptance**:
  - [ ] when turn completes, then cost calculated from usage totals
  - [ ] when cost >= max_budget_usd, then loop exits with budget_exceeded
- **depends-on**: Database (GetUsageTotals), Pricing
- **files**: `agent/loop.go`, `agent/pricing.go`

##### Auto-Compaction
- **state**: ready
- **why**: Summarize conversation when approaching context window limit
- **acceptance**:
  - [ ] when input tokens exceed threshold, then compaction attempted
  - [ ] when compaction succeeds, then history replaced with summary + recent messages
- **depends-on**: Anthropic Client, Database
- **files**: `agent/compact.go`

##### Sub-Agent Spawning
- **state**: ready
- **why**: Run separate agent with fresh history and filtered tools
- **acceptance**:
  - [ ] when RunSubAgent() called, then new history with user prompt
  - [ ] when tools filtered by subagent_type, then only matching tools available
  - [ ] when loop exits, then final text returned (no DB save)
- **depends-on**: Tool Filter, Anthropic Client
- **files**: `agent/loop.go:117+`

##### Fallback Model on Overload
- **state**: ready
- **why**: Handle 529 (overloaded) by retrying with fallback
- **acceptance**:
  - [ ] when API returns 529 and fallback_model set, then retry with fallback
  - [ ] when no fallback, then 529 treated as normal error
- **files**: `agent/loop.go`

---

#### Context 3: Permissions & Interactive Control

Permission modes, interactive approval flows, plan mode restrictions.

**Files**: `permissions/modes.go`, `permissions/gate.go`, `permissions/bypass.go`

##### Permission Modes (5 modes)
- **state**: ready
- **why**: Restrict agent actions per user preference
- **acceptance**:
  - [ ] default: read-only auto-allow, others ask
  - [ ] acceptEdits: read-only + in-cwd writes auto-allow
  - [ ] plan: read-only auto-allow, all writes deny
  - [ ] bypassPermissions: all auto-allow
  - [ ] dontAsk: all auto-allow (silent)
- **files**: `permissions/modes.go`

##### Interactive Permission Gating
- **state**: ready
- **why**: Ask user when permission is indeterminate
- **acceptance**:
  - [ ] when Check returns Ask, then permission_request event emitted
  - [ ] when response received, then waiting goroutine unblocks
  - [ ] when context cancelled, then request cleaned up
- **depends-on**: Events Emitter
- **files**: `permissions/gate.go`
- **notes**: No timeout on pending requests; orphaned requests remain indefinitely

##### Live Permission Mode Updates
- **state**: ready
- **why**: Change mode during active session
- **acceptance**:
  - [ ] when PATCH /sessions/{id} with permission_mode, then atomic store updates
- **files**: `permissions/gate.go` (ModeGate.SetMode)

---

#### Context 4: Database & Persistence

Per-session SQLite databases with message history, usage, metadata, and tasks.

**Files**: `db/store.go`, `db/models.go`, `db/tasks.go`, `db/migrations/*.sql`

##### Session Database Lifecycle
- **state**: ready
- **why**: Persist state across CLI reconnections
- **acceptance**:
  - [ ] when Open(), then DB created with migrations applied
  - [ ] when WAL mode enabled, then concurrent reads allowed
  - [ ] when Close(), then file handle released
- **files**: `db/store.go`

##### Message History
- **state**: ready
- **why**: Persist messages with token counts for replay
- **acceptance**:
  - [ ] when SaveMessage(), then row inserted with JSON content
  - [ ] when LoadMessages(), then rows in creation order
- **files**: `db/store.go`, `db/models.go`

##### Usage Tracking
- **state**: ready
- **why**: Calculate costs and enforce budgets
- **acceptance**:
  - [ ] when GetUsageTotals(), then summed input/output/cache tokens returned
- **files**: `db/store.go`, `db/models.go`

##### Task Management
- **state**: ready
- **why**: Track work items within a session
- **acceptance**:
  - [ ] CRUD operations with status tracking (pending, in_progress, completed, blocked)
  - [ ] Relationships (blocks, blockedBy) stored as JSON arrays
- **files**: `db/tasks.go`

---

#### Context 5: Tool System & MCP

Tool registry, caller, and MCP server integration.

**Files**: `toolcaller/registry.go`, `toolcaller/caller.go`, `tools/*/server.go`

##### Tool Registry & Discovery
- **state**: ready
- **why**: Central catalog for model tool access
- **acceptance**:
  - [ ] when external MCP connected, then Discover() registers all tools
  - [ ] when Get(name), then ToolRef returned
  - [ ] when name collision, then later registration overwrites
- **files**: `toolcaller/registry.go`

##### Tool Call Dispatch
- **state**: ready
- **why**: Route calls to correct MCP client
- **acceptance**:
  - [ ] when CallMany(), then parallel execution with semaphore (maxWorkers)
  - [ ] when tool not found, then error result returned
  - [ ] when tool errors, then result.IsError set
- **files**: `toolcaller/caller.go`

##### Tool Filtering
- **state**: ready
- **why**: Control tool visibility per session
- **acceptance**:
  - [ ] when Allow list set, then only listed tools included
  - [ ] when Deny set, then matched tools excluded (takes precedence)
- **files**: `toolcaller/registry.go`

##### Built-in MCP Tool Servers

| Tool | File | Description |
|------|------|-------------|
| galacta_bash | `tools/exec/server.go` | Bash command execution (timeout 120s default, max 600s) |
| galacta_read/write/edit/glob/grep | `tools/fs/server.go` | Filesystem operations |
| galacta_agent | `tools/agent/server.go` | Sub-agent spawning |
| galacta_ask_user | `tools/ask/server.go` | User question flow |
| galacta_enter/exit_plan_mode | `tools/plan/server.go` | Plan mode toggle |
| galacta_task_* | `tools/task/server.go` | Task CRUD |
| galacta_team_create/delete | `tools/team/server.go` | Team management |
| galacta_send_message | `tools/message/server.go` | Team messaging |
| galacta_enter_worktree | `tools/worktree/server.go` | Git worktree creation |
| galacta_web_fetch | `tools/web/server.go` | HTTP fetching |
| galacta_skill/register_skill | `tools/skill/server.go` | Skill invocation |

##### User Skill System
- **state**: building
- **why**: Custom reusable prompt templates
- **acceptance**:
  - [ ] when skill at ~/.claude/skills/{name}.md, then loaded on startup
  - [ ] when /skill invoked, then template rendered with args
- **files**: `tools/skill/server.go`, `tools/skill/registry.go`, `tools/skill/builtin.go`

---

#### Context 6: Anthropic API Integration

HTTP client for Messages API with streaming, retry, and rate limit handling.

**Files**: `anthropic/client.go`, `anthropic/types.go`, `anthropic/stream.go`, `anthropic/marshal.go`

##### Messages API Client
- **state**: ready
- **why**: Send history and receive model responses
- **acceptance**:
  - [ ] when CreateMessage(), then POST to /v1/messages
  - [ ] when 529, then caller can retry with fallback model
- **files**: `anthropic/client.go`

##### Key Management & OAuth Refresh
- **state**: ready
- **why**: Support static API keys and expiring OAuth tokens
- **acceptance**:
  - [ ] when 401 returned, then keyFunc called to refresh
  - [ ] when keychain token set, then re-read on refresh
- **files**: `anthropic/client.go`, `config.go`

##### Streaming Response Parsing
- **state**: ready
- **why**: Parse SSE events for real-time feedback
- **acceptance**:
  - [ ] when CreateMessageStream(), then SSE connected
  - [ ] when events parsed, then content blocks accumulated
- **files**: `anthropic/stream.go`

---

#### Context 7: Events & Streaming

Event emission and SSE transport.

**Files**: `events/types.go`, `events/emitter.go`

##### Event Emission
- **state**: ready
- **why**: Publish structured events to SSE stream
- **acceptance**:
  - [ ] when Emit(), then JSON marshalled to channel
  - [ ] when channel full, then warning logged and event dropped
  - [ ] when Close(), then channel closed
- **files**: `events/emitter.go`
- **notes**: Buffer size 256; silent drops on overflow

---

#### Context 8: Team Collaboration (Emerging)

Multi-agent coordination via message bus.

**Files**: `team/types.go`, `team/manager.go`, `team/bus.go`, `tools/team/server.go`, `tools/message/server.go`

##### Team Creation & Management
- **state**: building
- **why**: Coordinate multiple agents on complex projects
- **acceptance**:
  - [ ] when galacta_team_create, then team directory + config.json created
  - [ ] when galacta_team_delete, then team directory removed
- **files**: `team/manager.go`, `tools/team/server.go`

##### Message Bus (In-Memory Pub/Sub)
- **state**: ready
- **why**: Route messages between agents
- **acceptance**:
  - [ ] when agent registers, then buffered channel created (size 64)
  - [ ] when send(), then routed to recipient or broadcast
  - [ ] when inbox full, then message dropped
- **files**: `team/bus.go`
- **notes**: No backpressure, no ack, no timeout

---

#### Context 9: System Prompt Construction

Dynamic prompt assembly from template, environment, and CLAUDE.md files.

**Files**: `systemprompt/builder.go`, `systemprompt/template.go`, `systemprompt/env.go`, `systemprompt/claudemd.go`

##### System Prompt Assembly
- **state**: ready
- **why**: Customize instructions based on working directory
- **acceptance**:
  - [ ] when Build(), then template rendered with env + CLAUDE.md + tools
  - [ ] when user override provided, then appended
- **files**: `systemprompt/builder.go`, `systemprompt/template.go`

##### Environment Context Discovery
- **state**: ready
- **why**: Give model context about language/frameworks
- **acceptance**:
  - [ ] when CollectEnv(), then detected frameworks + git info included
- **files**: `systemprompt/env.go`, `systemprompt/claudemd.go`

---

### SCOPE-LOCAL Contexts

#### Context 10: Jeff CLI

Interactive terminal interface for daemon interaction.

**Files**: `cmd/jeff/main.go` (529 lines), `cmd/jeff/commands.go` (543 lines), `cmd/jeff/events.go`, `cmd/jeff/ui.go`

##### Interactive Session Loop
- **state**: ready
- **why**: Chat with agent in terminal
- **acceptance**:
  - [ ] when jeff run, then readline prompt appears
  - [ ] when message typed, then SSE events rendered in real-time
  - [ ] when /quit, then session ends with stats banner
- **files**: `cmd/jeff/main.go`, `cmd/jeff/commands.go`

##### Session Management Commands
- **state**: ready
- **why**: Create, list, resume, delete sessions
- **acceptance**:
  - [ ] jeff session create/list/messages/delete
  - [ ] jeff run --session/--continue
- **files**: `cmd/jeff/main.go`, `cmd/jeff/commands.go`

##### SSE Event Rendering
- **state**: ready
- **why**: Display real-time feedback
- **acceptance**:
  - [ ] text_delta -> append text; tool_start -> header; permission_request -> prompt
- **files**: `cmd/jeff/events.go`, `cmd/jeff/ui.go`

---

## Patterns & Architecture Summary

### Load-Bearing Patterns

1. **Interactive Gate + Permission Mode Duality** (`permissions/gate.go`, `modes.go`) -- Two-level gate: ModeGate (policy) + InteractiveGate (Ask flow). Intentional, clean separation. Impact: removing either breaks permission system.

2. **Per-Session MCP Tool Server Hierarchy** (`api/handler.go:350-425`) -- Built-in tools scoped per-session to working dir; external MCP servers shared globally. Intentional for isolation. Impact: removing breaks filesystem sandboxing.

3. **Event Emitter + SSE Channel** (`events/emitter.go`) -- Buffered channel decouples agent from HTTP I/O. Partial misapplication: silent drops on overflow.

4. **Message History Reconstruction from SQLite** (`agent/loop.go:497-540`) -- History rebuilt from DB on each run. Includes orphaned tool_use cleanup. Impact: removing allows malformed history to propagate.

5. **Semaphore-Bounded Tool Concurrency** (`toolcaller/caller.go:101-119`) -- Classic semaphore pattern (maxWorkers default 4). Correct implementation. Impact: removing allows unlimited goroutine explosion.

### Design Tension Points

1. **Handler Gigantism** -- `api/handler.go` is 1000 lines acting as orchestrator + state container + router. Fix: extract `SessionRunner`.
2. **Hardcoded Permission Patterns** -- Tool categorization uses string matching (`isReadOnly`, `isBash`). Fix: pluggable `PermissionRule` interface.
3. **Silent Event Drops** -- 256-buffer channel drops without client notification. Fix: emit `event_overflow` sentinel.
4. **Context Cancellation Window** -- Agent goroutine may continue briefly after client disconnect. Fix: add cancellation timeout + cleanup semaphore.
5. **System Prompt Rebuilds** -- `systemprompt.Build()` called per-message, scanning for CLAUDE.md each time. Fix: cache per working dir with TTL.

### Cross-Cutting Concerns

| Concern | Where | Mechanism | Consistent? | Gaps |
|---------|-------|-----------|-------------|------|
| Auth/Authz | `permissions/` | Mode-based gates | Partial | No API auth, no audit log |
| Input Validation | `api/handler.go` | Explicit checks | Inconsistent | No JSON schema, no size limits |
| Error Handling | Various | `writeError`, `fmt.Errorf`, log | Inconsistent | No error codes, bare errors |
| Logging | Various | `log.Printf` | Minimal | No levels, no structured output |
| Caching | `anthropic/client.go` | In-memory maps | Minimal | No TTL, no session caching |
| Rate Limiting | `anthropic/client.go` | Header tracking only | Partial | No client-side enforcement |
| Retry / Timeouts | `anthropic/client.go` | 10min HTTP, fallback model | Inconsistent | No tool call timeout |

### Data Flow: User Message -> Agent Loop -> Tool Execution -> SSE

```
POST /sessions/{id}/message
  -> Validate (non-empty, no active run)
  -> Load session metadata + history from SQLite
  -> Build system prompt (CLAUDE.md + env)
  -> Create permission gates (ModeGate + InteractiveGate)
  -> Build per-session tool caller
  -> Spawn goroutine: loop.Run(ctx, sessionID, message)
     -> iterate():
        -> streamTurn() -> Anthropic API (streaming SSE)
        -> Parse tool_use blocks
        -> Permission check (gate.CheckAndWait)
        -> toolcaller.CallMany() (parallel, semaphore-bounded)
        -> Emit tool_result events
        -> Check budget, auto-compact threshold
        -> Repeat until end_turn or max turns
  -> Stream events from emitter channel to HTTP SSE response
  -> Client disconnect -> cancel context -> cleanup active run
```

**State Holders**: SQLite per-session DB, in-memory history slice, emitter channel (256), pending permission map, team inbox channel (64), active runs map (handler).

---

## Open Questions

1. How are pre-compact messages handled on session reload? Does the full DB history include both pre-compact and post-compact messages?
2. What is the cleanup strategy for orphaned pending permission requests if a response never arrives?
3. How does plan mode interact with permission modes? Are they orthogonal or complementary?
4. What is the lifetime of sub-agent goroutines? How is context cancellation propagated?
5. How are orphaned teams cleaned up when no agents are registered?
6. What is the message ordering guarantee in the team message bus?
7. Is the 10-minute HTTP client timeout appropriate for all Anthropic API calls?
8. Should system prompt discovery (CLAUDE.md scanning) be cached per working directory?
9. What happens when an external MCP server disconnects mid-session?
10. Should the event channel use backpressure instead of silent drops?
