#!/usr/bin/env bash
set -euo pipefail

max_binary_bytes="${ONIBI_SSH_MAX_BINARY_BYTES:-14680064}"
max_rss_kib="${ONIBI_SSH_MAX_RSS_KIB:-81920}"
idle_seconds="${ONIBI_SSH_IDLE_SECONDS:-5}"
sigint_timeout_seconds="${ONIBI_SSH_SIGINT_TIMEOUT_SECONDS:-5}"
target="${ONIBI_SSH_TARGET:-}"
binary="${ONIBI_SSH_BINARY:-}"
ssh_config="${ONIBI_SSH_CONFIG:-}"
remote_dir="${ONIBI_SSH_REMOTE_DIR:-/tmp/onibi-ssh-smoke.$$}"
build_tags="${ONIBI_SSH_BUILD_TAGS:-onibi_remote}"
gcflags="${ONIBI_SSH_GCFLAGS:-all=-l}"
size_only=false
keep_remote=false

usage() {
  cat >&2 <<'EOF'
usage: scripts/ssh-smoke.sh [--size-only] [--binary <path>] [--target user@host] [--ssh-config <path>] [--remote-dir <path>] [--keep-remote]

Gates:
  linux/arm64 stripped onibi binary <= ONIBI_SSH_MAX_BINARY_BYTES (default 14680064, 14 MiB)
  idle RSS on target <= ONIBI_SSH_MAX_RSS_KIB (default 81920, 80 MiB)
  SIGINT leaves no new onibi-* tmux sessions on target

Environment:
  ONIBI_SSH_TARGET=user@host
  ONIBI_SSH_BINARY=/path/to/linux-arm64/onibi
  ONIBI_SSH_CONFIG=/path/to/ssh_config
  ONIBI_SSH_BUILD_TAGS=onibi_remote
  ONIBI_SSH_GCFLAGS=all=-l
  ONIBI_SSH_IDLE_SECONDS=5
  ONIBI_SSH_SIGINT_TIMEOUT_SECONDS=5
EOF
}

while (($#)); do
  case "$1" in
    --size-only) size_only=true; shift ;;
    --binary) binary="${2:-}"; shift 2 ;;
    --target) target="${2:-}"; shift 2 ;;
    --ssh-config) ssh_config="${2:-}"; shift 2 ;;
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
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -tags "$build_tags" -gcflags "$gcflags" -ldflags='-s -w -buildid=' -o "$binary" ./cmd/onibi
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
  echo "missing --target or ONIBI_SSH_TARGET for RSS gate; use --size-only for local size gate" >&2
  exit 2
fi
if [[ -n "$ssh_config" && ! -f "$ssh_config" ]]; then
  echo "ssh config not found: $ssh_config" >&2
  exit 2
fi

ssh_cmd=(ssh)
scp_cmd=(scp)
if [[ -n "$ssh_config" ]]; then
  ssh_cmd+=(-F "$ssh_config")
  scp_cmd+=(-F "$ssh_config")
fi

"${ssh_cmd[@]}" "$target" 'sh -s' -- "$remote_dir" <<'REMOTE'
set -eu
rm -rf "$1"
mkdir -p "$1"
REMOTE
"${scp_cmd[@]}" "$binary" "$target:$remote_dir/onibi" >/dev/null
cleanup_remote() {
  if ! "$keep_remote"; then
    "${ssh_cmd[@]}" "$target" 'sh -s' -- "$remote_dir" >/dev/null 2>&1 <<'REMOTE' || true
set -eu
rm -rf "$1"
REMOTE
  fi
}
trap 'rm -rf "$tmp"; cleanup_remote' EXIT

"${ssh_cmd[@]}" "$target" 'sh -s' -- "$remote_dir" "$max_rss_kib" "$idle_seconds" "$sigint_timeout_seconds" <<'REMOTE'
set -eu
dir="$1"
max_rss="$2"
idle="$3"
sigint_timeout="$4"
bin="$dir/onibi"
home="$dir/home"
runtime="$dir/run"
data="$home/.local/share"
config="$home/.config"
cache="$home/.cache"
mkdir -p "$home" "$runtime" "$data" "$config" "$cache"
chmod 0755 "$bin"
list_onibi_tmux() {
  if command -v tmux >/dev/null 2>&1; then
    tmux list-sessions -F '#{session_name}' 2>/dev/null | grep '^onibi-' || true
  fi
}
new_tmux_sessions() {
  while IFS= read -r session; do
    [ -n "$session" ] || continue
    if ! grep -Fxq "$session" "$dir/tmux-before.txt"; then
      printf '%s\n' "$session"
    fi
  done < "$dir/tmux-after.txt"
}
list_onibi_tmux >"$dir/tmux-before.txt"
HOME="$home" XDG_DATA_HOME="$data" XDG_CONFIG_HOME="$config" XDG_CACHE_HOME="$cache" XDG_RUNTIME_DIR="$runtime" "$bin" up --transport=lan-loopback --no-qr --shell sh --no-login-shell --log-file "$dir/onibi.log" >"$dir/stdout.log" 2>"$dir/stderr.log" &
pid="$!"
cleanup() {
  kill -TERM "$pid" >/dev/null 2>&1 || true
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
kill -INT "$pid" >/dev/null 2>&1 || true
for _ in $(seq 1 "$sigint_timeout"); do
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if kill -0 "$pid" >/dev/null 2>&1; then
  cat "$dir/stderr.log" >&2 || true
  echo "onibi still running after SIGINT" >&2
  exit 1
fi
wait "$pid" >/dev/null 2>&1 || true
trap - EXIT INT TERM
list_onibi_tmux >"$dir/tmux-after.txt"
orphans="$(new_tmux_sessions)"
if [ -n "$orphans" ]; then
  echo "new onibi tmux sessions remained after SIGINT:" >&2
  printf '%s\n' "$orphans" >&2
  exit 1
fi
echo "SIGINT cleanup ok: no new onibi-* tmux sessions"
REMOTE
