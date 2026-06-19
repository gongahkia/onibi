#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
root="${1:-${KELP_PI_QUOTA_TEST_ROOT:-}}"
max_bytes="${KELP_PI_QUOTA_FILL_MAX_BYTES:-1073741824}"
target_free_bytes="${KELP_PI_QUOTA_FILL_TARGET_FREE_BYTES:-16777216}"
floor_bytes="${KELP_PI_QUOTA_FILL_FLOOR_BYTES:-33554432}"

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

available_bytes() {
  df -Pk "$1" | awk 'NR == 2 { print $4 * 1024 }'
}

expect_refusal() {
  name="$1"
  shift
  stderr="$root/$name.stderr"
  set +e
  "$@" 2>"$stderr" >/dev/null
  code="$?"
  set -e
  [ "$code" = "75" ] || fail "$name exited $code, expected 75"
  grep -q 'storage quota' "$stderr" || fail "$name did not print storage quota error"
  pass "$name refused below quota floor"
}

[ "${KELP_PI_RUN_DISK_FILL_TEST:-}" = "1" ] || fail "set KELP_PI_RUN_DISK_FILL_TEST=1"
[ -n "$root" ] || fail "usage: $0 QUOTA_TEST_ROOT"
command -v "$agent_bin" >/dev/null 2>&1 || fail "$agent_bin missing"

mkdir -p "$root"
for dir in corpus evidence bundles index audit keys policy scope; do
  mkdir -p "$root/$dir"
done
mkdir -p "$root/workspace/raw"
printf '%s\n' '{"template-id":"quota","matched-at":"https://app.example.test","info":{"name":"quota","severity":"low"}}' > "$root/workspace/raw/nuclei.jsonl"
printf '%s\n' 'upload bytes' > "$root/upload.txt"

available_before="$(available_bytes "$root")"
[ "$available_before" -le "$max_bytes" ] || fail "available bytes $available_before exceed safety max $max_bytes"
[ "$available_before" -gt "$target_free_bytes" ] || fail "available bytes $available_before already below target $target_free_bytes"

fill_bytes=$((available_before - target_free_bytes))
fill_mib=$((fill_bytes / 1048576))
[ "$fill_mib" -gt 0 ] || fail "test filesystem too small to fill safely"
dd if=/dev/zero of="$root/quota-filler.bin" bs=1048576 count="$fill_mib" >/dev/null 2>&1 || true

available_after="$(available_bytes "$root")"
[ "$available_after" -lt "$floor_bytes" ] || fail "available bytes $available_after did not fall below floor $floor_bytes"

expect_refusal scan "$agent_bin" scan nmap --data-dir "$root" --target 127.0.0.1 --dry-run --min-free-bytes "$floor_bytes"
expect_refusal normalize "$agent_bin" normalize nuclei --data-dir "$root" --input "$root/workspace/raw/nuclei.jsonl" --workspace "$root/workspace" --min-free-bytes "$floor_bytes"
expect_refusal upload "$agent_bin" upload accept --data-dir "$root" --input "$root/upload.txt" --name upload.txt --min-free-bytes "$floor_bytes"

rm -f "$root/quota-filler.bin" "$root/scan.stderr" "$root/normalize.stderr" "$root/upload.stderr"
pass "disk-fill quota harness complete"
