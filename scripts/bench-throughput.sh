#!/usr/bin/env bash
set -euo pipefail

bytes="${ONIBI_BENCH_PTY_BYTES:-1048576}"
count="${ONIBI_BENCH_COUNT:-5}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/bench-throughput.sh [--bytes N] [--count N]

Environment:
  ONIBI_BENCH_PTY_BYTES  payload bytes per benchmark iteration (default 1048576)
  ONIBI_BENCH_COUNT      benchmark repetitions (default 5)
EOF
}

while (($#)); do
  case "$1" in
    --bytes) bytes="${2:-}"; shift 2 ;;
    --count) count="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

case "$bytes" in ''|*[!0-9]*) echo "--bytes must be a positive integer" >&2; exit 2 ;; esac
case "$count" in ''|*[!0-9]*) echo "--count must be a positive integer" >&2; exit 2 ;; esac
if ((bytes <= 0 || count <= 0)); then
  echo "--bytes and --count must be > 0" >&2
  exit 2
fi

ONIBI_BENCH_PTY_BYTES="$bytes" go test ./internal/web -run '^$' -bench '^BenchmarkWSPTYThroughput$' -benchtime=1x -count="$count"
