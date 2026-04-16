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

echo "Building OnibiWeb..."
npm --prefix "${web_root}" run build

if [[ ! -f "${dist_root}/index.html" ]]; then
  echo "Expected dist output at ${dist_root}, but index.html was not found." >&2
  exit 1
fi

echo "Syncing built assets into Onibi bundle resources..."
rm -rf "${bundle_root}"
mkdir -p "${bundle_root}"
cp -R "${dist_root}/." "${bundle_root}/"

echo "Bundled web assets updated at ${bundle_root}"
