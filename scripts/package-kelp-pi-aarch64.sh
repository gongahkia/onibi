#!/usr/bin/env sh
set -eu

KELP_LLAMA_PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/linux-aarch64}" \
KELP_PI_PACKAGE_DIR="${KELP_PI_PACKAGE_DIR:-.kelp-pi/dist/kelp-pi-linux-aarch64}" \
KELP_PI_PACKAGE_TARGET="${KELP_PI_PACKAGE_TARGET:-aarch64-linux-gnu}" \
KELP_PI_REQUIRE_LLAMA_LIBS=1 \
  scripts/package-kelp-pi.sh
