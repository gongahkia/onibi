#!/usr/bin/env bash

set -euo pipefail

GREEN="\e[32m"
YELLOW="\e[33m"
BLUE="\e[34m"
RED="\e[31m"
ENDCOLOR="\e[0m"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
CARGO_BIN_DIR="${CARGO_HOME:-$HOME/.cargo}/bin"
INSTALL_COMPLETIONS=false
COMPLETION_SHELLS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --with-completions)
            INSTALL_COMPLETIONS=true
            shift
            ;;
        --shell)
            INSTALL_COMPLETIONS=true
            COMPLETION_SHELLS+=("$2")
            shift 2
            ;;
        *)
            printf "${RED}Unknown installer option:${ENDCOLOR} %s\n" "$1" >&2
            printf "Usage: %s [--with-completions] [--shell bash|zsh|fish]\n" "$0" >&2
            exit 1
            ;;
    esac
done

printf "${BLUE}Installing Kelp from${ENDCOLOR} ${REPO_ROOT}\n"

ensure_rust_toolchain() {
    if command -v cargo >/dev/null 2>&1; then
        return
    fi

    printf "${YELLOW}Cargo was not found, installing Rust with rustup...${ENDCOLOR}\n"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal

    if [[ -f "$HOME/.cargo/env" ]]; then
        # shellcheck disable=SC1090
        source "$HOME/.cargo/env"
    fi

    if ! command -v cargo >/dev/null 2>&1; then
        printf "${RED}Cargo is still unavailable after rustup installation.${ENDCOLOR}\n" >&2
        exit 1
    fi
}

ensure_rust_toolchain

printf "${YELLOW}Building and installing the Kelp binary...${ENDCOLOR}\n"
cargo install --path "$REPO_ROOT" --locked --force

install_completion() {
    local shell="$1"
    local binary_path="${CARGO_BIN_DIR}/kelp"
    local target_dir
    local target_file

    case "$shell" in
        bash)
            target_dir="${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions"
            target_file="${target_dir}/kelp"
            ;;
        zsh)
            target_dir="${ZDOTDIR:-$HOME}/.zfunc"
            target_file="${target_dir}/_kelp"
            ;;
        fish)
            target_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
            target_file="${target_dir}/kelp.fish"
            ;;
        *)
            printf "${RED}Unsupported completion shell:${ENDCOLOR} %s\n" "$shell" >&2
            exit 1
            ;;
    esac

    mkdir -p "$target_dir"
    "$binary_path" completions "$shell" > "$target_file"
    printf "${GREEN}Installed ${shell} completions:${ENDCOLOR} %s\n" "$target_file"
}

if [[ "$INSTALL_COMPLETIONS" == true ]]; then
    if [[ ${#COMPLETION_SHELLS[@]} -eq 0 ]]; then
        COMPLETION_SHELLS=(bash zsh fish)
    fi

    for shell in "${COMPLETION_SHELLS[@]}"; do
        install_completion "$shell"
    done
fi

printf "${GREEN}Kelp installed successfully.${ENDCOLOR}\n"
printf "${BLUE}Binary location:${ENDCOLOR} %s/kelp\n" "$CARGO_BIN_DIR"

if [[ ":$PATH:" != *":$CARGO_BIN_DIR:"* ]]; then
    printf "${YELLOW}Add this directory to your PATH if needed:${ENDCOLOR}\n"
    printf "  export PATH=\"%s:\$PATH\"\n" "$CARGO_BIN_DIR"
fi

if [[ "$INSTALL_COMPLETIONS" == true ]]; then
    printf "${YELLOW}Restart your shell or reload its completion cache to pick up the new scripts.${ENDCOLOR}\n"
fi

printf "${GREEN}Try:${ENDCOLOR} kelp init\n"
