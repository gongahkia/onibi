#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-${KELP_PI_IMAGE_ROOT:-}}"
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT\n' "$0" >&2
  exit 64
}

install -d "$root/usr/local/sbin"
install -m 0755 "$script_dir/validate-pi-node.sh" "$root/usr/local/sbin/kelp-pi-validate-node"
install -m 0755 "$script_dir/validate-scanner-sandbox.sh" "$root/usr/local/sbin/kelp-pi-validate-scanner-sandbox"
install -m 0755 "$script_dir/validate-allow-outbound-reload.sh" "$root/usr/local/sbin/kelp-pi-validate-allow-outbound-reload"
install -m 0755 "$script_dir/validate-dns-egress.sh" "$root/usr/local/sbin/kelp-pi-validate-dns-egress"
install -m 0755 "$script_dir/validate-ap-isolation.sh" "$root/usr/local/sbin/kelp-pi-validate-ap-isolation"
install -m 0755 "$script_dir/validate-field-acceptance.sh" "$root/usr/local/sbin/kelp-pi-validate-field-acceptance"
install -m 0755 "$script_dir/validate-ollama-load.sh" "$root/usr/local/sbin/kelp-pi-validate-ollama-load"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-node"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-scanner-sandbox"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-allow-outbound-reload"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-dns-egress"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-ap-isolation"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-field-acceptance"
printf '%s\n' "$root/usr/local/sbin/kelp-pi-validate-ollama-load"
