#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
scanner_user="${KELP_PI_SCANNER_USER:-kelp-pi-scanner}"
systemd_run_bin="${KELP_PI_SYSTEMD_RUN_BIN:-systemd-run}"
nft_bin="${KELP_PI_NFT_BIN:-nft}"
target_ip=""
target_url=""
control_url="${KELP_PI_CONTROL_PROBE_URL:-}"
public_url="${KELP_PI_PUBLIC_PROBE_URL:-https://example.com}"

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

while [ $# -gt 0 ]; do
  case "$1" in
    --target-ip)
      [ $# -ge 2 ] || fail "--target-ip requires a value"
      target_ip="$2"
      shift 2
      ;;
    --target-url)
      [ $# -ge 2 ] || fail "--target-url requires a value"
      target_url="$2"
      shift 2
      ;;
    --control-url)
      [ $# -ge 2 ] || fail "--control-url requires a value"
      control_url="$2"
      shift 2
      ;;
    --public-url)
      [ $# -ge 2 ] || fail "--public-url requires a value"
      public_url="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"
[ -n "$target_ip" ] || fail "validate-scanner-sandbox requires --target-ip"
[ -n "$target_url" ] || fail "validate-scanner-sandbox requires --target-url"
[ -n "$control_url" ] || fail "validate-scanner-sandbox requires --control-url or KELP_PI_CONTROL_PROBE_URL"

need "$agent_bin"
need "$systemd_run_bin"
need "$nft_bin"
need curl
need id

scanner_uid="$(id -u "$scanner_user")"
[ "$scanner_uid" != "0" ] || fail "scanner user must not be root"
pass "scanner sandbox user=$scanner_user uid=$scanner_uid"

"$agent_bin" hardening apply-scanner-targets --target-ip "$target_ip" --nft-bin "$nft_bin" >/dev/null
pass "scanner nft target set loaded for $target_ip"
pass "scanner sandbox properties: User=$scanner_user NoNewPrivileges PrivateTmp PrivateDevices ProtectSystem=strict NFTSet=user:inet:kelp_pi_filter:scanner_users"

run_scanner_curl() {
  "$systemd_run_bin" \
    --wait \
    --pipe \
    --collect \
    --quiet \
    --property="User=$scanner_user" \
    --property=NoNewPrivileges=yes \
    --property=PrivateTmp=yes \
    --property=PrivateDevices=yes \
    --property=ProtectSystem=strict \
    --property=ProtectHome=yes \
    --property=CapabilityBoundingSet= \
    --property=AmbientCapabilities= \
    --property=RestrictSUIDSGID=yes \
    --property=RestrictRealtime=yes \
    --property=LockPersonality=yes \
    --property=SystemCallArchitectures=native \
    --property=RestrictAddressFamilies=AF_INET \
    --property=NFTSet=user:inet:kelp_pi_filter:scanner_users \
    -- \
    curl -fsS --max-time 5 "$1" >/dev/null 2>&1
}

run_scanner_curl "$target_url" || fail "scanner sandbox could not reach in-scope target"
pass "scanner sandbox reached in-scope target"

if run_scanner_curl "$control_url"; then
  fail "scanner sandbox reached control-plane probe URL"
fi
pass "scanner sandbox blocked control-plane probe"

if run_scanner_curl "$public_url"; then
  fail "scanner sandbox reached public probe URL"
fi
pass "scanner sandbox blocked public probe"
