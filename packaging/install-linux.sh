#!/usr/bin/env bash
set -euo pipefail

REPO="${ONIBI_REPO:-gongahkia/onibi}"
VERSION="${ONIBI_VERSION:-1.5.0}"
PREFIX="${ONIBI_PREFIX:-/usr/local}"
BIN_DIR="${PREFIX}/bin"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
SYSTEMD_USER_DIR="${CONFIG_HOME}/systemd/user"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

arch_asset() {
  case "$(uname -m)" in
    x86_64) echo "onibi-headless-${VERSION}-linux-x86_64" ;;
    aarch64|arm64) echo "onibi-headless-${VERSION}-linux-arm64" ;;
    armv7l) echo "armv7 is not supported in Onibi ${VERSION}; use a 64-bit OS" >&2; exit 1 ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

install_file() {
  local source="$1"
  local destination="$2"
  local mode="$3"
  if install -m "$mode" "$source" "$destination" 2>/dev/null; then
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo install -m "$mode" "$source" "$destination"
  else
    echo "cannot write $destination; rerun with ONIBI_PREFIX pointing to a writable prefix" >&2
    exit 1
  fi
}

require curl
require chmod
require install

ASSET="$(arch_asset)"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"

echo "Downloading ${URL}"
curl -fL "$URL" -o "${TMP_DIR}/onibi"
chmod +x "${TMP_DIR}/onibi"

if ! mkdir -p "$BIN_DIR" 2>/dev/null; then
  if command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$BIN_DIR"
  else
    echo "cannot create $BIN_DIR; rerun with ONIBI_PREFIX pointing to a writable prefix" >&2
    exit 1
  fi
fi
install_file "${TMP_DIR}/onibi" "${BIN_DIR}/onibi" 755

mkdir -p "$SYSTEMD_USER_DIR"
UNIT_URL="https://raw.githubusercontent.com/${REPO}/v${VERSION}/packaging/systemd/onibi.service"
curl -fL "$UNIT_URL" -o "${SYSTEMD_USER_DIR}/onibi.service"

if command -v systemctl >/dev/null 2>&1; then
  systemctl --user daemon-reload
  systemctl --user enable --now onibi.service
  echo "Onibi systemd user service enabled."
else
  echo "systemctl not found; start Onibi manually with: ${BIN_DIR}/onibi --headless --auto-transports"
fi

echo "Onibi installed."
echo "Run: onibi setup"
echo "Check: onibi status"
