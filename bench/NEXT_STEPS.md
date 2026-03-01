# Next Steps: Herald → Galacta Extraction

## Phase 1: Rename

- Rename the daemon binary from `herald` to `galacta`
- Rename the CLI binary from `hld` to `jeff` (Galacta's herald)
- Update all internal references: package names, log prefixes, help text, API paths, env vars, config keys
- Update bench scripts and docs to use new names
- Remove the `herald_ls` tool (no Claude Code equivalent, Bash covers it)

## Phase 2: Priority Features (from COMPAT.md)

Before extracting, address the highest-impact gaps:

### P0 — System Prompt
- Add a default system prompt that ships with Galacta
- Include tool usage guidance (use read not cat, use edit not sed, etc.)
- Include safety rails for destructive operations (git push, rm -rf, etc.)
- Support CLAUDE.md / project instruction loading
- Keep it overridable per-session

### P1 — Agent Tool
- Implement sub-agent spawning within the daemon
- Agent types: general-purpose (full tools), explore (read-only), plan (read-only + plan output)
- Each sub-agent runs as a separate session/goroutine in the daemon
- Parent session can wait for or stream sub-agent results

### P2 — Missing Tools
- `WebSearch` — web search via API (Brave, Google, etc.)
- `AskUserQuestion` — structured prompts with options, sent as events to the client
- `EnterPlanMode` / `ExitPlanMode` — plan-then-execute workflow (flag on session)

### P3 — Session Management
- `/clear` equivalent — reset conversation history
- `/compact` equivalent — summarize and compress context
- Model switching per-message (not just per-session)
- `jeff run` should output session ID to stderr for scripting

### P4 — CLI Flags
- `--effort` (reasoning effort level)
- `--output-format` (text, json, stream-json)
- `--max-budget-usd` (spending cap)
- `--tools` / `--allowedTools` / `--disallowedTools` (tool filtering)

## Phase 3: Extract to Own Repo

1. Create repo at `github.com/kidkuddy/galacta`
2. New `go.mod` with module `github.com/kidkuddy/galacta`
3. Directory structure:
   ```
   galacta/
   ├── cmd/
   │   ├── galacta/        # daemon entry point
   │   └── jeff/           # CLI entry point
   ├── anthropic/          # API client
   ├── agent/              # agent loop
   ├── api/                # HTTP API server
   ├── db/                 # SQLite session store
   ├── events/             # SSE event system
   ├── permissions/        # permission modes
   ├── tools/              # built-in MCP tool servers
   │   ├── fs/             # filesystem tools
   │   ├── exec/           # bash execution
   │   └── web/            # web fetch
   ├── toolcaller/         # tool registry and dispatch
   ├── bench/              # benchmarks and reports
   ├── Makefile
   ├── README.md
   ├── COMPAT.md
   └── REPORT.md
   ```
4. Move all code from `backend/pkg/herald/` → root packages
5. Move `backend/cmd/herald/` → `cmd/galacta/`
6. Move `backend/cmd/hld/` → `cmd/jeff/`
7. Preserve full git history via `git log` (commit messages stay intact)
8. Same 3 external deps: mcp-go, sqlite, uuid

## Phase 4: Clean Up gnz

1. Remove `backend/pkg/herald/` from gnz repo
2. Remove `backend/cmd/herald/` and `backend/cmd/hld/` from gnz
3. Remove herald-related entries from gnz Makefile (if any)
4. Commit the removal
5. If gnz ever needs to import galacta: `go get github.com/kidkuddy/galacta`
