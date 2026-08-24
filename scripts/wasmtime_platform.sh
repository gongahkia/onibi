#!/bin/sh
# Describe the pinned Wasmtime C API for the current host platform.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

find_linux_lua() {
  for library in liblua-5.4.so liblua5.4.so.0 liblua5.4.so; do
    if command -v ldconfig >/dev/null 2>&1; then
      candidate=$(ldconfig -p 2>/dev/null | awk -v library="$library" '$1 == library { print $NF; exit }')
      if test -n "$candidate" && test -f "$candidate"; then
        printf '%s\n' "$candidate"
        return 0
      fi
    fi
    for directory in /lib64 /usr/lib64 /lib/x86_64-linux-gnu /usr/lib/x86_64-linux-gnu; do
      candidate="$directory/$library"
      if test -f "$candidate"; then
        printf '%s\n' "$candidate"
        return 0
      fi
    done
  done
  return 1
}

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)
    archive="wasmtime-v47.0.3-x86_64-linux-c-api.tar.xz"
    checksum="aaa3621f2a3d8393696702897f8f78a1cc504437d500701496d560125aefd732"
    library="libwasmtime.so"
    build_rpath='$ORIGIN/../../../.zenbu/wasmtime-c-api/lib'
    install_rpath='$ORIGIN/../lib/zenbu'
    lua_library=$(find_linux_lua || true)
    ;;
  Darwin-arm64)
    archive="wasmtime-v47.0.3-aarch64-macos-c-api.tar.xz"
    checksum="1854c8f03a764c89afe77fa88d9092ab89a368e527cd27a12959b1d91152324e"
    library="libwasmtime.dylib"
    build_rpath='@loader_path/../../../.zenbu/wasmtime-c-api/lib'
    install_rpath='@loader_path/../lib/zenbu'
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
  build-rpath) printf '%s\n' "$build_rpath" ;;
  install-rpath) printf '%s\n' "$install_rpath" ;;
  env)
    emit ZENBU_WASMTIME_C_API_ARCHIVE_DIR "$archive_dir"
    emit ZENBU_WASMTIME_C_API_DIR "$selected_dir"
    emit ZENBU_WASMTIME_C_API_INCLUDE_DIR "$selected_dir/include"
    emit ZENBU_WASMTIME_C_API_LIB_DIR "$selected_dir/lib"
    emit ZENBU_WASMTIME_C_API_LIBRARY "$library"
    emit ZENBU_WASMTIME_C_API_BUILD_RPATH "$build_rpath"
    emit ZENBU_WASMTIME_C_API_INSTALL_RPATH "$install_rpath"
    emit ZENBU_LUA_LIBRARY "$lua_library"
    printf '%s\n' 'export ZENBU_WASMTIME_C_API_ARCHIVE_DIR ZENBU_WASMTIME_C_API_DIR ZENBU_WASMTIME_C_API_INCLUDE_DIR ZENBU_WASMTIME_C_API_LIB_DIR ZENBU_WASMTIME_C_API_LIBRARY ZENBU_WASMTIME_C_API_BUILD_RPATH ZENBU_WASMTIME_C_API_INSTALL_RPATH ZENBU_LUA_LIBRARY'
    ;;
  *)
    echo "usage: $0 {archive|checksum|library|archive-dir|selected-dir|build-rpath|install-rpath|env}" >&2
    exit 2
    ;;
esac
