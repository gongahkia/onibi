#!/usr/bin/env sh
set -eu

PACKAGE_DIR="${KELP_PI_PACKAGE_DIR:-.kelp-pi/dist/kelp-pi-linux-aarch64}"
MANIFEST="$PACKAGE_DIR/package-manifest.json"
BIN="$PACKAGE_DIR/bin/kelp-pi"
LIB_DIR="$PACKAGE_DIR/lib"
PACKAGE_POLICY="$PACKAGE_DIR/policies/appsec-agent-baseline.toml"
PACKAGE_MODELS="$PACKAGE_DIR/models/manifest.toml"
MODEL_MANIFEST="${KELP_PI_MODEL_MANIFEST:-models/manifest.toml}"

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{ print $1 }'
}

need awk
need file
need grep
need node

node scripts/verify-model-manifest.mjs "$MODEL_MANIFEST" >/dev/null

[ -s "$MANIFEST" ] || fail "missing package manifest: $MANIFEST"
[ -x "$BIN" ] || fail "missing packaged binary: $BIN"
[ -d "$LIB_DIR" ] || fail "missing packaged lib dir: $LIB_DIR"
[ -s "$PACKAGE_POLICY" ] || fail "missing packaged policy: $PACKAGE_POLICY"
[ -s "$PACKAGE_MODELS" ] || fail "missing packaged model manifest: $PACKAGE_MODELS"

file "$BIN" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || fail "binary is not ELF aarch64"
find "$LIB_DIR" -type f \( -name 'libllama*.so*' -o -name 'libggml*.so*' \) | grep -q . || fail "missing llama/ggml shared libs"
find "$LIB_DIR" -type f \( -name 'libllama*.dylib' -o -name 'libggml*.dylib' \) | grep -q . && fail "macOS dylibs found in aarch64 package"

node - "$MANIFEST" "$PACKAGE_DIR" <<'NODE'
const { readFileSync } = require("node:fs");
const { createHash } = require("node:crypto");
const { join } = require("node:path");
const manifest = JSON.parse(readFileSync(process.argv[2], "utf8"));
const root = process.argv[3];
function sha(path) {
  return createHash("sha256").update(readFileSync(join(root, path))).digest("hex");
}
if (manifest.schemaVersion !== "kelp.pi.package.v1") throw new Error("bad manifest schema");
if (manifest.target !== "aarch64-linux-gnu.2.36") throw new Error("bad target");
if (sha(manifest.binary.path) !== manifest.binary.sha256) throw new Error("binary sha mismatch");
if (sha(manifest.modelManifest.path) !== manifest.modelManifest.sha256) throw new Error("model manifest sha mismatch");
if (sha(manifest.policy.path) !== manifest.policy.sha256) throw new Error("policy sha mismatch");
for (const lib of manifest.libraries) {
  if (sha(lib.path) !== lib.sha256) throw new Error(`library sha mismatch: ${lib.path}`);
}
NODE

printf 'OK package preflight %s\n' "$PACKAGE_DIR"
