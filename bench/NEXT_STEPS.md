# Next Steps

## Completed

All phases from the original extraction plan have been completed:

- [x] Rename daemon from `herald` to `galacta`, CLI from `hld` to `jeff`
- [x] Default system prompt with tool guidance, CLAUDE.md discovery, environment context
- [x] Agent tool — sub-agent spawning (general-purpose, explore, plan types)
- [x] Skill tool — built-in and user-defined skill execution
- [x] Task tools — task creation, listing, updates, dependencies
- [x] Team tools — multi-agent teams, messaging, coordination
- [x] AskUserQuestion — structured prompts with options
- [x] EnterPlanMode / ExitPlanMode — plan-then-execute workflow
- [x] WebSearch — web search via Anthropic server tool
- [x] Worktree — git worktree isolation
- [x] CLI flags: `--effort`, `--output-format`, `--max-budget-usd`, `--tools`, `--allowedTools`, `--fallback-model`, `--continue`, `--system-prompt`
- [x] Slash commands: `/cost`, `/model`, `/permissions`, `/compact`, `/tasks`, `/skills`, `/plan`, `/help`
- [x] Jeff CLI visual overhaul — spinners, box-drawn UI, session banner, multiline input
- [x] Extract to own repo at `github.com/kidkuddy/Galacta`

## Remaining Work

### Benchmark Updates

1. **Update bench script** — rename `herald`/`hld` references to `galacta`/`jeff` in `bench.sh`
2. **Add tool execution benchmarks** — scenarios that exercise `galacta_bash`, `galacta_read`, `galacta_grep` to measure tool dispatch overhead
3. **Multi-turn benchmarks** — 5-10 message conversations to test memory growth and session state management
4. **Higher concurrency tests** — C=16, C=32 to find the scaling ceiling
5. **Re-run all benchmarks** — fresh numbers with the current codebase

### Feature Gaps

| Feature | Priority | Notes |
|---------|----------|-------|
| `/diff` | Medium | Show uncommitted git changes |
| `/export` | Low | Export conversation as markdown |
| `/fork` | Low | Fork conversation at current point |
| `/rewind` | Low | Rewind to previous turn |
| Multi-root directories | Low | `--add-dir` for multiple working dirs |

### Quality

- Increase benchmark runs to 5+ for tighter confidence intervals
- Higher-frequency RSS sampling (50ms instead of 200ms)
- Add CI integration for automated benchmark regression tracking
