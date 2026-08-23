#!/bin/sh
# Describe the pinned Wasmtime C API for the current host platform.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)
    archive="wasmtime-v47.0.3-x86_64-linux-c-api.tar.xz"
    checksum="aaa3621f2a3d8393696702897f8f78a1cc504437d500701496d560125aefd732"
    library="libwasmtime.so"
    rpath='$ORIGIN/../../../.zenbu/wasmtime-c-api/lib:$ORIGIN/../lib/zenbu'
    lua_library=""
    ;;
  Darwin-arm64)
    archive="wasmtime-v47.0.3-aarch64-macos-c-api.tar.xz"
    checksum="1854c8f03a764c89afe77fa88d9092ab89a368e527cd27a12959b1d91152324e"
    library="libwasmtime.dylib"
    rpath='@loader_path/../../../.zenbu/wasmtime-c-api/lib:@loader_path/../lib/zenbu'
    lua_library=""
    if command -v brew >/dev/null 2>&1; then
      lua_prefix=$(brew --prefix lua@5.4 2>/dev/null || true)
      lua_candidate="$lua_prefix/lib/liblua.5.4.dylib"
      if test -f "$lua_candidate"; then lua_library="$lua_candidate"; fi
    fi
    ;;
  *)
    echo "Zenbu supports the pinned Wasmtime C API on Linux-x86_64 and Darwin-arm64; unsupported platform: $(uname -s)-$(uname -m)" >&2
    exit 1
    ;;
esac

archive_dir="$repo_dir/.zenbu/${archive%.tar.xz}"
selected_dir="$repo_dir/.zenbu/wasmtime-c-api"

quote_shell() {
  printf "'"
  printf "%s" "$1" | sed "s/'/'\\\\''/g"
  printf "'"
}

emit() {
  printf '%s=' "$1"
  quote_shell "$2"
  printf '\n'
}

case "${1:-env}" in
  archive) printf '%s\n' "$archive" ;;
  checksum) printf '%s\n' "$checksum" ;;
  library) printf '%s\n' "$library" ;;
  archive-dir) printf '%s\n' "$archive_dir" ;;
  selected-dir) printf '%s\n' "$selected_dir" ;;
  rpath) printf '%s\n' "$rpath" ;;
  env)
    emit ZENBU_WASMTIME_C_API_ARCHIVE_DIR "$archive_dir"
    emit ZENBU_WASMTIME_C_API_DIR "$selected_dir"
    emit ZENBU_WASMTIME_C_API_INCLUDE_DIR "$selected_dir/include"
    emit ZENBU_WASMTIME_C_API_LIB_DIR "$selected_dir/lib"
    emit ZENBU_WASMTIME_C_API_LIBRARY "$library"
    emit ZENBU_WASMTIME_C_API_RPATH "$rpath"
    emit ZENBU_LUA_LIBRARY "$lua_library"
    printf '%s\n' 'export ZENBU_WASMTIME_C_API_ARCHIVE_DIR ZENBU_WASMTIME_C_API_DIR ZENBU_WASMTIME_C_API_INCLUDE_DIR ZENBU_WASMTIME_C_API_LIB_DIR ZENBU_WASMTIME_C_API_LIBRARY ZENBU_WASMTIME_C_API_RPATH ZENBU_LUA_LIBRARY'
    ;;
  *)
    echo "usage: $0 {archive|checksum|library|archive-dir|selected-dir|rpath|env}" >&2
    exit 2
    ;;
esac
