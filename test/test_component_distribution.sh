#!/usr/bin/env bash
# End-to-end adversarial checks for the Linux Component distribution format.
set -euo pipefail

repo_dir=${1:?usage: test_component_distribution.sh REPOSITORY-ROOT}
tool="$repo_dir/scripts/zenbu-component-distribution.sh"
fixture="$repo_dir/test/fixtures/m9_conformance_component.wasm.b64"
invalid_fixture="$repo_dir/test/fixtures/m9_wasi_component.wasm.b64"
temporary=$(mktemp -d "${TMPDIR:-/tmp}/zenbu-component-distribution-test.XXXXXX")

cleanup() {
  rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'test_component_distribution: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local expected=$1
  shift
  local output="$temporary/failure.log"
  if "$@" >"$output" 2>&1; then
    cat "$output" >&2
    fail "command unexpectedly succeeded: $*"
  fi
  grep -Fq "$expected" "$output" || {
    cat "$output" >&2
    fail "failure did not include $expected: $*"
  }
}

run() {
  "$@" >/dev/null
}

make_package() {
  local destination=$1
  local version=$2
  local binary=$3
  mkdir "$destination"
  cp "$repo_dir/examples/wasm-component-conformance/zenbu-plugin.toml" \
    "$destination/zenbu-plugin.toml"
  sed -i "s/version = \"1.0.0\"/version = \"$version\"/" \
    "$destination/zenbu-plugin.toml"
  base64 -d "$binary" > "$destination/plugin.wasm"
}

# The packager correctly refuses an invalid Component. This helper creates a
# syntactically valid, independently signed archive to exercise the later host
# staging boundary during install.
make_raw_signed_bundle() {
  local package=$1
  local private=$2
  local public=$3
  local output=$4
  local not_before=$5
  local expires=$6
  local work="$temporary/raw-bundle"
  local signer manifest_hash component_hash
  mkdir "$work" "$work/payload"
  cp "$package/zenbu-plugin.toml" "$work/payload/zenbu-plugin.toml"
  cp "$package/plugin.wasm" "$work/payload/plugin.wasm"
  signer=$("$tool" key-id "$public")
  manifest_hash="sha256:$(sha256sum "$work/payload/zenbu-plugin.toml" | awk '{ print $1 }')"
  component_hash="sha256:$(sha256sum "$work/payload/plugin.wasm" | awk '{ print $1 }')"
  cat > "$work/bundle.toml" <<EOF
format = 1
plugin_id = "com.example.conformance"
plugin_version = "1.2.0"
runtime = "wasm-component"
api = 1
signer = "$signer"
not_before = "$not_before"
expires = "$expires"
manifest_sha256 = "$manifest_hash"
component_sha256 = "$component_hash"
EOF
  openssl pkeyutl -sign -rawin -inkey "$private" -in "$work/bundle.toml" \
    -out "$work/bundle.sig"
  tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
    -C "$work" -czf "$output" bundle.toml bundle.sig \
    payload/zenbu-plugin.toml payload/plugin.wasm
}

make_package "$temporary/v1" 1.0.0 "$fixture"
make_package "$temporary/v2" 1.1.0 "$fixture"
make_package "$temporary/invalid-v3" 1.2.0 "$invalid_fixture"
mkdir "$temporary/keys" "$temporary/store"
not_before=$(date -u +%F)
expires=$(date -u -d '+365 days' +%F)
expired_not_before=$(date -u -d '-2 days' +%F)
expired_expires=$(date -u -d '-1 day' +%F)

run "$tool" keygen "$temporary/keys/signing.pem" "$temporary/keys/signing.pub"
run "$tool" keygen "$temporary/keys/unknown.pem" "$temporary/keys/unknown.pub"
run "$tool" keygen "$temporary/keys/expired.pem" "$temporary/keys/expired.pub"
run "$tool" trust-add "$temporary/store" "$temporary/keys/signing.pub" "$not_before" "$expires"
run "$tool" trust-add "$temporary/store" "$temporary/keys/expired.pub" "$expired_not_before" "$expired_expires"
run "$tool" bundle "$temporary/v1" "$temporary/keys/signing.pem" "$temporary/v1.zcp" "$not_before" "$expires"
run "$tool" bundle "$temporary/v1" "$temporary/keys/signing.pem" "$temporary/v1-repeat.zcp" "$not_before" "$expires"
cmp -s "$temporary/v1.zcp" "$temporary/v1-repeat.zcp" \
  || fail 'fixed inputs did not produce a deterministic bundle'
run "$tool" verify "$temporary/v1.zcp" "$temporary/store"

mkdir "$temporary/tampered"
tar -xzf "$temporary/v1.zcp" -C "$temporary/tampered"
printf 'tampered' >> "$temporary/tampered/payload/plugin.wasm"
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
  -C "$temporary/tampered" -czf "$temporary/tampered.zcp" bundle.toml bundle.sig \
  payload/zenbu-plugin.toml payload/plugin.wasm
expect_failure integrity-mismatch "$tool" verify "$temporary/tampered.zcp" "$temporary/store"

mkdir "$temporary/signature-tampered"
tar -xzf "$temporary/v1.zcp" -C "$temporary/signature-tampered"
dd if=/dev/zero of="$temporary/signature-tampered/bundle.sig" bs=64 count=1 status=none
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
  -C "$temporary/signature-tampered" -czf "$temporary/signature-tampered.zcp" bundle.toml bundle.sig \
  payload/zenbu-plugin.toml payload/plugin.wasm
expect_failure signature-invalid "$tool" verify "$temporary/signature-tampered.zcp" "$temporary/store"

run "$tool" bundle "$temporary/v1" "$temporary/keys/signing.pem" "$temporary/expired.zcp" "$expired_not_before" "$expired_expires"
expect_failure bundle-expired "$tool" verify "$temporary/expired.zcp" "$temporary/store"
run "$tool" bundle "$temporary/v1" "$temporary/keys/unknown.pem" "$temporary/unknown.zcp" "$not_before" "$expires"
expect_failure signer-unknown "$tool" verify "$temporary/unknown.zcp" "$temporary/store"
run "$tool" bundle "$temporary/v1" "$temporary/keys/expired.pem" "$temporary/expired-key.zcp" "$not_before" "$expires"
expect_failure signer-expired "$tool" verify "$temporary/expired-key.zcp" "$temporary/store"

run "$tool" install "$temporary/v1.zcp" "$temporary/store"
grep -Fq 'version = "1.0.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'first install did not publish v1 as active'

# A second active directory with the same manifest ID represents an invalid
# store. Candidate staging must reject it and leave the healthy v1 link alone.
ln -s "$temporary/store/packages/com.example.conformance/1.0.0" \
  "$temporary/store/active/duplicate-id"
run "$tool" bundle "$temporary/v2" "$temporary/keys/signing.pem" "$temporary/v2.zcp" "$not_before" "$expires"
expect_failure 'multiple discovered packages declare the same plugin ID' \
  "$tool" install "$temporary/v2.zcp" "$temporary/store"
grep -Fq 'version = "1.0.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'duplicate-ID rejection replaced the healthy active generation'
rm "$temporary/store/active/duplicate-id"

make_raw_signed_bundle "$temporary/invalid-v3" "$temporary/keys/signing.pem" \
  "$temporary/keys/signing.pub" "$temporary/invalid.zcp" "$not_before" "$expires"
expect_failure extension-abi-mismatch "$tool" install "$temporary/invalid.zcp" "$temporary/store"
grep -Fq 'version = "1.0.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'failed upgrade replaced the healthy active generation'

run "$tool" install "$temporary/v2.zcp" "$temporary/store"
grep -Fq 'version = "1.1.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'valid upgrade did not publish v2'
run "$tool" rollback "$temporary/store" com.example.conformance 1.0.0
grep -Fq 'version = "1.0.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'rollback did not repoint the active generation to v1'
archive_hash=$(sha256sum "$temporary/v2.zcp" | awk '{ print $1 }')
run "$tool" install-cached "$archive_hash" "$temporary/store"
grep -Fq 'version = "1.1.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'offline cache install did not republish v2'

signer=$("$tool" key-id "$temporary/keys/signing.pub")
run "$tool" revoke "$temporary/store" "$signer" 'integration test revocation'
expect_failure signer-revoked "$tool" verify "$temporary/v1.zcp" "$temporary/store"
grep -Fq 'version = "1.1.0"' "$temporary/store/active/com.example.conformance/zenbu-plugin.toml" \
  || fail 'revocation changed an already-active local generation'

printf 'component distribution integration test: passed\n'
