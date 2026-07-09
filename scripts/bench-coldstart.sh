#!/usr/bin/env bash
set -euo pipefail

binary="${ONIBI_BENCH_BINARY:-}"
iterations="${ONIBI_BENCH_ITERATIONS:-5}"
timeout_seconds="${ONIBI_BENCH_TIMEOUT_SECONDS:-10}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/bench-coldstart.sh [--binary PATH] [--iterations N]

Measures process start -> HTTPS /healthz ready for `onibi up --transport=lan`.

Environment:
  ONIBI_BENCH_BINARY           onibi binary path
  ONIBI_BENCH_ITERATIONS       iterations (default 5)
  ONIBI_BENCH_TIMEOUT_SECONDS  per-iteration timeout (default 10)
EOF
}

while (($#)); do
  case "$1" in
    --binary) binary="${2:-}"; shift 2 ;;
    --iterations) iterations="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

case "$iterations" in ''|*[!0-9]*) echo "--iterations must be a positive integer" >&2; exit 2 ;; esac
if ((iterations <= 0)); then
  echo "--iterations must be > 0" >&2
  exit 2
fi

tmp="$(mktemp -d /tmp/onibi-bench.XXXXXX)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

if [[ -z "$binary" ]]; then
  if [[ -x ./bin/onibi ]]; then
    binary="./bin/onibi"
  else
    go build -o "$tmp/onibi" ./cmd/onibi
    binary="$tmp/onibi"
  fi
fi
[[ -x "$binary" ]] || { echo "binary is not executable: $binary" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl required" >&2; exit 1; }

values="$tmp/values.txt"
for i in $(seq 1 "$iterations"); do
  run="$tmp/run-$i"
  home="$run/home"
  runtime="$run/run"
  data="$home/.local/share"
  config="$home/.config"
  cache="$home/.cache"
  mac_state="$home/Library/Application Support/onibi"
  mkdir -p "$data/onibi" "$mac_state" "$config" "$cache" "$runtime"
  chmod 700 "$home" "$runtime" "$data/onibi" "$mac_state"
  port="$((21000 + RANDOM % 20000))"
  tee "$data/onibi/config.yaml" "$mac_state/config.yaml" >/dev/null <<EOF
shell:
  default: sh
  login: false
terminal:
  default: none
web:
  listen_addr: "127.0.0.1:$port"
transport:
  mode: lan
update:
  auto: false
EOF
  start_ms="$(python3 - <<'PY'
import time
print(time.monotonic_ns() // 1_000_000)
PY
)"
  HOME="$home" XDG_DATA_HOME="$data" XDG_CONFIG_HOME="$config" XDG_CACHE_HOME="$cache" XDG_RUNTIME_DIR="$runtime" ONIBI_STORE_KEY_BACKEND=dotenv \
    "$binary" up --transport=lan --no-qr --shell sh --no-login-shell --log-file "$run/onibi.log" >"$run/stdout.log" 2>"$run/stderr.log" &
  pid="$!"
  deadline=$((SECONDS + timeout_seconds))
  ready_ms=""
  while ((SECONDS < deadline)); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      cat "$run/stdout.log" >&2 || true
      cat "$run/stderr.log" >&2 || true
      echo "onibi exited before readiness" >&2
      exit 1
    fi
    if curl -kfsS --max-time 0.25 "https://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      ready_ms="$(python3 - <<'PY'
import time
print(time.monotonic_ns() // 1_000_000)
PY
)"
      break
    fi
    sleep 0.025
  done
  kill -INT "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
  if [[ -z "$ready_ms" ]]; then
    cat "$run/stdout.log" >&2 || true
    cat "$run/stderr.log" >&2 || true
    echo "timed out waiting for /healthz" >&2
    exit 1
  fi
  ms=$((ready_ms - start_ms))
  echo "$ms" | tee -a "$values"
done

python3 - "$values" <<'PY'
import statistics, sys
vals = [int(line.strip()) for line in open(sys.argv[1]) if line.strip()]
print(f"cold_start_health_ms min={min(vals)} median={statistics.median(vals):.0f} mean={statistics.mean(vals):.0f} max={max(vals)} n={len(vals)}")
PY
