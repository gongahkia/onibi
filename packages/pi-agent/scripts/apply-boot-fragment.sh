#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-${KELP_PI_IMAGE_ROOT:-}}"

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT\n' "$0" >&2
  exit 64
}

fragment="${KELP_PI_BOOT_FRAGMENT:-$root/boot/firmware/config.txt.kelp-pi-fragment}"
boot_config="${KELP_PI_BOOT_CONFIG:-$root/boot/firmware/config.txt}"
begin="# kelp-pi hardening begin"
end="# kelp-pi hardening end"

[ -f "$fragment" ] || {
  printf 'missing boot fragment: %s\n' "$fragment" >&2
  exit 66
}

mkdir -p "$(dirname "$boot_config")"
touch "$boot_config"
tmp="$(mktemp "${boot_config}.tmp.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

awk -v begin="$begin" -v end="$end" '
  $0 == begin { skip = 1; next }
  $0 == end { skip = 0; next }
  skip != 1 { print }
' "$boot_config" > "$tmp"

{
  cat "$tmp"
  printf '%s\n' "$begin"
  cat "$fragment"
  printf '%s\n' "$end"
} > "$boot_config"

printf '%s\n' "$boot_config"
