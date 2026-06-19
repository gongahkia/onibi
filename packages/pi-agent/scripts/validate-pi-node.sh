#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
service="${KELP_PI_SERVICE:-kelp-pi-agent.service}"
model_path="${KELP_PI_MODEL_PATH:-/proc/device-tree/model}"
network_config="${KELP_PI_NETWORK_CONFIG:-/etc/kelp-pi/network-hardening.json}"
nm_profile="${KELP_PI_NM_PROFILE:-/etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection}"
dnsmasq_config="${KELP_PI_DNSMASQ_CONFIG:-/etc/dnsmasq.d/kelp-pi-captive.conf}"
boot_config="${KELP_PI_BOOT_CONFIG:-/boot/firmware/config.txt}"
nuclei_bin="${KELP_PI_NUCLEI_BIN:-/opt/kelp-pi/bin/nuclei}"
nuclei_manifest="${KELP_PI_NUCLEI_MANIFEST:-/etc/kelp-pi/nuclei-binary.json}"
nuclei_version="v3.9.0"
nuclei_asset_sha256="733ceb77896fc5a9cafb70d07cabdd43fd9f186c28cbc335eec5b78d5c35d850"
nuclei_binary_sha256="6b6f19f038f959c2ec90d9f3e3f039256987d1eb78d5c292d7ec9a384513e27f"
portal_ip="${KELP_PI_PORTAL_IP:-10.42.0.1}"
egress_probe="${KELP_PI_EGRESS_PROBE:-https://example.com}"

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

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"

need "$agent_bin"
need systemctl
need systemd-analyze
need nft
need curl
need awk
need file
need grep
need sed
need sysctl
need tr
need uname
need dmesg

[ "$(uname -m)" = "aarch64" ] || fail "host architecture is not aarch64"
[ -f "$model_path" ] || fail "$model_path missing"
model="$(tr -d '\000' < "$model_path")"
case "$model" in
  *"Raspberry Pi 5"*) ;;
  *) fail "host model is not Raspberry Pi 5: $model" ;;
esac
pass "Raspberry Pi 5 aarch64 host: $model"

agent_file="$(file "$agent_bin")"
printf 'agent binary file=%s\n' "$agent_file"
printf '%s\n' "$agent_file" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || fail "agent binary is not an aarch64 ELF"

agent_version="$("$agent_bin" version)"
printf '%s\n' "$agent_version" | grep -Eq '^kelp-pi-agent [0-9]+' || fail "agent version failed"
pass "agent version: $agent_version"

id kelp-pi >/dev/null 2>&1 || fail "kelp-pi user missing"
pass "kelp-pi user"
id kelp-pi-scanner >/dev/null 2>&1 || fail "kelp-pi-scanner user missing"
id -nG kelp-pi-scanner | tr ' ' '\n' | grep -qx kelp-pi || fail "kelp-pi-scanner missing kelp-pi group"
pass "kelp-pi-scanner user"

systemctl start "$service"
active_state="$(systemctl is-active "$service" 2>/dev/null || true)"
[ "$active_state" = "active" ] || fail "$service not active: $active_state"
systemctl status "$service" --no-pager --lines=5 >/dev/null
printf 'systemctl is-active %s=%s\n' "$service" "$active_state"
systemctl show "$service" -p ActiveState -p SubState -p MainPID -p ExecMainStatus -p FragmentPath
pass "service active: $service"

score="$(systemd-analyze security "$service" --no-pager | awk '/Overall exposure level/ { for (i = 1; i <= NF; i++) if ($i ~ /^[0-9]+(\.[0-9]+)?$/) print $i }' | tail -n 1)"
[ -n "$score" ] || fail "systemd security score missing"
awk -v score="$score" 'BEGIN { exit !(score < 3.0) }' || fail "systemd security score $score >= 3.0"
pass "systemd security score $score"

[ -x "$nuclei_bin" ] || fail "$nuclei_bin missing or not executable"
[ -f "$nuclei_manifest" ] || fail "$nuclei_manifest missing"
grep -q "\"version\":\"$nuclei_version\"" "$nuclei_manifest" || fail "nuclei manifest version mismatch"
grep -q "\"sha256\":\"$nuclei_asset_sha256\"" "$nuclei_manifest" || fail "nuclei asset sha256 mismatch"
grep -q "\"binary_sha256\":\"$nuclei_binary_sha256\"" "$nuclei_manifest" || fail "nuclei binary sha256 mismatch"
installed_nuclei_sha256="$(hash_file "$nuclei_bin")"
[ "$installed_nuclei_sha256" = "$nuclei_binary_sha256" ] || fail "nuclei installed binary sha256 mismatch"
pass "nuclei pinned binary sha256=$installed_nuclei_sha256"

[ -f "$nm_profile" ] || fail "$nm_profile missing"
grep -qx 'mode=ap' "$nm_profile" || fail "NetworkManager AP mode missing"
grep -qx 'ap-isolation=1' "$nm_profile" || fail "NetworkManager AP client isolation missing"
grep -qx 'key-mgmt=sae' "$nm_profile" || fail "NetworkManager WPA3 SAE missing"
grep -qx 'pmf=3' "$nm_profile" || fail "NetworkManager PMF required missing"
grep -qx 'never-default=true' "$nm_profile" || fail "NetworkManager never-default missing"
grep -qx 'ignore-auto-dns=true' "$nm_profile" || fail "NetworkManager ignore-auto-dns missing"
pass "NetworkManager AP profile"

[ -f "$dnsmasq_config" ] || fail "$dnsmasq_config missing"
grep -qx 'no-resolv' "$dnsmasq_config" || fail "dnsmasq no-resolv missing"
grep -qx 'no-poll' "$dnsmasq_config" || fail "dnsmasq no-poll missing"
for domain in captive.apple.com connectivitycheck.gstatic.com clients3.google.com; do
  grep -qx "address=/$domain/$portal_ip" "$dnsmasq_config" || fail "dnsmasq sinkhole missing for $domain"
done
pass "dnsmasq captive sinkhole config"

[ "$(sysctl -n net.ipv4.ip_forward)" = "0" ] || fail "IPv4 forwarding enabled"
[ "$(sysctl -n net.ipv6.conf.all.forwarding)" = "0" ] || fail "IPv6 forwarding enabled"
pass "kernel forwarding disabled"

[ -f "$boot_config" ] || fail "$boot_config missing"
for line in \
  dtoverlay=disable-bt \
  dtparam=audio=off \
  dtoverlay=vc4-kms-v3d,noaudio \
  dtparam=i2c_arm=off \
  dtparam=spi=off \
  hdmi_blanking=1
do
  grep -qx "$line" "$boot_config" || fail "boot config missing $line"
done
pass "boot peripheral disables"

dmesg_output="$(dmesg 2>/dev/null || true)"
[ -n "$dmesg_output" ] || fail "dmesg output unavailable"
for pattern in \
  Bluetooth \
  hci_uart \
  snd_bcm2835 \
  i2c-bcm2835 \
  spi-bcm2835
do
  printf '%s\n' "$dmesg_output" | grep -qi "$pattern" && fail "disabled subsystem initialized in dmesg: $pattern"
done
pass "disabled bus/audio peripherals absent from dmesg"

[ -f "$network_config" ] || fail "$network_config missing"
"$agent_bin" hardening apply-network --config "$network_config"
ruleset="$(nft list ruleset)"
printf '%s\n' "$ruleset" | grep -q 'table inet kelp_pi_filter' || fail "kelp_pi_filter table missing"
printf '%s\n' "$ruleset" | grep -q 'policy drop' || fail "nftables default drop missing"
printf '%s\n' "$ruleset" | grep -q 'ct state established,related accept' || fail "nftables established-session rule missing"
printf '%s\n' "$ruleset" | grep -q 'set scanner_users' || fail "scanner_users set missing"
printf '%s\n' "$ruleset" | grep -q 'set scanner_ipv4_targets' || fail "scanner_ipv4_targets set missing"
printf '%s\n' "$ruleset" | grep -q 'meta skuid @scanner_users drop' || fail "scanner drop rule missing"
printf '%s\n' 'nft ruleset output chain:'
printf '%s\n' "$ruleset" | sed -n '/chain output/,/}/p'
pass "nftables loaded"

if curl -fsS --max-time 5 "$egress_probe" >/dev/null 2>&1; then
  fail "unexpected outbound access to $egress_probe"
fi
pass "default outbound denial"

if command -v dig >/dev/null 2>&1; then
  for domain in captive.apple.com connectivitycheck.gstatic.com clients3.google.com; do
    dig +short "@$portal_ip" "$domain" | grep -qx "$portal_ip" || fail "$domain did not resolve to $portal_ip"
  done
  pass "captive DNS sinkhole"
else
  printf 'SKIP dig missing; captive DNS sinkhole not checked\n'
fi
