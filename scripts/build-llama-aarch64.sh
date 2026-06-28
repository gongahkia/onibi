#!/usr/bin/env sh
set -eu

IMAGE="${KELP_PI_AARCH64_IMAGE:-debian:bookworm}"
BUILD_DIR="${KELP_LLAMA_AARCH64_BUILD_DIR:-.kelp-pi/llama-build/linux-aarch64}"
PREFIX="${KELP_LLAMA_AARCH64_PREFIX:-.kelp-pi/llama/linux-aarch64}"
JOBS="${KELP_LLAMA_JOBS:-4}"
REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ "${KELP_LLAMA_FORCE:-0}" != "1" ] && [ -d "$PREFIX/lib" ]; then
  if find "$PREFIX/lib" -maxdepth 1 \( -name 'libllama*.so' -o -name 'libllama*.so.*' \) | grep -q .; then
    printf '%s\n' "$PREFIX"
    exit 0
  fi
fi

docker run --rm --platform linux/arm64 \
  -v "$REPO_ROOT:/work" \
  -w /work \
  -e BUILD_DIR="$BUILD_DIR" \
  -e PREFIX="$PREFIX" \
  -e JOBS="$JOBS" \
  "$IMAGE" \
  sh -lc '
    set -eu
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends ca-certificates cmake build-essential git pkg-config
    cmake -S vendor/llama.cpp -B "$BUILD_DIR" \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_INSTALL_PREFIX="$PREFIX" \
      -DBUILD_SHARED_LIBS=ON \
      -DLLAMA_BUILD_COMMON=OFF \
      -DLLAMA_BUILD_TESTS=OFF \
      -DLLAMA_BUILD_TOOLS=OFF \
      -DLLAMA_BUILD_EXAMPLES=OFF \
      -DLLAMA_BUILD_SERVER=OFF \
      -DLLAMA_BUILD_APP=OFF \
      -DGGML_NATIVE=OFF \
      -DGGML_OPENMP=OFF \
      -DGGML_BLAS=OFF
    cmake --build "$BUILD_DIR" --target llama --parallel "$JOBS"
    cmake --install "$BUILD_DIR"
  '
printf '%s\n' "$PREFIX"
