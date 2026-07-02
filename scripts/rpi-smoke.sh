#!/usr/bin/env bash
set -euo pipefail

max_binary_bytes="${ONIBI_RPI_MAX_BINARY_BYTES:-14680064}"
max_rss_kib="${ONIBI_RPI_MAX_RSS_KIB:-81920}"
idle_seconds="${ONIBI_RPI_IDLE_SECONDS:-5}"
target="${ONIBI_RPI_TARGET:-}"
binary="${ONIBI_RPI_BINARY:-}"
remote_dir="${ONIBI_RPI_REMOTE_DIR:-/tmp/onibi-rpi-smoke.$$}"
size_only=false
keep_remote=false

usage() {
  cat >&2 <<'EOF'
usage: scripts/rpi-smoke.sh [--size-only] [--binary <path>] [--target user@host] [--remote-dir <path>] [--keep-remote]

Gates:
  linux/arm64 stripped onibi binary <= ONIBI_RPI_MAX_BINARY_BYTES (default 14680064, 14 MiB)
  idle RSS on target <= ONIBI_RPI_MAX_RSS_KIB (default 81920, 80 MiB)

Environment:
  ONIBI_RPI_TARGET=user@raspberrypi.local
  ONIBI_RPI_BINARY=/path/to/linux-arm64/onibi
  ONIBI_RPI_IDLE_SECONDS=5
EOF
}

while (($#)); do
  case "$1" in
    --size-only) size_only=true; shift ;;
    --binary) binary="${2:-}"; shift 2 ;;
    --target) target="${2:-}"; shift 2 ;;
    --remote-dir) remote_dir="${2:-}"; shift 2 ;;
    --keep-remote) keep_remote=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [[ -z "$binary" ]]; then
  binary="$tmp/onibi"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o "$binary" ./cmd/onibi
fi

if [[ ! -x "$binary" ]]; then
  echo "binary not executable: $binary" >&2
  exit 1
fi

size="$(wc -c < "$binary" | tr -d '[:space:]')"
if ((size > max_binary_bytes)); then
  echo "linux/arm64 onibi size ${size} exceeds ${max_binary_bytes} bytes" >&2
  exit 1
fi
echo "binary size ok: ${size}/${max_binary_bytes} bytes"

if "$size_only"; then
  exit 0
fi
if [[ -z "$target" ]]; then
  echo "missing --target or ONIBI_RPI_TARGET for RSS gate; use --size-only for local size gate" >&2
  exit 2
fi

ssh "$target" 'sh -s' -- "$remote_dir" <<'REMOTE'
set -eu
rm -rf "$1"
mkdir -p "$1"
REMOTE
scp "$binary" "$target:$remote_dir/onibi" >/dev/null
cleanup_remote() {
  if ! "$keep_remote"; then
    ssh "$target" 'sh -s' -- "$remote_dir" >/dev/null 2>&1 <<'REMOTE' || true
set -eu
rm -rf "$1"
REMOTE
  fi
}
trap 'rm -rf "$tmp"; cleanup_remote' EXIT

ssh "$target" 'sh -s' -- "$remote_dir" "$max_rss_kib" "$idle_seconds" <<'REMOTE'
set -eu
dir="$1"
max_rss="$2"
idle="$3"
bin="$dir/onibi"
home="$dir/home"
runtime="$dir/run"
data="$home/.local/share"
mkdir -p "$home" "$runtime" "$data"
chmod 0755 "$bin"
HOME="$home" XDG_DATA_HOME="$data" XDG_RUNTIME_DIR="$runtime" "$bin" run --log-file "$dir/onibi.log" >"$dir/stdout.log" 2>"$dir/stderr.log" &
pid="$!"
cleanup() {
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
sleep "$idle"
if ! kill -0 "$pid" >/dev/null 2>&1; then
  cat "$dir/stderr.log" >&2 || true
  echo "onibi exited before RSS measurement" >&2
  exit 1
fi
rss="$(ps -o rss= -p "$pid" | tr -d '[:space:]')"
if [ -z "$rss" ]; then
  echo "unable to read RSS for pid $pid" >&2
  exit 1
fi
if [ "$rss" -gt "$max_rss" ]; then
  echo "idle RSS ${rss} KiB exceeds ${max_rss} KiB" >&2
  exit 1
fi
echo "idle RSS ok: ${rss}/${max_rss} KiB"
REMOTE
