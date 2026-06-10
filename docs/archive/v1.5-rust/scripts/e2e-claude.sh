#!/usr/bin/env bash
set -euo pipefail

PORT="${ONIBI_PORT:-17893}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="$ROOT_DIR/.codex/artifacts"
mkdir -p "$ARTIFACT_DIR"

if ! command -v claude >/dev/null 2>&1; then
  echo "SKIP: Claude Code is not installed"
  exit 0
fi

if [[ "${ONIBI_RUN_CLAUDE_E2E:-0}" != "1" ]]; then
  echo "SKIP: set ONIBI_RUN_CLAUDE_E2E=1 to run the destructive-agent-gated Claude Code flow"
  exit 0
fi

if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: curl and jq are required"
  exit 0
fi

if command -v onibi >/dev/null 2>&1; then
  ONIBI=(onibi)
else
  ONIBI=(cargo run --quiet --manifest-path "$ROOT_DIR/app/src-tauri/Cargo.toml" --)
fi

SERVER_PID=""
INSTALLED_ADAPTER="0"
cleanup() {
  if [[ "$INSTALLED_ADAPTER" == "1" ]]; then
    "${ONIBI[@]}" adapter uninstall claude-code >/dev/null 2>&1 || true
  fi
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if ! curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
  "${ONIBI[@]}" --headless --port "$PORT" >"$ARTIFACT_DIR/e2e-onibi.log" 2>&1 &
  SERVER_PID="$!"
  for _ in {1..80}; do
    if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 0.25
  done
fi

curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null
TOKEN="$("${ONIBI[@]}" token show)"
"${ONIBI[@]}" adapter install claude-code >/dev/null
INSTALLED_ADAPTER="1"

claude -p \
  --allowedTools 'Bash(echo *)' \
  --output-format stream-json \
  --include-hook-events \
  --verbose \
  'Use the Bash tool to run exactly this command: echo "hi from claude"' \
  >"$ARTIFACT_DIR/e2e-claude.log" 2>&1 &
CLAUDE_PID="$!"

APPROVAL_ID=""
for _ in {1..120}; do
  APPROVAL_ID="$(curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/v1/approval/pending" | jq -r '.[0].approval_id // empty')"
  if [[ -n "$APPROVAL_ID" ]]; then
    break
  fi
  sleep 0.5
done

if [[ -z "$APPROVAL_ID" ]]; then
  echo "FAIL: no Claude Code approval was observed"
  kill "$CLAUDE_PID" >/dev/null 2>&1 || true
  exit 1
fi

curl -fsS -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"decision":"allow"}' \
  "http://127.0.0.1:$PORT/v1/approval/$APPROVAL_ID/decide" >/dev/null

wait "$CLAUDE_PID"
echo "PASS: Claude Code approval round-trip completed"
