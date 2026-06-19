#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
field_verifier="$repo_root/scripts/verify-pi-field-acceptance.sh"
load_dir=""
refuse_dir=""

usage() {
  printf '%s\n' "usage: $0 --load-field-dir DIR --refuse-field-dir DIR"
}

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

kv_from_log() {
  awk -v key="$2" '
    {
      for (i = 1; i <= NF; i++) {
        if ($i ~ "^" key "=") {
          sub("^" key "=", "", $i)
          print $i
          exit
        }
      }
    }
  ' "$1"
}

verify_field_dir() {
  dir="$1"
  summary_key="$2"
  [ -d "$dir" ] || fail "field dir missing: $dir"
  "$field_verifier" "$dir" >/dev/null
  grep -q "^OK $summary_key " "$dir/summary.txt" || fail "$summary_key missing from $dir/summary.txt"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --load-field-dir)
      [ $# -ge 2 ] || fail "--load-field-dir requires a value"
      load_dir="$2"
      shift 2
      ;;
    --refuse-field-dir)
      [ $# -ge 2 ] || fail "--refuse-field-dir requires a value"
      refuse_dir="$2"
      shift 2
      ;;
    *)
      fail "unexpected argument: $1"
      ;;
  esac
done

[ -n "$load_dir" ] || fail "requires --load-field-dir"
[ -n "$refuse_dir" ] || fail "requires --refuse-field-dir"
[ "$load_dir" != "$refuse_dir" ] || fail "load and refusal evidence must come from separate field runs"
[ -x "$field_verifier" ] || fail "field verifier missing: $field_verifier"
need awk
need grep

verify_field_dir "$load_dir" ollama-load
verify_field_dir "$refuse_dir" ollama-refuse

load_log="$load_dir/ollama-load.log"
refuse_log="$refuse_dir/ollama-refuse.log"
load_ram="$(kv_from_log "$load_log" ram_bytes)"
load_min="$(kv_from_log "$load_log" min_pi_ram_bytes)"
refuse_ram="$(kv_from_log "$refuse_log" ram_bytes)"
refuse_min="$(kv_from_log "$refuse_log" min_pi_ram_bytes)"

[ -n "$load_ram" ] || fail "load ram_bytes missing"
[ -n "$load_min" ] || fail "load min_pi_ram_bytes missing"
[ -n "$refuse_ram" ] || fail "refusal ram_bytes missing"
[ -n "$refuse_min" ] || fail "refusal min_pi_ram_bytes missing"

awk -v ram="$load_ram" -v min="$load_min" 'BEGIN { exit !(ram >= min) }' || fail "load artifact is below Pi Ollama RAM threshold"
awk -v ram="$refuse_ram" -v min="$refuse_min" 'BEGIN { exit !(ram < min) }' || fail "refusal artifact is not below Pi Ollama RAM threshold"

pass "P8 Ollama evidence verified: load_ram_bytes=$load_ram refuse_ram_bytes=$refuse_ram"
