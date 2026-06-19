#!/usr/bin/env sh
set -eu

artifact_dir="${1:-}"
max_seconds="${KELP_PI_MAX_ACCEPTANCE_SECONDS:-1800}"

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

[ -n "$artifact_dir" ] || fail "usage: $0 FIELD_ACCEPTANCE_DIR"
[ -d "$artifact_dir" ] || fail "field acceptance dir missing: $artifact_dir"
need awk
need grep

summary="$artifact_dir/summary.txt"
timing="$artifact_dir/timing.txt"
host="$artifact_dir/host.txt"
[ -f "$summary" ] || fail "summary missing: $summary"
[ -f "$timing" ] || fail "timing missing: $timing"
[ -f "$host" ] || fail "host missing: $host"

grep -q '^uname=.*aarch64' "$host" || fail "host uname is not aarch64"
grep -q 'Raspberry Pi' "$artifact_dir/node.log" || fail "node log missing Raspberry Pi proof"

for check in \
  node \
  scanner-sandbox \
  nuclei-scan \
  allow-outbound-reload \
  dns-egress \
  ap-isolation
do
  grep -q "^OK $check " "$summary" || fail "$check did not pass in summary"
  [ -s "$artifact_dir/$check.log" ] || fail "$check log missing or empty"
done

duration_seconds="$(awk -F= '/^duration_seconds=/ { print $2 }' "$timing" | tail -n 1)"
[ -n "$duration_seconds" ] || fail "duration_seconds missing"
awk -v duration="$duration_seconds" -v max="$max_seconds" 'BEGIN { exit !(duration <= max) }' || fail "duration $duration_seconds exceeds $max_seconds"
grep -q "^OK duration_seconds=$duration_seconds " "$summary" || fail "duration summary missing"

grep -q 'systemd security score' "$artifact_dir/node.log" || fail "node log missing systemd security score"
grep -q 'default outbound denial' "$artifact_dir/node.log" || fail "node log missing outbound denial"
grep -q 'disabled bus/audio peripherals absent from dmesg' "$artifact_dir/node.log" || fail "node log missing disabled peripheral proof"
grep -q 'scanner sandbox blocked control-plane probe' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox did not block control plane"
grep -q 'scanner sandbox blocked public probe' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox did not block public internet"
grep -q 'pinned Nuclei scan ran' "$artifact_dir/nuclei-scan.log" || fail "pinned Nuclei scan proof missing"
grep -q 'control-plane session survived allow-outbound reload' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound reload proof missing"
grep -q 'zero upstream DNS egress observed' "$artifact_dir/dns-egress.log" || fail "DNS egress proof missing"
grep -q 'AP clients cannot ping each other' "$artifact_dir/ap-isolation.log" || fail "AP isolation proof missing"

pass "field acceptance artifact verified: $artifact_dir"
