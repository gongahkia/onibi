#!/usr/bin/env bash
set -euo pipefail

out_dir="${1:-}"
domain="${ONIBI_INSTALL_DOMAIN:-get.onibi.sh}"

if [[ -z "$out_dir" ]]; then
  echo "usage: GPG_PUBLIC_KEY_B64=... $0 <pages-dir>" >&2
  exit 2
fi
case "$out_dir" in
  /|"") echo "refusing unsafe pages dir: $out_dir" >&2; exit 2 ;;
esac
if [[ -z "$domain" || "$domain" == *$'\n'* ]]; then
  echo "invalid ONIBI_INSTALL_DOMAIN" >&2
  exit 2
fi

mkdir -p "$out_dir"
scripts/render-install.sh "$out_dir/index.html"
printf '%s\n' "$domain" >"$out_dir/CNAME"
: >"$out_dir/.nojekyll"

if grep -q "__ONIBI_RELEASE_GPG_KEY_B64__" "$out_dir/index.html"; then
  echo "installer key placeholder was not replaced" >&2
  exit 1
fi
sh -n "$out_dir/index.html"
printf 'prepared %s for %s\n' "$out_dir" "$domain"
