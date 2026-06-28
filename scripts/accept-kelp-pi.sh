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
PACKAGE_DIR="${KELP_PI_PACKAGE_DIR:-}"
LOCAL_BIN="${KELP_PI_BINARY:-${PACKAGE_DIR:+$PACKAGE_DIR/bin/kelp-pi}}"
LOCAL_BIN="${LOCAL_BIN:-zig-out/bin/kelp-pi}"
LOCAL_VERIFY_BIN="${KELP_PI_VERIFY_BINARY:-./zig-out/bin/kelp-pi}"
LOCAL_MODEL="${KELP_PI_MODEL:-.kelp-pi/models/Qwen_Qwen3-0.6B-Q4_K_M.gguf}"
REMOTE_MODEL="$REMOTE_ROOT/models/Qwen_Qwen3-0.6B-Q4_K_M.gguf"
LOCAL_LLAMA_LIB_DIR="${KELP_PI_LLAMA_LIB_DIR:-${PACKAGE_DIR:+$PACKAGE_DIR/lib}}"
REMOTE_LLAMA_LIB_DIR="${KELP_PI_REMOTE_LLAMA_LIB_DIR:-$REMOTE_ROOT/lib}"
REMOTE_ENV="LD_LIBRARY_PATH='$REMOTE_LLAMA_LIB_DIR':\${LD_LIBRARY_PATH:-}"
REMOTE="$KELP_PI_SSH_USER@$KELP_PI_SSH_HOST"
EVIDENCE_DIR="${KELP_PI_ACCEPTANCE_EVIDENCE_DIR:-.kelp-pi/acceptance-evidence}"

SSH_BASE="ssh -p $SSH_PORT"
SCP_BASE="scp -P $SSH_PORT"
if [ -n "${KELP_PI_SSH_KEY:-}" ]; then
  SSH_BASE="$SSH_BASE -i $KELP_PI_SSH_KEY"
  SCP_BASE="$SCP_BASE -i $KELP_PI_SSH_KEY"
fi

fail() {
  echo "FAIL $*" >&2
  exit 1
}

run_remote() {
  # shellcheck disable=SC2086
  $SSH_BASE "$REMOTE" "$@"
}

copy_to_remote() {
  # shellcheck disable=SC2086
  $SCP_BASE "$1" "$REMOTE:$2"
}

write_evidence() {
  name="$1"
  content="$2"
  printf '%s\n' "$content" > "$EVIDENCE_DIR/$name"
}

mkdir -p "$EVIDENCE_DIR"
if [ -n "$PACKAGE_DIR" ] && [ -f "$PACKAGE_DIR/package-manifest.json" ]; then
  cp "$PACKAGE_DIR/package-manifest.json" "$EVIDENCE_DIR/package-manifest.json"
fi

run_remote "mkdir -p '$REMOTE_ROOT/models' '$REMOTE_ROOT/normalized' '$REMOTE_ROOT/audit'"

if [ -f "$LOCAL_BIN" ]; then
  copy_to_remote "$LOCAL_BIN" "$REMOTE_BIN"
  run_remote "chmod 0755 '$REMOTE_BIN'"
else
  run_remote "command -v '$REMOTE_BIN' >/dev/null"
fi

if [ -n "$LOCAL_LLAMA_LIB_DIR" ]; then
  run_remote "mkdir -p '$REMOTE_LLAMA_LIB_DIR'"
  # shellcheck disable=SC2086
  $SCP_BASE -r "$LOCAL_LLAMA_LIB_DIR/." "$REMOTE:$REMOTE_LLAMA_LIB_DIR/"
fi

host_output="$(run_remote "uname -m; cat /proc/device-tree/model 2>/dev/null || true")"
write_evidence "host.txt" "$host_output"
printf '%s\n' "$host_output" | sed -n '1p' | grep -q '^aarch64$' || fail "remote host is not aarch64"
printf '%s\n' "$host_output" | grep -qi 'Raspberry Pi 5' || fail "remote host is not Raspberry Pi 5"
binary_file_output="$(run_remote "if command -v file >/dev/null 2>&1; then file '$REMOTE_BIN'; fi")"
write_evidence "remote-binary-file.txt" "$binary_file_output"
if [ -n "$binary_file_output" ]; then
  printf '%s\n' "$binary_file_output" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || fail "remote binary is not ELF aarch64"
fi
ldd_output="$(run_remote "$REMOTE_ENV ldd '$REMOTE_BIN' 2>/dev/null || true")"
write_evidence "remote-ldd.txt" "$ldd_output"
[ -n "$ldd_output" ] || fail "remote ldd produced no output"
printf '%s\n' "$ldd_output" | grep -q 'not found' && fail "remote ldd has unresolved libraries"
printf '%s\n' "$ldd_output" | grep -q 'not a dynamic executable' && fail "remote binary is not dynamically linked"

if [ -f "$LOCAL_MODEL" ]; then
  copy_to_remote "$LOCAL_MODEL" "$REMOTE_MODEL"
else
  echo "missing local model $LOCAL_MODEL" >&2
  exit 66
fi
write_evidence "remote-model-sha256.txt" "$(run_remote "sha256sum '$REMOTE_MODEL' 2>/dev/null || shasum -a 256 '$REMOTE_MODEL' 2>/dev/null || true")"

run_remote "$REMOTE_ENV '$REMOTE_BIN' keygen --data-dir '$REMOTE_ROOT' --label acceptance-pi >/tmp/kelp-pi-keygen.json"
doctor_output="$(run_remote "$REMOTE_ENV '$REMOTE_BIN' doctor --data-dir '$REMOTE_ROOT'")"
write_evidence "doctor.json" "$doctor_output"
printf '%s\n' "$doctor_output" | grep -q '"id":"llama-linked","status":"pass"' || fail "remote doctor missing llama-linked pass"
write_evidence "thermal-before.txt" "$(run_remote "for f in /sys/class/thermal/thermal_zone*/temp; do [ -r \"\$f\" ] && printf \"%s=\" \"\$f\" && cat \"\$f\"; done 2>/dev/null || true")"
warm_output="$(run_remote "$REMOTE_ENV '$REMOTE_BIN' model warm --data-dir '$REMOTE_ROOT' --id qwen3-0.6b-q4_k_m --model-path '$REMOTE_MODEL' --n-predict 1 --threads '${KELP_LLAMA_THREADS:-2}'")"
write_evidence "model-warm.json" "$warm_output"
printf '%s\n' "$warm_output" | grep -q '"loaded":true' || fail "remote model warm did not load model"
printf '%s\n' "$warm_output" | grep -q '"elapsedSeconds":' || fail "remote model warm missing elapsedSeconds"
printf '%s\n' "$warm_output" | grep -q '"peakRssBytes":' || fail "remote model warm missing peakRssBytes"
write_evidence "thermal-after.txt" "$(run_remote "for f in /sys/class/thermal/thermal_zone*/temp; do [ -r \"\$f\" ] && printf \"%s=\" \"\$f\" && cat \"\$f\"; done 2>/dev/null || true")"
run_remote "$REMOTE_ENV '$REMOTE_BIN' scope set --data-dir '$REMOTE_ROOT' --host http://fixture.local --until 2026-12-31T00:00:00Z"

TOKEN="$(
  run_remote "$REMOTE_ENV '$REMOTE_BIN' approval-request --data-dir '$REMOTE_ROOT' --scope-id default --command 'nuclei http://fixture.local'" |
    sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
)"
if [ -z "$TOKEN" ]; then
  echo "failed to parse approval token" >&2
  exit 77
fi

run_remote "$REMOTE_ENV '$REMOTE_BIN' scan nuclei --data-dir '$REMOTE_ROOT' --target http://fixture.local --approval-token '$TOKEN' --dry-run" &&
  { echo "scan unexpectedly allowed pending token" >&2; exit 77; } || true
run_remote "$REMOTE_ENV '$REMOTE_BIN' approve --data-dir '$REMOTE_ROOT' '$TOKEN'"
run_remote "$REMOTE_ENV '$REMOTE_BIN' scan nuclei --data-dir '$REMOTE_ROOT' --target http://fixture.local --approval-token '$TOKEN' --dry-run"

run_remote "printf '%s\n' '{\"finding\":\"default admin marker\"}' > '$REMOTE_ROOT/finding.json'"
run_remote "printf '%s\n' '{\"findings\":[{\"id\":\"acceptance-fixture\"}]}' > '$REMOTE_ROOT/normalized/findings.json'"
run_remote "$REMOTE_ENV '$REMOTE_BIN' index ingest --data-dir '$REMOTE_ROOT' --input '$REMOTE_ROOT/finding.json' --path evidence/finding.json"
run_remote "$REMOTE_ENV '$REMOTE_BIN' ask default --data-dir '$REMOTE_ROOT'"
run_remote "$REMOTE_ENV '$REMOTE_BIN' bundle assemble --data-dir '$REMOTE_ROOT' --run-id acceptance --workspace '$REMOTE_ROOT' --output '$REMOTE_ROOT/bundle'"

rm -rf .kelp-pi/acceptance-bundle
mkdir -p .kelp-pi
# shellcheck disable=SC2086
$SCP_BASE -r "$REMOTE:$REMOTE_ROOT/bundle" .kelp-pi/acceptance-bundle
verify_output="$("$LOCAL_VERIFY_BIN" verify-bundle .kelp-pi/acceptance-bundle)"
write_evidence "bundle-verify.json" "$verify_output"
printf '%s\n' "$verify_output"
