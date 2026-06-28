#!/usr/bin/env sh
set -eu

PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/host}"
OUT="${KELP_PI_PACKAGE_DIR:-.kelp-pi/dist/kelp-pi}"
BIN="${KELP_PI_BINARY:-zig-out/bin/kelp-pi}"
TARGET="${KELP_PI_PACKAGE_TARGET:-host}"
REQUIRE_LLAMA_LIBS="${KELP_PI_REQUIRE_LLAMA_LIBS:-0}"

if [ ! -f "$BIN" ]; then
  echo "missing binary $BIN" >&2
  exit 66
fi

sha256_file() {
  shasum -a 256 "$1" | awk '{ print $1 }'
}

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

rm -rf "$OUT"
mkdir -p "$OUT/bin"
cp "$BIN" "$OUT/bin/kelp-pi"

if [ -d "$PREFIX/lib" ]; then
  mkdir -p "$OUT/lib"
  cp -R "$PREFIX/lib/." "$OUT/lib/"
elif [ "$REQUIRE_LLAMA_LIBS" = "1" ]; then
  echo "missing llama lib dir $PREFIX/lib" >&2
  exit 66
fi

cat > "$OUT/run-kelp-pi.sh" <<'EOF'
#!/usr/bin/env sh
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export LD_LIBRARY_PATH="$ROOT/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export DYLD_LIBRARY_PATH="$ROOT/lib${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
exec "$ROOT/bin/kelp-pi" "$@"
EOF
chmod 0755 "$OUT/run-kelp-pi.sh"

binary_file="$(file "$OUT/bin/kelp-pi" 2>/dev/null || true)"
binary_sha="$(sha256_file "$OUT/bin/kelp-pi")"
llama_commit="$(git -C vendor/llama.cpp rev-parse HEAD 2>/dev/null || printf 'unknown')"
manifest="$OUT/package-manifest.json"
{
  printf '{\n'
  printf '  "schemaVersion": "kelp.pi.package.v1",\n'
  printf '  "target": "%s",\n' "$(json_escape "$TARGET")"
  printf '  "binary": {"path": "bin/kelp-pi", "sha256": "%s", "file": "%s"},\n' "$binary_sha" "$(json_escape "$binary_file")"
  printf '  "llamaCommit": "%s",\n' "$(json_escape "$llama_commit")"
  printf '  "libraries": [\n'
  first=1
  if [ -d "$OUT/lib" ]; then
    find "$OUT/lib" -type f \( -name '*.so' -o -name '*.so.*' -o -name '*.dylib' \) | sort | while IFS= read -r lib; do
      rel="${lib#$OUT/}"
      sha="$(sha256_file "$lib")"
      file_out="$(file "$lib" 2>/dev/null || true)"
      if [ "$first" -eq 0 ]; then
        printf ',\n'
      fi
      first=0
      printf '    {"path": "%s", "sha256": "%s", "file": "%s"}' "$(json_escape "$rel")" "$sha" "$(json_escape "$file_out")"
    done
  fi
  printf '\n  ]\n'
  printf '}\n'
} > "$manifest"

printf '%s\n' "$OUT"
