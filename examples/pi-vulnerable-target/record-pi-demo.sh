#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
asset="$repo_root/docs/assets/kelp-pi-fixture-demo.cast"
run_dir="$repo_root/.kelpclaw/pi-vulnerable-target-field"
walkthrough="$repo_root/examples/pi-vulnerable-target/pi-field-walkthrough.sh"
verifier="$repo_root/scripts/verify-pi-launch-evidence.sh"

usage() {
  printf '%s\n' "usage: $0 [--asset PATH] [--out DIR] --pi-host HOST --fixture-ip IP --control-url URL --client-a IP --client-b IP --client-ssh-user USER --upstream-interface IFACE --updated-config PATH --until RFC3339 [--max-seconds N] [--ollama-check load|refuse] [--readonly-root]"
}

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

shell_quote() {
  printf "'"
  printf '%s' "$1" | sed "s/'/'\\\\''/g"
  printf "'"
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
    --out)
      [ $# -ge 2 ] || fail "--out requires a value"
      run_dir="$2"
      walkthrough_args="${walkthrough_args:+$walkthrough_args
}--out
$2"
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
need chmod
need dirname
need mktemp
need mkdir
need node
need sed

mkdir -p "$(dirname "$asset")"
cmd_file="$(mktemp "${TMPDIR:-/tmp}/kelp-pi-demo.XXXXXX")"
trap 'rm -f "$cmd_file"' EXIT HUP INT TERM
{
  printf '%s\n' "#!/usr/bin/env sh"
  printf '%s\n' "set -eu"
  printf '%s' "exec "
  shell_quote "$walkthrough"
  if [ -n "$walkthrough_args" ]; then
    old_ifs="$IFS"
    IFS='
'
    for arg in $walkthrough_args; do
      printf '%s' " "
      shell_quote "$arg"
    done
    IFS="$old_ifs"
  fi
  printf '\n'
} >"$cmd_file"
chmod 0755 "$cmd_file"

asciinema rec "$asset" --overwrite --command "$cmd_file"
"$verifier" --demo-asset "$asset" "$run_dir"
printf '%s\n' "$asset"
