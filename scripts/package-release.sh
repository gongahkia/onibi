#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${1:-$REPO_ROOT/dist}"
FORMULA_DIR="$REPO_ROOT/Formula"
FORMULA_PATH="$FORMULA_DIR/kelp.rb"

VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$REPO_ROOT/Cargo.toml" | head -n 1)"
REPOSITORY_URL="$(sed -n 's/^repository = "\(.*\)"/\1/p' "$REPO_ROOT/Cargo.toml" | head -n 1)"
TARGET_TRIPLE="$(rustc -vV | sed -n 's/^host: //p')"
RELEASE_TAG="v$VERSION"

BINARY_PACKAGE_ROOT="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE"
BINARY_ARCHIVE_PATH="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE.tar.gz"
BINARY_CHECKSUM_PATH="$BINARY_ARCHIVE_PATH.sha256"

SOURCE_PACKAGE_ROOT="$OUTPUT_DIR/kelp-v$VERSION-source"
SOURCE_ARCHIVE_PATH="$OUTPUT_DIR/kelp-v$VERSION-source.tar.gz"
SOURCE_CHECKSUM_PATH="$SOURCE_ARCHIVE_PATH.sha256"

BINARY_PATH="$REPO_ROOT/target/release/kelp"

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

write_checksum_file() {
    local file="$1"
    local checksum_path="$2"
    printf '%s  %s\n' "$(sha256_file "$file")" "$(basename "$file")" > "$checksum_path"
}

copy_tree() {
    local source="$1"
    local target="$2"
    rm -rf "$target"
    mkdir -p "$(dirname "$target")"
    cp -R "$source" "$target"
}

write_formula() {
    local source_sha="$1"
    local source_asset

    source_asset="$(basename "$SOURCE_ARCHIVE_PATH")"
    mkdir -p "$FORMULA_DIR"
    cat > "$FORMULA_PATH" <<EOF
class Kelp < Formula
  desc "Strict, local-first planner CLI for tasks, projects, and reviews"
  homepage "$REPOSITORY_URL"
  url "$REPOSITORY_URL/releases/download/$RELEASE_TAG/$source_asset"
  sha256 "$source_sha"
  license "MIT"

  depends_on "rust" => :build

  def install
    system "cargo", "install", *std_cargo_args(path: ".")
    generate_completions_from_executable(bin/"kelp", "completions")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/kelp --version")
  end
end
EOF
    cp "$FORMULA_PATH" "$OUTPUT_DIR/kelp.rb"
}

if [[ -z "$VERSION" || -z "$REPOSITORY_URL" ]]; then
    printf 'Failed to determine package metadata from Cargo.toml.\n' >&2
    exit 1
fi

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

printf 'Building kelp %s for %s...\n' "$VERSION" "$TARGET_TRIPLE"
cargo build --manifest-path "$REPO_ROOT/Cargo.toml" --release --locked

printf 'Creating binary archive...\n'
rm -rf "$BINARY_PACKAGE_ROOT"
mkdir -p "$BINARY_PACKAGE_ROOT/completions"
"$BINARY_PATH" completions bash > "$BINARY_PACKAGE_ROOT/completions/kelp.bash"
"$BINARY_PATH" completions zsh > "$BINARY_PACKAGE_ROOT/completions/_kelp"
"$BINARY_PATH" completions fish > "$BINARY_PACKAGE_ROOT/completions/kelp.fish"
cp "$BINARY_PATH" "$BINARY_PACKAGE_ROOT/kelp"
cp "$REPO_ROOT/README2.md" "$BINARY_PACKAGE_ROOT/README2.md"
cp "$REPO_ROOT/LICENSE" "$BINARY_PACKAGE_ROOT/LICENSE"
cp "$REPO_ROOT/installer.sh" "$BINARY_PACKAGE_ROOT/installer.sh"
tar -C "$OUTPUT_DIR" -czf "$BINARY_ARCHIVE_PATH" "$(basename "$BINARY_PACKAGE_ROOT")"
write_checksum_file "$BINARY_ARCHIVE_PATH" "$BINARY_CHECKSUM_PATH"

printf 'Creating source archive...\n'
rm -rf "$SOURCE_PACKAGE_ROOT"
mkdir -p "$SOURCE_PACKAGE_ROOT"
cp "$REPO_ROOT/Cargo.toml" "$SOURCE_PACKAGE_ROOT/Cargo.toml"
cp "$REPO_ROOT/Cargo.lock" "$SOURCE_PACKAGE_ROOT/Cargo.lock"
cp "$REPO_ROOT/README2.md" "$SOURCE_PACKAGE_ROOT/README2.md"
cp "$REPO_ROOT/LICENSE" "$SOURCE_PACKAGE_ROOT/LICENSE"
cp "$REPO_ROOT/installer.sh" "$SOURCE_PACKAGE_ROOT/installer.sh"
copy_tree "$REPO_ROOT/src" "$SOURCE_PACKAGE_ROOT/src"
copy_tree "$REPO_ROOT/tests" "$SOURCE_PACKAGE_ROOT/tests"
copy_tree "$REPO_ROOT/scripts" "$SOURCE_PACKAGE_ROOT/scripts"
tar -C "$OUTPUT_DIR" -czf "$SOURCE_ARCHIVE_PATH" "$(basename "$SOURCE_PACKAGE_ROOT")"
write_checksum_file "$SOURCE_ARCHIVE_PATH" "$SOURCE_CHECKSUM_PATH"

printf 'Updating Homebrew formula...\n'
write_formula "$(sha256_file "$SOURCE_ARCHIVE_PATH")"

printf 'Release artifacts created:\n'
printf '  %s\n' "$BINARY_ARCHIVE_PATH"
printf '  %s\n' "$BINARY_CHECKSUM_PATH"
printf '  %s\n' "$SOURCE_ARCHIVE_PATH"
printf '  %s\n' "$SOURCE_CHECKSUM_PATH"
printf '  %s\n' "$FORMULA_PATH"
