#!/usr/bin/env bash
set -euo pipefail

forbidden_re='^(brew|curl|wget|fetch|aria2c|go|npm|npx|node|python|python3|pip|pip3|ruby|gem|bundle|mkcert|cloudflared|ngrok|tailscale)$'

die() {
  echo "no-runtime-deps-check: $*" >&2
  exit 1
}

check_trace() {
  local trace="$1"
  if [ ! -s "$trace" ]; then
    die "trace is empty: $trace"
  fi
  local hits
  hits="$(awk -F'"' '/execve(at)?\(/ { n=split($2, p, "/"); print p[n] }' "$trace" | grep -E "$forbidden_re" | sort -u || true)"
  if [ -n "$hits" ]; then
    echo "no-runtime-deps-check: forbidden runtime dependency executables observed:" >&2
    echo "$hits" >&2
    exit 1
  fi
}

if [ -n "${ONIBI_RUNTIME_TRACE_FILE:-}" ]; then
  check_trace "$ONIBI_RUNTIME_TRACE_FILE"
  echo "no-runtime-deps-check: passed trace $ONIBI_RUNTIME_TRACE_FILE"
  exit 0
fi

timeout_bin="$(command -v timeout || command -v gtimeout || true)"
[ -n "$timeout_bin" ] || die "timeout not found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

binary="${ONIBI_BINARY:-}"
if [ -z "$binary" ]; then
  if [ -x "./bin/onibi" ]; then
    binary="./bin/onibi"
  else
    command -v go >/dev/null 2>&1 || die "ONIBI_BINARY unset and go not found"
    go build -o "$tmp/onibi" ./cmd/onibi
    binary="$tmp/onibi"
  fi
fi
[ -x "$binary" ] || die "binary is not executable: $binary"

duration="${ONIBI_RUNTIME_AUDIT_SECONDS:-30}"
case "$duration" in
  ''|*[!0-9]*) die "ONIBI_RUNTIME_AUDIT_SECONDS must be integer seconds" ;;
esac

home="$tmp/home"
runtime="$tmp/run"
mkdir -p "$home" "$runtime" "$home/.local/share/onibi" "$home/.config" "$home/.cache"
chmod 700 "$home" "$runtime" "$home/.local/share/onibi"

port="$((20000 + RANDOM % 20000))"
cat >"$home/.local/share/onibi/config.yaml" <<EOF
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

trace="$tmp/trace.log"
stdout="$tmp/stdout.log"
stderr="$tmp/stderr.log"
status=0

if command -v strace >/dev/null 2>&1; then
  set +e
  HOME="$home" XDG_DATA_HOME="$home/.local/share" XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$home/.cache" XDG_RUNTIME_DIR="$runtime" \
    "$timeout_bin" -s INT "$duration" strace -f -qq -e trace=execve,execveat -o "$trace" \
    "$binary" up --transport=lan --no-qr --shell sh --no-login-shell --log-file "$tmp/onibi.log" >"$stdout" 2>"$stderr"
  status=$?
  set -e
elif command -v dtruss >/dev/null 2>&1; then
  [ "$(id -u)" -eq 0 ] || die "dtruss requires root; run with sudo or use Linux strace"
  set +e
  HOME="$home" XDG_DATA_HOME="$home/.local/share" XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$home/.cache" XDG_RUNTIME_DIR="$runtime" \
    "$timeout_bin" -s INT "$duration" dtruss -f -t execve \
    "$binary" up --transport=lan --no-qr --shell sh --no-login-shell --log-file "$tmp/onibi.log" >"$stdout" 2>"$trace"
  status=$?
  set -e
else
  die "need strace or dtruss"
fi

case "$status" in
  0|124|130|143) ;;
  *)
    cat "$stdout" >&2 || true
    cat "$stderr" >&2 || true
    die "onibi start exited with status $status"
    ;;
esac

check_trace "$trace"
echo "no-runtime-deps-check: passed ${duration}s onibi start audit"
