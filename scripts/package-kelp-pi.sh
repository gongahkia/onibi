#!/usr/bin/env sh
set -eu

PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/host}"
OUT="${KELP_PI_PACKAGE_DIR:-.kelp-pi/dist/kelp-pi}"
BIN="${KELP_PI_BINARY:-zig-out/bin/kelp-pi}"

if [ ! -f "$BIN" ]; then
  echo "missing binary $BIN" >&2
  exit 66
fi

rm -rf "$OUT"
mkdir -p "$OUT/bin"
cp "$BIN" "$OUT/bin/kelp-pi"

if [ -d "$PREFIX/lib" ]; then
  mkdir -p "$OUT/lib"
  cp -R "$PREFIX/lib/." "$OUT/lib/"
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
printf '%s\n' "$OUT"
