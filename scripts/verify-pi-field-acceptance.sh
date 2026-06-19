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

node_security_score_ok() {
  awk '
    /systemd security score/ {
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^[0-9]+(\.[0-9]+)?$/) {
          score=$i
        }
      }
    }
    END { exit !(score != "" && score < 3.0) }
  ' "$1"
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
grep -q 'Raspberry Pi 5 aarch64 host:' "$artifact_dir/node.log" || fail "node log missing Raspberry Pi 5 aarch64 proof"
grep -Eq 'agent binary file=.*ELF 64-bit.*(ARM aarch64|aarch64)' "$artifact_dir/node.log" || fail "node log missing aarch64 agent binary proof"

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

node_security_score_ok "$artifact_dir/node.log" || fail "node log missing systemd security score below 3.0"
grep -q 'agent version: kelp-pi-agent ' "$artifact_dir/node.log" || fail "node log missing agent version proof"
grep -q 'systemctl status kelp-pi-agent.service:' "$artifact_dir/node.log" || fail "node log missing systemctl status proof"
grep -Eq 'Active:[[:space:]]+active' "$artifact_dir/node.log" || fail "node log missing active status proof"
grep -q 'systemctl is-active kelp-pi-agent.service=active' "$artifact_dir/node.log" || fail "node log missing active systemd proof"
grep -q '^User=kelp-pi$' "$artifact_dir/node.log" || fail "node log missing service User proof"
grep -q '^Group=kelp-pi$' "$artifact_dir/node.log" || fail "node log missing service Group proof"
grep -q '^ProtectSystem=strict$' "$artifact_dir/node.log" || fail "node log missing ProtectSystem proof"
grep -Eq '^ProtectHome=(true|yes)$' "$artifact_dir/node.log" || fail "node log missing ProtectHome proof"
grep -Eq '^PrivateTmp=(true|yes)$' "$artifact_dir/node.log" || fail "node log missing PrivateTmp proof"
grep -Eq '^NoNewPrivileges=(true|yes)$' "$artifact_dir/node.log" || fail "node log missing NoNewPrivileges proof"
grep -q 'systemd hardening properties verified' "$artifact_dir/node.log" || fail "node log missing systemd hardening proof"
grep -q 'nft ruleset output chain:' "$artifact_dir/node.log" || fail "node log missing nft ruleset output proof"
grep -q 'policy drop' "$artifact_dir/node.log" || fail "node log missing nft default drop proof"
grep -q 'ct state established,related accept' "$artifact_dir/node.log" || fail "node log missing nft established-session proof"
grep -Eq 'outbound denial probe url=.* exit=[1-9][0-9]*' "$artifact_dir/node.log" || fail "node log missing outbound denial nonzero-exit proof"
grep -q 'default outbound denial' "$artifact_dir/node.log" || fail "node log missing outbound denial"
grep -q 'NetworkManager AP profile proof:' "$artifact_dir/node.log" || fail "node log missing NetworkManager AP profile proof"
for nm_line in mode=ap ap-isolation=1 key-mgmt=sae pmf=3 never-default=true ignore-auto-dns=true; do
  grep -qx "$nm_line" "$artifact_dir/node.log" || fail "node log missing NetworkManager AP proof: $nm_line"
done
grep -q 'dnsmasq captive sinkhole config proof:' "$artifact_dir/node.log" || fail "node log missing dnsmasq config proof"
for dnsmasq_line in \
  no-resolv \
  no-poll \
  address=/captive.apple.com/10.42.0.1 \
  address=/connectivitycheck.gstatic.com/10.42.0.1 \
  address=/clients3.google.com/10.42.0.1
do
  grep -qx "$dnsmasq_line" "$artifact_dir/node.log" || fail "node log missing dnsmasq proof: $dnsmasq_line"
done
grep -q 'boot peripheral disable config:' "$artifact_dir/node.log" || fail "node log missing boot peripheral config proof"
for boot_line in \
  dtoverlay=disable-bt \
  dtparam=audio=off \
  dtoverlay=vc4-kms-v3d,noaudio \
  dtparam=i2c_arm=off \
  dtparam=spi=off \
  hdmi_blanking=1
do
  grep -qx "$boot_line" "$artifact_dir/node.log" || fail "node log missing boot config proof: $boot_line"
done
grep -q 'dmesg disabled subsystem absence patterns:' "$artifact_dir/node.log" || fail "node log missing dmesg pattern proof"
for dmesg_pattern in Bluetooth hci_uart snd_bcm2835 i2c-bcm2835 spi-bcm2835; do
  grep -qx "$dmesg_pattern" "$artifact_dir/node.log" || fail "node log missing dmesg absence proof: $dmesg_pattern"
done
grep -q 'disabled bus/audio peripherals absent from dmesg' "$artifact_dir/node.log" || fail "node log missing disabled peripheral proof"
grep -q 'scanner sandbox user=' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox user proof missing"
grep -q 'scanner sandbox systemd-run properties:' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox property proof missing"
grep -q '^NoNewPrivileges=yes$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox NoNewPrivileges proof missing"
grep -q '^PrivateTmp=yes$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox PrivateTmp proof missing"
grep -q '^PrivateDevices=yes$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox PrivateDevices proof missing"
grep -q '^ProtectSystem=strict$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox ProtectSystem proof missing"
grep -q '^ProtectHome=yes$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox ProtectHome proof missing"
grep -q '^CapabilityBoundingSet=$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox capability bound proof missing"
grep -q '^AmbientCapabilities=$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox ambient capability proof missing"
grep -q '^RestrictAddressFamilies=AF_INET$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox address-family proof missing"
grep -q '^NFTSet=user:inet:kelp_pi_filter:scanner_users$' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox NFTSet proof missing"
grep -q 'scanner sandbox reached in-scope target' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox target reachability proof missing"
grep -q 'scanner sandbox blocked control-plane probe' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox did not block control plane"
grep -q 'scanner sandbox blocked public probe' "$artifact_dir/scanner-sandbox.log" || fail "scanner sandbox did not block public internet"
grep -Eq 'pinned Nuclei binary file=.*ELF 64-bit.*(ARM aarch64|aarch64)' "$artifact_dir/nuclei-scan.log" || fail "pinned Nuclei ARM64 binary proof missing"
grep -q 'pinned Nuclei binary sha256=' "$artifact_dir/nuclei-scan.log" || fail "pinned Nuclei binary hash proof missing"
grep -q 'pinned Nuclei scan ran' "$artifact_dir/nuclei-scan.log" || fail "pinned Nuclei scan proof missing"
grep -q 'current nftables config applied' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound current config proof missing"
grep -q 'control-plane session established' "$artifact_dir/allow-outbound-reload.log" || fail "control-plane session setup proof missing"
grep -q 'allow-outbound ruleset output chain after reload:' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound ruleset proof missing"
grep -q 'policy drop' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound default drop proof missing"
grep -q 'ct state established,related accept' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound established-session proof missing"
grep -Eq 'ip daddr .+ tcp dport [0-9]+ accept' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound tcp dport accept proof missing"
grep -q 'control-plane session survived allow-outbound reload' "$artifact_dir/allow-outbound-reload.log" || fail "allow-outbound reload proof missing"
for domain in captive.apple.com connectivitycheck.gstatic.com clients3.google.com; do
  grep -q "$domain" "$artifact_dir/dns-egress.log" || fail "DNS portal response proof missing for $domain"
done
grep -Eq 'DNS probe output mapped captive domains to portal IP|local captive DNS probes resolved to' "$artifact_dir/dns-egress.log" || fail "DNS portal response proof missing"
grep -q 'zero upstream DNS egress observed' "$artifact_dir/dns-egress.log" || fail "DNS egress proof missing"
grep -q 'AP isolation clients client_a=' "$artifact_dir/ap-isolation.log" || fail "AP client identity proof missing"
grep -q 'both clients reached portal' "$artifact_dir/ap-isolation.log" || fail "AP portal reachability proof missing"
grep -q 'AP clients cannot ping each other' "$artifact_dir/ap-isolation.log" || fail "AP isolation proof missing"
grep -q 'clients cannot reach forbidden IP probes' "$artifact_dir/ap-isolation.log" || fail "AP forbidden-IP proof missing"

if grep -q '^OK ollama-load ' "$summary"; then
  [ -s "$artifact_dir/ollama-load.log" ] || fail "Ollama load log missing or empty"
  grep -q 'Ollama loaded' "$artifact_dir/ollama-load.log" || fail "Ollama load proof missing"
  grep -q 'Ollama hardware proof raspberry_pi=true ram_bytes=' "$artifact_dir/ollama-load.log" || fail "Ollama load hardware proof missing"
  grep -q 'on Raspberry Pi ram_bytes=' "$artifact_dir/ollama-load.log" || fail "Ollama load Pi RAM proof missing"
fi
if grep -q '^OK ollama-refuse ' "$summary"; then
  [ -s "$artifact_dir/ollama-refuse.log" ] || fail "Ollama refusal log missing or empty"
  grep -q 'Ollama refused' "$artifact_dir/ollama-refuse.log" || fail "Ollama refusal proof missing"
  grep -q 'Ollama hardware proof raspberry_pi=true ram_bytes=' "$artifact_dir/ollama-refuse.log" || fail "Ollama refusal hardware proof missing"
  grep -q 'on Raspberry Pi ram_bytes=' "$artifact_dir/ollama-refuse.log" || fail "Ollama refusal Pi RAM proof missing"
fi
if grep -q '^OK readonly-root ' "$summary"; then
  [ -s "$artifact_dir/readonly-root.log" ] || fail "read-only root log missing or empty"
  grep -q 'root mount options=' "$artifact_dir/readonly-root.log" || fail "read-only root mount-options proof missing"
  grep -q 'root filesystem mounted read-only' "$artifact_dir/readonly-root.log" || fail "read-only root proof missing"
  grep -q 'data dir mount target=' "$artifact_dir/readonly-root.log" || fail "read-only data-dir mount proof missing"
  grep -q 'data dir mount options=' "$artifact_dir/readonly-root.log" || fail "read-only data-dir mount-options proof missing"
  grep -q 'writable outside read-only root' "$artifact_dir/readonly-root.log" || fail "writable data-dir proof missing"
fi

pass "field acceptance artifact verified: $artifact_dir"
