#!/usr/bin/env bash
set -euo pipefail

PORT="${ONIBI_PORT:-17894}"
TIME_LIMIT_SECONDS="${ONIBI_DRY_RUN_LIMIT_SECONDS:-300}"
STARTED_AT="$(date +%s)"
LOG_DIR="${ONIBI_DRY_RUN_LOG_DIR:-$PWD/test-results/dry-run}"
mkdir -p "$LOG_DIR"

elapsed() {
  local now
  now="$(date +%s)"
  echo $((now - STARTED_AT))
}

step() {
  printf '[%3ss] %s\n' "$(elapsed)" "$*"
}

fail_if_slow() {
  local total
  total="$(elapsed)"
  if [ "$total" -gt "$TIME_LIMIT_SECONDS" ]; then
    echo "dry-run exceeded ${TIME_LIMIT_SECONDS}s: ${total}s" >&2
    exit 2
  fi
}

wait_for_status() {
  local token="$1"
  local deadline=$((SECONDS + 45))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -fsS -H "Authorization: Bearer ${token}" "http://127.0.0.1:${PORT}/v1/status" >"${LOG_DIR}/status.json" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_healthz() {
  local deadline=$((SECONDS + 45))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

step "building headless binary"
cargo build --manifest-path app/src-tauri/Cargo.toml --release --no-default-features

BIN="app/src-tauri/target/release/onibi"
CONFIG_DIR="$(mktemp -d)"
export ONIBI_CONFIG_DIR="$CONFIG_DIR"
export ONIBI_DISABLE_KEYRING=1
export ONIBI_NONINTERACTIVE=1

cleanup() {
  if [ -n "${ONIBI_DRY_RUN_PID:-}" ]; then
    kill "$ONIBI_DRY_RUN_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$CONFIG_DIR"
}
trap cleanup EXIT

step "running setup"
"$BIN" --port "$PORT" setup >"${LOG_DIR}/setup.txt"

TOKEN="$("$BIN" --port "$PORT" token show)"
step "starting headless daemon"
"$BIN" --headless --port "$PORT" --auto-transports >"${LOG_DIR}/daemon.log" 2>&1 &
ONIBI_DRY_RUN_PID="$!"
wait_for_healthz

step "checking unauthenticated status returns 401"
HTTP_CODE="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/v1/status")"
test "$HTTP_CODE" = "401"

step "checking authenticated status returns 200"
wait_for_status "$TOKEN"

step "capturing CLI status"
"$BIN" --port "$PORT" status >"${LOG_DIR}/cli-status.txt"

TOTAL="$(elapsed)"
step "dry-run complete in ${TOTAL}s"
fail_if_slow

cat <<EOF
Dry-run complete.
duration_seconds=${TOTAL}
log_dir=${LOG_DIR}
EOF
