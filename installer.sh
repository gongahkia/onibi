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

printf "${GREEN}Kelp installed successfully.${ENDCOLOR}\n"
printf "${BLUE}Binary location:${ENDCOLOR} %s/kelp\n" "$CARGO_BIN_DIR"

if [[ ":$PATH:" != *":$CARGO_BIN_DIR:"* ]]; then
    printf "${YELLOW}Add this directory to your PATH if needed:${ENDCOLOR}\n"
    printf "  export PATH=\"%s:\$PATH\"\n" "$CARGO_BIN_DIR"
fi

printf "${GREEN}Try:${ENDCOLOR} kelp init\n"
