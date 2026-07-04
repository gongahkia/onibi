#!/usr/bin/env bash
set -euo pipefail

out="${1:-}"
key="${GPG_PUBLIC_KEY_B64:-${ONIBI_RELEASE_GPG_KEY_B64:-}}"

if [[ -z "$out" ]]; then
  echo "usage: GPG_PUBLIC_KEY_B64=... $0 <output-file>" >&2
  exit 2
fi
if [[ -z "$key" ]]; then
  echo "missing GPG_PUBLIC_KEY_B64 or ONIBI_RELEASE_GPG_KEY_B64" >&2
  exit 2
fi

mkdir -p "$(dirname "$out")"
awk -v key="$key" '{ gsub("__ONIBI_RELEASE_GPG_KEY_B64__", key); print }' scripts/install.sh >"$out"
chmod 0644 "$out"

if grep -q "__ONIBI_RELEASE_GPG_KEY_B64__" "$out"; then
  echo "installer key placeholder was not replaced" >&2
  exit 1
fi
