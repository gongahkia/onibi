#!/usr/bin/env sh
set -eu

tcpdump_bin="${KELP_PI_TCPDUMP_BIN:-tcpdump}"
portal_ip="${KELP_PI_PORTAL_IP:-10.42.0.1}"
upstream_interface="${KELP_PI_UPSTREAM_INTERFACE:-}"
domains="captive.apple.com connectivitycheck.gstatic.com clients3.google.com"
duration_seconds=3
probe_command=""
capture_file=""
probe_output=""
tcpdump_pid=""

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
  if [ -n "$tcpdump_pid" ] && kill -0 "$tcpdump_pid" >/dev/null 2>&1; then
    kill "$tcpdump_pid" >/dev/null 2>&1 || true
    wait "$tcpdump_pid" >/dev/null 2>&1 || true
  fi
  [ -z "$capture_file" ] || rm -f "$capture_file"
  [ -z "$probe_output" ] || rm -f "$probe_output"
}
trap cleanup EXIT INT TERM

default_upstream_interface() {
  ip route show default | awk '{
    for (i = 1; i <= NF; i++) {
      if ($i == "dev") {
        print $(i + 1)
        exit
      }
    }
  }'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --upstream-interface)
      [ $# -ge 2 ] || fail "--upstream-interface requires a value"
      upstream_interface="$2"
      shift 2
      ;;
    --portal-ip)
      [ $# -ge 2 ] || fail "--portal-ip requires a value"
      portal_ip="$2"
      shift 2
      ;;
    --domain)
      [ $# -ge 2 ] || fail "--domain requires a value"
      domains="$domains $2"
      shift 2
      ;;
    --duration-seconds)
      [ $# -ge 2 ] || fail "--duration-seconds requires a value"
      duration_seconds="$2"
      shift 2
      ;;
    --probe-command)
      [ $# -ge 2 ] || fail "--probe-command requires a value"
      probe_command="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"
need "$tcpdump_bin"
need awk
need grep
need kill
need mktemp
need rm
need sed
need sleep

if [ -z "$upstream_interface" ]; then
  need ip
  upstream_interface="$(default_upstream_interface)"
fi
[ -n "$upstream_interface" ] || fail "upstream interface missing; pass --upstream-interface"

capture_file="$(mktemp)"
"$tcpdump_bin" -i "$upstream_interface" -n -l 'udp port 53 or tcp port 53' >"$capture_file" 2>/dev/null &
tcpdump_pid="$!"
sleep 1
kill -0 "$tcpdump_pid" >/dev/null 2>&1 || fail "tcpdump did not start on $upstream_interface"
pass "watching DNS egress on $upstream_interface"

if [ -n "$probe_command" ]; then
  probe_output="$(mktemp)"
  sh -c "$probe_command" >"$probe_output"
  sed -n '1,20p' "$probe_output"
  for domain in $domains; do
    grep -Fq "$domain" "$probe_output" || fail "DNS probe output did not contain $domain"
    grep -F "$domain" "$probe_output" | grep -Fq "$portal_ip" || fail "DNS probe output did not map $domain to $portal_ip"
  done
  pass "DNS probe output mapped captive domains to portal IP $portal_ip"
else
  need dig
  for domain in $domains; do
    dig +short "@$portal_ip" "$domain" | grep -qx "$portal_ip" || fail "$domain did not resolve to $portal_ip"
    printf 'DNS probe %s %s\n' "$domain" "$portal_ip"
  done
  pass "local captive DNS probes resolved to $portal_ip"
fi

sleep "$duration_seconds"
if [ -s "$capture_file" ]; then
  sed -n '1,20p' "$capture_file" >&2
  fail "upstream DNS egress observed on $upstream_interface"
fi
pass "zero upstream DNS egress observed"
