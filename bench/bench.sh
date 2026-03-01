#!/usr/bin/env bash
set -euo pipefail

# ── Herald vs Claude Code Benchmark ──────────────────────────────────────────
# Three-phase benchmark: cold start, warm session, parallel concurrency.
# Measures wall-clock latency, CPU time, peak RSS for identical prompts
# across Claude Code CLI and Herald (hld + daemon).

# ── Defaults ─────────────────────────────────────────────────────────────────
RUNS=3
SKIP_CLAUDE=false
SKIP_HERALD=false
HERALD_PORT=19191
WORKING_DIR=""
PHASE="all"
CONCURRENCY_LEVELS="2,4,8"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HERALD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BACKEND_DIR="$(cd "$HERALD_DIR/../.." && pwd)"
BIN_DIR="$BACKEND_DIR/bin"
HERALD_BIN="$BIN_DIR/herald"
HLD_BIN="$BIN_DIR/hld"
HERALD_PID=""
SAMPLER_PID=""
TEMP_DATA_DIR=""

# ── Usage ────────────────────────────────────────────────────────────────────
usage() {
    cat <<'EOF'
Usage: bench.sh [OPTIONS]

Options:
  --runs N              Number of runs per scenario (default: 3)
  --skip-claude         Skip Claude Code benchmarks
  --skip-herald         Skip Herald benchmarks
  --port PORT           Herald daemon port (default: 19191)
  --working-dir DIR     Working directory for hld (default: /tmp)
  --phase PHASE         Run specific phase: cold, warm, parallel, all (default: all)
  --concurrency LEVELS  Comma-separated concurrency levels (default: "2,4,8")
  -h, --help            Show this help

Examples:
  bash bench.sh                                    # Full benchmark, 3 runs
  bash bench.sh --runs 1 --skip-claude             # Quick herald-only test
  bash bench.sh --phase cold --runs 1              # Cold only, quick
  bash bench.sh --phase warm --runs 1              # Warm only
  bash bench.sh --phase parallel --concurrency "2,4" --runs 1
  bash bench.sh --skip-claude --runs 2             # Herald-only, all phases
EOF
    exit 0
}

# ── Parse Flags ──────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --runs)        RUNS="$2"; shift 2 ;;
        --skip-claude) SKIP_CLAUDE=true; shift ;;
        --skip-herald) SKIP_HERALD=true; shift ;;
        --port)        HERALD_PORT="$2"; shift 2 ;;
        --working-dir) WORKING_DIR="$2"; shift 2 ;;
        --phase)       PHASE="$2"; shift 2 ;;
        --concurrency) CONCURRENCY_LEVELS="$2"; shift 2 ;;
        -h|--help)     usage ;;
        *)             echo "Unknown flag: $1"; usage ;;
    esac
done

[[ -z "$WORKING_DIR" ]] && WORKING_DIR="/tmp"

# Validate phase
case "$PHASE" in
    cold|warm|parallel|all) ;;
    *) echo "Invalid phase: $PHASE (must be cold, warm, parallel, or all)"; exit 1 ;;
esac

# ── Cleanup Trap ─────────────────────────────────────────────────────────────
cleanup() {
    [[ -n "$SAMPLER_PID" ]] && kill "$SAMPLER_PID" 2>/dev/null || true
    if [[ -n "$HERALD_PID" ]]; then
        echo "Stopping herald daemon (PID $HERALD_PID)..."
        kill "$HERALD_PID" 2>/dev/null || true
        wait "$HERALD_PID" 2>/dev/null || true
    fi
    [[ -n "$TEMP_DATA_DIR" && -d "$TEMP_DATA_DIR" ]] && rm -rf "$TEMP_DATA_DIR"
}
trap cleanup EXIT INT TERM

# ── Helpers ──────────────────────────────────────────────────────────────────
log()  { echo "▸ $*"; }
logn() { echo ""; log "$@"; }
die()  { echo "✗ $*" >&2; exit 1; }

check_binary() {
    local name="$1" path="$2"
    if [[ ! -x "$path" ]]; then
        die "$name not found at $path — run 'make build' first"
    fi
}

# ── Prerequisites ────────────────────────────────────────────────────────────
logn "Checking prerequisites..."

if [[ "$SKIP_CLAUDE" == false ]]; then
    command -v claude >/dev/null 2>&1 || die "claude CLI not found in PATH"
    log "claude: $(command -v claude)"
fi

if [[ "$SKIP_HERALD" == false ]]; then
    log "Building herald and hld..."
    (cd "$HERALD_DIR" && make build 2>&1 | sed 's/^/  /')
    check_binary "herald" "$HERALD_BIN"
    check_binary "hld" "$HLD_BIN"
    log "herald: $HERALD_BIN"
    log "hld:    $HLD_BIN"
fi

# ── System Info ──────────────────────────────────────────────────────────────
logn "System info:"
log "CPU:    $(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo 'unknown')"
log "Cores:  $(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo '?')"
log "RAM:    $(( $(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1073741824 )) GB"
log "macOS:  $(sw_vers -productVersion 2>/dev/null || uname -r)"
log "Arch:   $(uname -m)"

# ── Results Directory ────────────────────────────────────────────────────────
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
RESULTS_DIR="$SCRIPT_DIR/results/$TIMESTAMP"
mkdir -p "$RESULTS_DIR"
log "Results: $RESULTS_DIR"
log "Phase:   $PHASE"

# ── CSV Headers ──────────────────────────────────────────────────────────────
CLAUDE_CSV="$RESULTS_DIR/claude.csv"
HERALD_CSV="$RESULTS_DIR/herald.csv"

if [[ "$SKIP_CLAUDE" == false ]]; then
    echo "phase,scenario,run,concurrency,wall_sec,user_sec,sys_sec,max_rss_bytes,timestamp" > "$CLAUDE_CSV"
fi
if [[ "$SKIP_HERALD" == false ]]; then
    echo "phase,scenario,run,concurrency,wall_sec,user_sec,sys_sec,max_rss_bytes,daemon_rss_before_kb,daemon_rss_after_kb,daemon_peak_rss_kb,timestamp" > "$HERALD_CSV"
fi

# ── Scenarios (parallel arrays — bash 3 compat) ─────────────────────────────
SCENARIO_NAMES=(
    "simple_text"
    "file_listing"
    "read_summarize"
    "heavy_reasoning"
)

SCENARIO_PROMPTS=(
    "What is 2+2? Reply with just the number."
    "List the files in /tmp. Just show the first 10 entries."
    "Read /etc/hosts and summarize it in one paragraph."
    "Explain the trade-offs between microservices and monolith architectures. Cover at least: deployment complexity, team scalability, data consistency, latency overhead, operational cost, and debugging difficulty. Be thorough but concise, aim for about 300 words."
)

WARMUP_PROMPT="Say hello in exactly one word."

# Get prompt by scenario index
get_prompt() {
    local idx="$1"
    echo "${SCENARIO_PROMPTS[$idx]}"
}

# ── Parse /usr/bin/time output (macOS format) ────────────────────────────────
parse_time_output() {
    local time_file="$1"
    local wall="" user="" sys="" rss=""

    wall=$(grep -oE '[0-9]+\.[0-9]+ real' "$time_file" | awk '{print $1}' || echo "0")
    user=$(grep -oE '[0-9]+\.[0-9]+ user' "$time_file" | awk '{print $1}' || echo "0")
    sys=$(grep -oE '[0-9]+\.[0-9]+ sys' "$time_file" | awk '{print $1}' || echo "0")
    rss=$(grep 'maximum resident set size' "$time_file" | awk '{print $1}' || echo "0")

    [[ -z "$wall" ]] && wall="0"
    [[ -z "$user" ]] && user="0"
    [[ -z "$sys" ]] && sys="0"
    [[ -z "$rss" ]] && rss="0"

    echo "$wall $user $sys $rss"
}

# ── Herald Daemon ────────────────────────────────────────────────────────────
start_herald_daemon() {
    TEMP_DATA_DIR="$(mktemp -d /tmp/herald-bench-XXXXXX)"
    log "Starting herald daemon on port $HERALD_PORT (data: $TEMP_DATA_DIR)..."

    "$HERALD_BIN" --port "$HERALD_PORT" --data-dir "$TEMP_DATA_DIR" &
    HERALD_PID=$!

    local waited=0
    while ! curl -sf "http://localhost:$HERALD_PORT/health" >/dev/null 2>&1; do
        sleep 0.2
        waited=$((waited + 1))
        if [[ $waited -ge 50 ]]; then
            die "Herald daemon failed to start within 10s"
        fi
    done
    log "Herald daemon ready (PID $HERALD_PID)"
}

get_daemon_rss_kb() {
    ps -o rss= -p "$HERALD_PID" 2>/dev/null | tr -d ' ' || echo "0"
}

# RSS sampler: writes peak RSS to a file
start_rss_sampler() {
    local peak_file="$1"
    echo "0" > "$peak_file"
    (
        local peak=0
        while true; do
            local rss
            rss=$(get_daemon_rss_kb)
            if [[ "$rss" -gt "$peak" ]] 2>/dev/null; then
                peak="$rss"
                echo "$peak" > "$peak_file"
            fi
            sleep 0.2
        done
    ) &
    SAMPLER_PID=$!
}

stop_rss_sampler() {
    if [[ -n "$SAMPLER_PID" ]]; then
        kill "$SAMPLER_PID" 2>/dev/null || true
        wait "$SAMPLER_PID" 2>/dev/null || true
        SAMPLER_PID=""
    fi
}

# ── Run Functions ────────────────────────────────────────────────────────────

# Run a single Claude invocation and record to CSV.
# Args: phase scenario run_num concurrency prompt [--continue]
run_claude_single() {
    local phase="$1" scenario="$2" run_num="$3" concurrency="$4" prompt="$5"
    local continue_flag="${6:-}"
    local label="${phase}_${scenario}_claude_${run_num}"
    local stdout_file="$RESULTS_DIR/${label}.txt"
    local time_file="$RESULTS_DIR/${label}.time"
    local ts
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    if [[ "$continue_flag" == "--continue" ]]; then
        /usr/bin/time -l env -u CLAUDECODE sh -c 'claude --print --continue "$1" 2>/dev/null' _ "$prompt" \
            >"$stdout_file" 2>"$time_file" || true
    else
        /usr/bin/time -l env -u CLAUDECODE sh -c 'claude --print "$1" 2>/dev/null' _ "$prompt" \
            >"$stdout_file" 2>"$time_file" || true
    fi

    if [[ ! -s "$stdout_file" ]]; then
        echo "  ⚠ Claude produced empty output for $label"
    fi

    local parsed
    parsed=$(parse_time_output "$time_file")
    read -r wall user sys rss <<< "$parsed"

    echo "$phase,$scenario,$run_num,$concurrency,$wall,$user,$sys,$rss,$ts" >> "$CLAUDE_CSV"
    printf "    run %d: wall=%.2fs  cpu=%.2fs  rss=%s bytes\n" "$run_num" "$wall" "$(echo "$user + $sys" | bc)" "$rss"
}

# Run a single Herald invocation and record to CSV.
# Args: phase scenario run_num concurrency prompt [session_id]
run_herald_single() {
    local phase="$1" scenario="$2" run_num="$3" concurrency="$4" prompt="$5"
    local session_id="${6:-}"
    local label="${phase}_${scenario}_herald_${run_num}"
    local stdout_file="$RESULTS_DIR/${label}.txt"
    local time_file="$RESULTS_DIR/${label}.time"
    local peak_file="$RESULTS_DIR/${label}.peak"
    local ts
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    local rss_before
    rss_before=$(get_daemon_rss_kb)

    start_rss_sampler "$peak_file"

    if [[ -n "$session_id" ]]; then
        /usr/bin/time -l sh -c \
            '"$1" run --mode bypassPermissions --herald "http://localhost:$2" -s "$3" "$4" < /dev/null 2>/dev/null' \
            _ "$HLD_BIN" "$HERALD_PORT" "$session_id" "$prompt" \
            >"$stdout_file" 2>"$time_file" || true
    else
        /usr/bin/time -l sh -c \
            '"$1" run --mode bypassPermissions --herald "http://localhost:$2" "$3" < /dev/null 2>/dev/null' \
            _ "$HLD_BIN" "$HERALD_PORT" "$prompt" \
            >"$stdout_file" 2>"$time_file" || true
    fi

    stop_rss_sampler

    local rss_after daemon_peak
    rss_after=$(get_daemon_rss_kb)
    daemon_peak=$(cat "$peak_file" 2>/dev/null || echo "0")

    if [[ ! -s "$stdout_file" ]]; then
        echo "  ⚠ Herald produced empty output for $label"
    fi

    local parsed
    parsed=$(parse_time_output "$time_file")
    read -r wall user sys rss <<< "$parsed"

    echo "$phase,$scenario,$run_num,$concurrency,$wall,$user,$sys,$rss,$rss_before,$rss_after,$daemon_peak,$ts" >> "$HERALD_CSV"
    printf "    run %d: wall=%.2fs  cpu=%.2fs  rss=%s bytes  daemon=%skb→%skb (peak %skb)\n" \
        "$run_num" "$wall" "$(echo "$user + $sys" | bc)" "$rss" "$rss_before" "$rss_after" "$daemon_peak"
}

# Run a single Claude invocation silently (for parallel burst).
# Args: prompt time_file stdout_file
run_claude_bg() {
    local prompt="$1" time_file="$2" stdout_file="$3"
    /usr/bin/time -l env -u CLAUDECODE sh -c 'claude --print "$1" 2>/dev/null' _ "$prompt" \
        >"$stdout_file" 2>"$time_file" || true
}

# Run a single Herald invocation silently (for parallel burst).
# Args: prompt time_file stdout_file
run_herald_bg() {
    local prompt="$1" time_file="$2" stdout_file="$3"
    /usr/bin/time -l sh -c \
        '"$1" run --mode bypassPermissions --herald "http://localhost:$2" "$3" < /dev/null 2>/dev/null' \
        _ "$HLD_BIN" "$HERALD_PORT" "$prompt" \
        >"$stdout_file" 2>"$time_file" || true
}

# ── Phase 1: Cold Start ─────────────────────────────────────────────────────
run_phase_cold() {
    logn "═══════════════════════════════════════════════════════════"
    log  "PHASE: COLD START ($RUNS runs per scenario)"
    log  "═══════════════════════════════════════════════════════════"

    local idx=0
    for scenario in "${SCENARIO_NAMES[@]}"; do
        prompt="${SCENARIO_PROMPTS[$idx]}"
        idx=$((idx + 1))
        logn "Scenario: $scenario"
        echo "  Prompt: ${prompt:0:60}..."

        if [[ "$SKIP_CLAUDE" == false ]]; then
            echo "  Claude Code:"
            for ((r=1; r<=RUNS; r++)); do
                run_claude_single "cold" "$scenario" "$r" 1 "$prompt"
            done
        fi

        if [[ "$SKIP_HERALD" == false ]]; then
            echo "  Herald:"
            for ((r=1; r<=RUNS; r++)); do
                run_herald_single "cold" "$scenario" "$r" 1 "$prompt"
            done
        fi
    done
}

# ── Phase 2: Warm Session ───────────────────────────────────────────────────
run_phase_warm() {
    logn "═══════════════════════════════════════════════════════════"
    log  "PHASE: WARM SESSION ($RUNS runs per scenario)"
    log  "═══════════════════════════════════════════════════════════"

    local idx=0
    for scenario in "${SCENARIO_NAMES[@]}"; do
        prompt="${SCENARIO_PROMPTS[$idx]}"
        idx=$((idx + 1))
        logn "Scenario: $scenario"
        echo "  Prompt: ${prompt:0:60}..."

        # ── Claude warm ──
        if [[ "$SKIP_CLAUDE" == false ]]; then
            echo "  Claude Code (warm):"
            # Warmup: create a session
            log "  Creating session (warmup)..."
            env -u CLAUDECODE claude --print "$WARMUP_PROMPT" >/dev/null 2>&1 || true

            for ((r=1; r<=RUNS; r++)); do
                run_claude_single "warm" "$scenario" "$r" 1 "$prompt" --continue
            done
        fi

        # ── Herald warm ──
        if [[ "$SKIP_HERALD" == false ]]; then
            echo "  Herald (warm):"
            # Warmup: create a session via hld
            log "  Creating session (warmup)..."
            "$HLD_BIN" run --mode bypassPermissions --herald "http://localhost:$HERALD_PORT" "$WARMUP_PROMPT" \
                < /dev/null >/dev/null 2>&1 || true

            # Get the most recent session ID from the daemon
            local session_id
            session_id=$(curl -s "http://localhost:$HERALD_PORT/sessions" | \
                python3 -c "import sys,json; r=json.load(sys.stdin); ss=r.get('data',r) if isinstance(r,dict) else r; print(ss[-1]['id'])" 2>/dev/null || echo "")

            if [[ -z "$session_id" ]]; then
                echo "  ⚠ Could not retrieve session ID, skipping warm Herald for $scenario"
                continue
            fi
            log "  Session ID: ${session_id:0:12}..."

            for ((r=1; r<=RUNS; r++)); do
                run_herald_single "warm" "$scenario" "$r" 1 "$prompt" "$session_id"
            done
        fi
    done
}

# ── Phase 3: Parallel / Concurrent Sessions ─────────────────────────────────
run_phase_parallel() {
    logn "═══════════════════════════════════════════════════════════"
    log  "PHASE: PARALLEL (simple_text, $RUNS runs per concurrency level)"
    log  "Concurrency levels: $CONCURRENCY_LEVELS"
    log  "═══════════════════════════════════════════════════════════"

    local prompt="${SCENARIO_PROMPTS[0]}"
    IFS=',' read -ra C_LEVELS <<< "$CONCURRENCY_LEVELS"

    for C in "${C_LEVELS[@]}"; do
        C=$(echo "$C" | tr -d ' ')
        logn "Concurrency: C=$C"

        # ── Claude parallel ──
        if [[ "$SKIP_CLAUDE" == false ]]; then
            echo "  Claude Code (C=$C):"
            for ((r=1; r<=RUNS; r++)); do
                local pids=()
                local burst_start
                burst_start=$(python3 -c "import time; print(time.time())")

                for ((i=1; i<=C; i++)); do
                    local label="parallel_simple_text_claude_r${r}_c${i}"
                    local tf="$RESULTS_DIR/${label}.time"
                    local sf="$RESULTS_DIR/${label}.txt"
                    run_claude_bg "$prompt" "$tf" "$sf" &
                    pids+=($!)
                done

                # Wait for all to finish
                for pid in "${pids[@]}"; do
                    wait "$pid" 2>/dev/null || true
                done

                local burst_end
                burst_end=$(python3 -c "import time; print(time.time())")
                local total_wall
                total_wall=$(python3 -c "print(f'{$burst_end - $burst_start:.3f}')")

                # Sum up per-process RSS
                local total_rss=0
                for ((i=1; i<=C; i++)); do
                    local label="parallel_simple_text_claude_r${r}_c${i}"
                    local tf="$RESULTS_DIR/${label}.time"
                    if [[ -f "$tf" ]]; then
                        local parsed
                        parsed=$(parse_time_output "$tf")
                        read -r _w _u _s prss <<< "$parsed"
                        total_rss=$((total_rss + prss))
                    fi
                done

                local ts
                ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
                echo "parallel,simple_text,$r,$C,$total_wall,0,0,$total_rss,$ts" >> "$CLAUDE_CSV"
                local throughput
                throughput=$(python3 -c "print(f'{$C / $total_wall:.2f}')" 2>/dev/null || echo "?")
                printf "    run %d: wall=%.3fs  total_rss=%s bytes  throughput=%s req/s\n" \
                    "$r" "$total_wall" "$total_rss" "$throughput"
            done
        fi

        # ── Herald parallel ──
        if [[ "$SKIP_HERALD" == false ]]; then
            echo "  Herald (C=$C):"
            for ((r=1; r<=RUNS; r++)); do
                local pids=()
                local rss_before
                rss_before=$(get_daemon_rss_kb)

                local peak_file="$RESULTS_DIR/parallel_simple_text_herald_r${r}_c${C}.peak"
                start_rss_sampler "$peak_file"

                local burst_start
                burst_start=$(python3 -c "import time; print(time.time())")

                for ((i=1; i<=C; i++)); do
                    local label="parallel_simple_text_herald_r${r}_c${i}"
                    local tf="$RESULTS_DIR/${label}.time"
                    local sf="$RESULTS_DIR/${label}.txt"
                    run_herald_bg "$prompt" "$tf" "$sf" &
                    pids+=($!)
                done

                for pid in "${pids[@]}"; do
                    wait "$pid" 2>/dev/null || true
                done

                local burst_end
                burst_end=$(python3 -c "import time; print(time.time())")
                local total_wall
                total_wall=$(python3 -c "print(f'{$burst_end - $burst_start:.3f}')")

                stop_rss_sampler

                local rss_after daemon_peak
                rss_after=$(get_daemon_rss_kb)
                daemon_peak=$(cat "$peak_file" 2>/dev/null || echo "0")

                # Sum up per-process RSS (hld client processes)
                local total_rss=0
                for ((i=1; i<=C; i++)); do
                    local label="parallel_simple_text_herald_r${r}_c${i}"
                    local tf="$RESULTS_DIR/${label}.time"
                    if [[ -f "$tf" ]]; then
                        local parsed
                        parsed=$(parse_time_output "$tf")
                        read -r _w _u _s prss <<< "$parsed"
                        total_rss=$((total_rss + prss))
                    fi
                done

                local ts
                ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
                echo "parallel,simple_text,$r,$C,$total_wall,0,0,$total_rss,$rss_before,$rss_after,$daemon_peak,$ts" >> "$HERALD_CSV"
                local throughput
                throughput=$(python3 -c "print(f'{$C / $total_wall:.2f}')" 2>/dev/null || echo "?")
                printf "    run %d: wall=%.3fs  total_rss=%s bytes  daemon=%skb→%skb (peak %skb)  throughput=%s req/s\n" \
                    "$r" "$total_wall" "$total_rss" "$rss_before" "$rss_after" "$daemon_peak" "$throughput"
            done
        fi
    done
}

# ── Start Herald Daemon ──────────────────────────────────────────────────────
if [[ "$SKIP_HERALD" == false ]]; then
    logn "Starting Herald daemon..."
    start_herald_daemon
fi

# ── Warmup (not recorded) ───────────────────────────────────────────────────
logn "Warmup (not recorded)..."

if [[ "$SKIP_CLAUDE" == false ]]; then
    log "Warming up Claude..."
    env -u CLAUDECODE claude --print "$WARMUP_PROMPT" >/dev/null 2>&1 || true
fi

if [[ "$SKIP_HERALD" == false ]]; then
    log "Warming up Herald..."
    "$HLD_BIN" run --mode bypassPermissions --herald "http://localhost:$HERALD_PORT" "$WARMUP_PROMPT" \
        < /dev/null >/dev/null 2>&1 || true
fi

# ── Run Phases ───────────────────────────────────────────────────────────────
case "$PHASE" in
    cold)
        run_phase_cold
        ;;
    warm)
        run_phase_warm
        ;;
    parallel)
        run_phase_parallel
        ;;
    all)
        run_phase_cold
        run_phase_warm
        run_phase_parallel
        ;;
esac

# ── Summary ──────────────────────────────────────────────────────────────────
print_phase_summary() {
    local phase="$1" title="$2"

    echo ""
    echo "═══════════════════════════════════════════════════════════"
    echo " $title"
    echo "═══════════════════════════════════════════════════════════"
    echo ""

    for scenario in "${SCENARIO_NAMES[@]}"; do
        echo "Scenario: $scenario"
        echo "─────────────────────────────────────────────────────────"

        # Claude averages
        c_wall="—" c_rss="—" c_user="—"
        if [[ "$SKIP_CLAUDE" == false ]]; then
            c_wall=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$5; n++} END {if(n>0) printf "%.2f", sum/n; else print "—"}' "$CLAUDE_CSV")
            c_user=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$6; n++} END {if(n>0) printf "%.2f", sum/n; else print "0"}' "$CLAUDE_CSV")
            c_sys=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$7; n++} END {if(n>0) printf "%.2f", sum/n; else print "0"}' "$CLAUDE_CSV")
            c_rss_raw=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$8; n++} END {if(n>0) printf "%.0f", sum/n; else print "0"}' "$CLAUDE_CSV")
            c_rss=$(echo "$c_rss_raw" | awk '{printf "%.0f", $1/1048576}')
        fi

        # Herald averages
        h_wall="—" h_rss="—" h_user="—"
        if [[ "$SKIP_HERALD" == false ]]; then
            h_wall=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$5; n++} END {if(n>0) printf "%.2f", sum/n; else print "—"}' "$HERALD_CSV")
            h_user=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$6; n++} END {if(n>0) printf "%.2f", sum/n; else print "0"}' "$HERALD_CSV")
            h_sys=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$7; n++} END {if(n>0) printf "%.2f", sum/n; else print "0"}' "$HERALD_CSV")
            h_rss_raw=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {sum+=$8; n++} END {if(n>0) printf "%.0f", sum/n; else print "0"}' "$HERALD_CSV")
            h_rss=$(echo "$h_rss_raw" | awk '{printf "%.0f", $1/1048576}')
            h_daemon_peak=$(awk -F, -v p="$phase" -v s="$scenario" '$1==p && $2==s {if($11+0>max+0) max=$11} END {printf "%.0f", max/1024}' "$HERALD_CSV")
        fi

        printf "%-12s %-12s %-12s" "" "Claude" "Herald"
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false ]]; then
            printf " %-12s %-8s" "Diff" "Speedup"
        fi
        echo ""

        # Wall time
        printf "%-12s %-12s %-12s" "Wall avg" "${c_wall}s" "${h_wall}s"
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false && "$c_wall" != "—" && "$h_wall" != "—" ]]; then
            diff_wall=$(echo "$c_wall - $h_wall" | bc 2>/dev/null || echo "?")
            if [[ "$h_wall" != "0" ]]; then
                speedup=$(echo "scale=2; $c_wall / $h_wall" | bc 2>/dev/null || echo "?")
                printf " %-12s %-8s" "-${diff_wall}s" "${speedup}x"
            fi
        fi
        echo ""

        # RSS — for Herald, show total = daemon peak + hld client
        h_total_rss="—"
        if [[ "$SKIP_HERALD" == false && "$h_rss" != "—" && "$h_daemon_peak" != "—" ]]; then
            h_total_rss=$((${h_rss:-0} + ${h_daemon_peak:-0}))
        fi
        printf "%-12s %-12s %-12s" "RSS total" "${c_rss} MB" "${h_total_rss} MB"
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false && "$c_rss" != "—" && "$h_total_rss" != "—" ]]; then
            if [[ "$h_total_rss" != "0" && "${h_total_rss:-0}" -gt 0 ]] 2>/dev/null; then
                diff_rss=$((${c_rss:-0} - ${h_total_rss:-0})) 2>/dev/null || diff_rss="?"
                rss_ratio=$(echo "scale=1; $c_rss / $h_total_rss" | bc 2>/dev/null || echo "?")
                printf " %-12s %-8s" "-${diff_rss} MB" "${rss_ratio}x"
            fi
        fi
        echo ""

        # CPU user
        printf "%-12s %-12s %-12s" "CPU user" "${c_user}s" "${h_user}s"
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false && "$c_user" != "—" && "$h_user" != "—" ]]; then
            diff_user=$(echo "$c_user - $h_user" | bc 2>/dev/null || echo "?")
            printf " %-12s" "-${diff_user}s"
        fi
        echo ""

        if [[ "$SKIP_HERALD" == false ]]; then
            echo "  (Herald: daemon ${h_daemon_peak:-?} MB + hld client ${h_rss:-?} MB)"
        fi

        echo ""
    done
}

print_parallel_summary() {
    echo ""
    echo "═══════════════════════════════════════════════════════════"
    echo " PARALLEL (simple_text)"
    echo "═══════════════════════════════════════════════════════════"
    echo ""

    IFS=',' read -ra C_LEVELS <<< "$CONCURRENCY_LEVELS"

    printf "%-14s %-14s %-14s %-10s\n" "Metric" "Claude" "Herald" "Speedup"
    echo "─────────────────────────────────────────────────────────"

    for C in "${C_LEVELS[@]}"; do
        C=$(echo "$C" | tr -d ' ')

        # Claude averages for this concurrency level
        c_wall="—" c_rss_total="—"
        if [[ "$SKIP_CLAUDE" == false ]]; then
            c_wall=$(awk -F, -v c="$C" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$5; n++} END {if(n>0) printf "%.2f", sum/n; else print "—"}' "$CLAUDE_CSV")
            c_rss_total=$(awk -F, -v c="$C" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$8; n++} END {if(n>0) printf "%.0f", sum/n; else print "0"}' "$CLAUDE_CSV")
            c_rss_mb=$(echo "$c_rss_total" | awk '{printf "%.0f", $1/1048576}')
        fi

        # Herald averages for this concurrency level
        h_wall="—" h_rss_clients="—" h_daemon_peak="—" h_total_mb="—"
        if [[ "$SKIP_HERALD" == false ]]; then
            h_wall=$(awk -F, -v c="$C" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$5; n++} END {if(n>0) printf "%.2f", sum/n; else print "—"}' "$HERALD_CSV")
            h_rss_clients_raw=$(awk -F, -v c="$C" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$8; n++} END {if(n>0) printf "%.0f", sum/n; else print "0"}' "$HERALD_CSV")
            h_rss_clients_mb=$(echo "$h_rss_clients_raw" | awk '{printf "%.0f", $1/1048576}')
            h_daemon_peak=$(awk -F, -v c="$C" '$1=="parallel" && $2=="simple_text" && $4==c {if($11+0>max+0) max=$11} END {printf "%.0f", max/1024}' "$HERALD_CSV")
            h_total_mb=$((h_rss_clients_mb + h_daemon_peak))
        fi

        # Wall time row
        wall_speedup="—"
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false && "$c_wall" != "—" && "$h_wall" != "—" && "$h_wall" != "0" ]]; then
            wall_speedup=$(echo "scale=1; $c_wall / $h_wall" | bc 2>/dev/null || echo "?")
        fi
        printf "%-14s %-14s %-14s %-10s\n" "C=$C wall" "${c_wall}s" "${h_wall}s" "${wall_speedup}x"

        # RSS row — total for both sides
        if [[ "$SKIP_CLAUDE" == false && "$SKIP_HERALD" == false ]]; then
            rss_speedup="—"
            if [[ "$h_total_mb" != "—" && "$h_total_mb" != "0" && "${c_rss_mb:-0}" -gt 0 ]] 2>/dev/null; then
                rss_speedup=$(echo "scale=1; $c_rss_mb / $h_total_mb" | bc 2>/dev/null || echo "?")
            fi
            printf "%-14s %-14s %-14s %-10s\n" "C=$C RSS tot" "${c_rss_mb:-?} MB" "${h_total_mb} MB" "${rss_speedup}x"
            printf "%-14s %-14s %-14s\n" "" "(${C}x node)" "(daemon ${h_daemon_peak}+cli ${h_rss_clients_mb})"
        fi
    done

    echo ""

    # Throughput line for highest concurrency
    local max_c="${C_LEVELS[${#C_LEVELS[@]}-1]}"
    max_c=$(echo "$max_c" | tr -d ' ')
    if [[ "$SKIP_CLAUDE" == false ]]; then
        local ct
        ct=$(awk -F, -v c="$max_c" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$5; n++} END {if(n>0) printf "%.2f", c/sum*n; else print "?"}' "$CLAUDE_CSV")
        printf "Throughput C=%s: Claude %s req/s" "$max_c" "$ct"
    fi
    if [[ "$SKIP_HERALD" == false ]]; then
        local ht
        ht=$(awk -F, -v c="$max_c" '$1=="parallel" && $2=="simple_text" && $4==c {sum+=$5; n++} END {if(n>0) printf "%.2f", c/sum*n; else print "?"}' "$HERALD_CSV")
        if [[ "$SKIP_CLAUDE" == false ]]; then
            printf ", Herald %s req/s" "$ht"
        else
            printf "Throughput C=%s: Herald %s req/s" "$max_c" "$ht"
        fi
    fi
    echo ""
    echo ""
}

logn "═══════════════════════════════════════════════════════════"
log  "BENCHMARK SUMMARY"
log  "═══════════════════════════════════════════════════════════"

# Print summaries for phases that were run
case "$PHASE" in
    cold)
        print_phase_summary "cold" "COLD START"
        ;;
    warm)
        print_phase_summary "warm" "WARM SESSION"
        ;;
    parallel)
        print_parallel_summary
        ;;
    all)
        print_phase_summary "cold" "COLD START"
        print_phase_summary "warm" "WARM SESSION"
        print_parallel_summary
        ;;
esac

log "Raw data: $RESULTS_DIR"
log "Done."
