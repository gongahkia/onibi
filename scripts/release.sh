#!/bin/sh
set -eu
version=${1:-0.1.0-dev}
mkdir -p dist build/release
for target in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64; do
  target_os=${target%/*}
  target_arch=${target#*/}
  stage="build/release/$target_os-$target_arch"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" GOARM=7 GOAMD64=v1 \
    go build -trimpath -ldflags="-s -w -X main.version=$version" -o "$stage/shack" ./cmd/shack
  cp README.md "$stage/README.md"
  tar -czf "dist/shack-$version-$target_os-$target_arch.tar.gz" -C "$stage" shack README.md
done
if command -v sha256sum >/dev/null 2>&1; then
  (cd dist && sha256sum ./*.tar.gz > SHA256SUMS)
else
  (cd dist && shasum -a 256 ./*.tar.gz > SHA256SUMS)
fi
