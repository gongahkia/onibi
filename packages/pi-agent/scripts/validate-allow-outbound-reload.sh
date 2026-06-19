#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
nft_bin="${KELP_PI_NFT_BIN:-nft}"
current_config=""
updated_config=""
session_command=""
settle_seconds=2
status_dir=""
session_status=""

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

cleanup() {
  if [ -n "${session_pid:-}" ] && kill -0 "$session_pid" >/dev/null 2>&1; then
    kill "$session_pid" >/dev/null 2>&1 || true
    wait "$session_pid" >/dev/null 2>&1 || true
  fi
  [ -z "$status_dir" ] || rm -rf "$status_dir"
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
  case "$1" in
    --current-config)
      [ $# -ge 2 ] || fail "--current-config requires a value"
      current_config="$2"
      shift 2
      ;;
    --updated-config)
      [ $# -ge 2 ] || fail "--updated-config requires a value"
      updated_config="$2"
      shift 2
      ;;
    --session-command)
      [ $# -ge 2 ] || fail "--session-command requires a value"
      session_command="$2"
      shift 2
      ;;
    --settle-seconds)
      [ $# -ge 2 ] || fail "--settle-seconds requires a value"
      settle_seconds="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"
[ -n "$current_config" ] || fail "requires --current-config"
[ -n "$updated_config" ] || fail "requires --updated-config"
[ -n "$session_command" ] || fail "requires --session-command"
[ -f "$current_config" ] || fail "current config missing: $current_config"
[ -f "$updated_config" ] || fail "updated config missing: $updated_config"

need "$agent_bin"
need "$nft_bin"
need sh
need sleep
need kill
need mktemp
need grep
need sed

status_dir="$(mktemp -d)"
session_status="$status_dir/session.status"

"$agent_bin" hardening apply-network --config "$current_config" --nft-bin "$nft_bin" >/dev/null
pass "current nftables config applied"

(
  sh -c "$session_command"
  printf '%s\n' "$?" >"$session_status"
) &
session_pid="$!"
sleep "$settle_seconds"
[ ! -s "$session_status" ] || fail "session command exited before reload"
kill -0 "$session_pid" >/dev/null 2>&1 || fail "session command exited before reload"
pass "control-plane session established"

"$agent_bin" hardening apply-network --config "$updated_config" --nft-bin "$nft_bin" >/dev/null
ruleset="$("$nft_bin" list ruleset)"
printf '%s\n' "$ruleset" | grep -q 'table inet kelp_pi_filter' || fail "kelp_pi_filter table missing after reload"
printf '%s\n' "$ruleset" | grep -q 'policy drop' || fail "nftables default drop missing after reload"
printf '%s\n' "$ruleset" | grep -q 'ct state established,related accept' || fail "nftables established-session rule missing after reload"
printf '%s\n' "$ruleset" | grep -Eq 'ip daddr .+ tcp dport [0-9]+ accept' || fail "allow-outbound tcp dport accept missing after reload"
printf '%s\n' 'allow-outbound ruleset output chain after reload:'
printf '%s\n' "$ruleset" | sed -n '/chain output/,/}/p'
sleep "$settle_seconds"
[ ! -s "$session_status" ] || fail "control-plane session dropped after reload"
kill -0 "$session_pid" >/dev/null 2>&1 || fail "control-plane session dropped after reload"
pass "control-plane session survived allow-outbound reload"
