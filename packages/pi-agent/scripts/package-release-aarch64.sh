#!/usr/bin/env sh
set -eu

script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
crate_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
[ "${1:-}" = "--" ] && shift
out_dir="${1:-$crate_dir/dist}"
target="aarch64-unknown-linux-gnu"
asset="kelp-pi-agent-aarch64"
binary="$crate_dir/target/$target/release/kelp-pi-agent"

"$script_dir/build-aarch64-linux.sh"

install -d "$out_dir"
if command -v aarch64-linux-gnu-objcopy >/dev/null 2>&1; then
  aarch64-linux-gnu-objcopy --strip-all "$binary" "$out_dir/$asset"
else
  install -m 0755 "$binary" "$out_dir/$asset"
fi
chmod 0755 "$out_dir/$asset"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$out_dir" && sha256sum "$asset" > "$asset.sha256")
else
  (cd "$out_dir" && shasum -a 256 "$asset" | awk '{ print $1 "  " $2 }' > "$asset.sha256")
fi

printf '%s\n' "$out_dir/$asset"
printf '%s\n' "$out_dir/$asset.sha256"
