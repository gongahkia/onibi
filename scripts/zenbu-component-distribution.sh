#!/usr/bin/env bash
# Linux-only signed Zenbu Component package distribution tooling.
#
# This intentionally has no network client, resolver, marketplace, or Lua
# package support. It signs an exact four-member archive and stages a Component
# through the ordinary Zenbu host before changing an active package symlink.
set -euo pipefail
umask 077

program=zenbu-component-distribution
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

max_archive_bytes=67108864
max_metadata_bytes=4096
max_manifest_bytes=65536
max_component_bytes=33554432
expected_members='bundle.toml
bundle.sig
payload/zenbu-plugin.toml
payload/plugin.wasm'

fail() {
  local code=$1
  shift
  printf '%s: %s: %s\n' "$program" "$code" "$*" >&2
  exit 2
}

usage() {
  cat >&2 <<EOF
usage:
  $0 keygen PRIVATE-KEY.pem PUBLIC-KEY.pem
  $0 key-id PUBLIC-KEY.pem
  $0 trust-add STORE PUBLIC-KEY.pem NOT-BEFORE EXPIRES
  $0 revoke STORE KEY-ID REASON
  $0 bundle PACKAGE-DIRECTORY PRIVATE-KEY.pem OUTPUT.zcp NOT-BEFORE EXPIRES
  $0 verify BUNDLE.zcp STORE
  $0 install BUNDLE.zcp STORE
  $0 install-cached ARCHIVE-SHA256 STORE
  $0 rollback STORE PLUGIN-ID VERSION
  $0 list STORE
EOF
  exit 2
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail missing-tool "required command is unavailable: $1"
}

require_linux_tools() {
  for tool in awk cmp cp date dirname find grep ln mkdir mktemp mv openssl readlink sed sha256sum sort stat tar tr; do
    require_tool "$tool"
  done
  tar --version 2>/dev/null | grep -Fq 'GNU tar' \
    || fail unsupported-platform 'GNU tar is required by the Linux distribution format'
  date --version 2>/dev/null | grep -Fq 'GNU coreutils' \
    || fail unsupported-platform 'GNU coreutils date is required by the Linux distribution format'
  mv --version 2>/dev/null | grep -Fq 'GNU coreutils' \
    || fail unsupported-platform 'GNU mv -T is required by the Linux distribution format'
}

absolute_existing_dir() {
  test -d "$1" || fail invalid-path "directory does not exist: $1"
  (CDPATH= cd -- "$1" && pwd -P)
}

sha256_file() {
  sha256sum -- "$1" | awk '{ print $1 }'
}

sha256_tagged_file() {
  printf 'sha256:%s\n' "$(sha256_file "$1")"
}

valid_hash() {
  printf '%s\n' "$1" | grep -Eq '^sha256:[0-9a-f]{64}$'
}

valid_archive_hash() {
  printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$'
}

valid_plugin_id() {
  printf '%s\n' "$1" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'
}

valid_version() {
  printf '%s\n' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

valid_date() {
  printf '%s\n' "$1" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' \
    && normalized=$(date -u -d "$1" +%F 2>/dev/null) \
    && test "$normalized" = "$1"
}

date_epoch() {
  date -u -d "$1" +%s 2>/dev/null || fail invalid-date "invalid ISO date: $1"
}

require_date_range() {
  valid_date "$1" || fail invalid-date "expected YYYY-MM-DD: $1"
  valid_date "$2" || fail invalid-date "expected YYYY-MM-DD: $2"
  test "$(date_epoch "$1")" -le "$(date_epoch "$2")" \
    || fail invalid-date-range "not-before is later than expiry"
}

require_current_date() {
  value=$1
  label=$2
  today=$(date -u +%F)
  test "$(date_epoch "$today")" -ge "$(date_epoch "$value")" \
    || fail "$label-not-yet-valid" "$label is not valid until $value"
}

require_unexpired_date() {
  value=$1
  label=$2
  today=$(date -u +%F)
  test "$(date_epoch "$today")" -le "$(date_epoch "$value")" \
    || fail "$label-expired" "$label expired on $value"
}

file_size() {
  stat -c %s -- "$1" 2>/dev/null || fail invalid-path "cannot inspect file: $1"
}

require_regular_file() {
  test -f "$1" && test ! -L "$1" \
    || fail invalid-path "expected a regular non-symlink file: $1"
}

require_size_at_most() {
  local value
  value=$(file_size "$1")
  test "$value" -le "$2" \
    || fail size-limit "$3 exceeds the $2-byte format limit"
}

host_environment() {
  if test -d "$repo_dir/_opam"; then
    require_tool opam
    eval "$(opam env --switch="$repo_dir" --set-switch)"
  fi
  require_tool dune
  eval "$("$script_dir/zenbu-env.sh")"
}

host_check() {
  host_environment
  dune exec --root "$repo_dir" bin/zenbu_headless.exe -- plugin-check "$1"
}

host_root_check() {
  host_environment
  dune exec --root "$repo_dir" bin/zenbu_headless.exe -- plugin-root-check "$1"
}

plugin_metadata() {
  local package_dir=$1
  local manifest="$package_dir/zenbu-plugin.toml"
  require_regular_file "$manifest"
  host_environment
  dune exec --root "$repo_dir" bin/zenbu_headless.exe -- plugin-metadata "$manifest"
}

metadata_id=''
metadata_version=''
metadata_runtime=''
metadata_api=''
metadata_entrypoint=''

read_plugin_metadata() {
  local metadata_output expected_output
  metadata_output=$(plugin_metadata "$1") || fail invalid-manifest 'Zenbu could not parse package metadata'
  metadata_id=$(printf '%s\n' "$metadata_output" | sed -n '1s/^id=//p')
  metadata_version=$(printf '%s\n' "$metadata_output" | sed -n '2s/^version=//p')
  metadata_runtime=$(printf '%s\n' "$metadata_output" | sed -n '3s/^runtime=//p')
  metadata_api=$(printf '%s\n' "$metadata_output" | sed -n '4s/^api=//p')
  metadata_entrypoint=$(printf '%s\n' "$metadata_output" | sed -n '5s/^entrypoint=//p')
  expected_output=$(printf 'id=%s\nversion=%s\nruntime=%s\napi=%s\nentrypoint=%s' \
    "$metadata_id" "$metadata_version" "$metadata_runtime" "$metadata_api" "$metadata_entrypoint")
  test "$metadata_output" = "$expected_output" \
    || fail invalid-manifest 'plugin metadata output was not canonical'
  valid_plugin_id "$metadata_id" || fail invalid-manifest 'package has an invalid plugin ID'
  valid_version "$metadata_version" || fail invalid-manifest 'package has an unsupported version'
  test "$metadata_runtime" = wasm-component \
    || fail unsupported-runtime 'only wasm-component packages may be distributed'
  test "$metadata_api" = 1 || fail incompatible-api 'package does not target Extension API v1'
  test "$metadata_entrypoint" = plugin.wasm \
    || fail unsupported-entrypoint 'signed Component packages must use plugin.wasm'
}

key_id() {
  local temporary
  require_regular_file "$1"
  temporary=$(mktemp "${TMPDIR:-/tmp}/zenbu-component-public.XXXXXX")
  trap 'rm -f "$temporary"' EXIT HUP INT TERM
  openssl pkey -pubin -in "$1" -pubout -outform DER -out "$temporary" >/dev/null 2>&1 \
    || fail invalid-key "not a readable public key: $1"
  printf 'sha256:%s\n' "$(sha256_file "$temporary")"
  trap - EXIT HUP INT TERM
  rm -f "$temporary"
}

require_ed25519_public_key() {
  require_regular_file "$1"
  openssl pkey -pubin -in "$1" -text -noout 2>/dev/null |
    grep -Fxq 'ED25519 Public-Key:' \
    || fail invalid-key "signed Component distribution requires an Ed25519 public key: $1"
}

public_key_from_private() {
  openssl pkey -in "$1" -pubout -out "$2" >/dev/null 2>&1 \
    || fail invalid-key "not a readable private key: $1"
}

keygen() {
  local private=$1
  local public=$2
  test ! -e "$private" || fail destination-exists "refusing to overwrite private key: $private"
  test ! -e "$public" || fail destination-exists "refusing to overwrite public key: $public"
  test -d "$(dirname -- "$private")" || fail invalid-path "private-key parent does not exist"
  test -d "$(dirname -- "$public")" || fail invalid-path "public-key parent does not exist"
  openssl genpkey -algorithm Ed25519 -out "$private" >/dev/null 2>&1 \
    || fail key-generation 'OpenSSL could not generate an Ed25519 key'
  chmod 600 "$private"
  public_key_from_private "$private" "$public"
  printf 'created Ed25519 key pair; key-id=%s\n' "$(key_id "$public")"
}

prepare_store() {
  local store=$1
  test -n "$store" || fail invalid-store 'store path is empty'
  mkdir -p "$store/trust" "$store/revoked" "$store/cache" "$store/packages" "$store/active" \
    || fail invalid-store "cannot initialize store: $store"
  store=$(absolute_existing_dir "$store")
  case "$store" in
    /) fail invalid-store 'store must not be the filesystem root' ;;
  esac
  printf '%s\n' "$store"
}

write_trust_metadata() (
  destination=$1
  id=$2
  not_before=$3
  expires=$4
  printf 'format = 1\nkey_id = "%s"\nnot_before = "%s"\nexpires = "%s"\n' \
    "$id" "$not_before" "$expires" > "$destination"
)

trust_add() {
  local store public not_before expires id suffix key_destination metadata_destination temporary
  store=$(prepare_store "$1")
  public=$2
  not_before=$3
  expires=$4
  require_date_range "$not_before" "$expires"
  require_ed25519_public_key "$public"
  id=$(key_id "$public")
  suffix=${id#sha256:}
  key_destination="$store/trust/$suffix.pem"
  metadata_destination="$store/trust/$suffix.toml"
  test ! -e "$key_destination" && test ! -e "$metadata_destination" \
    || fail trust-exists "key is already trusted: $id"
  temporary=$(mktemp "$store/trust/.zenbu-key.XXXXXX")
  cp -- "$public" "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$key_destination"
  temporary=$(mktemp "$store/trust/.zenbu-key-meta.XXXXXX")
  write_trust_metadata "$temporary" "$id" "$not_before" "$expires"
  mv "$temporary" "$metadata_destination"
  printf 'trusted %s from %s through %s\n' "$id" "$not_before" "$expires"
}

read_trust_metadata() {
  local metadata_path=$1
  local expected_id=$2
  local canonical
  require_regular_file "$metadata_path"
  trust_id=$(sed -n '2s/^key_id = "\([^"]*\)"$/\1/p' "$metadata_path")
  trust_not_before=$(sed -n '3s/^not_before = "\([^"]*\)"$/\1/p' "$metadata_path")
  trust_expires=$(sed -n '4s/^expires = "\([^"]*\)"$/\1/p' "$metadata_path")
  canonical=$(mktemp "$(dirname -- "$metadata_path")/.zenbu-key-meta-read.XXXXXX") \
    || fail trust-invalid "could not inspect key metadata: $metadata_path"
  write_trust_metadata "$canonical" "$trust_id" "$trust_not_before" "$trust_expires" \
    || { rm -f "$canonical"; fail trust-invalid "could not inspect key metadata: $metadata_path"; }
  if ! cmp -s "$canonical" "$metadata_path"; then
    rm -f "$canonical"
    fail trust-invalid "key metadata is not canonical: $metadata_path"
  fi
  rm -f "$canonical" || fail trust-invalid "could not clean key metadata inspection: $metadata_path"
  test "$trust_id" = "$expected_id" || fail trust-invalid 'trust metadata key ID does not match signer'
  valid_hash "$trust_id" || fail trust-invalid 'trust metadata has an invalid key ID'
  require_date_range "$trust_not_before" "$trust_expires"
}

revoke() {
  local store id reason suffix temporary
  store=$(prepare_store "$1")
  id=$2
  shift 2
  reason=$*
  valid_hash "$id" || fail invalid-key-id 'key ID must use sha256:<64 lowercase hex digits>'
  test -n "$reason" || fail invalid-revocation 'revocation reason is required'
  suffix=${id#sha256:}
  test -f "$store/trust/$suffix.pem" || fail signer-unknown "key is not trusted: $id"
  test ! -e "$store/revoked/$suffix" || fail already-revoked "key is already revoked: $id"
  temporary=$(mktemp "$store/revoked/.zenbu-revocation.XXXXXX")
  printf '%s\n' "$reason" > "$temporary"
  mv "$temporary" "$store/revoked/$suffix"
  printf 'revoked %s: %s\n' "$id" "$reason"
}

write_bundle_metadata() (
  destination=$1
  id=$2
  version=$3
  signer=$4
  not_before=$5
  expires=$6
  manifest_hash=$7
  component_hash=$8
  printf 'format = 1\nplugin_id = "%s"\nplugin_version = "%s"\nruntime = "wasm-component"\napi = 1\nsigner = "%s"\nnot_before = "%s"\nexpires = "%s"\nmanifest_sha256 = "%s"\ncomponent_sha256 = "%s"\n' \
    "$id" "$version" "$signer" "$not_before" "$expires" "$manifest_hash" "$component_hash" > "$destination"
)

bundle_id=''
bundle_version=''
bundle_runtime=''
bundle_api=''
bundle_signer=''
bundle_not_before=''
bundle_expires=''
bundle_manifest_hash=''
bundle_component_hash=''

read_bundle_metadata() {
  local metadata_path=$1
  local canonical
  require_regular_file "$metadata_path"
  require_size_at_most "$metadata_path" "$max_metadata_bytes" 'bundle metadata'
  bundle_id=$(sed -n '2s/^plugin_id = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_version=$(sed -n '3s/^plugin_version = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_runtime=$(sed -n '4s/^runtime = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_api=$(sed -n '5s/^api = \([0-9][0-9]*\)$/\1/p' "$metadata_path")
  bundle_signer=$(sed -n '6s/^signer = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_not_before=$(sed -n '7s/^not_before = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_expires=$(sed -n '8s/^expires = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_manifest_hash=$(sed -n '9s/^manifest_sha256 = "\([^"]*\)"$/\1/p' "$metadata_path")
  bundle_component_hash=$(sed -n '10s/^component_sha256 = "\([^"]*\)"$/\1/p' "$metadata_path")
  valid_plugin_id "$bundle_id" || fail bundle-invalid 'bundle metadata has an invalid plugin ID'
  valid_version "$bundle_version" || fail bundle-invalid 'bundle metadata has an invalid plugin version'
  test "$bundle_runtime" = wasm-component || fail bundle-invalid 'bundle runtime must be wasm-component'
  test "$bundle_api" = 1 || fail bundle-invalid 'bundle API must be 1'
  valid_hash "$bundle_signer" || fail bundle-invalid 'bundle signer is not a SHA-256 key ID'
  valid_hash "$bundle_manifest_hash" || fail bundle-invalid 'bundle manifest digest is invalid'
  valid_hash "$bundle_component_hash" || fail bundle-invalid 'bundle Component digest is invalid'
  require_date_range "$bundle_not_before" "$bundle_expires"
  canonical=$(mktemp "$(dirname -- "$metadata_path")/.zenbu-bundle-meta-read.XXXXXX") \
    || fail bundle-invalid 'could not inspect bundle metadata'
  write_bundle_metadata "$canonical" "$bundle_id" "$bundle_version" "$bundle_signer" \
    "$bundle_not_before" "$bundle_expires" "$bundle_manifest_hash" "$bundle_component_hash" \
    || { rm -f "$canonical"; fail bundle-invalid 'could not inspect bundle metadata'; }
  if ! cmp -s "$canonical" "$metadata_path"; then
    rm -f "$canonical"
    fail bundle-invalid 'bundle metadata is not canonical or contains unknown fields'
  fi
  rm -f "$canonical" || fail bundle-invalid 'could not clean bundle metadata inspection'
}

create_bundle() {
  local package_dir private output not_before expires manifest component temporary public signer output_temporary
  package_dir=$(absolute_existing_dir "$1")
  private=$2
  output=$3
  not_before=$4
  expires=$5
  test ! -e "$output" || fail destination-exists "refusing to overwrite bundle: $output"
  test -d "$(dirname -- "$output")" || fail invalid-path 'bundle output parent does not exist'
  require_date_range "$not_before" "$expires"
  require_regular_file "$private"
  read_plugin_metadata "$package_dir"
  host_check "$package_dir" \
    || fail package-stage-failed 'package does not stage successfully in the Zenbu host'
  manifest="$package_dir/zenbu-plugin.toml"
  component="$package_dir/plugin.wasm"
  require_regular_file "$manifest"
  require_regular_file "$component"
  require_size_at_most "$manifest" "$max_manifest_bytes" 'plugin manifest'
  require_size_at_most "$component" "$max_component_bytes" 'Component binary'
  temporary=$(mktemp -d "$(dirname -- "$output")/.zenbu-component-bundle.XXXXXX")
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  public="$temporary/signer.pem"
  public_key_from_private "$private" "$public"
  require_ed25519_public_key "$public"
  signer=$(key_id "$public")
  mkdir "$temporary/payload"
  cp -- "$manifest" "$temporary/payload/zenbu-plugin.toml"
  cp -- "$component" "$temporary/payload/plugin.wasm"
  write_bundle_metadata "$temporary/bundle.toml" "$metadata_id" "$metadata_version" "$signer" \
    "$not_before" "$expires" "$(sha256_tagged_file "$manifest")" "$(sha256_tagged_file "$component")"
  openssl pkeyutl -sign -rawin -inkey "$private" -in "$temporary/bundle.toml" \
    -out "$temporary/bundle.sig" >/dev/null 2>&1 \
    || fail signing-failed 'OpenSSL could not sign bundle metadata with Ed25519'
  test "$(file_size "$temporary/bundle.sig")" = 64 \
    || fail signing-failed 'Ed25519 signature was not 64 bytes'
  output_temporary=$(mktemp "$(dirname -- "$output")/.zenbu-component-archive.XXXXXX")
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$temporary" -czf "$output_temporary" bundle.toml bundle.sig \
    payload/zenbu-plugin.toml payload/plugin.wasm \
    || fail archive-failed 'GNU tar could not create the Component bundle'
  mv "$output_temporary" "$output"
  trap - EXIT HUP INT TERM
  rm -rf "$temporary"
  printf 'created %s for %s@%s signed by %s\n' "$output" "$metadata_id" "$metadata_version" "$signer"
}

verify_archive_shape() {
  local archive=$1
  local members
  require_regular_file "$archive"
  require_size_at_most "$archive" "$max_archive_bytes" 'bundle archive'
  members=$(tar -tzf "$archive" 2>/dev/null) \
    || fail bundle-invalid 'archive is not a readable gzip tar file'
  test "$members" = "$expected_members" \
    || fail bundle-invalid 'archive members must be exactly bundle.toml, bundle.sig, and two payload files'
}

extract_bundle() {
  local archive=$1
  local destination=$2
  local shape expected_shape
  verify_archive_shape "$archive"
  (
    ulimit -f 131072
    tar -xzf "$archive" --no-same-owner --no-same-permissions -C "$destination" \
      bundle.toml bundle.sig payload/zenbu-plugin.toml payload/plugin.wasm
  ) >/dev/null 2>&1 || fail bundle-invalid 'archive extraction failed or exceeded the local extraction limit'
  shape=$(CDPATH= cd -- "$destination" && find . -mindepth 1 -printf '%y %p\n' | LC_ALL=C sort)
  expected_shape='d ./payload
f ./bundle.sig
f ./bundle.toml
f ./payload/plugin.wasm
f ./payload/zenbu-plugin.toml'
  test "$shape" = "$expected_shape" \
    || fail bundle-invalid 'archive contains a symlink, non-regular member, or unexpected path'
  require_size_at_most "$destination/bundle.toml" "$max_metadata_bytes" 'bundle metadata'
  test "$(file_size "$destination/bundle.sig")" = 64 \
    || fail bundle-invalid 'bundle signature must be exactly 64 bytes'
  require_size_at_most "$destination/payload/zenbu-plugin.toml" "$max_manifest_bytes" 'plugin manifest'
  require_size_at_most "$destination/payload/plugin.wasm" "$max_component_bytes" 'Component binary'
}

verify_bundle_into() {
  local archive=$1
  local store=$2
  local extraction=$3
  local actual_manifest_hash actual_component_hash suffix public trust_metadata derived_id
  extract_bundle "$archive" "$extraction"
  read_bundle_metadata "$extraction/bundle.toml"
  actual_manifest_hash=$(sha256_tagged_file "$extraction/payload/zenbu-plugin.toml")
  actual_component_hash=$(sha256_tagged_file "$extraction/payload/plugin.wasm")
  test "$actual_manifest_hash" = "$bundle_manifest_hash" \
    || fail integrity-mismatch 'payload manifest digest does not match signed metadata'
  test "$actual_component_hash" = "$bundle_component_hash" \
    || fail integrity-mismatch 'payload Component digest does not match signed metadata'
  suffix=${bundle_signer#sha256:}
  public="$store/trust/$suffix.pem"
  trust_metadata="$store/trust/$suffix.toml"
  test -f "$public" && test -f "$trust_metadata" \
    || fail signer-unknown "no trusted public key for $bundle_signer"
  derived_id=$(key_id "$public")
  test "$derived_id" = "$bundle_signer" \
    || fail trust-invalid 'trusted public-key contents do not match the signer ID'
  require_ed25519_public_key "$public"
  read_trust_metadata "$trust_metadata" "$bundle_signer"
  test ! -e "$store/revoked/$suffix" \
    || fail signer-revoked "signer $bundle_signer was revoked: $(tr '\n' ' ' < "$store/revoked/$suffix")"
  require_current_date "$trust_not_before" signer
  require_unexpired_date "$trust_expires" signer
  require_current_date "$bundle_not_before" bundle
  require_unexpired_date "$bundle_expires" bundle
  openssl pkeyutl -verify -rawin -pubin -inkey "$public" \
    -in "$extraction/bundle.toml" -sigfile "$extraction/bundle.sig" >/dev/null 2>&1 \
    || fail signature-invalid 'Ed25519 signature did not verify against the trusted signer'
  read_plugin_metadata "$extraction/payload"
  test "$metadata_id" = "$bundle_id" \
    || fail identity-mismatch 'payload plugin ID differs from signed bundle metadata'
  test "$metadata_version" = "$bundle_version" \
    || fail version-mismatch 'payload plugin version differs from signed bundle metadata'
  test "$metadata_runtime" = "$bundle_runtime" && test "$metadata_api" = "$bundle_api" \
    || fail metadata-mismatch 'payload runtime/API differs from signed bundle metadata'
  test "$metadata_entrypoint" = plugin.wasm \
    || fail metadata-mismatch 'payload entrypoint must be plugin.wasm'
}

verify_bundle() {
  local archive=$1
  local store temporary
  store=$(prepare_store "$2")
  temporary=$(mktemp -d "$store/.zenbu-component-verify.XXXXXX")
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  verify_bundle_into "$archive" "$store" "$temporary"
  printf 'verified %s@%s signer=%s expires=%s\n' "$bundle_id" "$bundle_version" "$bundle_signer" "$bundle_expires"
  trap - EXIT HUP INT TERM
  rm -rf "$temporary"
}

write_install_metadata() (
  destination=$1
  archive_hash=$2
  printf 'format = 1\narchive_sha256 = "%s"\n' "$archive_hash" > "$destination"
)

read_install_metadata() {
  local path=$1
  local temporary
  require_regular_file "$path"
  installed_archive_hash=$(sed -n '2s/^archive_sha256 = "\([^"]*\)"$/\1/p' "$path")
  temporary=$(mktemp "$(dirname -- "$path")/.zenbu-install-meta-read.XXXXXX") \
    || fail store-invalid "could not inspect install metadata: $path"
  write_install_metadata "$temporary" "$installed_archive_hash" \
    || { rm -f "$temporary"; fail store-invalid "could not inspect install metadata: $path"; }
  if ! cmp -s "$temporary" "$path"; then
    rm -f "$temporary"
    fail store-invalid "install metadata is not canonical: $path"
  fi
  rm -f "$temporary" || fail store-invalid "could not clean install metadata inspection: $path"
  valid_archive_hash "$installed_archive_hash" \
    || fail store-invalid "install metadata has an invalid archive SHA-256: $path"
}

active_target() {
  local store=$1
  local id=$2
  local active="$store/active/$id"
  local target
  if test ! -e "$active" && test ! -L "$active"; then
    return 1
  fi
  test -L "$active" || fail store-invalid "active package is not a symlink: $active"
  target=$(readlink -f "$active") || fail store-invalid "active package has a broken symlink: $active"
  case "$target" in
    "$store"/packages/*) ;;
    *) fail store-invalid "active package escapes the managed package directory: $active" ;;
  esac
  test -d "$target" || fail store-invalid "active package target is not a directory: $active"
  printf '%s\n' "$target"
}

version_compare() {
  local left=$1
  local right=$2
  local old_ifs=$IFS
  local left_major left_minor left_patch right_major right_minor right_patch pair left_component right_component first
  IFS=.
  set -- $left
  left_major=$1
  left_minor=$2
  left_patch=$3
  set -- $right
  right_major=$1
  right_minor=$2
  right_patch=$3
  IFS=$old_ifs
  for pair in "$left_major:$right_major" "$left_minor:$right_minor" "$left_patch:$right_patch"; do
    left_component=${pair%%:*}
    right_component=${pair#*:}
    if test "${#left_component}" -lt "${#right_component}"; then
      printf '%s\n' -1
      return
    elif test "${#left_component}" -gt "${#right_component}"; then
      printf '%s\n' 1
      return
    elif test "$left_component" != "$right_component"; then
      first=$(printf '%s\n%s\n' "$left_component" "$right_component" | LC_ALL=C sort | sed -n '1p')
      if test "$first" = "$left_component"; then
        printf '%s\n' -1
      else
        printf '%s\n' 1
      fi
      return
    fi
  done
  printf '%s\n' 0
}

validate_active_root() {
  local store=$1
  if find "$store/active" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
    host_root_check "$store/active" \
      || fail package-stage-failed 'the current active package root does not stage successfully'
  fi
}

candidate_root() {
  local store=$1
  local candidate=$2
  local id=$3
  local root=$4
  local active_id active_path target
  find "$store/active" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort |
    while IFS= read -r active_id; do
      active_path="$store/active/$active_id"
      test -L "$active_path" || fail store-invalid "active root contains a non-symlink: $active_path"
      target=$(active_target "$store" "$active_id") \
        || fail store-invalid "active package cannot be resolved: $active_path"
      if test "$active_id" != "$id"; then
        ln -s "$target" "$root/$active_id" \
          || fail store-invalid "could not link active package into candidate root: $active_path"
      fi
    done || fail store-invalid 'could not construct the candidate package root'
  ln -s "$candidate" "$root/$id" \
    || fail store-invalid "could not link candidate package into candidate root: $id"
}

cache_archive() {
  local archive=$1
  local store=$2
  local archive_hash destination temporary
  archive_hash=$(sha256_file "$archive")
  destination="$store/cache/$archive_hash.zcp"
  if test -e "$destination"; then
    test "$(sha256_file "$destination")" = "$archive_hash" \
      || fail cache-corrupt "cache entry digest differs from its name: $destination"
  else
    temporary=$(mktemp "$store/cache/.zenbu-component-cache.XXXXXX")
    cp -- "$archive" "$temporary" \
      || fail cache-write "could not copy archive into the cache: $archive"
    mv "$temporary" "$destination" \
      || fail cache-write "could not publish archive into the cache: $destination"
  fi
  printf '%s\n' "$archive_hash"
}

install_verified_bundle() {
  local archive=$1
  local store=$2
  local archive_hash=$3
  local temporary=$4
  local candidate="$temporary/payload"
  local destination="$store/packages/$bundle_id/$bundle_version"
  local active="$store/active/$bundle_id"
  local target current_metadata_root current_version comparison candidate_root_dir
  local existing_package stored_manifest_hash stored_component_hash
  existing_package=0
  if test -e "$destination" || test -L "$destination"; then
    test -d "$destination" && test ! -L "$destination" \
      || fail store-invalid "stored package version is not a directory: $bundle_id@$bundle_version"
    read_install_metadata "$destination/.zenbu-component-install"
    test "$installed_archive_hash" = "$archive_hash" \
      || fail version-conflict "stored package version has different signed provenance: $bundle_id@$bundle_version"
    stored_manifest_hash=$(sha256_tagged_file "$destination/zenbu-plugin.toml")
    stored_component_hash=$(sha256_tagged_file "$destination/plugin.wasm")
    test "$stored_manifest_hash" = "$bundle_manifest_hash" \
      && test "$stored_component_hash" = "$bundle_component_hash" \
      || fail store-invalid "stored package payload differs from its signed provenance: $bundle_id@$bundle_version"
    candidate=$destination
    existing_package=1
  fi
  if test -e "$store/active/$bundle_id" || test -L "$store/active/$bundle_id"; then
    target=$(active_target "$store" "$bundle_id") \
      || fail store-invalid "active package cannot be resolved: $bundle_id"
    read_install_metadata "$target/.zenbu-component-install"
    current_metadata_root=$(mktemp -d "$temporary/current-root.XXXXXX") \
      || fail staging-failed 'could not allocate an active-root staging directory'
    candidate_root "$store" "$target" "$bundle_id" "$current_metadata_root" \
      || fail store-invalid 'could not construct the current active package root'
    # This host check confirms every current active package is healthy before
    # an update may replace the target's active link.
    host_root_check "$current_metadata_root" \
      || fail package-stage-failed 'the current active package root does not stage successfully'
    rm -rf "$current_metadata_root"
    current_version=$(plugin_metadata "$target" | sed -n '2s/^version=//p')
    valid_version "$current_version" || fail store-invalid 'active package has an invalid version'
    comparison=$(version_compare "$bundle_version" "$current_version")
    test "$comparison" = 1 \
      || fail rollback-required "automatic install requires a version newer than active $current_version; use rollback for an explicit downgrade"
  fi
  candidate_root_dir="$temporary/candidate-root"
  mkdir "$candidate_root_dir" \
    || fail staging-failed 'could not allocate a candidate-root staging directory'
  candidate_root "$store" "$candidate" "$bundle_id" "$candidate_root_dir" \
    || fail store-invalid 'could not construct the candidate package root'
  host_root_check "$candidate_root_dir" \
    || fail package-stage-failed 'candidate package did not stage successfully with the active package root'
  if test "$existing_package" = 0; then
    write_install_metadata "$candidate/.zenbu-component-install" "$archive_hash" \
      || fail store-write 'could not write package provenance metadata'
    mkdir -p "$store/packages/$bundle_id" \
      || fail store-write "could not create package directory for $bundle_id"
    mv "$candidate" "$destination" \
      || fail store-write "could not publish package version $bundle_id@$bundle_version"
  fi
  ln -s "$destination" "$temporary/active-link" \
    || fail store-write "could not prepare active link for $bundle_id"
  mv -Tf "$temporary/active-link" "$active" \
    || fail active-publish "could not atomically publish active package $bundle_id"
  printf 'installed %s@%s archive=%s\n' "$bundle_id" "$bundle_version" "$archive_hash"
}

with_install_lock() {
  local store=$1
  local lock
  local status=0
  shift
  lock="$store/.install.lock"
  mkdir "$lock" 2>/dev/null || fail install-busy "another install or rollback holds $lock"
  trap 'rmdir "$lock" 2>/dev/null || true' EXIT
  trap 'exit 130' HUP INT TERM
  ( "$@" ) || status=$?
  trap - EXIT HUP INT TERM
  rmdir "$lock" 2>/dev/null || fail install-lock 'could not release the install lock'
  return "$status"
}

install_unlocked() {
  local archive=$1
  local store=$2
  local temporary archive_hash
  temporary=$(mktemp -d "$store/.zenbu-component-install.XXXXXX") \
    || fail staging-failed 'could not allocate an install staging directory'
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  verify_bundle_into "$archive" "$store" "$temporary" || return $?
  archive_hash=$(cache_archive "$archive" "$store") || return $?
  install_verified_bundle "$archive" "$store" "$archive_hash" "$temporary" || return $?
  trap - EXIT HUP INT TERM
  rm -rf "$temporary"
}

install_bundle() {
  local archive=$1
  local store
  store=$(prepare_store "$2")
  with_install_lock "$store" install_unlocked "$archive" "$store"
}

install_cached() {
  local archive_hash=$1
  local store archive
  store=$(prepare_store "$2")
  valid_archive_hash "$archive_hash" || fail invalid-archive-hash 'cache key must be 64 lowercase hexadecimal characters'
  archive="$store/cache/$archive_hash.zcp"
  require_regular_file "$archive"
  test "$(sha256_file "$archive")" = "$archive_hash" \
    || fail cache-corrupt "cache entry digest differs from requested key: $archive_hash"
  with_install_lock "$store" install_unlocked "$archive" "$store"
}

rollback_unlocked() {
  local store=$1
  local id=$2
  local version=$3
  local destination="$store/packages/$id/$version"
  local archive temporary candidate_root_dir
  test -d "$destination" || fail rollback-unavailable "stored package does not exist: $id@$version"
  read_install_metadata "$destination/.zenbu-component-install"
  archive="$store/cache/$installed_archive_hash.zcp"
  require_regular_file "$archive"
  test "$(sha256_file "$archive")" = "$installed_archive_hash" \
    || fail cache-corrupt "stored package cache entry digest differs from provenance: $id@$version"
  temporary=$(mktemp -d "$store/.zenbu-component-rollback.XXXXXX") \
    || fail staging-failed 'could not allocate a rollback staging directory'
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  verify_bundle_into "$archive" "$store" "$temporary" || return $?
  test "$bundle_id" = "$id" && test "$bundle_version" = "$version" \
    || fail rollback-invalid 'stored provenance bundle does not match the requested package version'
  validate_active_root "$store"
  candidate_root_dir="$temporary/candidate-root"
  mkdir "$candidate_root_dir" \
    || fail staging-failed 'could not allocate a rollback candidate-root staging directory'
  candidate_root "$store" "$destination" "$id" "$candidate_root_dir" \
    || fail store-invalid 'could not construct the rollback candidate root'
  host_root_check "$candidate_root_dir" \
    || fail package-stage-failed 'rollback candidate did not stage successfully with the active package root'
  ln -s "$destination" "$temporary/active-link" \
    || fail store-write "could not prepare rollback link for $id"
  mv -Tf "$temporary/active-link" "$store/active/$id" \
    || fail active-publish "could not atomically publish rollback for $id"
  printf 'rolled back %s to %s\n' "$id" "$version"
  trap - EXIT HUP INT TERM
  rm -rf "$temporary"
}

rollback() {
  local store id version
  store=$(prepare_store "$1")
  id=$2
  version=$3
  valid_plugin_id "$id" || fail invalid-plugin-id 'plugin ID is invalid'
  valid_version "$version" || fail invalid-version 'version must use MAJOR.MINOR.PATCH'
  with_install_lock "$store" rollback_unlocked "$store" "$id" "$version"
}

list_store() {
  local store=$1
  local print id target version
  store=$(prepare_store "$store")
  if ! find "$store/active" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
    print='no active signed Component packages'
    printf '%s\n' "$print"
    return
  fi
  find "$store/active" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort |
    while IFS= read -r id; do
      target=$(active_target "$store" "$id")
      read_install_metadata "$target/.zenbu-component-install"
      version=$(plugin_metadata "$target" | sed -n '2s/^version=//p')
      printf '%s %s %s\n' "$id" "$version" "$installed_archive_hash"
    done
}

require_linux_tools
test "$#" -ge 1 || usage
case "$1" in
  keygen) test "$#" = 3 || usage; keygen "$2" "$3" ;;
  key-id) test "$#" = 2 || usage; key_id "$2" ;;
  trust-add) test "$#" = 5 || usage; trust_add "$2" "$3" "$4" "$5" ;;
  revoke) test "$#" = 4 || usage; revoke "$2" "$3" "$4" ;;
  bundle) test "$#" = 6 || usage; create_bundle "$2" "$3" "$4" "$5" "$6" ;;
  verify) test "$#" = 3 || usage; verify_bundle "$2" "$3" ;;
  install) test "$#" = 3 || usage; install_bundle "$2" "$3" ;;
  install-cached) test "$#" = 3 || usage; install_cached "$2" "$3" ;;
  rollback) test "$#" = 4 || usage; rollback "$2" "$3" "$4" ;;
  list) test "$#" = 2 || usage; list_store "$2" ;;
  *) usage ;;
esac
