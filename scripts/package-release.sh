#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${1:-$REPO_ROOT/dist}"
FORMULA_DIR="$REPO_ROOT/Formula"
FORMULA_PATH="$FORMULA_DIR/kelp.rb"

VERSION="1.0.0"
REPOSITORY_URL="https://github.com/gongahkia/kelp"
TARGET_TRIPLE="$(zig env | sed -n 's/.*"target": "\([^"]*\)".*/\1/p' | cut -d. -f1-3 | tr '.' '-')"
if [[ -z "$TARGET_TRIPLE" ]]; then
    case "$(uname -s)-$(uname -m)" in
        Darwin-arm64) TARGET_TRIPLE="aarch64-apple-darwin" ;;
        Darwin-x86_64) TARGET_TRIPLE="x86_64-apple-darwin" ;;
        Linux-x86_64) TARGET_TRIPLE="x86_64-linux-gnu" ;;
        Linux-aarch64|Linux-arm64) TARGET_TRIPLE="aarch64-linux-gnu" ;;
        *) TARGET_TRIPLE="unknown" ;;
    esac
fi
RELEASE_TAG="v$VERSION"

BINARY_PACKAGE_ROOT="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE"
BINARY_ARCHIVE_PATH="$OUTPUT_DIR/kelp-v$VERSION-$TARGET_TRIPLE.tar.gz"
BINARY_CHECKSUM_PATH="$BINARY_ARCHIVE_PATH.sha256"
SOURCE_PACKAGE_ROOT="$OUTPUT_DIR/kelp-v$VERSION-source"
SOURCE_ARCHIVE_PATH="$OUTPUT_DIR/kelp-v$VERSION-source.tar.gz"
SOURCE_CHECKSUM_PATH="$SOURCE_ARCHIVE_PATH.sha256"
BINARY_PATH="$REPO_ROOT/zig-out/bin/kelp"

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

copy_if_exists() {
    local source="$1"
    local target="$2"
    if [[ -e "$source" ]]; then
        cp "$source" "$target"
    fi
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
  desc "Strict, local-first planner CLI and Lazygit-style TUI"
  homepage "$REPOSITORY_URL"
  url "$REPOSITORY_URL/releases/download/$RELEASE_TAG/$source_asset"
  sha256 "$source_sha"
  license "MIT"

  depends_on "zig" => :build

  def install
    system "zig", "build", "-Doptimize=ReleaseSafe", "--prefix", prefix
    generate_completions_from_executable(bin/"kelp", "completions")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/kelp --version")
  end
end
EOF
    cp "$FORMULA_PATH" "$OUTPUT_DIR/kelp.rb"
}

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

printf 'Building kelp %s with Zig...\n' "$VERSION"
(cd "$REPO_ROOT" && zig build -Doptimize=ReleaseSafe)

printf 'Creating binary archive...\n'
rm -rf "$BINARY_PACKAGE_ROOT"
mkdir -p "$BINARY_PACKAGE_ROOT/completions"
"$BINARY_PATH" completions bash > "$BINARY_PACKAGE_ROOT/completions/kelp.bash"
"$BINARY_PATH" completions zsh > "$BINARY_PACKAGE_ROOT/completions/_kelp"
"$BINARY_PATH" completions fish > "$BINARY_PACKAGE_ROOT/completions/kelp.fish"
cp "$BINARY_PATH" "$BINARY_PACKAGE_ROOT/kelp"
cp "$REPO_ROOT/README.md" "$BINARY_PACKAGE_ROOT/README.md"
copy_if_exists "$REPO_ROOT/LICENSE" "$BINARY_PACKAGE_ROOT/LICENSE"
cp "$REPO_ROOT/installer.sh" "$BINARY_PACKAGE_ROOT/installer.sh"
tar -C "$OUTPUT_DIR" -czf "$BINARY_ARCHIVE_PATH" "$(basename "$BINARY_PACKAGE_ROOT")"
write_checksum_file "$BINARY_ARCHIVE_PATH" "$BINARY_CHECKSUM_PATH"

printf 'Creating source archive...\n'
rm -rf "$SOURCE_PACKAGE_ROOT"
mkdir -p "$SOURCE_PACKAGE_ROOT"
cp "$REPO_ROOT/build.zig" "$SOURCE_PACKAGE_ROOT/build.zig"
cp "$REPO_ROOT/README.md" "$SOURCE_PACKAGE_ROOT/README.md"
copy_if_exists "$REPO_ROOT/LICENSE" "$SOURCE_PACKAGE_ROOT/LICENSE"
cp "$REPO_ROOT/installer.sh" "$SOURCE_PACKAGE_ROOT/installer.sh"
copy_tree "$REPO_ROOT/src" "$SOURCE_PACKAGE_ROOT/src"
copy_tree "$REPO_ROOT/scripts" "$SOURCE_PACKAGE_ROOT/scripts"
copy_tree "$REPO_ROOT/Formula" "$SOURCE_PACKAGE_ROOT/Formula"
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
