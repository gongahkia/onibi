#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-${KELP_PI_IMAGE_ROOT:-}}"
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
crate_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT\n' "$0" >&2
  exit 64
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "$1" >&2
    exit 69
  }
}

need install

install -d "$root/etc/systemd/system" "$root/usr/lib/sysusers.d" "$root/usr/lib/tmpfiles.d"
install -m 0644 "$crate_dir/systemd/kelp-pi-agent.service" "$root/etc/systemd/system/kelp-pi-agent.service"
install -m 0644 "$crate_dir/systemd/kelp-pi.service" "$root/etc/systemd/system/kelp-pi.service"
install -m 0644 "$crate_dir/systemd/kelp-pi-agent.sysusers.conf" "$root/usr/lib/sysusers.d/kelp-pi-agent.conf"
install -m 0644 "$crate_dir/systemd/kelp-pi-agent.tmpfiles.conf" "$root/usr/lib/tmpfiles.d/kelp-pi-agent.conf"

printf '%s\n' \
  "$root/etc/systemd/system/kelp-pi-agent.service" \
  "$root/etc/systemd/system/kelp-pi.service" \
  "$root/usr/lib/sysusers.d/kelp-pi-agent.conf" \
  "$root/usr/lib/tmpfiles.d/kelp-pi-agent.conf"
