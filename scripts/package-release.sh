#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${1:-$REPO_ROOT/dist}"
VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$REPO_ROOT/Cargo.toml" | head -n 1)"
TARGET_TRIPLE="$(rustc -vV | sed -n 's/^host: //p')"
PACKAGE_ROOT="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE"
ARCHIVE_PATH="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE.tar.gz"
CHECKSUM_PATH="$ARCHIVE_PATH.sha256"
BINARY_PATH="$REPO_ROOT/target/release/kelp"

if [[ -z "$VERSION" ]]; then
    printf 'Failed to determine the Kelp version from Cargo.toml.\n' >&2
    exit 1
fi

mkdir -p "$PACKAGE_ROOT/completions"

printf 'Building kelp %s for %s...\n' "$VERSION" "$TARGET_TRIPLE"
cargo build --manifest-path "$REPO_ROOT/Cargo.toml" --release --locked

printf 'Generating shell completions...\n'
"$BINARY_PATH" completions bash > "$PACKAGE_ROOT/completions/kelp.bash"
"$BINARY_PATH" completions zsh > "$PACKAGE_ROOT/completions/_kelp"
"$BINARY_PATH" completions fish > "$PACKAGE_ROOT/completions/kelp.fish"

cp "$BINARY_PATH" "$PACKAGE_ROOT/kelp"
cp "$REPO_ROOT/README.md" "$PACKAGE_ROOT/README.md"
cp "$REPO_ROOT/installer.sh" "$PACKAGE_ROOT/installer.sh"

tar -C "$OUTPUT_DIR" -czf "$ARCHIVE_PATH" "$(basename "$PACKAGE_ROOT")"
sha256sum "$ARCHIVE_PATH" > "$CHECKSUM_PATH"

printf 'Release bundle created:\n'
printf '  %s\n' "$ARCHIVE_PATH"
printf '  %s\n' "$CHECKSUM_PATH"
