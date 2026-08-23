#!/bin/sh
# Package the already-built release binaries and their runtime dependencies.
set -eu

if test "$#" -ne 2; then
  echo "usage: $0 RELEASE_BUILD_DIR OUTPUT_DIR" >&2
  exit 2
fi

release_build_dir=$1
output_dir=$2
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
platform="$script_dir/wasmtime_platform.sh"

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) archive_platform="linux-x86_64" ;;
  Darwin-arm64) archive_platform="darwin-arm64" ;;
  *)
    echo "cannot package a Zenbu release for unsupported host: $(uname -s)-$(uname -m)" >&2
    exit 2
    ;;
esac

version=$(tr -d '\n' < "$repo_dir/VERSION")
bundle_name="zenbu-${version}-${archive_platform}"
archive_path="$output_dir/${bundle_name}.tar.gz"
runtime_dir=$($platform selected-dir)
runtime_library=$($platform library)

for executable in zenbu.exe zenbu_headless.exe; do
  test -x "$release_build_dir/default/bin/$executable" || {
    echo "missing release binary: $release_build_dir/default/bin/$executable" >&2
    exit 2
  }
done
test -f "$runtime_dir/lib/$runtime_library" || {
  echo "missing pinned Wasmtime runtime: $runtime_dir/lib/$runtime_library" >&2
  exit 2
}
test ! -e "$archive_path" || {
  echo "refusing to overwrite existing release archive: $archive_path" >&2
  exit 2
}

mkdir -p "$output_dir"
temporary=$(mktemp -d "${TMPDIR:-/tmp}/zenbu-release.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
bundle="$temporary/$bundle_name"
mkdir -p "$bundle/bin" "$bundle/lib/zenbu"
install -m 755 "$runtime_dir/lib/$runtime_library" "$bundle/lib/zenbu/$runtime_library"

case "$archive_platform" in
  linux-x86_64)
    install -m 755 "$release_build_dir/default/bin/zenbu.exe" "$bundle/bin/zenbu"
    install -m 755 "$release_build_dir/default/bin/zenbu_headless.exe" \
      "$bundle/bin/zenbu-headless"
    ;;
  darwin-arm64)
    mkdir -p "$bundle/libexec"
    install -m 755 "$release_build_dir/default/bin/zenbu.exe" "$bundle/libexec/zenbu"
    install -m 755 "$release_build_dir/default/bin/zenbu_headless.exe" \
      "$bundle/libexec/zenbu-headless"
    eval "$("$script_dir/zenbu-env.sh")"
    test -n "${ZENBU_LUA_LIBRARY:-}" && test -f "$ZENBU_LUA_LIBRARY" || {
      echo "macOS release packaging requires Homebrew Lua 5.4; run: brew install lua@5.4" >&2
      exit 2
    }
    lua_library="$bundle/lib/zenbu/liblua.5.4.dylib"
    cp -L "$ZENBU_LUA_LIBRARY" "$lua_library"

    for executable in "$bundle/libexec/zenbu" "$bundle/libexec/zenbu-headless"; do
      ffi_library=$(otool -L "$executable" | awk '/\/libffi\.[0-9][0-9.]*\.dylib/{print $1; exit}')
      test -n "$ffi_library" && test -f "$ffi_library" || {
        echo "could not resolve the Homebrew libffi dependency for $executable" >&2
        exit 2
      }
      ffi_name=$(basename "$ffi_library")
      if test ! -f "$bundle/lib/zenbu/$ffi_name"; then
        cp -L "$ffi_library" "$bundle/lib/zenbu/$ffi_name"
      fi
      install_name_tool -change "$ffi_library" "@rpath/$ffi_name" "$executable"
    done

    for launcher in zenbu zenbu-headless; do
      cat > "$bundle/bin/$launcher" <<'EOF'
#!/bin/sh
set -eu
bundle_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export ZENBU_LUA_LIBRARY="$bundle_dir/lib/zenbu/liblua.5.4.dylib"
exec "$bundle_dir/libexec/LAUNCH_TARGET" "$@"
EOF
      sed -i '' "s/LAUNCH_TARGET/$launcher/" "$bundle/bin/$launcher"
      chmod 755 "$bundle/bin/$launcher"
    done
    ;;
esac

tar -C "$temporary" -czf "$archive_path" "$bundle_name"
echo "packaged $archive_path"
