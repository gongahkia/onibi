#!/usr/bin/env sh
set -eu

data_dir="${KELP_PI_DATA_DIR:-/var/lib/kelp-pi}"
probe_file=""

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

cleanup() {
  [ -z "$probe_file" ] || rm -f "$probe_file"
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
  case "$1" in
    --data-dir)
      [ $# -ge 2 ] || fail "--data-dir requires a value"
      data_dir="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ "$(id -u)" = "0" ] || fail "run as root on the Pi"
need findmnt
need grep
need head
need mktemp
need rm
need sync
need tr

root_options="$(findmnt -no OPTIONS /)"
printf '%s\n' "$root_options" | tr ',' '\n' | grep -qx ro || fail "root filesystem is not mounted read-only"
pass "root filesystem mounted read-only"

[ -d "$data_dir" ] || fail "$data_dir missing"
data_target="$(findmnt -T "$data_dir" -no TARGET | head -n 1)"
[ -n "$data_target" ] || fail "$data_dir mount not found"
[ "$data_target" != "/" ] || fail "$data_dir is still backed by root filesystem"
data_options="$(findmnt -T "$data_dir" -no OPTIONS | head -n 1)"
printf '%s\n' "$data_options" | tr ',' '\n' | grep -qx rw || fail "$data_dir is not writable"

probe_file="$(mktemp "$data_dir/.kelp-readonly-root.XXXXXX")"
printf '%s\n' "kelp-pi readonly-root probe" >"$probe_file"
sync
rm -f "$probe_file"
probe_file=""
pass "$data_dir is writable outside read-only root"
