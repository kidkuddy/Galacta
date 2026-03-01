# Benchmark Report: Galacta vs Claude Code

**Date:** 2026-03-01
**System:** Apple M1, 8 cores, 16 GB RAM, macOS 26.3, arm64
**Runs:** 2 per scenario per phase

---

## TL;DR

Galacta is **6.5x faster** on simple prompts. At 8 concurrent requests, Galacta delivers **6.1 req/s** vs Claude's **0.67 req/s** — a **9x throughput advantage**. Memory tells the real story: Galacta's full stack (daemon + 8 jeff clients) uses ~121 MB vs Claude's 2.2 GB — an **18x reduction**.

---

## What's Being Measured

Both tools call the same Anthropic API. The difference is architecture:

- **Claude Code**: Spawns a full Node.js process per invocation (~300 MB, ~5-8s startup). Each process loads the runtime, parses tools, and manages its own API connection.
- **Galacta**: Persistent Go daemon (~23 MB) handles all sessions. The `jeff` CLI (~12 MB) is a thin RPC client that sends the prompt to the daemon and streams output back.

**Memory accounting:**
- **Claude**: RSS of the Node process (includes runtime, V8 heap, tool definitions)
- **Galacta total**: daemon peak RSS + jeff client RSS. The daemon is amortized across all sessions — it's running whether you send 1 or 100 requests.

---

## Cold Start

Fresh process per prompt. No prior session state.

| Scenario | Claude | Galacta | Speedup | Claude RSS | Galacta Total | Ratio |
|----------|--------|---------|---------|------------|--------------|-------|
| simple_text | 7.54s | 1.16s | **6.5x** | 297 MB | 30 MB (18+12) | 9.9x |
| file_listing | 17.56s | 5.95s | **3.0x** | 309 MB | 30 MB (19+11) | 10.3x |
| read_summarize | 14.54s | 4.34s | **3.4x** | 295 MB | 34 MB (23+11) | 8.7x |
| heavy_reasoning | 23.71s | 16.32s | **1.5x** | 333 MB | 34 MB (22+12) | 9.8x |

Galacta total = daemon peak + jeff client. The daemon cost is amortized — it's the same ~23 MB process regardless of workload.

The speedup narrows on heavy_reasoning because the API response itself takes 15s+, so startup overhead (Galacta's main advantage) matters less proportionally.

## Warm Session

Second message sent to an existing session. Galacta reuses the daemon session in-memory. Claude uses `--continue` but still spawns a fresh Node process.

| Scenario | Claude | Galacta | Speedup | Claude RSS | Galacta Total | Ratio |
|----------|--------|---------|---------|------------|--------------|-------|
| simple_text | 8.37s | 0.95s | **8.8x** | 275 MB | 34 MB (22+12) | 8.1x |
| file_listing | 13.31s | 3.48s | **3.8x** | 331 MB | 35 MB (23+12) | 9.5x |
| read_summarize | 10.70s | 2.72s | **3.9x** | 281 MB | 32 MB (21+11) | 8.8x |
| heavy_reasoning | 17.44s | 7.92s | **2.2x** | 294 MB | 35 MB (23+12) | 8.4x |

Warm sessions help Galacta more than Claude. Galacta's daemon already has the session loaded — no session creation overhead. Claude still boots a full Node runtime regardless.

## Parallel / Concurrent (simple_text)

N simultaneous requests fired at once. This is the real test of daemon architecture.

| Concurrency | Claude Wall | Galacta Wall | Speedup | Claude RSS | Galacta Total | Ratio |
|-------------|-------------|--------------|---------|------------|--------------|-------|
| C=2 | 7.77s | 1.61s | **4.8x** | 558 MB | 46 MB (23+23) | 12.1x |
| C=4 | 13.26s | 1.42s | **9.3x** | 1,108 MB | 71 MB (25+46) | 15.6x |
| C=8 | 12.00s | 1.31s | **9.1x** | 2,178 MB | 121 MB (28+93) | 18.0x |

| Metric | Claude | Galacta |
|--------|--------|---------|
| C=8 Throughput | 0.67 req/s | 6.10 req/s |

**Galacta total** = daemon peak + sum of N jeff client processes. The daemon stays under 30 MB even at C=8 because sessions are lightweight goroutines. The jeff clients are ~12 MB each but short-lived.

**Claude** scales linearly in memory: 8 Node processes = 8 x 270 MB = 2.2 GB. On a 16 GB machine, you'd hit memory pressure around ~40 concurrent Claude processes.

Galacta wall time is nearly flat (~1.3s) regardless of concurrency — API calls fan out in parallel goroutines within the daemon. Claude wall time grows as processes compete for resources.

## CPU Comparison

| Phase | Claude CPU/run | Galacta CPU/run |
|-------|---------------|-----------------|
| Cold (simple) | 5.30s | 0.02s |
| Warm (simple) | 7.09s | 0.01s |

Galacta's CPU usage is negligible. The `jeff` client is a compiled Go binary that just does HTTP — no runtime to boot, no JavaScript to parse, no V8 to warm up. All the actual compute happens server-side in the Anthropic API.

## Key Takeaways

1. **Startup cost is Galacta's biggest win.** Claude's Node.js process takes ~5-8s to boot. Galacta's Go CLI takes <0.1s — the daemon is already running. This makes Galacta **6-9x faster** for simple operations.

2. **Memory efficiency scales with concurrency.** At C=8, Galacta uses 121 MB total (daemon + 8 clients) vs Claude's 2.2 GB. The daemon is amortized — adding more sessions costs ~12 MB per jeff client, not ~300 MB per Node process.

3. **Throughput scales naturally.** Galacta's daemon handles concurrent sessions as goroutines. At C=8: 6.1 req/s vs 0.67 req/s. The gap would widen further at higher concurrency levels.

4. **For long-running API calls, the gap narrows.** Heavy reasoning (16s API response) shows only 1.5x speedup — because the API latency dominates, not the tool overhead. Galacta's advantage is most dramatic on short, frequent operations.

---

## Analysis & Next Steps

### What the numbers validate

The daemon architecture thesis holds up. A persistent Go process with goroutine-per-session beats spawn-per-invocation on every axis: latency, memory, CPU, throughput. The flat wall-time curve under concurrency (1.31s at C=8 vs 1.61s at C=2) confirms that the daemon is doing what it should — fan out API calls in parallel with near-zero coordination overhead.

### Gaps in the current benchmark

- **Only 2 runs per scenario.** Variance is noticeable — warm heavy_reasoning went 11.7s then 4.1s across two runs. Need 5+ runs for tighter confidence intervals.
- **RSS sampling at 200ms.** Short parallel bursts (1.3s total) can miss memory spikes between samples. Higher-frequency sampling or `/proc`-based peak tracking would be more accurate.
- **No tool execution benchmarks.** All scenarios are prompt-only or use Claude's built-in tools. Galacta's real value will show when custom MCP tools are in play — tool dispatch through a daemon vs tool dispatch through Node.

### Where to focus next

1. **MCP tool benchmarks** — add scenarios that invoke registered MCP tools (file read, DB query, etc.) to measure Galacta's tool dispatch overhead vs Claude's.
2. **Longer session benchmarks** — multi-turn conversations (5-10 messages) to test daemon memory growth, session state management, and GC behavior over time.
3. **Higher concurrency** — push to C=16, C=32 to find the ceiling. Galacta should handle it given Go's goroutine model, but want to verify daemon RSS stays bounded.
4. **Connection pooling** — if Galacta isn't already reusing HTTP connections to the Anthropic API across sessions, that's free latency savings.
