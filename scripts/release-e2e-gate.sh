#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <binary>" >&2
  exit 2
fi

binary="$1"

if [ "${ONIBI_RELEASE_SNAPSHOT:-false}" = "true" ]; then
  exit 0
fi

tag="${ONIBI_RELEASE_TAG:-}"
if [ -z "$tag" ] && command -v git >/dev/null 2>&1; then
  tag="$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || true)"
fi
case "$tag" in
  v*) ;;
  *) exit 0 ;;
esac

if [ ! -f "$binary" ]; then
  echo "release e2e gate: binary not found: $binary" >&2
  exit 1
fi

if ! command -v strings >/dev/null 2>&1; then
  echo "release e2e gate: strings not found" >&2
  exit 1
fi

if strings "$binary" | grep -Fq "unsafe-cloudflare-no-e2e"; then
  echo "release e2e gate: forbidden unsafe-cloudflare-no-e2e marker found in tagged release binary $binary" >&2
  exit 1
fi

echo "release e2e gate: passed $binary for $tag"
