#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-${KELP_PI_IMAGE_ROOT:-}}"
version="v3.10.0"
asset="nuclei_3.10.0_linux_arm64.zip"
sha256="b0ddb1f0cc894b7fa79e45043d00a5ffd2cc9fc15e169bf567d1a384eae51427"
url="https://github.com/projectdiscovery/nuclei/releases/download/$version/$asset"

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT\n' "$0" >&2
  exit 64
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "$1" >&2
    exit 69
  }
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

need curl
need unzip
need install
need awk

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
zip="$tmp/$asset"

curl -fsSL "$url" -o "$zip"
actual="$(hash_file "$zip")"
[ "$actual" = "$sha256" ] || {
  printf 'sha256 mismatch for %s: expected %s found %s\n' "$asset" "$sha256" "$actual" >&2
  exit 65
}

unzip -q "$zip" -d "$tmp/unzip"
test -x "$tmp/unzip/nuclei"
binary_sha256="$(hash_file "$tmp/unzip/nuclei")"
install -d "$root/opt/kelp-pi/bin" "$root/etc/kelp-pi"
install -m 0755 "$tmp/unzip/nuclei" "$root/opt/kelp-pi/bin/nuclei"
printf '{"version":"%s","asset":"%s","sha256":"%s","binary_sha256":"%s","path":"/opt/kelp-pi/bin/nuclei"}\n' "$version" "$asset" "$sha256" "$binary_sha256" > "$root/etc/kelp-pi/nuclei-binary.json"
printf '%s\n' "$root/opt/kelp-pi/bin/nuclei"
