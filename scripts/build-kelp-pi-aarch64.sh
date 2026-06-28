#!/usr/bin/env sh
set -eu

TARGET="${KELP_PI_TARGET:-aarch64-linux-gnu.2.36}"
PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/linux-aarch64}"
IMAGE="${KELP_PI_AARCH64_IMAGE:-debian:bookworm}"
ZIG_VERSION="${KELP_ZIG_VERSION:-0.15.2}"
SYSROOT="${KELP_PI_SYSROOT:-/}"
SYSTEM_INCLUDE_DIR="${KELP_PI_SYSTEM_INCLUDE_DIR:-/usr/include}"
SYSTEM_LIB_DIR="${KELP_PI_SYSTEM_LIB_DIR:-/usr/lib/aarch64-linux-gnu}"
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
  -e SYSROOT="$SYSROOT" \
  -e SYSTEM_INCLUDE_DIR="$SYSTEM_INCLUDE_DIR" \
  -e SYSTEM_LIB_DIR="$SYSTEM_LIB_DIR" \
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
    set -- "$zig_dir/zig" build
    if [ -n "$SYSROOT" ]; then set -- "$@" --sysroot "$SYSROOT"; fi
    if [ -n "$TARGET" ]; then set -- "$@" -Dtarget="$TARGET"; fi
    if [ -n "$SYSTEM_INCLUDE_DIR" ]; then set -- "$@" -Dsystem-include-dir="$SYSTEM_INCLUDE_DIR"; fi
    if [ -n "$SYSTEM_LIB_DIR" ]; then set -- "$@" -Dsystem-lib-dir="$SYSTEM_LIB_DIR"; fi
    set -- "$@" -Doptimize=ReleaseSafe -Dllama=true -Dllama-prefix="$PREFIX"
    "$@"
  '
