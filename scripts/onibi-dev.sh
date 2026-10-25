#!/bin/sh
# Bootstrap only the small Go helper; the application build has real stage
# progress and bounded diagnostic output. No installed Onibi or Rust is needed.
set -eu
onibi_source_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$onibi_source_root"
case "${1:-}" in
  build) sh "$onibi_source_root/scripts/onibi-banner.sh" >&2 ;;
esac
printf '%s\n' '[prepare] Loading Onibi build tools (first run may compile Go dependencies)...' >&2
exec go run ./cmd/onibi-dev "$@"
