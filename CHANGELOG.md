# Changelog

All notable changes to this project will be documented here.

## [Unreleased]

### Added
- `POST /sessions/{id}/archive` endpoint to archive a session
- Archived sessions are excluded from `GET /sessions` by default
- Pass `?include_archived` to include archived sessions in the listing
- `archived` boolean field included in all session info responses

## [0.1.0] — 2025

### Added
- Auto-compact: sessions are automatically compacted when context is near limit
- Session usage totals returned in session info and messages responses
- Improved `POST /sessions/{id}/compact` endpoint with optional instructions
- `/usage` endpoint returning rate limit utilization from API response headers
- `galacta_register_skill` tool for agents to define new skills at runtime
- Readline autocomplete for Jeff CLI commands and session IDs
- Visual overhaul of Jeff CLI: spinners, box-drawn UI, `/skills` command
- Multi-agent teams with messaging, shared tasks, and team bus
- Git worktrees tool for isolated parallel work (`galacta_enter_worktree`)
- Plugin skills system — skills loaded from `.claude/skills/*.md`
- `WebSearch`, `AskUserQuestion`, and `PlanMode` tools
- `galacta_agent` tool for spawning sub-agents
- `galacta_task_*` tools for in-session task tracking
- System prompt builder with `CLAUDE.md` discovery and environment context
- Tool filtering via `tools` and `allowed_tools` session options
- `make install` target and land shark banner
- Jeff CLI as the primary user-facing interface to the Galacta daemon
- Per-session SQLite databases with WAL mode
- Migration system for schema evolution
- Named the agent Jeff, operating under Galacta
