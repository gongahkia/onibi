#!/usr/bin/env sh
set -eu

TARGET="${KELP_PI_TARGET:-aarch64-linux-gnu}"
PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/linux-aarch64}"

zig build \
  -Dtarget="$TARGET" \
  -Doptimize=ReleaseSafe \
  -Dllama=true \
  -Dllama-prefix="$PREFIX"
