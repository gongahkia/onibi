#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${ONIBI_BUILD_CACHE:-"${repo_root}/.build/onibi-tools"}"
ghostty_commit="${GHOSTTY_COMMIT:-0deaac08ed1a95330346afabbad03da701708331}"
zig_version="${ZIG_VERSION:-0.15.2}"
ghostty_root="${ONIBI_GHOSTTY_SOURCE:-"${cache_root}/ghostty"}"
wasm_output="${repo_root}/OnibiWeb/public/ghostty-vt.wasm"

mkdir -p "${cache_root}"

resolve_zig() {
  if [[ -n "${ZIG:-}" ]]; then
    printf '%s\n' "${ZIG}"
    return
  fi

  if command -v zig >/dev/null 2>&1 && [[ "$(zig version)" == "${zig_version}" ]]; then
    command -v zig
    return
  fi

  if command -v brew >/dev/null 2>&1 && brew --prefix "zig@0.15" >/dev/null 2>&1; then
    local brew_zig
    brew_zig="$(brew --prefix "zig@0.15")/bin/zig"
    if [[ -x "${brew_zig}" && "$("${brew_zig}" version)" == "${zig_version}" ]]; then
      printf '%s\n' "${brew_zig}"
      return
    fi
  fi

  local os arch platform archive_name archive_url zig_dir archive_path
  os="$(uname -s)"
  arch="$(uname -m)"

  case "${os}:${arch}" in
    Darwin:arm64) platform="aarch64-macos" ;;
    Darwin:x86_64) platform="x86_64-macos" ;;
    Linux:arm64|Linux:aarch64) platform="aarch64-linux" ;;
    Linux:x86_64) platform="x86_64-linux" ;;
    *)
      echo "Unsupported Zig bootstrap platform: ${os} ${arch}" >&2
      echo "Install Zig ${zig_version} and rerun with ZIG=/path/to/zig." >&2
      exit 1
      ;;
  esac

  archive_name="zig-${platform}-${zig_version}.tar.xz"
  archive_url="https://ziglang.org/download/${zig_version}/${archive_name}"
  zig_dir="${cache_root}/zig-${platform}-${zig_version}"
  archive_path="${cache_root}/${archive_name}"

  if [[ ! -x "${zig_dir}/zig" ]]; then
    echo "Downloading Zig ${zig_version} for ${platform}..."
    curl -L "${archive_url}" -o "${archive_path}"
    rm -rf "${zig_dir}"
    tar -C "${cache_root}" -xf "${archive_path}"
  fi

  printf '%s\n' "${zig_dir}/zig"
}

zig_bin="$(resolve_zig)"

if [[ ! -d "${ghostty_root}/.git" ]]; then
  echo "Cloning Ghostty into ${ghostty_root}..."
  rm -rf "${ghostty_root}"
  git clone https://github.com/ghostty-org/ghostty.git "${ghostty_root}"
fi

echo "Checking out Ghostty ${ghostty_commit}..."
git -C "${ghostty_root}" fetch --tags --quiet origin "${ghostty_commit}"
git -C "${ghostty_root}" checkout --quiet "${ghostty_commit}"

echo "Building ghostty-vt.wasm with Zig $(${zig_bin} version)..."
"${zig_bin}" build \
  -Demit-lib-vt \
  -Dtarget=wasm32-freestanding \
  -Doptimize=ReleaseSmall \
  --build-file "${ghostty_root}/build.zig" \
  --cache-dir "${ghostty_root}/.zig-cache" \
  --global-cache-dir "${cache_root}/zig-cache" \
  --prefix "${ghostty_root}/zig-out"

if [[ ! -f "${ghostty_root}/zig-out/bin/ghostty-vt.wasm" ]]; then
  echo "Ghostty build completed, but ghostty-vt.wasm was not produced." >&2
  exit 1
fi

mkdir -p "$(dirname "${wasm_output}")"
cp "${ghostty_root}/zig-out/bin/ghostty-vt.wasm" "${wasm_output}"
echo "Wrote ${wasm_output}"
