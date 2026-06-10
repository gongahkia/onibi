#!/usr/bin/env bash
set -euo pipefail

export ONIBI_INSTALL_PROFILE=pi
: "${ONIBI_VERSION:=1.5.0}"

if [ "$(uname -m)" != "aarch64" ] && [ "$(uname -m)" != "arm64" ]; then
  echo "Pi install requires a 64-bit Raspberry Pi OS userland (aarch64)." >&2
  exit 1
fi

echo "Installing Onibi for Raspberry Pi 5 / Pi OS 64-bit."
echo "The daemon stores config in \${XDG_CONFIG_HOME:-\$HOME/.config}/onibi and does not require udev rules."

curl -fsSL "https://raw.githubusercontent.com/gongahkia/onibi/v${ONIBI_VERSION}/packaging/install-linux.sh" | bash
