#!/bin/sh
# Build and validate a Zenbu Wasm Component source package against SDK 1.0.0.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
sdk_dir="$repo_dir/sdk/wasm-component"
sdk_version=$(tr -d '\n' < "$sdk_dir/VERSION")
wit_source="$repo_dir/docs/wit/zenbu-plugin.wit"
sdk_wit="$sdk_dir/wit/zenbu-plugin.wit"
sdk_helper="$sdk_dir/rust/zenbu_sdk.rs"
template_dir="$sdk_dir/template"
expected_wit_bindgen='=0.41.0'
expected_cargo_component='0.21.1'

fail() {
  echo "zenbu-component-package: $*" >&2
  exit 2
}

usage() {
  cat >&2 <<EOF
usage:
  $0 new DESTINATION
  $0 build PACKAGE-DIRECTORY
  $0 check PACKAGE-DIRECTORY
  $0 verify-sdk
  $0 sync-contract
EOF
  exit 2
}

copy_if_changed() {
  source=$1
  destination=$2
  destination_dir=$(dirname -- "$destination")
  mkdir -p "$destination_dir"
  if test ! -f "$destination" || ! cmp -s "$source" "$destination"; then
    temporary=$(mktemp "$destination_dir/.zenbu-component.XXXXXX")
    cp "$source" "$temporary"
    mv "$temporary" "$destination"
  fi
}

sync_contract() {
  test -f "$wit_source" || fail "missing generated WIT contract: $wit_source"
  test -f "$sdk_helper" || fail "missing SDK helper: $sdk_helper"
  hello_lock="$repo_dir/examples/wasm-component-hello/Cargo.lock"
  test -f "$hello_lock" || fail "missing reference SDK lockfile: $hello_lock"
  for destination in \
    "$sdk_wit" \
    "$template_dir/wit/zenbu-plugin.wit" \
    "$template_dir/src/zenbu_sdk.rs" \
    "$repo_dir/examples/wasm-component-hello/wit/zenbu-plugin.wit" \
    "$repo_dir/examples/wasm-component-hello/src/zenbu_sdk.rs" \
    "$repo_dir/examples/wasm-component-conformance/wit/zenbu-plugin.wit" \
    "$repo_dir/examples/wasm-component-conformance/src/zenbu_sdk.rs"; do
    case "$destination" in
      */zenbu-plugin.wit) copy_if_changed "$wit_source" "$destination" ;;
      */zenbu_sdk.rs) copy_if_changed "$sdk_helper" "$destination" ;;
    *) fail "internal unsupported SDK destination: $destination" ;;
    esac
  done
  template_lock="$template_dir/Cargo.lock"
  temporary=$(mktemp "$template_dir/.zenbu-component-lock.XXXXXX")
  sed 's/name = "zenbu_component_hello"/name = "zenbu_component_guest"/' \
    "$hello_lock" > "$temporary"
  if test ! -f "$template_lock" || ! cmp -s "$temporary" "$template_lock"; then
    mv "$temporary" "$template_lock"
  else
    rm "$temporary"
  fi
}

verify_sdk() {
  test "$sdk_version" = '1.0.0' || fail "unsupported SDK version: $sdk_version"
  test -f "$wit_source" || fail "missing generated WIT contract: $wit_source"
  test -f "$sdk_helper" || fail "missing SDK helper: $sdk_helper"
  for candidate in \
    "$sdk_wit" \
    "$template_dir/wit/zenbu-plugin.wit" \
    "$repo_dir/examples/wasm-component-hello/wit/zenbu-plugin.wit" \
    "$repo_dir/examples/wasm-component-conformance/wit/zenbu-plugin.wit"; do
    cmp -s "$wit_source" "$candidate" || fail "stale WIT snapshot: $candidate; run make extension-docs"
  done
  for candidate in \
    "$template_dir/src/zenbu_sdk.rs" \
    "$repo_dir/examples/wasm-component-hello/src/zenbu_sdk.rs" \
    "$repo_dir/examples/wasm-component-conformance/src/zenbu_sdk.rs"; do
    cmp -s "$sdk_helper" "$candidate" || fail "stale SDK helper: $candidate; run make extension-docs"
  done
  test -f "$template_dir/Cargo.lock" || fail "missing SDK template lockfile"
  grep -Fq 'package zenbu:plugin@1.0.0;' "$wit_source" || fail "unexpected WIT package version"
  grep -Fq 'pub const SDK_VERSION: &str = "1.0.0";' "$sdk_helper" || fail "SDK helper version does not match VERSION"
  grep -Fq 'pub const WIT_BINDGEN_VERSION: &str = "0.41.0";' "$sdk_helper" || fail "SDK helper does not pin wit-bindgen"
  grep -Fq 'pub const CARGO_COMPONENT_VERSION: &str = "0.21.1";' "$sdk_helper" || fail "SDK helper does not pin cargo-component"
}

metadata_value() {
  manifest=$1
  key=$2
  awk -F '=' -v key="$key" '
    /^\[package\.metadata\.zenbu\][[:space:]]*$/ { active = 1; next }
    /^\[/ { active = 0 }
    active && $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
      value = $2
      sub(/^[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      sub(/^[\047\042]/, "", value)
      sub(/[\047\042][[:space:]]*$/, "", value)
      print value
      exit
    }
  ' "$manifest"
}

component_target_value() {
  manifest=$1
  key=$2
  awk -F '=' -v key="$key" '
    /^\[package\.metadata\.component\.target\][[:space:]]*$/ { active = 1; next }
    /^\[/ { active = 0 }
    active && $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
      value = $2
      sub(/^[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      sub(/^[\047\042]/, "", value)
      sub(/[\047\042][[:space:]]*$/, "", value)
      print value
      exit
    }
  ' "$manifest"
}

validate_source_package() {
  package_dir=$1
  test -d "$package_dir" || fail "package directory does not exist: $package_dir"
  manifest="$package_dir/zenbu-plugin.toml"
  cargo_manifest="$package_dir/Cargo.toml"
  test -f "$manifest" || fail "missing Zenbu manifest: $manifest"
  test -f "$cargo_manifest" || fail "missing Cargo manifest: $cargo_manifest"
  test -f "$package_dir/Cargo.lock" || fail "missing Cargo.lock; reproducible builds require it"
  test -f "$package_dir/wit/zenbu-plugin.wit" || fail "missing pinned WIT snapshot"
  test -f "$package_dir/src/zenbu_sdk.rs" || fail "missing SDK helper source"
  cmp -s "$wit_source" "$package_dir/wit/zenbu-plugin.wit" || fail "package WIT differs from SDK $sdk_version"
  cmp -s "$sdk_helper" "$package_dir/src/zenbu_sdk.rs" || fail "package SDK helper differs from SDK $sdk_version"
  grep -Eq '^[[:space:]]*wit-bindgen[[:space:]]*=[[:space:]]*"=0\.41\.0"[[:space:]]*$' "$cargo_manifest" \
    || fail "Cargo.toml must pin wit-bindgen $expected_wit_bindgen"
  test "$(component_target_value "$cargo_manifest" path)" = 'wit' \
    || fail 'Cargo Component target path must be the local pinned wit directory'
  test "$(component_target_value "$cargo_manifest" world)" = 'extension' \
    || fail 'Cargo Component world must be extension'
  test "$(metadata_value "$cargo_manifest" sdk)" = "$sdk_version" \
    || fail "package must declare SDK version $sdk_version"
  artifact=$(metadata_value "$cargo_manifest" artifact)
  case "$artifact" in
    '' | *[!A-Za-z0-9_-]*) fail 'package metadata artifact must contain only letters, digits, hyphens, or underscores' ;;
  esac
}

check_cargo_component() {
  cargo_home=${CARGO_HOME:-"$HOME/.cargo"}
  if test -x "$cargo_home/bin/cargo-component"; then
    PATH="$cargo_home/bin:$PATH"
    export PATH
  fi
  command -v cargo >/dev/null 2>&1 || fail 'cargo is required'
  version=$(cargo component --version 2>/dev/null | awk 'NR == 1 { print $NF }') || fail "cargo-component $expected_cargo_component is required; install it with: cargo install cargo-component --version $expected_cargo_component --locked"
  test "$version" = "$expected_cargo_component" \
    || fail "cargo-component $expected_cargo_component is required; found $version"
}

host_check() {
  package_dir=$1
  if test -d "$repo_dir/_opam"; then
    command -v opam >/dev/null 2>&1 || fail 'opam is required to use the local Zenbu switch'
    eval "$(opam env --switch="$repo_dir" --set-switch)"
  fi
  command -v dune >/dev/null 2>&1 || fail 'dune is required for host validation'
  eval "$("$script_dir/zenbu-env.sh")"
  dune exec --root "$repo_dir" bin/zenbu_headless.exe -- plugin-check "$package_dir"
}

new_package() {
  destination=$1
  test ! -e "$destination" || fail "refusing to overwrite existing destination: $destination"
  verify_sdk
  parent=$(dirname -- "$destination")
  test -d "$parent" || fail "destination parent does not exist: $parent"
  cp -R "$template_dir" "$destination"
  echo "created $destination with Zenbu Component SDK $sdk_version"
}

build_package() {
  package_dir=$1
  validate_source_package "$package_dir"
  check_cargo_component
  temporary=$(mktemp -d "${TMPDIR:-/tmp}/zenbu-component-build.XXXXXX")
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  artifact=$(metadata_value "$package_dir/Cargo.toml" artifact)
  (
    cd "$package_dir"
    CARGO_TARGET_DIR="$temporary/target" cargo component build --locked --release \
      --target wasm32-unknown-unknown --manifest-path Cargo.toml
  )
  built="$temporary/target/wasm32-unknown-unknown/release/$artifact.wasm"
  test -f "$built" || fail "Cargo Component did not produce expected artifact: $built"
  output=$(mktemp "$package_dir/.plugin.wasm.XXXXXX")
  cp "$built" "$output"
  mv "$output" "$package_dir/plugin.wasm"
  host_check "$package_dir"
  trap - EXIT HUP INT TERM
  rm -rf "$temporary"
}

check_package() {
  package_dir=$1
  validate_source_package "$package_dir"
  test -f "$package_dir/plugin.wasm" || fail 'missing plugin.wasm; run build first'
  host_check "$package_dir"
}

test "$#" -ge 1 || usage
case "$1" in
  new) test "$#" = 2 || usage; new_package "$2" ;;
  build) test "$#" = 2 || usage; build_package "$2" ;;
  check) test "$#" = 2 || usage; check_package "$2" ;;
  verify-sdk) test "$#" = 1 || usage; verify_sdk ;;
  sync-contract) test "$#" = 1 || usage; sync_contract ;;
  *) usage ;;
esac
