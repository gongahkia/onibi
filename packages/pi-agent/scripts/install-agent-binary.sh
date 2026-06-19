#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-${KELP_PI_IMAGE_ROOT:-}}"
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
crate_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
agent_bin="${KELP_PI_AGENT_BUILD:-$crate_dir/target/aarch64-unknown-linux-gnu/release/kelp-pi-agent}"

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT\n' "$0" >&2
  exit 64
}

[ -x "$agent_bin" ] || {
  printf 'missing executable agent binary: %s\n' "$agent_bin" >&2
  printf 'run: pnpm --filter @kelpclaw/pi-agent build:pi\n' >&2
  exit 66
}

install -d "$root/usr/local/bin"
install -m 0755 "$agent_bin" "$root/usr/local/bin/kelp-pi-agent"
printf '%s\n' "$root/usr/local/bin/kelp-pi-agent"
