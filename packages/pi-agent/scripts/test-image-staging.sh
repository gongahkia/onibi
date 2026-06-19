#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
root="${1:-}"
cleanup=""

if [ -z "$root" ]; then
  root="$(mktemp -d)/root"
  cleanup="$(dirname "$root")"
fi

cleanup_temp() {
  [ -z "$cleanup" ] || rm -rf "$cleanup"
}
trap cleanup_temp EXIT

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

"$script_dir/build-aarch64-linux.sh" >/dev/null
"$script_dir/stage-image-root.sh" "$root" \
  --wpa3-passphrase correct-horse-battery \
  --allow-outbound 198.51.100.10:443 >/dev/null

test -x "$root/usr/local/bin/kelp-pi-agent" || fail "agent binary missing"
if command -v file >/dev/null 2>&1; then
  file "$root/usr/local/bin/kelp-pi-agent" | grep -q 'ARM aarch64' || fail "agent binary is not aarch64"
fi
test -f "$root/etc/systemd/system/kelp-pi-agent.service" || fail "systemd unit missing"
test -f "$root/usr/lib/sysusers.d/kelp-pi-agent.conf" || fail "sysusers file missing"
test -f "$root/usr/lib/tmpfiles.d/kelp-pi-agent.conf" || fail "tmpfiles file missing"
test -x "$root/usr/local/sbin/kelp-pi-validate-node" || fail "field validator missing"
test -x "$root/opt/kelp-pi/bin/nuclei" || fail "nuclei binary missing"
grep -q '"binary_sha256":"6b6f19f038f959c2ec90d9f3e3f039256987d1eb78d5c292d7ec9a384513e27f"' "$root/etc/kelp-pi/nuclei-binary.json" || fail "nuclei manifest missing binary hash"
grep -qx 'key-mgmt=sae' "$root/etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection" || fail "WPA3 profile missing"
grep -qx 'ap-isolation=1' "$root/etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection" || fail "AP isolation missing"
grep -qx 'no-resolv' "$root/etc/dnsmasq.d/kelp-pi-captive.conf" || fail "dnsmasq no-resolv missing"
grep -q 'ip daddr 198.51.100.10 tcp dport 443 accept' "$root/etc/nftables.d/kelp-pi.nft" || fail "nft allowlist missing"
grep -qx 'dtoverlay=disable-bt' "$root/boot/firmware/config.txt" || fail "boot hardening missing"

pass "image staging smoke complete"
