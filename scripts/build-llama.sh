#!/usr/bin/env sh
set -eu

BUILD_DIR="${KELP_LLAMA_BUILD_DIR:-.kelp-pi/llama-build/host}"
PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/host}"
NATIVE="${KELP_LLAMA_NATIVE:-OFF}"
JOBS="${KELP_LLAMA_JOBS:-}"

if [ -z "$JOBS" ]; then
  JOBS="$(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || printf '4')"
fi

if [ "${KELP_LLAMA_FORCE:-0}" != "1" ] && [ -d "$PREFIX/lib" ]; then
  if find "$PREFIX/lib" -maxdepth 1 \( -name 'libllama*.dylib' -o -name 'libllama*.so*' \) | grep -q .; then
    printf '%s\n' "$PREFIX"
    exit 0
  fi
fi

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
  -DGGML_NATIVE="$NATIVE" \
  -DGGML_OPENMP=OFF \
  -DGGML_BLAS=OFF \
  -DGGML_ACCELERATE=OFF \
  -DGGML_METAL=OFF

cmake --build "$BUILD_DIR" --target llama --parallel "$JOBS"
cmake --install "$BUILD_DIR"
printf '%s\n' "$PREFIX"
