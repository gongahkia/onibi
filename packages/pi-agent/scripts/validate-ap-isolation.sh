#!/usr/bin/env sh
set -eu

ssh_bin="${KELP_PI_SSH_BIN:-ssh}"
ssh_user="${KELP_PI_CLIENT_SSH_USER:-}"
portal_ip="${KELP_PI_PORTAL_IP:-10.42.0.1}"
client_a=""
client_b=""
forbidden_ips=""

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

ssh_target() {
  if [ -n "$ssh_user" ]; then
    printf '%s@%s\n' "$ssh_user" "$1"
  else
    printf '%s\n' "$1"
  fi
}

client_ping() {
  client="$1"
  target="$2"
  "$ssh_bin" \
    -o BatchMode=yes \
    -o ConnectTimeout=5 \
    -o StrictHostKeyChecking=accept-new \
    "$(ssh_target "$client")" \
    ping -c 2 -W 2 "$target" >/dev/null 2>&1
}

while [ $# -gt 0 ]; do
  case "$1" in
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
    --forbidden-ip)
      [ $# -ge 2 ] || fail "--forbidden-ip requires a value"
      append_forbidden_ip "$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ -n "$client_a" ] || fail "requires --client-a"
[ -n "$client_b" ] || fail "requires --client-b"
[ "$client_a" != "$client_b" ] || fail "client addresses must differ"
[ -n "$forbidden_ips" ] || fail "requires at least one --forbidden-ip to prove only the portal IP responds"
need "$ssh_bin"

pass "AP isolation clients client_a=$client_a client_b=$client_b portal_ip=$portal_ip"
client_ping "$client_a" "$portal_ip" || fail "$client_a cannot reach portal $portal_ip"
client_ping "$client_b" "$portal_ip" || fail "$client_b cannot reach portal $portal_ip"
pass "both clients reached portal $portal_ip"

if client_ping "$client_a" "$client_b"; then
  fail "$client_a reached isolated peer $client_b"
fi
if client_ping "$client_b" "$client_a"; then
  fail "$client_b reached isolated peer $client_a"
fi
pass "AP clients cannot ping each other"

for forbidden_ip in $forbidden_ips; do
  if client_ping "$client_a" "$forbidden_ip"; then
    fail "$client_a reached forbidden IP $forbidden_ip"
  fi
  if client_ping "$client_b" "$forbidden_ip"; then
    fail "$client_b reached forbidden IP $forbidden_ip"
  fi
done
pass "clients cannot reach forbidden IP probes"
