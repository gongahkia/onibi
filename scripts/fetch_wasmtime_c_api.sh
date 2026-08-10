#!/bin/sh
# Fetch the exact private Wasmtime C API used by the M9 Component adapter.
set -eu

version="47.0.3"
archive="wasmtime-v${version}-x86_64-linux-c-api.tar.xz"
checksum="aaa3621f2a3d8393696702897f8f78a1cc504437d500701496d560125aefd732"
url="https://github.com/bytecodealliance/wasmtime/releases/download/v${version}/${archive}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
target=${ZENBU_WASMTIME_C_API_DIR:-"$repo_dir/.zenbu/${archive%.tar.xz}"}
target_root=$(dirname -- "$target")

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) ;;
  *)
    echo "Zenbu M9 currently pins the official x86_64 Linux Wasmtime C API; unsupported platform: $(uname -s)-$(uname -m)" >&2
    exit 1
    ;;
esac

if test -f "$target/include/wasmtime.h" && test -f "$target/lib/libwasmtime.so"; then
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
if ! command -v sha256sum >/dev/null 2>&1; then
  echo "sha256sum is required to verify the Wasmtime C API archive" >&2
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
actual=$(sha256sum "$archive_path" | awk '{print $1}')
if test "$actual" != "$checksum"; then
  echo "Wasmtime C API checksum mismatch: expected $checksum, got $actual" >&2
  exit 1
fi

tar -xJf "$archive_path" -C "$temporary"
extracted="$temporary/${archive%.tar.xz}"
if ! test -f "$extracted/include/wasmtime.h" || ! test -f "$extracted/lib/libwasmtime.so"; then
  echo "Wasmtime C API archive did not contain the expected headers and shared library" >&2
  exit 1
fi

mv "$extracted" "$target"
echo "installed pinned Wasmtime C API v$version at $target"
