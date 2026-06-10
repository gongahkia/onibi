#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web_root="${repo_root}/OnibiWeb"
dist_root="${web_root}/dist"
bundle_root="${repo_root}/Onibi/Resources/OnibiWeb"

if [[ ! -d "${web_root}" ]]; then
  echo "OnibiWeb directory not found at ${web_root}" >&2
  exit 1
fi

if [[ ! -d "${bundle_root}" ]]; then
  echo "Bundled web assets directory not found at ${bundle_root}" >&2
  echo "Run ./scripts/sync_web_assets.sh to initialize bundled assets." >&2
  exit 1
fi

echo "Building OnibiWeb for sync verification..."
npm --prefix "${web_root}" run build >/dev/null

if [[ ! -f "${dist_root}/index.html" ]]; then
  echo "Expected dist output at ${dist_root}, but index.html was not found." >&2
  exit 1
fi

compare_root="$(mktemp -d)"
trap 'rm -rf "${compare_root}"' EXIT
mkdir -p "${compare_root}/dist" "${compare_root}/bundle"

rsync -a --delete --checksum --exclude '.DS_Store' "${dist_root}/" "${compare_root}/dist/"
rsync -a --delete --checksum --exclude '.DS_Store' "${bundle_root}/" "${compare_root}/bundle/"

if diff -qr "${compare_root}/dist" "${compare_root}/bundle" >/dev/null; then
  echo "Onibi web assets are synced."
  exit 0
fi

echo "Onibi web assets are out of sync." >&2
echo "Run ./scripts/sync_web_assets.sh and commit updated files under Onibi/Resources/OnibiWeb/." >&2

diff -qr "${compare_root}/dist" "${compare_root}/bundle" || true
exit 1
