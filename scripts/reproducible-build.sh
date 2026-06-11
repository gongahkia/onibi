#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
version="repro"
commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
date="1970-01-01T00:00:00Z"
ldflags="-s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=${version} -X github.com/gongahkia/onibi/internal/buildinfo.Commit=${commit} -X github.com/gongahkia/onibi/internal/buildinfo.Date=${date}"

build_once() {
  local out="$1"
  mkdir -p "$out"
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/onibi" ./cmd/onibi
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/onibi-notify" ./clients/onibi-notify
}

build_once "$tmp/a"
build_once "$tmp/b"

cmp "$tmp/a/onibi" "$tmp/b/onibi"
cmp "$tmp/a/onibi-notify" "$tmp/b/onibi-notify"

if command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$tmp/a/onibi" "$tmp/a/onibi-notify"
else
  sha256sum "$tmp/a/onibi" "$tmp/a/onibi-notify"
fi
