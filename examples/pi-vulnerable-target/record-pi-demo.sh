#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
asset="$repo_root/docs/assets/kelp-pi-fixture-demo.cast"
walkthrough="$repo_root/examples/pi-vulnerable-target/pi-field-walkthrough.sh"

usage() {
  printf '%s\n' "usage: $0 [--asset PATH] --pi-host HOST --fixture-ip IP --control-url URL --client-a IP --client-b IP --client-ssh-user USER --upstream-interface IFACE --updated-config PATH --until RFC3339"
}

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

walkthrough_args=""
while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --asset)
      [ $# -ge 2 ] || fail "--asset requires a value"
      asset="$2"
      shift 2
      ;;
    *)
      walkthrough_args="${walkthrough_args:+$walkthrough_args
}$1"
      shift
      ;;
  esac
done

[ -n "$walkthrough_args" ] || {
  usage >&2
  exit 64
}
need asciinema
need dirname
need mkdir

mkdir -p "$(dirname "$asset")"
set -- "$walkthrough"
if [ -n "$walkthrough_args" ]; then
  old_ifs="$IFS"
  IFS='
'
  for arg in $walkthrough_args; do
    set -- "$@" "$arg"
  done
  IFS="$old_ifs"
fi

asciinema rec "$asset" --overwrite --command "$*"
printf '%s\n' "$asset"
