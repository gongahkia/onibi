#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
data_dir="${KELP_PI_DATA_DIR:-/var/lib/kelp-pi}"
nuclei_bin="${KELP_PI_NUCLEI_BIN:-/opt/kelp-pi/bin/nuclei}"
nuclei_manifest="${KELP_PI_NUCLEI_MANIFEST:-/etc/kelp-pi/nuclei-binary.json}"
templates_sha="${KELP_PI_NUCLEI_TEMPLATES_SHA:-cce82b61d26bed35074cd57bc9d0aebd703a81d3}"
binary_sha256="6b6f19f038f959c2ec90d9f3e3f039256987d1eb78d5c292d7ec9a384513e27f"
target=""
approval_token=""
run_id=""
timeout_seconds=120
output_file=""

fail() {
  printf 'FAIL %s\n' "$*" >&2
  [ -z "$output_file" ] || sed -n '1,160p' "$output_file" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

cleanup() {
  [ -z "$output_file" ] || rm -f "$output_file"
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
  case "$1" in
    --agent-bin)
      [ $# -ge 2 ] || fail "--agent-bin requires a value"
      agent_bin="$2"
      shift 2
      ;;
    --data-dir)
      [ $# -ge 2 ] || fail "--data-dir requires a value"
      data_dir="$2"
      shift 2
      ;;
    --nuclei-bin)
      [ $# -ge 2 ] || fail "--nuclei-bin requires a value"
      nuclei_bin="$2"
      shift 2
      ;;
    --nuclei-manifest)
      [ $# -ge 2 ] || fail "--nuclei-manifest requires a value"
      nuclei_manifest="$2"
      shift 2
      ;;
    --target)
      [ $# -ge 2 ] || fail "--target requires a value"
      target="$2"
      shift 2
      ;;
    --approval-token)
      [ $# -ge 2 ] || fail "--approval-token requires a value"
      approval_token="$2"
      shift 2
      ;;
    --run-id)
      [ $# -ge 2 ] || fail "--run-id requires a value"
      run_id="$2"
      shift 2
      ;;
    --templates-sha)
      [ $# -ge 2 ] || fail "--templates-sha requires a value"
      templates_sha="$2"
      shift 2
      ;;
    --timeout-seconds)
      [ $# -ge 2 ] || fail "--timeout-seconds requires a value"
      timeout_seconds="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ -n "$target" ] || fail "requires --target"
[ -n "$approval_token" ] || fail "requires --approval-token"

need "$agent_bin"
need awk
need date
need file
need grep
need mktemp
need rm
need sed

[ -n "$run_id" ] || run_id="field-nuclei-$(date -u '+%Y%m%d%H%M%S')"

[ -x "$nuclei_bin" ] || fail "$nuclei_bin missing or not executable"
[ -f "$nuclei_manifest" ] || fail "$nuclei_manifest missing"
grep -q "\"binary_sha256\":\"$binary_sha256\"" "$nuclei_manifest" || fail "nuclei manifest binary hash mismatch"
installed_nuclei_sha256="$(hash_file "$nuclei_bin")"
[ "$installed_nuclei_sha256" = "$binary_sha256" ] || fail "nuclei installed binary sha256 mismatch"
nuclei_file="$(file "$nuclei_bin")"
printf 'pinned Nuclei binary file=%s\n' "$nuclei_file"
printf '%s\n' "$nuclei_file" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || fail "pinned Nuclei binary is not an aarch64 ELF"
pass "pinned Nuclei binary sha256=$installed_nuclei_sha256"

output_file="$(mktemp)"
"$agent_bin" scan nuclei \
  --data-dir "$data_dir" \
  --target "$target" \
  --scanner-bin "$nuclei_bin" \
  --approval-token "$approval_token" \
  --run-id "$run_id" \
  --templates-sha "$templates_sha" \
  --max-scan-duration-seconds "$timeout_seconds" \
  -- \
  "$@" >"$output_file" 2>&1 || fail "pinned Nuclei scan failed"

pass "pinned Nuclei scan ran with $nuclei_bin"
