#!/usr/bin/env sh
set -eu

output_dir=""
target_ip=""
target_url=""
nuclei_target=""
nuclei_approval_token=""
control_url=""
current_config=""
updated_config=""
session_command=""
client_a=""
client_b=""
ssh_user=""
portal_ip="${KELP_PI_PORTAL_IP:-10.42.0.1}"
upstream_interface=""
dns_probe_command=""
forbidden_ips=""
nuclei_args=""

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

append_forbidden_ip() {
  forbidden_ips="${forbidden_ips:+$forbidden_ips }$1"
}

run_check() {
  name="$1"
  shift
  log="$output_dir/$name.log"
  printf 'RUN %s\n' "$name" | tee -a "$output_dir/summary.txt"
  if "$@" >"$log" 2>&1; then
    printf 'OK %s %s\n' "$name" "$log" | tee -a "$output_dir/summary.txt"
    return 0
  fi
  printf 'FAIL %s %s\n' "$name" "$log" | tee -a "$output_dir/summary.txt" >&2
  sed -n '1,80p' "$log" >&2
  exit 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --output-dir)
      [ $# -ge 2 ] || fail "--output-dir requires a value"
      output_dir="$2"
      shift 2
      ;;
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
    --nuclei-target)
      [ $# -ge 2 ] || fail "--nuclei-target requires a value"
      nuclei_target="$2"
      shift 2
      ;;
    --nuclei-approval-token)
      [ $# -ge 2 ] || fail "--nuclei-approval-token requires a value"
      nuclei_approval_token="$2"
      shift 2
      ;;
    --control-url)
      [ $# -ge 2 ] || fail "--control-url requires a value"
      control_url="$2"
      shift 2
      ;;
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
    --client-a)
      [ $# -ge 2 ] || fail "--client-a requires a value"
      client_a="$2"
      shift 2
      ;;
    --client-b)
      [ $# -ge 2 ] || fail "--client-b requires a value"
      client_b="$2"
      shift 2
      ;;
    --ssh-user)
      [ $# -ge 2 ] || fail "--ssh-user requires a value"
      ssh_user="$2"
      shift 2
      ;;
    --portal-ip)
      [ $# -ge 2 ] || fail "--portal-ip requires a value"
      portal_ip="$2"
      shift 2
      ;;
    --upstream-interface)
      [ $# -ge 2 ] || fail "--upstream-interface requires a value"
      upstream_interface="$2"
      shift 2
      ;;
    --dns-probe-command)
      [ $# -ge 2 ] || fail "--dns-probe-command requires a value"
      dns_probe_command="$2"
      shift 2
      ;;
    --forbidden-ip)
      [ $# -ge 2 ] || fail "--forbidden-ip requires a value"
      append_forbidden_ip "$2"
      shift 2
      ;;
    --nuclei-arg)
      [ $# -ge 2 ] || fail "--nuclei-arg requires a value"
      nuclei_args="${nuclei_args:+$nuclei_args
}$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"
[ -n "$output_dir" ] || fail "requires --output-dir"
[ -n "$target_ip" ] || fail "requires --target-ip"
[ -n "$target_url" ] || fail "requires --target-url"
[ -n "$nuclei_target" ] || nuclei_target="$target_url"
[ -n "$nuclei_approval_token" ] || fail "requires --nuclei-approval-token"
[ -n "$control_url" ] || fail "requires --control-url"
[ -n "$current_config" ] || fail "requires --current-config"
[ -n "$updated_config" ] || fail "requires --updated-config"
[ -n "$session_command" ] || fail "requires --session-command"
[ -n "$client_a" ] || fail "requires --client-a"
[ -n "$client_b" ] || fail "requires --client-b"
[ -n "$upstream_interface" ] || fail "requires --upstream-interface"
[ -n "$dns_probe_command" ] || fail "requires --dns-probe-command"

need date
need kelp-pi-validate-node
need kelp-pi-validate-scanner-sandbox
need kelp-pi-validate-allow-outbound-reload
need kelp-pi-validate-dns-egress
need kelp-pi-validate-ap-isolation
need kelp-pi-validate-nuclei-scan
need mkdir
need sed
need tee
need uname

mkdir -p "$output_dir"
: >"$output_dir/summary.txt"
{
  printf 'started_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'uname=%s\n' "$(uname -a)"
} >"$output_dir/host.txt"

run_check node kelp-pi-validate-node

run_check scanner-sandbox \
  kelp-pi-validate-scanner-sandbox \
  --target-ip "$target_ip" \
  --target-url "$target_url" \
  --control-url "$control_url"

set -- kelp-pi-validate-nuclei-scan --target "$nuclei_target" --approval-token "$nuclei_approval_token"
if [ -n "$nuclei_args" ]; then
  set -- "$@" --
  old_ifs="$IFS"
  IFS='
'
  for nuclei_arg in $nuclei_args; do
    set -- "$@" "$nuclei_arg"
  done
  IFS="$old_ifs"
fi
run_check nuclei-scan "$@"

run_check allow-outbound-reload \
  kelp-pi-validate-allow-outbound-reload \
  --current-config "$current_config" \
  --updated-config "$updated_config" \
  --session-command "$session_command"

run_check dns-egress \
  kelp-pi-validate-dns-egress \
  --upstream-interface "$upstream_interface" \
  --portal-ip "$portal_ip" \
  --probe-command "$dns_probe_command"

set -- kelp-pi-validate-ap-isolation --client-a "$client_a" --client-b "$client_b" --portal-ip "$portal_ip"
[ -z "$ssh_user" ] || set -- "$@" --ssh-user "$ssh_user"
for forbidden_ip in $forbidden_ips; do
  set -- "$@" --forbidden-ip "$forbidden_ip"
done
run_check ap-isolation "$@"

pass "field acceptance logs written to $output_dir"
