#!/usr/bin/env sh
set -eu

TARGET="${KELP_PI_TARGET:-aarch64-linux-gnu}"
PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/linux-aarch64}"
IMAGE="${KELP_PI_AARCH64_IMAGE:-debian:bookworm}"
ZIG_VERSION="${KELP_ZIG_VERSION:-0.15.2}"
REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ "${KELP_PI_SKIP_LLAMA_BUILD:-0}" != "1" ]; then
  KELP_LLAMA_AARCH64_PREFIX="$PREFIX" scripts/build-llama-aarch64.sh
fi

docker run --rm --platform linux/arm64 \
  -v "$REPO_ROOT:/work" \
  -w /work \
  -e TARGET="$TARGET" \
  -e PREFIX="$PREFIX" \
  -e ZIG_VERSION="$ZIG_VERSION" \
  "$IMAGE" \
  sh -lc '
    set -eu
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends ca-certificates curl xz-utils build-essential pkg-config libsqlite3-dev
    zig_dir="/tmp/zig-$ZIG_VERSION"
    if [ ! -x "$zig_dir/zig" ]; then
      curl -fsSL "https://ziglang.org/download/$ZIG_VERSION/zig-aarch64-linux-$ZIG_VERSION.tar.xz" -o /tmp/zig.tar.xz
      mkdir -p "$zig_dir"
      tar -xJf /tmp/zig.tar.xz -C "$zig_dir" --strip-components=1
    fi
    "$zig_dir/zig" build \
      -Dtarget="$TARGET" \
      -Doptimize=ReleaseSafe \
      -Dllama=true \
      -Dllama-prefix="$PREFIX"
  '
