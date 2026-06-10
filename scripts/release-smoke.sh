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

for tarball in "${artifacts[@]}"; do
  work="$tmp/$(basename "$tarball" .tar.gz)"
  mkdir -p "$work"
  tar -xzf "$tarball" -C "$work"
  home="$work/home"
  runtime="$work/run"
  mkdir -p "$home" "$runtime"
  test -x "$work/onibi"
  test -x "$work/onibi-notify"
  case "$(basename "$tarball")" in
    *"_${host_os}_${host_label}.tar.gz")
      "$work/onibi" version
      HOME="$home" XDG_DATA_HOME="$home/.local/share" XDG_RUNTIME_DIR="$runtime" \
        "$work/onibi" doctor --mode preflight --offline
      ;;
  esac
done
