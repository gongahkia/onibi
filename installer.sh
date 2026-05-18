#!/usr/bin/env bash

set -euo pipefail

GREEN="\e[32m"
YELLOW="\e[33m"
BLUE="\e[34m"
RED="\e[31m"
ENDCOLOR="\e[0m"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
INSTALL_DIR="${KELP_INSTALL_DIR:-$HOME/.local/bin}"
BUILD_FROM_SOURCE=false
RELEASE_VERSION=""
INSTALL_COMPLETIONS=false
COMPLETION_SHELLS=()
REPOSITORY_URL="${KELP_INSTALL_REPOSITORY:-https://github.com/gongahkia/kelp}"
RELEASE_BASE_URL="${KELP_INSTALL_BASE_URL:-$REPOSITORY_URL/releases}"
SOURCE_PATH="${KELP_INSTALL_SOURCE_PATH:-}"

usage() {
    cat <<'EOF'
Usage: installer.sh [OPTIONS]

Options:
  --release-version VERSION   Install a binary release for the given version before falling back.
  --build-from-source         Skip binary release download and build with Zig immediately.
  --with-completions          Install bash, zsh, and fish completions after installing kelp.
  --shell SHELL               Install completions for one shell (bash, zsh, or fish).
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --release-version)
            RELEASE_VERSION="$2"
            shift 2
            ;;
        --build-from-source)
            BUILD_FROM_SOURCE=true
            shift
            ;;
        --with-completions)
            INSTALL_COMPLETIONS=true
            shift
            ;;
        --shell)
            INSTALL_COMPLETIONS=true
            COMPLETION_SHELLS+=("$2")
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf "${RED}Unknown installer option:${ENDCOLOR} %s\n" "$1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

download_file() {
    local url="$1"
    local destination="$2"
    if command -v curl >/dev/null 2>&1; then
        curl --fail --location --silent --show-error "$url" --output "$destination"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$destination" "$url"
    else
        printf "${RED}curl or wget is required to download release artifacts.${ENDCOLOR}\n" >&2
        return 1
    fi
}

ensure_zig() {
    if command -v zig >/dev/null 2>&1; then
        return
    fi
    printf "${RED}zig was not found. Install Zig 0.15+ and retry.${ENDCOLOR}\n" >&2
    exit 1
}

resolve_target_triple() {
    local os arch
    os="$(uname -s)"
    arch="$(uname -m)"
    case "$os" in
        Darwin)
            case "$arch" in
                arm64|aarch64) printf 'aarch64-apple-darwin' ;;
                x86_64) printf 'x86_64-apple-darwin' ;;
                *) return 1 ;;
            esac
            ;;
        Linux)
            case "$arch" in
                x86_64) printf 'x86_64-linux-gnu' ;;
                aarch64|arm64) printf 'aarch64-linux-gnu' ;;
                *) return 1 ;;
            esac
            ;;
        *)
            return 1
            ;;
    esac
}

install_from_source() {
    ensure_zig
    local source="${SOURCE_PATH:-$REPO_ROOT}"
    if [[ ! -f "$source/build.zig" ]]; then
        printf "${RED}No build.zig found at source path:${ENDCOLOR} %s\n" "$source" >&2
        exit 1
    fi
    printf "${YELLOW}Building kelp from Zig source...${ENDCOLOR}\n"
    (cd "$source" && zig build -Doptimize=ReleaseSafe)
    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$source/zig-out/bin/kelp" "$INSTALL_DIR/kelp"
}

install_from_release() {
    local version="$1"
    local target_triple archive_name archive_url tmp_dir binary_path
    target_triple="$(resolve_target_triple)" || {
        printf "${YELLOW}No binary release target is configured for this platform; falling back to source install.${ENDCOLOR}\n"
        return 1
    }
    archive_name="kelp-v${version}-${target_triple}.tar.gz"
    archive_url="${RELEASE_BASE_URL}/download/v${version}/${archive_name}"
    tmp_dir="$(mktemp -d)"
    printf "${YELLOW}Downloading kelp %s binary release...${ENDCOLOR}\n" "$version"
    if ! download_file "$archive_url" "$tmp_dir/$archive_name"; then
        rm -rf "$tmp_dir"
        return 1
    fi
    tar -xzf "$tmp_dir/$archive_name" -C "$tmp_dir"
    binary_path="$(find "$tmp_dir" -type f -name kelp | head -n 1)"
    if [[ -z "$binary_path" ]]; then
        printf "${RED}Release archive did not contain a kelp binary.${ENDCOLOR}\n" >&2
        rm -rf "$tmp_dir"
        return 1
    fi
    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$binary_path" "$INSTALL_DIR/kelp"
    rm -rf "$tmp_dir"
}

install_completion() {
    local shell="$1"
    local binary_path="$INSTALL_DIR/kelp"
    local target_dir target_file
    case "$shell" in
        bash)
            target_dir="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
            target_file="$target_dir/kelp"
            ;;
        zsh)
            target_dir="${ZDOTDIR:-$HOME}/.zfunc"
            target_file="$target_dir/_kelp"
            ;;
        fish)
            target_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
            target_file="$target_dir/kelp.fish"
            ;;
        *)
            printf "${RED}Unsupported completion shell:${ENDCOLOR} %s\n" "$shell" >&2
            exit 1
            ;;
    esac
    mkdir -p "$target_dir"
    "$binary_path" completions "$shell" > "$target_file"
    printf "${GREEN}Installed %s completions:${ENDCOLOR} %s\n" "$shell" "$target_file"
}

printf "${BLUE}Installing kelp...${ENDCOLOR}\n"

if [[ "$BUILD_FROM_SOURCE" == true ]]; then
    install_from_source
elif [[ -n "$RELEASE_VERSION" ]]; then
    if ! install_from_release "$RELEASE_VERSION"; then
        printf "${YELLOW}Binary release install failed; falling back to source install.${ENDCOLOR}\n"
        install_from_source
    fi
else
    install_from_source
fi

if [[ "$INSTALL_COMPLETIONS" == true ]]; then
    if [[ ${#COMPLETION_SHELLS[@]} -eq 0 ]]; then
        COMPLETION_SHELLS=(bash zsh fish)
    fi
    for shell in "${COMPLETION_SHELLS[@]}"; do
        install_completion "$shell"
    done
fi

printf "${GREEN}kelp installed successfully.${ENDCOLOR}\n"
printf "${BLUE}Binary location:${ENDCOLOR} %s/kelp\n" "$INSTALL_DIR"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    printf "${YELLOW}Add this directory to your PATH if needed:${ENDCOLOR}\n"
    printf "  export PATH=\"%s:\$PATH\"\n" "$INSTALL_DIR"
fi
printf "${GREEN}Try:${ENDCOLOR} kelp\n"
