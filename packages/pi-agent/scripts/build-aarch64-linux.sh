#!/usr/bin/env sh
set -eu
ulimit -n 4096 2>/dev/null || true
cargo zigbuild --target aarch64-unknown-linux-gnu --bin kelp-pi-agent --release "$@"
