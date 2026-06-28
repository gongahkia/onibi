#!/usr/bin/env sh
set -eu

CONFIG="${KELP_PI_ACCEPTANCE_ENV:-.kelp-pi/acceptance.env}"
if [ ! -f "$CONFIG" ]; then
  echo "missing $CONFIG" >&2
  echo "required: KELP_PI_SSH_HOST and KELP_PI_SSH_USER" >&2
  exit 64
fi

# shellcheck disable=SC1090
. "$CONFIG"

: "${KELP_PI_SSH_HOST:?missing KELP_PI_SSH_HOST}"
: "${KELP_PI_SSH_USER:?missing KELP_PI_SSH_USER}"

SSH_PORT="${KELP_PI_SSH_PORT:-22}"
REMOTE_ROOT="${KELP_PI_REMOTE_DATA_DIR:-/tmp/kelp-pi-acceptance}"
REMOTE_BIN="${KELP_PI_REMOTE_BIN:-$REMOTE_ROOT/kelp-pi}"
LOCAL_BIN="${KELP_PI_BINARY:-zig-out/bin/kelp-pi}"
LOCAL_MODEL="${KELP_PI_MODEL:-.kelp-pi/models/Qwen_Qwen3-0.6B-Q4_K_M.gguf}"
REMOTE_MODEL="$REMOTE_ROOT/models/Qwen_Qwen3-0.6B-Q4_K_M.gguf"
REMOTE="$KELP_PI_SSH_USER@$KELP_PI_SSH_HOST"

SSH_BASE="ssh -p $SSH_PORT"
SCP_BASE="scp -P $SSH_PORT"
if [ -n "${KELP_PI_SSH_KEY:-}" ]; then
  SSH_BASE="$SSH_BASE -i $KELP_PI_SSH_KEY"
  SCP_BASE="$SCP_BASE -i $KELP_PI_SSH_KEY"
fi

run_remote() {
  # shellcheck disable=SC2086
  $SSH_BASE "$REMOTE" "$@"
}

copy_to_remote() {
  # shellcheck disable=SC2086
  $SCP_BASE "$1" "$REMOTE:$2"
}

run_remote "mkdir -p '$REMOTE_ROOT/models' '$REMOTE_ROOT/normalized' '$REMOTE_ROOT/audit'"

if [ -f "$LOCAL_BIN" ]; then
  copy_to_remote "$LOCAL_BIN" "$REMOTE_BIN"
  run_remote "chmod 0755 '$REMOTE_BIN'"
else
  run_remote "command -v '$REMOTE_BIN' >/dev/null"
fi

if [ -f "$LOCAL_MODEL" ]; then
  copy_to_remote "$LOCAL_MODEL" "$REMOTE_MODEL"
else
  echo "missing local model $LOCAL_MODEL" >&2
  exit 66
fi

run_remote "'$REMOTE_BIN' keygen --data-dir '$REMOTE_ROOT' --label acceptance-pi >/tmp/kelp-pi-keygen.json"
run_remote "'$REMOTE_BIN' doctor --data-dir '$REMOTE_ROOT'"
run_remote "'$REMOTE_BIN' model warm --data-dir '$REMOTE_ROOT' --id qwen3-0.6b-q4_k_m --model-path '$REMOTE_MODEL'"
run_remote "'$REMOTE_BIN' scope set --data-dir '$REMOTE_ROOT' --host http://fixture.local --until 2026-12-31T00:00:00Z"

TOKEN="$(
  run_remote "'$REMOTE_BIN' approval-request --data-dir '$REMOTE_ROOT' --scope-id default --command 'nuclei http://fixture.local'" |
    sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
)"
if [ -z "$TOKEN" ]; then
  echo "failed to parse approval token" >&2
  exit 77
fi

run_remote "'$REMOTE_BIN' scan nuclei --data-dir '$REMOTE_ROOT' --target http://fixture.local --approval-token '$TOKEN' --dry-run" &&
  { echo "scan unexpectedly allowed pending token" >&2; exit 77; } || true
run_remote "'$REMOTE_BIN' approve --data-dir '$REMOTE_ROOT' '$TOKEN'"
run_remote "'$REMOTE_BIN' scan nuclei --data-dir '$REMOTE_ROOT' --target http://fixture.local --approval-token '$TOKEN' --dry-run"

run_remote "printf '%s\n' '{\"finding\":\"default admin marker\"}' > '$REMOTE_ROOT/finding.json'"
run_remote "printf '%s\n' '{\"findings\":[{\"id\":\"acceptance-fixture\"}]}' > '$REMOTE_ROOT/normalized/findings.json'"
run_remote "'$REMOTE_BIN' index ingest --data-dir '$REMOTE_ROOT' --input '$REMOTE_ROOT/finding.json' --path evidence/finding.json"
run_remote "'$REMOTE_BIN' ask default --data-dir '$REMOTE_ROOT'"
run_remote "'$REMOTE_BIN' bundle assemble --data-dir '$REMOTE_ROOT' --run-id acceptance --workspace '$REMOTE_ROOT' --output '$REMOTE_ROOT/bundle'"

rm -rf .kelp-pi/acceptance-bundle
mkdir -p .kelp-pi
# shellcheck disable=SC2086
$SCP_BASE -r "$REMOTE:$REMOTE_ROOT/bundle" .kelp-pi/acceptance-bundle
./zig-out/bin/kelp-pi verify-bundle .kelp-pi/acceptance-bundle
