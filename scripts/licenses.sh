#!/bin/sh
set -eu
out=${1:-build/THIRD_PARTY_LICENSES.txt}
mkdir -p "$(dirname "$out")"
{
  printf 'Go runtime license\n\n'
  go_root=$(go env GOROOT)
  if test -f "$go_root/LICENSE"; then cat "$go_root/LICENSE"; else cat "$go_root/../LICENSE"; fi
  go list -deps -f '{{with .Module}}{{if .Version}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/shack | sort -u |
  while IFS=' ' read -r module directory; do
    test -n "$module" || continue
    printf '\n\n===== %s =====\n' "$module"
    for name in LICENSE LICENSE.txt LICENSE.md LICENCE COPYING NOTICE NOTICE.txt; do
      if test -f "$directory/$name"; then cat "$directory/$name"; printf '\n'; fi
    done
  done
} > "$out"
