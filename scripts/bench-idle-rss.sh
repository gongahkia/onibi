#!/usr/bin/env bash
set -euo pipefail

binary="${ONIBI_BENCH_BINARY:-}"
idle_seconds="${ONIBI_BENCH_IDLE_SECONDS:-5}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/bench-idle-rss.sh [--binary PATH] [--idle-seconds N]

Measures RSS for daemon-only and web+active-session modes.

Environment:
  ONIBI_BENCH_BINARY       onibi binary path
  ONIBI_BENCH_IDLE_SECONDS idle wait before sampling (default 5)
EOF
}

while (($#)); do
  case "$1" in
    --binary) binary="${2:-}"; shift 2 ;;
    --idle-seconds) idle_seconds="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

case "$idle_seconds" in ''|*[!0-9]*) echo "--idle-seconds must be a positive integer" >&2; exit 2 ;; esac
if ((idle_seconds <= 0)); then
  echo "--idle-seconds must be > 0" >&2
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

measure() {
  local name="$1"
  shift
  local run="$tmp/$name"
  local home="$run/home"
  local runtime="$run/run"
  local data="$home/.local/share"
  local config="$home/.config"
  local cache="$home/.cache"
  local mac_state="$home/Library/Application Support/onibi"
  mkdir -p "$data/onibi" "$mac_state" "$config" "$cache" "$runtime"
  chmod 700 "$home" "$runtime" "$data/onibi" "$mac_state"
  local port="$((21000 + RANDOM % 20000))"
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
  HOME="$home" XDG_DATA_HOME="$data" XDG_CONFIG_HOME="$config" XDG_CACHE_HOME="$cache" XDG_RUNTIME_DIR="$runtime" ONIBI_STORE_KEY_BACKEND=dotenv \
    "$binary" "$@" --log-file "$run/onibi.log" >"$run/stdout.log" 2>"$run/stderr.log" &
  local pid="$!"
  sleep "$idle_seconds"
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    cat "$run/stdout.log" >&2 || true
    cat "$run/stderr.log" >&2 || true
    echo "$name exited before RSS sample" >&2
    exit 1
  fi
  local rss_kib
  rss_kib="$(ps -o rss= -p "$pid" | tr -d '[:space:]')"
  kill -INT "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
  if [[ -z "$rss_kib" ]]; then
    echo "unable to read RSS for $name" >&2
    exit 1
  fi
  awk -v name="$name" -v kib="$rss_kib" 'BEGIN { printf "%s_rss_mib %.1f\n", name, kib / 1024 }'
}

measure daemon_only run
measure web_active_session up --transport=lan --no-qr --shell sh --no-login-shell
