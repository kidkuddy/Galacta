# Galacta - Manual E2E Testing Plan

## 1. DAEMON STARTUP & HEALTH

### 1.1 Daemon Initialization
- **Action**: `galacta serve`
- **Verify**:
  - Logs "READY" and listens on port 9090 (or GALACTA_PORT)
  - `~/.galacta/` data directory created (or GALACTA_DATA_DIR)
  - `~/.galacta/sessions/` exists
  - SQLite migrations applied
  - API key loaded from ANTHROPIC_API_KEY or macOS Keychain

### 1.2 Health Check
- **Action**: `curl http://localhost:9090/health`
- **Verify**: Returns `{"ok": true, "data": {"version": "...", "status": "ok", "active_sessions": 0, "total_tools": N}}`

### 1.3 Graceful Shutdown
- **Action**: Send SIGINT/SIGTERM to daemon
- **Verify**: Closes all sessions, disconnects MCP clients, exits cleanly

---

## 2. SESSION MANAGEMENT

### 2.1 Create Session
- **Action**: `POST /sessions` with `{"working_dir": "/path", "model": "claude-sonnet-4-6", "permission_mode": "default"}`
- **Verify**: Returns 201 with session object (id, name, working_dir, model, permission_mode, status: "idle")
- **Verify**: Session DB created at `~/.galacta/sessions/{id}.db`

### 2.2 Create Session with Overrides
- **Action**: Create with `system_prompt`, `effort`, `max_budget_usd`, `fallback_model`, `tools`, `allowed_tools`
- **Verify**: All metadata stored in session DB meta table

### 2.3 List Sessions
- **Action**: `GET /sessions`
- **Verify**: Returns array of non-archived sessions with usage stats
- **Action**: `GET /sessions?working_dir=/path` -- filters by directory
- **Action**: `GET /sessions?include_archived=true` -- includes archived

### 2.4 Get Session
- **Action**: `GET /sessions/{id}`
- **Verify**: Returns full metadata including model, permission_mode, status

### 2.5 Update Session
- **Action**: `PATCH /sessions/{id}` with `{"model": "claude-opus-4-6"}`
- **Verify**: Metadata updated in DB
- **Breakpoint**: If session is running, does the change take effect on next API call?

### 2.6 Delete Session
- **Action**: `DELETE /sessions/{id}`
- **Verify**: If running, context cancelled immediately. DB file deleted from disk. Returns success.

### 2.7 Archive Session
- **Action**: `POST /sessions/{id}/archive`
- **Verify**: Marked archived, excluded from default listings

---

## 3. MESSAGE STREAMING (SSE)

### 3.1 Send Message
- **Action**: `POST /sessions/{id}/message` with `{"message": "hello"}`
- **Verify**:
  - Content-Type: `text/event-stream`
  - Streams SSE events in real-time
  - Session status transitions: idle -> running -> idle

### 3.2 SSE Event Types
Verify each event type:
- `text_delta` -- streamed token-by-token text
- `thinking_delta` -- extended thinking tokens (when effort is set)
- `tool_start` -- includes tool name and input JSON
- `tool_result` -- includes output, duration_ms, is_error
- `usage` -- input/output/cache tokens and cost in USD
- `turn_complete` -- stop_reason: end_turn | max_turns | aborted | budget_exceeded | error
- `permission_request` -- requestID, tool name, input
- `plan_mode_changed` -- {active: true/false}

### 3.3 Message Persistence
- **Action**: Send message, then query DB directly
- **Verify**:
  - User message saved with role, content JSON
  - Assistant message saved with role, content, token counts, model, stop_reason
  - Tool results saved as user messages with tool_result blocks
  - Order preserved on reload

### 3.4 List Messages
- **Action**: `GET /sessions/{id}/messages`
- **Verify**: Returns array of all messages with usage totals

---

## 4. PERMISSION MODES

### 4.1 Mode: `default`
- **Action**: Create session with `permission_mode: "default"`, trigger bash command
- **Verify**:
  - Read-only tools (read, glob, grep) auto-approve
  - Bash commands prompt for permission
  - Write/edit tools prompt for permission
  - Edits outside cwd prompt for permission

### 4.2 Mode: `acceptEdits`
- **Action**: Create session with `permission_mode: "acceptEdits"`
- **Verify**:
  - File edits inside cwd auto-approve
  - Bash commands still prompt
  - Everything else same as default

### 4.3 Mode: `bypassPermissions`
- **Action**: Create session with `permission_mode: "bypassPermissions"`
- **Verify**: All tool calls auto-approve without prompts

### 4.4 Mode: `dontAsk`
- **Action**: Create session with `permission_mode: "dontAsk"`
- **Verify**: Auto-approves everything (same as bypass)

### 4.5 Mode: `plan`
- **Action**: Create session with `permission_mode: "plan"`
- **Verify**:
  - Only read-only tools available
  - Write/edit/bash calls denied with error message
  - Tool list filtered before execution

### 4.6 Permission Request Flow
- **Action**: Send message that triggers permission, don't respond
- **Verify**:
  - `permission_request` event emitted with request_id
  - Agent blocks waiting on channel
  - Respond: `POST /sessions/{id}/permission/{requestID}` with `{"approved": true}`
  - Approved -> tool executes
  - Denied -> tool_result with error

### 4.7 Dynamic Mode Change Mid-Conversation
- **Action**: Session running, `PATCH` permission_mode, send next message
- **Verify**: New mode applies immediately on next permission check
- **Breakpoint**: What if you switch from `bypassPermissions` to `plan` while a bash tool is queued?

---

## 5. MODEL SWITCHING

### 5.1 Switch Model Mid-Session (CLI)
- **Action**: `/model claude-opus-4-6` during active session
- **Verify**: Next API call uses the new model
- **Verify**: `/session` shows updated model

### 5.2 Switch Model Mid-Session (API)
- **Action**: `PATCH /sessions/{id}` with `{"model": "claude-haiku-4-5-20251001"}`
- **Verify**: Next message uses haiku

### 5.3 Switch to Invalid Model
- **Action**: `/model nonexistent-model-123`
- **Verify**: Error returned, current model unchanged

### 5.4 Switch Model Between Turns
- **Action**: Send message with sonnet, switch to opus, send another
- **Verify**: First response uses sonnet, second uses opus, conversation context preserved

### 5.5 Fallback Model on 529
- **Action**: Set `fallback_model`, primary model returns 529 (overloaded)
- **Verify**:
  - First attempt fails with 529
  - Automatic retry with fallback model
  - Logs "model X overloaded, falling back to Y"

---

## 6. TURN CANCELLATION

### 6.1 Cancel via Session Delete
- **Action**: Delete session while agent is mid-turn
- **Verify**:
  - Context cancelled
  - Agent loop exits with "aborted" stop_reason
  - SSE stream closes
  - Session removed

### 6.2 Cancel via Client Disconnect
- **Action**: Close SSE connection while agent is running
- **Verify**:
  - Context cancellation propagates
  - Ongoing tool calls terminate
  - Partial results handled gracefully

### 6.3 Cancel During Tool Execution
- **Action**: Cancel while a bash command is running
- **Verify**:
  - Bash process killed
  - Tool result captured as error/partial
  - Agent loop exits cleanly

### 6.4 Cancel During Permission Wait
- **Action**: Agent is waiting for permission approval, cancel/disconnect
- **Verify**: Permission channel unblocked, agent exits

### 6.5 Cancel During API Call
- **Action**: Cancel while streaming from Anthropic API
- **Verify**: HTTP request cancelled, partial content discarded

---

## 7. COMPACT MODE

### 7.1 Manual Compact
- **Action**: `/compact` (default keep 10 messages)
- **Verify**:
  - Sends all messages to API for summarization
  - Summary extracted from `<summary>...</summary>` tags
  - Old messages deleted from DB
  - Summary message saved
  - Returns removed/remaining counts

### 7.2 Manual Compact with Custom Keep
- **Action**: `/compact 5`
- **Verify**: Only keeps 5 most recent messages + summary

### 7.3 Manual Compact with Instructions
- **Action**: `POST /sessions/{id}/compact` with `{"instructions": "focus on architecture decisions"}`
- **Verify**: Instructions appended to compact prompt

### 7.4 Auto-Compact Trigger
- **Action**: Send many messages to approach context window limit
- **Verify**:
  - Triggers when input tokens >= (context_window - 13000 reserve - 20000 buffer)
  - Compact runs transparently in background
  - Conversation summarized automatically
  - Loop continues without user intervention
  - Old messages replaced with summary

### 7.5 Auto-Compact Thresholds by Model
- **Opus/Sonnet (200k context)**: triggers around 167k tokens
- **Haiku (100k context)**: triggers around 67k tokens
- **Verify**: `shouldAutoCompact()` returns true at threshold

### 7.6 Compact Edge Cases
- **Breakpoint**: Compact with only 1-2 messages (nothing to summarize)
- **Breakpoint**: Compact when conversation has tool results with large outputs
- **Breakpoint**: Compact fails (API error) -- does the session continue?
- **Breakpoint**: Auto-compact during multi-turn tool loop

---

## 8. TOOL EXECUTION

### 8.1 File System Tools

**galacta_read**:
- [ ] Read entire file with line numbers (1-based)
- [ ] Read with offset + limit for large files
- [ ] Binary file detection (error: "file appears to be binary")
- [ ] Missing file (graceful error)
- [ ] File > 1MB (line buffer handling)

**galacta_write**:
- [ ] Create new file
- [ ] Overwrite existing file
- [ ] Creates parent directories if needed
- [ ] Respects permission gate

**galacta_edit**:
- [ ] Exact string replacement
- [ ] Multiline replacement
- [ ] Fails if find string not found
- [ ] Special characters in find/replace
- [ ] Preserves surrounding content

**galacta_glob**:
- [ ] Pattern matching (`*.go`, `**/*.ts`)
- [ ] Returns sorted file paths
- [ ] Working dir boundary respected

**galacta_grep**:
- [ ] Regex search with context lines (-B, -C, -A)
- [ ] Case insensitive (-i)
- [ ] Multiline patterns
- [ ] Line numbering

### 8.2 Bash Execution

- [ ] Simple command: `ls -la`
- [ ] Command with stdout + stderr
- [ ] Exit code != 0 (captured, not fatal)
- [ ] Timeout (default 120s, max 600s)
- [ ] Custom timeout parameter
- [ ] Long-running command killed at timeout
- [ ] Command with interactive stdin (should fail/hang)

### 8.3 Web Tools

**galacta_web_fetch**:
- [ ] Fetch URL, returns markdown
- [ ] Strips scripts/styles
- [ ] Respects max_bytes (default 1MB)
- [ ] Unreachable URL error

**galacta_web_search** (server tool):
- [ ] Basic search query
- [ ] Domain filtering (allowed/blocked)
- [ ] Rate limited to 5 uses per turn

### 8.4 Tool Concurrency
- [ ] Sequential execution for permission UX
- [ ] CallMany with max concurrency (4)
- [ ] Context cancellation stops queued tools

### 8.5 Tool Errors
- [ ] Tool not found -> error message
- [ ] Tool execution error -> is_error flag set, agent continues
- [ ] Timeout -> clean termination message

---

## 9. AGENT LOOP

### 9.1 Single Turn
- **Action**: Simple question, no tools needed
- **Verify**: message_start -> text_delta(s) -> usage -> turn_complete(end_turn)

### 9.2 Multi-Turn with Tools
- **Action**: Ask agent to read a file and explain it
- **Verify**:
  - Turn 1: agent calls galacta_read
  - Tool result appended as user message
  - Turn 2: agent explains file content
  - Loop ends with end_turn

### 9.3 Max Turns Enforcement
- **Action**: Set low max_turns, trigger recursive tool use
- **Verify**: Loop stops at max_turns with "max_turns" stop_reason

### 9.4 Budget Enforcement
- **Action**: Set `max_budget_usd: 0.01`, send messages
- **Verify**: turn_complete with "budget_exceeded" when cost exceeds budget

### 9.5 Extended Thinking
- **Action**: Create session with `effort: "high"`
- **Verify**:
  - Thinking config sent with 16384 budget tokens
  - `thinking_delta` events streamed
  - Regular text follows thinking

### 9.6 Orphaned Tool Use Cleanup
- **Action**: Force-crash mid-tool-use, resume session
- **Verify**:
  - Orphaned tool_use (no matching tool_result) detected
  - Orphaned message deleted
  - Warning logged
  - Session resumes cleanly

---

## 10. PLAN MODE

### 10.1 Enter Plan Mode
- **Action**: Agent calls `galacta_enter_plan_mode`
- **Verify**:
  - PlanState.active = true
  - `plan_mode_changed` event: `{active: true}`
  - Tool list filtered to read-only

### 10.2 Write Denied in Plan Mode
- **Action**: Agent tries galacta_write / galacta_edit / galacta_bash in plan mode
- **Verify**: Tool denied / filtered from available tools

### 10.3 Exit Plan Mode
- **Action**: Agent calls `galacta_exit_plan_mode`
- **Verify**:
  - PlanState.active = false
  - `plan_mode_changed` event: `{active: false}`
  - All tools available again

### 10.4 Plan Mode + Permission Mode Interaction
- **Breakpoint**: Enter plan mode while in `bypassPermissions` -- does plan mode still restrict writes?
- **Breakpoint**: Switch permission mode to `plan` then enter/exit plan mode tool -- conflict?

---

## 11. JEFF CLI

### 11.1 Session Creation
- **Action**: `jeff`
- **Verify**: Creates new session in cwd, prints banner, enters interactive loop

### 11.2 CLI Flags
- [ ] `--session {id}` -- resume existing session
- [ ] `--model {name}` -- override model
- [ ] `--dir {path}` -- working directory
- [ ] `--mode {mode}` -- permission mode
- [ ] `--continue` -- resume most recent session in cwd
- [ ] `--output-format stream|json|text` -- output mode
- [ ] `--effort low|medium|high` -- thinking budget
- [ ] `--max-budget-usd {amount}` -- spending cap
- [ ] `--tools {name}` -- tool allow list
- [ ] `--allowedTools {glob}` -- tool glob patterns
- [ ] `--fallback-model {model}` -- fallback on 529
- [ ] `--system-prompt {text}` -- custom system prompt

### 11.3 One-Shot Mode
- **Action**: `jeff "explain this codebase"`
- **Verify**: Sends message, streams output, exits after turn_complete

### 11.4 Banner Display
- **Verify**: ASCII art with model, permissions, directory, session ID

### 11.5 Interactive Commands
| Command | Test |
|---|---|
| `/quit`, `/exit` | Cleanly exits |
| `/history` | Shows all messages with formatting |
| `/session` | Shows box-drawn session info |
| `/model [name]` | Display/change model |
| `/permissions [mode]` | Display/change mode |
| `/compact [N]` | Compact conversation |
| `/cost` | Show usage box (tokens, cost) |
| `/usage` | Show rate limit bars (5h, 7d) |
| `/tasks` | List tasks with status icons |
| `/skills` | Show available skills |
| `/plan` | Info about plan mode |
| `/clear` | Clear screen |
| `/help` | Display all commands |

### 11.6 Multiline Input
- **Action**: Type `"""`, enter content across lines, type `"""`
- **Verify**: Prompt changes, content accumulated, sent on close

### 11.7 Tab Completion
- **Action**: `/` + Tab
- **Verify**: Auto-completes slash commands

### 11.8 Output Formats
- `stream` (default): Formatted with spinner, boxes, colors
- `json`: Each event as JSON line
- `text`: Only text_delta raw output

### 11.9 CLI Edge Cases
- [ ] `jeff --continue` with no prior sessions -> error
- [ ] `jeff --session invalid-id` -> error
- [ ] Ctrl-D (EOF) -> clean exit
- [ ] Ctrl-C -> shows ^C, continues loop
- [ ] Empty input (just Enter) -> ignored
- [ ] Very long single-line input (>10KB)

---

## 12. SKILL & COMMAND SYSTEM

### 12.1 Built-in Skills
- **Action**: `/skills`
- **Verify**: Shows `commit`, `review-pr`

### 12.2 Project Skills
- **Action**: Create `{cwd}/.claude/skills/my-skill.md` with frontmatter
- **Verify**: Listed in `/skills`, invocable by name

### 12.3 Global Skills
- **Action**: Create `~/.claude/skills/global-skill/SKILL.md`
- **Verify**: Listed from any directory

### 12.4 Global Commands
- **Action**: Create `~/.claude/commands/cmd.md`
- **Verify**: Listed as skill, first line = description

### 12.5 Plugin Skills
- **Action**: Plugin with skills at `~/.claude/plugins/installed_plugins.json`
- **Verify**: Discovered and listed

### 12.6 Skill Invocation
- **Action**: `galacta_skill` tool with name + args
- **Verify**: Template loaded, `{{.Args}}` replaced, prompt injected

### 12.7 API Skill Listing
- **Action**: `GET /skills?working_dir=/path`
- **Verify**: Returns JSON array of `{name, description}`

---

## 13. TASK MANAGEMENT

### 13.1 CRUD Operations
- [ ] `galacta_task_create` -- creates with auto-increment ID, status "pending"
- [ ] `galacta_task_get` -- returns full task details
- [ ] `galacta_task_update` -- status, subject, owner changes
- [ ] `galacta_task_list` -- returns all non-deleted tasks

### 13.2 Status Transitions
- [ ] pending -> in_progress -> completed
- [ ] "deleted" status archives task

### 13.3 CLI /tasks
- **Verify**: Table with ID, subject, owner, status icons (● pending, ◐ in_progress, ✓ completed)

---

## 14. TEAM & MULTI-AGENT

### 14.1 Team Creation
- **Action**: `galacta_team_create` with team_name
- **Verify**: Config at `~/.galacta/teams/{name}.json`, task dir created

### 14.2 Sub-Agent Spawning
- **Action**: `galacta_agent` with prompt, subagent_type, team_name
- **Verify**:
  - Subagent types: `general-purpose` (all tools), `Explore`/`Plan` (read-only)
  - Emits `subagent_start` event
  - Returns final text output
  - Max turns enforced

### 14.3 Agent Tool Filtering
- [ ] `general-purpose` -- all tools
- [ ] `Explore` -- read-only tools only
- [ ] `Plan` -- read-only tools only

### 14.4 Team Messaging
- **Action**: Sub-agents send messages via team bus
- **Verify**: Messages flow between agents, injected as user messages

### 14.5 Team Deletion
- **Action**: `galacta_team_delete`
- **Verify**: Config and task list deleted

---

## 15. AUTH & API CLIENT

### 15.1 OAuth Token Handling
- **Action**: Set ANTHROPIC_API_KEY to `sk-ant-oat01-...`
- **Verify**: Bearer header set, `anthropic-beta: oauth-2025-04-20` header added

### 15.2 Keychain Fallback
- **Action**: Unset ANTHROPIC_API_KEY
- **Verify**: Key read from macOS Keychain

### 15.3 Rate Limit Capture
- **Action**: Send message, check `/usage`
- **Verify**: 5h and 7d utilization parsed from response headers

### 15.4 401 Auto-Refresh
- **Action**: Key becomes invalid mid-session
- **Verify**: 401 triggers re-read from Keychain, retries, if still 401 returns error

### 15.5 Streaming Assembly
- **Verify**:
  - `message_start` initializes usage
  - `content_block_start` creates ContentBlock
  - `text_delta` accumulates text
  - `input_json_delta` accumulates tool input JSON
  - `content_block_stop` finalizes tool_use with parsed Input
  - `message_delta` updates usage
  - `message_stop` ends stream

---

## 16. SYSTEM PROMPT

### 16.1 CLAUDE.md Discovery
- **Action**: Create `{cwd}/CLAUDE.md`
- **Verify**: Content loaded and included in system prompt

### 16.2 Custom System Prompt
- **Action**: Set `system_prompt` in session creation
- **Verify**: Custom prompt used/appended

### 16.3 Tool List in Prompt
- **Verify**: Available tool names injected into system prompt

---

## 17. DATABASE & PERSISTENCE

### 17.1 Schema Verification
- **Action**: `sqlite3 ~/.galacta/sessions/{id}.db .schema`
- **Verify**: `messages`, `meta`, `tasks` tables exist

### 17.2 WAL Mode
- **Verify**: `.db-wal` and `.db-shm` files created during writes

### 17.3 Session Resume
- **Action**: Create session, send messages, exit Jeff, `jeff --session {id}`
- **Verify**: History loaded, conversation continues

### 17.4 Data Integrity After Crash
- **Action**: Kill daemon mid-write
- **Verify**: SQLite WAL ensures consistency on restart

---

## 18. ERROR HANDLING & EDGE CASES

### 18.1 Request Validation
- [ ] Missing working_dir -> 400
- [ ] Invalid permission_mode -> 400
- [ ] Non-existent session ID -> 404
- [ ] Message to running session -> 409 "session is already running"

### 18.2 Tool Failures
- [ ] Tool execution error -> is_error flag, agent continues
- [ ] Tool timeout -> clean termination message
- [ ] Tool not found -> error message returned

### 18.3 Context Cancellation
- [ ] Delete session while running -> context cancelled, "aborted"
- [ ] Client disconnect -> propagates cancellation

### 18.4 File Edge Cases
- [ ] Read binary file -> "file appears to be binary"
- [ ] Read non-existent file -> error
- [ ] Write to read-only path -> error
- [ ] Edit with find string not found -> error
- [ ] Glob with no matches -> empty array

### 18.5 Network Errors
- [ ] API unreachable -> error event
- [ ] API timeout -> error event
- [ ] Invalid API key -> 401 -> refresh attempt -> error

---

## 19. BREAKPOINT SCENARIOS (Where Things Can Break)

### 19.1 Model Switch During Active Turn
- **Scenario**: PATCH model while agent is mid-stream
- **Risk**: Next API call uses new model but conversation history was built for old model
- **Test**: Switch from sonnet to opus mid-turn, verify no crash

### 19.2 Permission Switch During Permission Wait
- **Scenario**: Agent waiting for permission approval, switch mode to bypassPermissions
- **Risk**: Pending permission channel never resolves / double-resolves
- **Test**: Verify pending request is handled correctly

### 19.3 Compact During Tool Loop
- **Scenario**: Auto-compact triggers while agent has pending tool_use blocks
- **Risk**: Summary loses tool context, orphaned tool_use IDs
- **Test**: Fill context with tool-heavy conversation, trigger auto-compact

### 19.4 Delete Session During Compact
- **Scenario**: Delete session while compact API call is in flight
- **Risk**: DB write after delete, orphaned goroutines
- **Test**: Trigger compact, immediately delete session

### 19.5 Concurrent Messages to Same Session
- **Scenario**: Two POST /message to same session simultaneously
- **Risk**: Race condition, double-running
- **Test**: Verify second request gets 409

### 19.6 Rapid Mode Switching
- **Scenario**: Rapidly alternate between permission modes
- **Risk**: Race condition in mode application
- **Test**: Script rapid PATCH calls, verify consistent behavior

### 19.7 Large Tool Output
- **Scenario**: Bash command outputs 10MB+ of text
- **Risk**: Memory pressure, event channel overflow
- **Test**: `cat /dev/urandom | head -c 10000000 | base64`

### 19.8 Empty Conversation Compact
- **Scenario**: Compact with 0-1 messages
- **Risk**: No content to summarize, API error
- **Test**: Create session, immediately `/compact`

### 19.9 Session Resume After Schema Migration
- **Scenario**: Upgrade galacta, resume old session with new schema
- **Risk**: Missing columns, migration failures
- **Test**: Create session with old binary, upgrade, resume

### 19.10 Sub-Agent Crash
- **Scenario**: Sub-agent hits error/panic
- **Risk**: Team state inconsistent, parent agent stuck
- **Test**: Spawn agent with bad prompt, verify parent recovers

### 19.11 Fallback Model Cascade
- **Scenario**: Both primary and fallback models return 529
- **Risk**: Infinite retry loop
- **Test**: Set both models to overloaded, verify error returned

### 19.12 Budget Hit During Tool Execution
- **Scenario**: Budget exceeded between tool call and tool result
- **Risk**: Tool executes but result never sent to API
- **Test**: Set tight budget, trigger multi-tool call

---

## 20. INTEGRATION SCENARIOS

### 20.1 Full Developer Workflow
1. `galacta serve`
2. `jeff`
3. "explore this codebase" -> agent uses glob, grep, read
4. "run the tests" -> agent uses bash (permission gate)
5. `/cost` -> verify usage
6. `/compact` -> summarize
7. Continue working
8. `/quit`

### 20.2 Model + Permission Switching Flow
1. Start with sonnet + default mode
2. Send coding question
3. `/model claude-opus-4-6` (upgrade for complex task)
4. Send complex question
5. `/permissions bypassPermissions` (trust the agent)
6. "refactor this file" -> auto-approves all tools
7. `/permissions plan` -> read-only
8. "review what you did" -> only reads allowed

### 20.3 Budget-Constrained Session
1. Create with `max_budget_usd: 0.10`
2. Send multiple messages
3. `/cost` after each
4. Watch budget_exceeded trigger
5. Verify agent stops

### 20.4 Long Session with Auto-Compact
1. Send 50+ messages with tool calls
2. Monitor token counts
3. Auto-compact triggers
4. `/history` shows summary + recent messages
5. Continue conversation -- context preserved

### 20.5 Multi-Agent Research Task
1. Create team
2. Spawn Explore agent for codebase research
3. Spawn general-purpose agent for implementation
4. Both share task list
5. Messages flow between agents
6. Clean shutdown

### 20.6 Session Resume Across Daemon Restarts
1. Create session, send messages
2. Stop daemon (SIGTERM)
3. Restart daemon
4. `jeff --session {id}`
5. Verify history loaded, conversation continues

---

## 21. PERFORMANCE & LOAD

### 21.1 Multiple Concurrent Sessions
- **Action**: Create 5+ sessions, run messages in parallel
- **Verify**: Isolated DBs, events don't cross, no panics

### 21.2 Event Channel Pressure
- **Action**: Rapid event emission (many tool calls)
- **Verify**: Buffered channel (256) handles load, warns on overflow

### 21.3 Large Conversation
- **Action**: 100+ message session
- **Verify**: Auto-compact prevents OOM, DB performance stable

### 21.4 Tool Concurrency
- **Action**: Multiple approved tools at once
- **Verify**: Up to maxWorkers (4) in parallel

---

## 22. CHECKLIST

- [ ] Daemon startup and health
- [ ] Session CRUD (create, read, update, delete, archive)
- [ ] Message streaming (all SSE event types)
- [ ] Permission modes (default, acceptEdits, bypassPermissions, dontAsk, plan)
- [ ] Permission request/approve/deny flow
- [ ] Dynamic permission mode change
- [ ] Model switching (CLI + API)
- [ ] Fallback model on 529
- [ ] Turn cancellation (delete, disconnect, during tool, during permission wait)
- [ ] Manual compact (/compact, with N, with instructions)
- [ ] Auto-compact trigger and execution
- [ ] File tools (read, write, edit, glob, grep)
- [ ] Bash execution (success, failure, timeout)
- [ ] Web tools (fetch, search)
- [ ] Agent loop (single turn, multi-turn, max turns)
- [ ] Budget enforcement
- [ ] Extended thinking (effort levels)
- [ ] Plan mode (enter, deny writes, exit)
- [ ] CLI banner and all 14 commands
- [ ] Multiline input
- [ ] Tab completion
- [ ] Output formats (stream, json, text)
- [ ] One-shot mode
- [ ] Skill system (built-in, project, global, plugin)
- [ ] Task management (CRUD, status transitions)
- [ ] Team creation and multi-agent
- [ ] Sub-agent spawning and tool filtering
- [ ] OAuth token handling
- [ ] Keychain fallback
- [ ] Rate limit capture and /usage
- [ ] 401 auto-refresh
- [ ] CLAUDE.md discovery
- [ ] Database schema and WAL mode
- [ ] Session resume
- [ ] All error handling cases
- [ ] All breakpoint scenarios (Section 19)
- [ ] All integration scenarios (Section 20)
- [ ] Performance under load
