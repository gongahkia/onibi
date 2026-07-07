#!/usr/bin/env sh
set -eu

apply=0
activate_ap=0
ack=0
root="${KELP_PI_HARDENING_ROOT:-}"
ap_interface="${KELP_PI_AP_INTERFACE:-wlan0}"
ap_ssid="${KELP_PI_AP_SSID:-Kelp-Pi}"
ap_passphrase="${KELP_PI_AP_PASSPHRASE:-}"
ap_address="${KELP_PI_AP_ADDRESS:-10.42.0.1}"
ap_prefix="${KELP_PI_AP_PREFIX:-24}"
dhcp_start="${KELP_PI_DHCP_START:-10.42.0.20}"
dhcp_end="${KELP_PI_DHCP_END:-10.42.0.240}"
control_plane_host="${KELP_PI_CONTROL_PLANE_HOST:-}"
recovery_doc="${KELP_PI_RECOVERY_DOC:-docs/pi-recovery.md}"
backup_dir=""

usage() {
  cat <<'USAGE'
usage: apply-kelp-pi-network-hardening.sh --render-dir DIR [options]
       apply-kelp-pi-network-hardening.sh --apply --i-understand-ssh-risk [options]

Options:
  --render-dir DIR             Render files under DIR instead of applying to /
  --apply                      Write rendered files under / and load nftables
  --activate-ap                Also activate the NetworkManager AP profile
  --i-understand-ssh-risk      Required with --apply
  --ap-interface IFACE         Default: wlan0
  --ap-ssid SSID               Default: Kelp-Pi
  --ap-passphrase SECRET       WPA-PSK passphrase; required with --activate-ap
  --control-plane-host HOST    Optional host allowed for outbound TCP/443
  --recovery-doc PATH          Recovery runbook. Default: docs/pi-recovery.md
USAGE
}

fail() {
  printf 'ERROR %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --render-dir)
      [ $# -ge 2 ] || fail "--render-dir requires a value"
      root="$2"
      shift 2
      ;;
    --apply)
      apply=1
      root="/"
      shift
      ;;
    --activate-ap)
      activate_ap=1
      shift
      ;;
    --i-understand-ssh-risk)
      ack=1
      shift
      ;;
    --ap-interface)
      [ $# -ge 2 ] || fail "--ap-interface requires a value"
      ap_interface="$2"
      shift 2
      ;;
    --ap-ssid)
      [ $# -ge 2 ] || fail "--ap-ssid requires a value"
      ap_ssid="$2"
      shift 2
      ;;
    --ap-passphrase)
      [ $# -ge 2 ] || fail "--ap-passphrase requires a value"
      ap_passphrase="$2"
      shift 2
      ;;
    --control-plane-host)
      [ $# -ge 2 ] || fail "--control-plane-host requires a value"
      control_plane_host="$2"
      shift 2
      ;;
    --recovery-doc)
      [ $# -ge 2 ] || fail "--recovery-doc requires a value"
      recovery_doc="$2"
      shift 2
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ -n "$root" ] || fail "choose --render-dir DIR or --apply"
case "$ap_ssid" in
  ""|*\"*|*\\*) fail "AP SSID must be non-empty and must not contain quotes or backslashes" ;;
esac
case "$ap_passphrase" in
  *\"*|*\\*) fail "AP passphrase must not contain quotes or backslashes" ;;
esac
[ "$activate_ap" != "1" ] || [ -n "$ap_passphrase" ] || fail "--activate-ap requires --ap-passphrase"
[ -r "$recovery_doc" ] || fail "recovery doc not readable: $recovery_doc"

if [ "$apply" = "1" ]; then
  [ "$ack" = "1" ] || fail "--apply requires --i-understand-ssh-risk after reading $recovery_doc"
  [ "$(id -u)" = "0" ] || fail "--apply must run as root"
  need nft
  need install
  backup_dir="/var/backups/kelp-pi-network-hardening/$(date -u +%Y%m%dT%H%M%SZ)"
  install -d "$backup_dir"
  for rel in \
    etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection \
    etc/dnsmasq.d/kelp-pi-captive.conf \
    etc/nftables.d/kelp-pi.nft \
    etc/sysctl.d/90-kelp-pi-network.conf \
    etc/kelp-pi/network-hardening.json
  do
    src="/$rel"
    if [ -e "$src" ]; then
      mkdir -p "$backup_dir/$(dirname "$rel")"
      cp -p "$src" "$backup_dir/$rel"
    fi
  done
fi

target_path() {
  rel="$1"
  if [ "$root" = "/" ]; then
    printf '/%s\n' "$rel"
  else
    printf '%s/%s\n' "$root" "$rel"
  fi
}

write_file() {
  rel="$1"
  mode="$2"
  path="$(target_path "$rel")"
  mkdir -p "$(dirname "$path")"
  cat > "$path"
  chmod "$mode" "$path"
}

control_plane_rule=""
if [ -n "$control_plane_host" ]; then
  control_plane_rule="    ip daddr $control_plane_host tcp dport 443 accept"
fi

wifi_security_block=""
if [ -n "$ap_passphrase" ]; then
  wifi_security_block="
[wifi-security]
key-mgmt=wpa-psk
psk=$ap_passphrase
"
fi

write_file "etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection" 0600 <<EOF_NM
[connection]
id=kelp-pi-ap
type=wifi
interface-name=$ap_interface
autoconnect=false

[wifi]
mode=ap
ssid=$ap_ssid
$wifi_security_block

[ipv4]
method=shared
address1=$ap_address/$ap_prefix

[ipv6]
method=disabled
EOF_NM

write_file "etc/dnsmasq.d/kelp-pi-captive.conf" 0644 <<EOF_DNS
interface=$ap_interface
bind-interfaces
dhcp-range=$dhcp_start,$dhcp_end,12h
no-resolv
address=/captive.apple.com/$ap_address
address=/connectivitycheck.gstatic.com/$ap_address
address=/clients3.google.com/$ap_address
address=/connectivitycheck.android.com/$ap_address
EOF_DNS

write_file "etc/nftables.d/kelp-pi.nft" 0644 <<EOF_NFT
flush table inet kelp_pi_filter

table inet kelp_pi_filter {
  set scanner_users {
    typeof meta skuid
  }

  set scanner_ipv4_targets {
    type ipv4_addr
  }

  chain input {
    type filter hook input priority filter; policy drop;
    iifname "lo" accept
    ct state established,related accept
    tcp dport 22 accept
    iifname "$ap_interface" udp dport { 53, 67 } accept
    iifname "$ap_interface" tcp dport { 80, 443, 8080 } accept
    iifname "$ap_interface" ip protocol icmp accept
  }

  chain forward {
    type filter hook forward priority filter; policy drop;
  }

  chain output {
    type filter hook output priority filter; policy drop;
    oifname "lo" accept
    ct state established,related accept
$control_plane_rule
    meta skuid @scanner_users ip daddr @scanner_ipv4_targets accept
    meta skuid @scanner_users drop
  }
}
EOF_NFT

write_file "etc/sysctl.d/90-kelp-pi-network.conf" 0644 <<'EOF_SYSCTL'
net.ipv4.ip_forward=0
net.ipv6.conf.all.forwarding=0
EOF_SYSCTL

write_file "etc/kelp-pi/network-hardening.json" 0644 <<EOF_JSON
{
  "schemaVersion": "kelp.pi.network-hardening.v1",
  "apInterface": "$ap_interface",
  "apSsid": "$ap_ssid",
  "apAddress": "$ap_address",
  "apPrefix": $ap_prefix,
  "dhcpStart": "$dhcp_start",
  "dhcpEnd": "$dhcp_end",
  "controlPlaneHost": "$control_plane_host",
  "sshRecoveryPreserved": true,
  "recoveryDoc": "$recovery_doc"
}
EOF_JSON

write_file "usr/local/sbin/kelp-pi-recover-network.sh" 0755 <<'EOF_RECOVER'
#!/usr/bin/env sh
set -eu
nft delete table inet kelp_pi_filter 2>/dev/null || true
nmcli connection down kelp-pi-ap 2>/dev/null || true
nmcli connection delete kelp-pi-ap 2>/dev/null || true
systemctl restart NetworkManager 2>/dev/null || true
systemctl restart ssh 2>/dev/null || systemctl restart sshd 2>/dev/null || true
printf 'Kelp Pi network hardening rollback attempted. Re-run docs/pi-recovery.md checks.\n'
EOF_RECOVER

printf 'Rendered Kelp Pi network hardening files under %s\n' "$root"
printf 'Recovery runbook: %s\n' "$recovery_doc"

if [ "$apply" = "1" ]; then
  nft -f /etc/nftables.d/kelp-pi.nft
  sysctl --system >/dev/null 2>&1 || true
  systemctl enable --now nftables >/dev/null 2>&1 || true
  systemctl restart dnsmasq >/dev/null 2>&1 || true
  if [ "$activate_ap" = "1" ]; then
    nmcli connection reload
    nmcli connection up kelp-pi-ap
  fi
  printf 'Applied nft/sysctl hardening. Backup: %s\n' "$backup_dir"
  printf 'SSH recovery helper: /usr/local/sbin/kelp-pi-recover-network.sh\n'
fi
