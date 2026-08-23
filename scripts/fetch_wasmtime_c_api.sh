#!/bin/sh
# Fetch the exact private Wasmtime C API used by the M9 Component adapter.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
platform="$script_dir/wasmtime_platform.sh"
archive=$($platform archive)
checksum=$($platform checksum)
library=$($platform library)
archive_dir=$($platform archive-dir)
selected_dir=$($platform selected-dir)
url="https://github.com/bytecodealliance/wasmtime/releases/download/v47.0.3/${archive}"
target=${ZENBU_WASMTIME_C_API_ARCHIVE_DIR:-"$archive_dir"}
target_root=$(dirname -- "$target")

activate_selected_runtime() {
  if test -e "$selected_dir" && ! test -L "$selected_dir"; then
    echo "Wasmtime C API selected-runtime path exists and is not a symlink: $selected_dir" >&2
    exit 1
  fi
  rm -f "$selected_dir"
  ln -s "$target" "$selected_dir"
}

if test -f "$target/include/wasmtime.h" && test -f "$target/lib/$library"; then
  activate_selected_runtime
  exit 0
fi

if test -e "$target"; then
  echo "Wasmtime C API destination exists but is incomplete: $target" >&2
  echo "Remove that exact directory after inspecting it, then retry." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to fetch the pinned Wasmtime C API" >&2
  exit 1
fi
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  echo "sha256sum or shasum is required to verify the Wasmtime C API archive" >&2
  exit 1
fi
if ! command -v tar >/dev/null 2>&1; then
  echo "tar is required to unpack the Wasmtime C API archive" >&2
  exit 1
fi

mkdir -p "$target_root"
temporary=$(mktemp -d "$target_root/.wasmtime-fetch.XXXXXX")
cleanup() {
  rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM

archive_path="$temporary/$archive"
curl --fail --location --retry 3 --output "$archive_path" "$url"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive_path" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
fi
if test "$actual" != "$checksum"; then
  echo "Wasmtime C API checksum mismatch: expected $checksum, got $actual" >&2
  exit 1
fi

tar -xJf "$archive_path" -C "$temporary"
extracted="$temporary/${archive%.tar.xz}"
if ! test -f "$extracted/include/wasmtime.h" || ! test -f "$extracted/lib/$library"; then
  echo "Wasmtime C API archive did not contain the expected headers and shared library" >&2
  exit 1
fi

mv "$extracted" "$target"
activate_selected_runtime
echo "installed pinned Wasmtime C API v47.0.3 at $target"
