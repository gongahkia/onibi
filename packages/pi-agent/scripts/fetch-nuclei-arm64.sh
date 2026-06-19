#!/usr/bin/env sh
set -eu

root="${1:-${KELP_PI_IMAGE_ROOT:-}}"
version="v3.9.0"
asset="nuclei_3.9.0_linux_arm64.zip"
sha256="733ceb77896fc5a9cafb70d07cabdd43fd9f186c28cbc335eec5b78d5c35d850"
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
install -d "$root/opt/kelp-pi/bin" "$root/etc/kelp-pi"
install -m 0755 "$tmp/unzip/nuclei" "$root/opt/kelp-pi/bin/nuclei"
printf '{"version":"%s","asset":"%s","sha256":"%s","path":"/opt/kelp-pi/bin/nuclei"}\n' "$version" "$asset" "$sha256" > "$root/etc/kelp-pi/nuclei-binary.json"
printf '%s\n' "$root/opt/kelp-pi/bin/nuclei"
