# galacta -- Findings

_Audited: 2026-03-06 | Mode: standard_

All findings from the quality, security, and health audit. Grouped by category, sorted by severity within each group.

---

## SEC -- Security

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| SEC-001 | **critical** | `api/server.go` (all routes) | No authentication on any HTTP endpoint. Any network-accessible client can create sessions, run commands, and respond to permission prompts. | Add Bearer token auth middleware or bind to localhost with session-scoped token. |
| SEC-003 | high | `tools/fs/server.go:36-49`, `permissions/modes.go:90-111` | Path traversal via absolute paths. `galacta_read` accepts `/etc/passwd` without validating path is within working directory. Permission checks only gate writes. | Add `isInsideCwd()` check to all file read operations. |
| SEC-005 | high | `tools/exec/server.go:54` | Bash command injection: `exec.CommandContext(ctx, "/bin/bash", "-c", command)` executes arbitrary commands. Intentional design, gated by permission modes. | Document trust requirement. Already gated by permission modes. |
| SEC-002 | warning | `api/server.go:79` | `Access-Control-Allow-Origin: *` allows any origin to access the API. | Restrict to known origins or configurable allowlist. |
| SEC-007 | warning | `config.go:88-137` | Keychain parsing logs auth method: `"galacta: using API key from macOS Keychain"`. | Avoid logging which auth method is active. |
| SEC-004 | medium | `tools/fs/server.go:216-250` | `galacta_glob` accepts arbitrary patterns without restriction. | Document that glob is scoped to workingDir; consider restricting. |
| SEC-008 | medium | `cmd/jeff/main.go:290-293` | URL parameter not escaped: `url += "?working_dir=" + workingDir`. Special chars in workingDir cause malformed URLs. | Use `url.QueryEscape()`. |
| SEC-006 | info | `db/store.go` (all queries) | SQL injection mitigated: all queries use parameterized statements. | No action needed. |

---

## PERF -- Performance

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| PERF-001 | high | `agent/loop.go:95,157,258,311` | Unbounded history slice growth. No absolute limit on message count per turn. Auto-compact mitigates but doesn't prevent OOM for edge cases. | Enforce hard maximum on history size; reject if exceeded. |
| PERF-003 | high | `anthropic/client.go:51-53` | HTTP client timeout is 10 minutes. If API hangs, session blocks for 10 minutes with no per-message timeout. | Add per-message context timeout (e.g., 5 min for streaming). |
| PERF-002 | medium | `events/emitter.go:26-30` | Event channel (256 buffer) silently drops events when full. Streaming clients miss events. | Increase buffer, implement backpressure, or emit overflow sentinel. |
| PERF-004 | medium | `toolcaller/caller.go:102-119` | No per-tool-call timeout. Stuck tools block the semaphore indefinitely. Only exec tool has its own timeout. | Propagate context deadlines to all MCP tool calls. |
| PERF-005 | low | `api/handler.go:687-711` | No limit on number of tools discovered from MCP servers. | Log tool count; warn if > 1000. |

---

## ERR -- Error Handling

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| ERR-001 | medium | `cmd/jeff/main.go:382,449,464,481,500` | `json.Decoder.Decode()` errors swallowed; silently fails to decode HTTP responses. | Check and log decode errors before using result. |
| ERR-002 | medium | `cmd/jeff/main.go:174`, `db/store.go:193`, `api/handler.go:396` | `fmt.Sscanf()` failures ignored; unparseable values default to zero. | Check return value and handle parse errors. |
| ERR-003 | low | `toolcaller/registry.go:36`, `db/store.go:172-181`, `agent/loop.go:112` | Bare error returns without wrapping context. | Wrap with `fmt.Errorf("context: %w", err)`. |
| ERR-004 | low | `tools/ask/server.go:72`, `cmd/jeff/events.go:121` | `json.Unmarshal` errors ignored on non-critical paths. | Log or handle unmarshal failures. |
| ERR-005 | low | `systemprompt/claudemd.go:43,53,66,88` | `filepath.Abs()` errors ignored; assumes resolution always succeeds. | Check error return. |
| ERR-006 | low | `api/handler.go:171,175,359,362` | `json.Unmarshal()` errors in session creation not checked. | Validate JSON parse results. |

---

## ARCH -- Architecture

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| ARCH-001 | medium | `api/handler.go` (1000 lines) | Handler is session orchestrator + state container + router. Fat controller pattern. | Extract `SessionRunner` struct to own caller, store, gates. |
| ARCH-002 | medium | `permissions/modes.go` | Permission rules use hardcoded string patterns (`isReadOnly`, `isBash`, `isWriteEdit`). Adding new rules requires code changes. | Introduce `PermissionRule` interface with pluggable checkers. |
| ARCH-003 | medium | `events/emitter.go` | Silent event drops (fail-silent instead of fail-fast). Buffer overflow not communicated to client. | Emit `event_overflow` event; add metrics. |

---

## OPS -- Operations

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| OPS-001 | high | `cmd/galacta/main.go:35-40`, `galacta.go:81-85` | No graceful shutdown of running sessions. `Shutdown()` only closes MCP clients; active agent loops orphaned. | Track active runs in WaitGroup; cancel all on SIGTERM; wait with timeout. |
| OPS-002 | medium | `api/handler.go:84-97` | `/health` only reports status "ok" and session count. No readiness probe (e.g., MCP servers connected). | Add `/ready` endpoint checking subsystem health. |
| OPS-003 | medium | `agent/loop.go`, `api/handler.go`, `galacta.go` | All logging via `log.Printf()`. No structured output, no levels, no trace IDs. | Migrate to `slog` with session ID context. |
| OPS-004 | medium | `db/store.go:39-40` | `SetMaxOpenConns(1)` serializes all DB access per session. Intentional for SQLite WAL but undocumented. | Document trade-off; consider connection pooling for read queries. |
| OPS-005 | medium | `api/handler.go:450-462` | If agent goroutine panics, defer cleanup runs but concurrent emitter sends may fail after emitter closed. | Ensure panic recovery doesn't leave dangling goroutines. |

---

## CFG -- Configuration

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| CFG-002 | medium | `cmd/galacta/main.go:27-30` | No startup validation of config (port validity, data dir writability, API key format). Invalid values only fail at use time. | Validate all config at startup; test data dir write permissions. |
| CFG-001 | low | `config.go:33`, `api/handler.go:137` | Default model hardcoded as `claude-sonnet-4-6`. | Already configurable via `GALACTA_DEFAULT_MODEL`. No action. |

---

## TEST -- Testing

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| TEST-001 | **critical** | (entire codebase) | Zero test files. No unit, integration, or e2e tests. All features verified only by manual testing. | Add tests for: permission modes (table-driven), agent loop turns (mock client), DB persistence, SSE serialization. |

---

## DEAD -- Dead Code

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| DEAD-001 | low | Various metadata key accesses | Some metadata keys (`effort`, `fallback_model`) return empty strings when unset; defaults applied. | Benign; no action needed. |
| DEAD-003 | info | `tools/web/server.go` | Web search tool code remains after removal (commit `f7128aa`). `galacta_web_fetch` still registered. | Document that web search removed; web_fetch retained intentionally. |

---

## DEPS -- Dependencies

| ID | Severity | Location | Description | Fix |
|----|----------|----------|-------------|-----|
| DEPS-002 | low | `tools/fs/server.go:100`, `cmd/jeff/events.go:51` | Large buffer allocations (1MB line buffer, 256KB initial) in file reading paths. | Consider streaming instead of full buffering for large files. |
| DEPS-001 | info | `go.mod` | Dependency freshness not verified without running `go list -u -m all`. | Run periodic dependency audit. |

---

## Finding Counts

| Severity | Count |
|----------|-------|
| Critical | 2 (SEC-001, TEST-001) |
| High | 5 (SEC-003, SEC-005, PERF-001, PERF-003, OPS-001) |
| Medium | 15 |
| Low | 8 |
| Info | 3 |
| **Total** | **33** |
