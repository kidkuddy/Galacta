# Galacta vs Claude Code Benchmark

Three-phase benchmark measuring CPU, RAM, and latency for identical prompts across Claude Code CLI and Galacta.

## Phases

### Cold Start
Fresh process startup cost per prompt. No prior session state. Galacta creates a new session each time, Claude spawns a new Node process.

### Warm Session
Second-message latency when a session already exists. Measures how much overhead is process startup vs API call. Galacta reuses an existing daemon session, Claude uses `--continue` to resume the most recent conversation.

### Parallel / Concurrent
How each tool handles N simultaneous requests. Galacta's daemon handles this with a single process (goroutine per session) vs Claude spawning N separate Node processes. Measures total wall-clock time, per-process RSS, daemon peak RSS, and throughput (req/s).

## Usage

```bash
# Full benchmark (3 runs, all phases)
bash bench/bench.sh

# Cold only, quick
bash bench/bench.sh --phase cold --runs 1

# Warm only
bash bench/bench.sh --phase warm --runs 1 --skip-claude

# Parallel only, custom concurrency levels
bash bench/bench.sh --phase parallel --concurrency "2,4" --runs 1

# Galacta-only, all phases
bash bench/bench.sh --skip-claude --runs 2

# 5 runs for better averages
bash bench/bench.sh --runs 5
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `--runs N` | 3 | Number of runs per scenario |
| `--skip-claude` | false | Skip Claude Code benchmarks |
| `--skip-galacta` | false | Skip Galacta benchmarks |
| `--port PORT` | 19191 | Galacta daemon port |
| `--working-dir DIR` | /tmp | Working directory for jeff |
| `--phase PHASE` | all | Run specific phase: cold, warm, parallel, all |
| `--concurrency LEVELS` | 2,4,8 | Comma-separated concurrency levels for parallel phase |

## Scenarios

| Name | Prompt | Tools Expected |
|------|--------|---------------|
| `simple_text` | "What is 2+2?" | 0 |
| `file_listing` | "List files in /tmp" | 1 |
| `read_summarize` | "Read /etc/hosts and summarize" | 1 |
| `heavy_reasoning` | Microservices vs monolith trade-offs | 0 |

Cold and warm phases run all scenarios. Parallel phase uses only `simple_text` (short prompt avoids long API waits dominating the measurement).

## Metrics

- **Wall-clock latency** — total elapsed time
- **User + Sys CPU time** — actual CPU consumed
- **Peak RSS** — max memory of the CLI process
- **Galacta daemon RSS** — sampled every 200ms during each run
- **Throughput** — requests/second (parallel phase only)

## Output

Results are written to `bench/results/YYYYMMDD-HHMMSS/`:

### CSV format

**claude.csv:**
```
phase,scenario,run,concurrency,wall_sec,user_sec,sys_sec,max_rss_bytes,timestamp
```

**galacta.csv:**
```
phase,scenario,run,concurrency,wall_sec,user_sec,sys_sec,max_rss_bytes,daemon_rss_before_kb,daemon_rss_after_kb,daemon_peak_rss_kb,timestamp
```

- `phase`: cold, warm, or parallel
- `concurrency`: 1 for cold/warm, N for parallel
- Per-run stdout and timing files for debugging

### Summary output

Three sections printed to stdout:

```
═══════════════════════════════════════
COLD START
═══════════════════════════════════════
(per-scenario comparison table)

═══════════════════════════════════════
WARM SESSION
═══════════════════════════════════════
(per-scenario comparison table)

═══════════════════════════════════════
PARALLEL (simple_text)
═══════════════════════════════════════
Metric         Claude         Galacta        Speedup
C=2 wall       4.50s          2.10s          2.1x
C=2 RSS tot    374 MB         24 MB*         15.6x
C=4 wall       8.20s          2.30s          3.6x
C=4 RSS tot    748 MB         24 MB*         31.2x
C=8 wall       15.1s          2.80s          5.4x
C=8 RSS tot    1496 MB        24 MB*         62.3x
* Galacta: single daemon. Claude: N separate Node processes
Throughput C=8: Claude 0.53 req/s, Galacta 2.86 req/s
```

## Prerequisites

- `claude` CLI in PATH
- `ANTHROPIC_API_KEY` set (for Galacta)
- macOS (uses `/usr/bin/time -l` format)
- `python3` in PATH (for high-resolution timing in parallel phase)
