#!/usr/bin/env sh
set -eu
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
crate_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
ulimit -n 4096 2>/dev/null || true
cargo zigbuild \
  --manifest-path "$crate_dir/Cargo.toml" \
  --target-dir "$crate_dir/target" \
  --target aarch64-unknown-linux-gnu \
  --bin kelp-pi-agent \
  --release \
  "$@"
