#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <binary>" >&2
  exit 2
fi

binary="$1"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

if [ "$(basename "$binary")" = "onibi" ]; then
  "$script_dir/macos-scenario-gate.sh" "${ONIBI_MACOS_SCENARIO_GATE_DIR:-artifacts/macos-scenario-gate-$tag}"
  "$script_dir/upgrade-recovery-gate.sh" "${ONIBI_UPGRADE_GATE_DIR:-artifacts/upgrade-recovery-gate-$tag}"
  "$script_dir/security-regression-gate.sh" "${ONIBI_SECURITY_GATE_DIR:-artifacts/security-regression-gate-$tag}"
fi
echo "release e2e gate: Cloudflare relay E2E is mandatory; passed $binary for $tag"
