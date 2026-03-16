# galacta -- Audit Summary

_Audited: 2026-03-06 | Mode: standard_

## TL;DR

Galacta is a well-structured Go daemon + CLI pair that orchestrates Claude interactions via MCP tool servers, SSE streaming, and per-session SQLite persistence. The architecture is coherent and sustainable, but the codebase has **zero tests**, **no API authentication**, and several security edge cases (path traversal, silent event drops) that need addressing before any multi-user or networked deployment. Verdict: **Refactor** -- the foundation is solid, but critical gaps in security and testing must be closed.

## Risk Score: 5/10

| Sub-score | Rating | Notes |
|-----------|--------|-------|
| Architecture | 3/10 | Clean layered + event-driven hybrid; handler.go (1000 lines) is a hotspot but manageable |
| Correctness | 5/10 | Core loops work; error handling inconsistent; orphaned tool_use cleanup is fragile |
| Security | 7/10 | No API auth, path traversal risk, overly broad CORS; permission modes well-implemented |
| Operability | 6/10 | No graceful shutdown of sessions, unstructured logging, no readiness probe |
| Changeability | 4/10 | Good separation of concerns; permission rules hardcoded; no tests to catch regressions |

## Scopes

| Scope | Path | Type | Entry Point | State |
|-------|------|------|-------------|-------|
| Galacta Daemon | `/` | HTTP daemon | `cmd/galacta/main.go` | Ready -- core agent loop, HTTP API, SSE streaming, MCP tools all functional |
| Jeff CLI | `cmd/jeff/` | Terminal CLI | `cmd/jeff/main.go` | Ready -- interactive REPL, session management, SSE event rendering |
| Benchmark Suite | `bench/prompteval/` | Standalone tool | `bench/prompteval/main.go` | Building -- exists but not fully functional |

## Findings Overview

| ID | Category | Severity | Location | Summary |
|----|----------|----------|----------|---------|
| SEC-001 | Security | **critical** | `api/server.go` (all routes) | No API authentication on any endpoint |
| TEST-001 | Testing | **critical** | (entire codebase) | Zero test files |
| SEC-003 | Security | high | `tools/fs/server.go:36-49` | Path traversal via absolute paths bypasses working dir |
| SEC-005 | Security | high | `tools/exec/server.go:54` | Bash command injection (intentional, permission-gated) |
| PERF-001 | Performance | high | `agent/loop.go:95,157,258,311` | Unbounded history slice growth |
| PERF-003 | Performance | high | `anthropic/client.go:51-53` | 10-minute HTTP client timeout; no per-message timeout |
| OPS-001 | Operations | high | `cmd/galacta/main.go:35-40` | No graceful shutdown of active sessions |
| SEC-002 | Security | warning | `api/server.go:79` | `Access-Control-Allow-Origin: *` |
| SEC-004 | Security | medium | `tools/fs/server.go:216-250` | Unvalidated glob patterns |
| SEC-007 | Security | warning | `config.go:88-137` | Keychain parsing logs auth method |
| SEC-008 | Security | medium | `cmd/jeff/main.go:290-293` | Unescaped URL query parameters |
| ERR-001 | Error Handling | medium | `cmd/jeff/main.go:382,449,464,481,500` | Swallowed JSON decode errors |
| ERR-002 | Error Handling | medium | `cmd/jeff/main.go:174`, `db/store.go:193` | Ignored Sscanf errors |
| PERF-002 | Performance | medium | `events/emitter.go:26-30` | Event channel silently drops on overflow |
| PERF-004 | Performance | medium | `toolcaller/caller.go:102-119` | No per-tool-call timeout |
| CONFIG-002 | Configuration | medium | `cmd/galacta/main.go:27-30` | No startup config validation |
| OPS-002 | Operations | medium | `api/handler.go:84-97` | Health check not comprehensive (no readiness) |
| OPS-003 | Operations | medium | Various | Unstructured `log.Printf` everywhere |
| OPS-004 | Operations | medium | `db/store.go:39-40` | Single SQLite connection (intentional but undocumented) |
| OPS-005 | Operations | medium | `api/handler.go:450-462` | Panic recovery edge case in agent goroutine |
| ARCH-001 | Architecture | medium | `api/handler.go` (1000 lines) | Handler is session orchestrator + state container + router |
| ARCH-002 | Architecture | medium | `permissions/modes.go` | Permission rules hardcoded as string patterns |
| ARCH-003 | Architecture | medium | `events/emitter.go` | Silent event drops (fail-silent instead of fail-fast) |
| ERR-003 | Error Handling | low | `toolcaller/registry.go:36`, `db/store.go:172-181` | Bare error returns without context |
| ERR-004 | Error Handling | low | `tools/ask/server.go:72` | Unmarshal errors ignored |
| ERR-005 | Error Handling | low | `systemprompt/claudemd.go:43,53,66,88` | `filepath.Abs` errors ignored |
| ERR-006 | Error Handling | low | `api/handler.go:171,175` | Missing error context in handlers |
| DEAD-001 | Dead Code | low | Various | Unused metadata key defaults |
| DEPS-002 | Dependencies | low | `tools/fs/server.go:100` | 1MB line buffer allocation |
| SEC-006 | Security | info | `db/store.go` | SQL injection mitigated (parameterized queries) |
| DEPS-001 | Dependencies | info | `go.mod` | Dependency freshness not verified |

## Top 5 Issues (by blast radius)

### 1. No API Authentication (SEC-001) -- `api/server.go` -- critical
- **What**: All HTTP endpoints are unauthenticated. Anyone with network access can create sessions, run commands, and respond to permission prompts.
- **Why it matters**: An attacker on the same network can execute arbitrary code via the bash tool (if permission mode allows it) or approve their own permission requests.
- **Fix**: Add Bearer token authentication middleware or bind exclusively to localhost with a session-scoped token.
- **Effort**: M

### 2. Zero Test Coverage (TEST-001) -- entire codebase -- critical
- **What**: No unit, integration, or e2e tests exist.
- **Why it matters**: Every change risks silent regressions in the agent loop, permission system, or session persistence. The orphaned tool_use cleanup in `agent/loop.go` is a prime example of fragile logic that needs test coverage.
- **Fix**: Start with table-driven tests for permission modes, agent loop turn logic, and DB persistence. Add integration tests for the SSE streaming path.
- **Effort**: L

### 3. Path Traversal via Absolute Paths (SEC-003) -- `tools/fs/server.go:36-49` -- high
- **What**: `galacta_read` accepts absolute paths (e.g., `/etc/passwd`) without validating they're within the working directory. Permission checks only gate writes, not reads.
- **Why it matters**: Any session can read any file on the filesystem the daemon process can access.
- **Fix**: Validate that all resolved paths (read and write) are within the working directory unless explicitly overridden.
- **Effort**: S

### 4. No Graceful Shutdown (OPS-001) -- `cmd/galacta/main.go:35-40` -- high
- **What**: `Shutdown()` closes MCP clients but does not cancel running agent loops. Active sessions are orphaned.
- **Why it matters**: In-flight API calls and tool executions continue after shutdown, potentially corrupting session DBs or leaking resources.
- **Fix**: Track all active sessions; send context cancellation to each on shutdown signal; wait with timeout.
- **Effort**: S

### 5. Silent Event Channel Drops (ARCH-003) -- `events/emitter.go:26-30` -- medium
- **What**: The 256-buffer event channel silently drops events when full, logging a warning but not notifying the client.
- **Why it matters**: Clients miss tool results, permission requests, or completion events. This is a correctness bug that manifests under load (fast tool execution, thinking blocks).
- **Fix**: Emit an `event_overflow` sentinel event instead of silently dropping; or implement backpressure with a short timeout.
- **Effort**: S

## Verdict: Refactor

The foundation is well-designed: clean separation between HTTP layer, agent loop, tool dispatch, and permission system. The MCP tool server pattern is extensible and the event-driven streaming works. However, the codebase cannot be deployed beyond local single-user use without addressing authentication (SEC-001), path traversal (SEC-003), and graceful shutdown (OPS-001). The complete absence of tests (TEST-001) makes any refactoring risky. **Refactor** -- not rewrite -- because the architecture is sound; the gaps are in security hardening, operational resilience, and test coverage.

## Recommended Next Actions (ordered by priority)

1. **Add localhost-only binding + session token auth** (SEC-001) -- Bind to `127.0.0.1` by default; generate a random token on startup and require it as Bearer header. Single PR.

2. **Fix path traversal in fs tools** (SEC-003) -- Add `isInsideCwd()` check to all file operations (read, glob, grep), not just writes. Single PR.

3. **Implement graceful shutdown** (OPS-001) -- Track active runs in a `sync.WaitGroup`; cancel all contexts on SIGTERM; wait with 10s timeout. Single PR.

4. **Add core test suite** (TEST-001) -- Start with: permission mode decisions (table-driven), agent loop turn logic (mock Anthropic client), DB persistence round-trip, SSE event serialization. Single PR per test area.

5. **Restrict CORS** (SEC-002) -- Replace `*` with configurable allowed origins, defaulting to `http://localhost:*`. Single PR.

6. **Add event overflow handling** (ARCH-003) -- Replace silent drop with `event_overflow` sentinel or short-timeout backpressure. Single PR.

7. **Extract SessionRunner from handler** (ARCH-001) -- Move session state management (caller build, prompt assembly, permission gates) into a `SessionRunner` struct. Reduces handler.go from 1000 to ~500 lines. Single PR.

8. **Add structured logging** (OPS-003) -- Migrate from `log.Printf` to `slog` with session ID context. Single PR.

9. **Validate config at startup** (CONFIG-002) -- Check port validity, data dir writability, API key format. Single PR.

10. **Add per-tool-call timeouts** (PERF-004) -- Propagate context deadlines to all MCP tool calls, not just exec. Default 2 minutes. Single PR.
