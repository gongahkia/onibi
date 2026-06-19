#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
service="${KELP_PI_SERVICE:-kelp-pi-agent.service}"
network_config="${KELP_PI_NETWORK_CONFIG:-/etc/kelp-pi/network-hardening.json}"
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

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"

need "$agent_bin"
need systemctl
need systemd-analyze
need nft
need curl

"$agent_bin" version | grep -Eq '^kelp-pi-agent [0-9]+' || fail "agent version failed"
pass "agent version"

id kelp-pi >/dev/null 2>&1 || fail "kelp-pi user missing"
pass "kelp-pi user"
id kelp-pi-scanner >/dev/null 2>&1 || fail "kelp-pi-scanner user missing"
id -nG kelp-pi-scanner | tr ' ' '\n' | grep -qx kelp-pi || fail "kelp-pi-scanner missing kelp-pi group"
pass "kelp-pi-scanner user"

systemctl start "$service"
systemctl is-active --quiet "$service" || fail "$service not active"
systemctl status "$service" --no-pager >/dev/null
pass "service active"

score="$(systemd-analyze security "$service" --no-pager | awk '/Overall exposure level/ { for (i = 1; i <= NF; i++) if ($i ~ /^[0-9]+(\.[0-9]+)?$/) print $i }' | tail -n 1)"
[ -n "$score" ] || fail "systemd security score missing"
awk -v score="$score" 'BEGIN { exit !(score < 3.0) }' || fail "systemd security score $score >= 3.0"
pass "systemd security score $score"

[ -f "$network_config" ] || fail "$network_config missing"
"$agent_bin" hardening apply-network --config "$network_config"
nft list ruleset | grep -q 'table inet kelp_pi_filter' || fail "kelp_pi_filter table missing"
nft list ruleset | grep -q 'policy drop' || fail "nftables default drop missing"
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
