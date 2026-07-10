#!/usr/bin/env bash
set -euo pipefail

count="${ONIBI_BENCH_COUNT:-5}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/bench-approval-rtt.sh [--count N]

Measures local approval queue request -> decide -> waiter delivery RTT.

Environment:
  ONIBI_BENCH_COUNT  benchmark repetitions (default 5)
EOF
}

while (($#)); do
  case "$1" in
    --count) count="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

case "$count" in ''|*[!0-9]*) echo "--count must be a positive integer" >&2; exit 2 ;; esac
if ((count <= 0)); then
  echo "--count must be > 0" >&2
  exit 2
fi
command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 1; }

tmp="$(mktemp -d /tmp/onibi-bench.XXXXXX)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

out="$tmp/approval-rtt.txt"
go test ./internal/approval -run '^$' -bench '^BenchmarkApprovalDecisionRoundTrip$' -benchtime=1x -count="$count" | tee "$out"
python3 - "$out" <<'PY'
import re, statistics, sys
vals = []
for line in open(sys.argv[1]):
    if not line.startswith("BenchmarkApprovalDecisionRoundTrip"):
        continue
    match = re.search(r"\s([0-9.]+)\s+ns/op", line)
    if match:
        vals.append(float(match.group(1)) / 1_000_000)
if vals:
    print(f"approval_decision_rtt_ms min={min(vals):.3f} median={statistics.median(vals):.3f} mean={statistics.mean(vals):.3f} max={max(vals):.3f} n={len(vals)}")
PY
