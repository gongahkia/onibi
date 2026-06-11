#!/usr/bin/env bash
set -euo pipefail

dist="${1:-dist}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
host_os="$(go env GOOS 2>/dev/null || uname -s | tr '[:upper:]' '[:lower:]')"
host_arch="$(go env GOARCH 2>/dev/null || uname -m)"
case "$host_arch" in
  amd64|x86_64) host_label="x86_64" ;;
  arm64|aarch64) host_label="arm64" ;;
  *) host_label="$host_arch" ;;
esac

shopt -s nullglob
artifacts=("$dist"/onibi_*_*.tar.gz)
if ((${#artifacts[@]} == 0)); then
  echo "no release tarballs under $dist" >&2
  exit 1
fi

if [[ ! -s "$dist/checksums.txt" ]]; then
  echo "missing $dist/checksums.txt" >&2
  exit 1
fi
if command -v shasum >/dev/null 2>&1; then
  (cd "$dist" && shasum -a 256 -c checksums.txt)
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist" && sha256sum -c checksums.txt)
else
  echo "no checksum verifier found (need shasum or sha256sum)" >&2
  exit 1
fi

for tarball in "${artifacts[@]}"; do
  work="$tmp/$(basename "$tarball" .tar.gz)"
  mkdir -p "$work"
  tar -xzf "$tarball" -C "$work"
  home="$work/home"
  runtime="$work/run"
  install_dir="$work/install"
  mkdir -p "$home" "$runtime" "$install_dir"
  test -x "$work/onibi"
  test -x "$work/onibi-notify"
  install -m 0755 "$work/onibi" "$install_dir/onibi"
  install -m 0755 "$work/onibi-notify" "$install_dir/onibi-notify"
  case "$(basename "$tarball")" in
    *"_${host_os}_${host_label}.tar.gz")
      "$install_dir/onibi" version
      HOME="$home" XDG_DATA_HOME="$home/.local/share" XDG_RUNTIME_DIR="$runtime" \
        "$install_dir/onibi" doctor --mode preflight --offline
      ;;
  esac
done
