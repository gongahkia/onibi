#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp" dist' EXIT
date="1970-01-01T00:00:00Z"
first="$tmp/first"
second="$tmp/second"

build_once() {
  local out="$1"
  rm -rf dist "$out"
  ONIBI_BUILD_DATE="$date" goreleaser build --snapshot --clean --skip=before --skip=post-hooks --single-target
  mkdir -p "$out"
  while IFS= read -r artifact; do
    rel="${artifact#dist/}"
    mkdir -p "$out/$(dirname "$rel")"
    cp "$artifact" "$out/$rel"
  done < <(find dist -type f -perm -111 | LC_ALL=C sort)
  if [ "$(find "$out" -type f | wc -l | tr -d ' ')" -eq 0 ]; then
    echo "reproducible build: no binary artifacts captured" >&2
    exit 1
  fi
}

hash_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

build_once "$first"
build_once "$second"

failed=0
while IFS= read -r artifact; do
  rel="${artifact#$first/}"
  peer="$second/$rel"
  if [ ! -f "$peer" ]; then
    echo "reproducible build: missing second artifact: $rel" >&2
    failed=1
    continue
  fi
  a="$(hash_file "$artifact")"
  b="$(hash_file "$peer")"
  if [ "$a" != "$b" ]; then
    echo "reproducible build: artifact differs: $rel" >&2
    echo "  first:  $a" >&2
    echo "  second: $b" >&2
    failed=1
  fi
done < <(find "$first" -type f | LC_ALL=C sort)

while IFS= read -r artifact; do
  rel="${artifact#$second/}"
  if [ ! -f "$first/$rel" ]; then
    echo "reproducible build: extra second artifact: $rel" >&2
    failed=1
  fi
done < <(find "$second" -type f | LC_ALL=C sort)

if [ "$failed" -ne 0 ]; then
  exit 1
fi

while IFS= read -r artifact; do
  rel="${artifact#$first/}"
  printf '%s  %s\n' "$(hash_file "$artifact")" "$rel"
done < <(find "$first" -type f | LC_ALL=C sort)
