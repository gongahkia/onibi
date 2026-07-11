#!/usr/bin/env bash
set -euo pipefail

tolerance_pct="${ONIBI_BENCH_TOLERANCE_PCT:-10}"
rss_tolerance_mib="${ONIBI_BENCH_RSS_TOLERANCE_MIB:-5}"
output_dir="${ONIBI_BENCH_TOLERANCE_DIR:-}"
cold_iterations="${ONIBI_BENCH_TOLERANCE_COLDSTART_ITERATIONS:-5}"
idle_seconds="${ONIBI_BENCH_TOLERANCE_IDLE_SECONDS:-5}"
pty_bytes="${ONIBI_BENCH_PTY_BYTES:-1048576}"
bench_count="${ONIBI_BENCH_COUNT:-5}"
approval_count="${ONIBI_BENCH_APPROVAL_COUNT:-$bench_count}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/bench-tolerance.sh [--tolerance-percent N] [--output-dir DIR]

Reruns local-only benchmark scripts twice and fails when median/value drift exceeds tolerance.

Environment:
  ONIBI_BENCH_TOLERANCE_PCT                   percent drift allowed (default 10)
  ONIBI_BENCH_RSS_TOLERANCE_MIB               RSS absolute drift allowed (default 5)
  ONIBI_BENCH_TOLERANCE_DIR                   directory for run logs
  ONIBI_BENCH_TOLERANCE_COLDSTART_ITERATIONS  cold-start iterations (default 5)
  ONIBI_BENCH_TOLERANCE_IDLE_SECONDS          RSS idle wait (default 5)
  ONIBI_BENCH_PTY_BYTES                       PTY payload bytes (default 1048576)
  ONIBI_BENCH_COUNT                           throughput repetitions (default 5)
  ONIBI_BENCH_APPROVAL_COUNT                  approval repetitions (default ONIBI_BENCH_COUNT)
EOF
}

while (($#)); do
  case "$1" in
    --tolerance-percent) tolerance_pct="${2:-}"; shift 2 ;;
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

for pair in \
  "tolerance-percent:$tolerance_pct" \
  "rss tolerance MiB:$rss_tolerance_mib" \
  "cold-start iterations:$cold_iterations" \
  "idle seconds:$idle_seconds" \
  "pty bytes:$pty_bytes" \
  "bench count:$bench_count" \
  "approval count:$approval_count"; do
  name="${pair%%:*}"
  value="${pair#*:}"
  case "$value" in ''|*[!0-9]*) echo "$name must be a positive integer" >&2; exit 2 ;; esac
  if ((value <= 0)); then
    echo "$name must be > 0" >&2
    exit 2
  fi
done

command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 1; }

cleanup=""
if [[ -z "$output_dir" ]]; then
  output_dir="$(mktemp -d /tmp/onibi-bench-tolerance.XXXXXX)"
  cleanup="$output_dir"
else
  mkdir -p "$output_dir"
fi
if [[ -n "$cleanup" ]]; then
  trap 'rm -rf "$cleanup"' EXIT
fi

run_suite() {
  local out="$1"
  {
    scripts/bench-coldstart.sh --iterations "$cold_iterations"
    scripts/bench-idle-rss.sh --idle-seconds "$idle_seconds"
    scripts/bench-throughput.sh --bytes "$pty_bytes" --count "$bench_count"
    scripts/bench-approval-rtt.sh --count "$approval_count"
  } | tee "$out"
}

run1="$output_dir/run-1.txt"
run2="$output_dir/run-2.txt"
run_suite "$run1"
run_suite "$run2"

python3 - "$tolerance_pct" "$rss_tolerance_mib" "$run1" "$run2" <<'PY'
import re
import statistics
import sys

tol = float(sys.argv[1])
rss_tol = float(sys.argv[2])

def parse(path):
    metrics = {}
    throughput = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line.startswith("cold_start_health_ms "):
                match = re.search(r"\bmedian=([0-9.]+)\b", line)
                if match:
                    metrics["cold_start_health_median_ms"] = float(match.group(1))
            elif line.startswith("approval_decision_rtt_ms "):
                match = re.search(r"\bmedian=([0-9.]+)\b", line)
                if match:
                    metrics["approval_decision_rtt_median_ms"] = float(match.group(1))
            else:
                match = re.match(r"^(daemon_only|web_active_session)_rss_mib\s+([0-9.]+)$", line)
                if match:
                    metrics[f"{match.group(1)}_rss_mib"] = float(match.group(2))
                if line.startswith("BenchmarkWSPTYThroughput"):
                    match = re.search(r"\s([0-9.]+)\s+MiB/s\b", line)
                    if match:
                        throughput.append(float(match.group(1)))
    if throughput:
        metrics["ws_pty_throughput_median_mib_s"] = statistics.median(throughput)
    return metrics

left = parse(sys.argv[3])
right = parse(sys.argv[4])
required = [
    "cold_start_health_median_ms",
    "daemon_only_rss_mib",
    "web_active_session_rss_mib",
    "ws_pty_throughput_median_mib_s",
    "approval_decision_rtt_median_ms",
]
missing = [name for name in required if name not in left or name not in right]
if missing:
    print("missing metrics: " + ", ".join(missing), file=sys.stderr)
    sys.exit(1)

failed = False
for name in required:
    a = left[name]
    b = right[name]
    avg = (a + b) / 2
    abs_delta = abs(a - b)
    delta = 0.0 if avg == 0 else abs(a - b) / avg * 100
    ok = delta <= tol or (name.endswith("_rss_mib") and abs_delta <= rss_tol)
    status = "ok" if ok else "fail"
    if name.endswith("_rss_mib"):
        print(f"{name} run1={a:.3f} run2={b:.3f} delta_pct={delta:.2f} tolerance_pct={tol:.2f} abs_delta_mib={abs_delta:.3f} rss_tolerance_mib={rss_tol:.3f} {status}")
    else:
        print(f"{name} run1={a:.3f} run2={b:.3f} delta_pct={delta:.2f} tolerance_pct={tol:.2f} {status}")
    failed = failed or not ok
if failed:
    sys.exit(1)
PY
