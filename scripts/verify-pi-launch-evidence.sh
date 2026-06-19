#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
run_dir=""
demo_asset="$repo_root/docs/assets/kelp-pi-fixture-demo.cast"
field_verifier="$repo_root/scripts/verify-pi-field-acceptance.sh"

usage() {
  printf '%s\n' "usage: $0 [--demo-asset PATH] RUN_DIR"
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

json_true() {
  node -e 'const fs=require("node:fs"); const data=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); process.exit(data.ok === true ? 0 : 1);' "$1"
}

json_string_field() {
  node -e 'const fs=require("node:fs"); const data=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const value=data[process.argv[2]]; process.exit(typeof value === "string" && value.length > 0 ? 0 : 1);' "$1" "$2"
}

cast_header_ok() {
  node -e 'const fs=require("node:fs"); const first=fs.readFileSync(process.argv[1],"utf8").split(/\n/u)[0]; const data=JSON.parse(first); process.exit(Number.isInteger(data.version) ? 0 : 1);' "$1"
}

cast_contains() {
  grep -q "$2" "$1"
}

json_has_citation() {
  node -e 'const fs=require("node:fs"); const data=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); process.exit(data.no_answer == null && Array.isArray(data.citations) && data.citations.length > 0 ? 0 : 1);' "$1"
}

timing_ok() {
  awk -F= '
    /^duration_seconds=/ { duration=$2 }
    /^max_seconds=/ { max=$2 }
    END { exit !(duration != "" && max != "" && duration <= max) }
  ' "$1"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --demo-asset)
      [ $# -ge 2 ] || fail "--demo-asset requires a value"
      demo_asset="$2"
      shift 2
      ;;
    *)
      [ -z "$run_dir" ] || fail "unexpected argument: $1"
      run_dir="$1"
      shift
      ;;
  esac
done

[ -n "$run_dir" ] || fail "requires RUN_DIR"
[ -d "$run_dir" ] || fail "run dir missing: $run_dir"
need awk
need node
[ -x "$field_verifier" ] || fail "field verifier missing: $field_verifier"

for file in \
  scope.json \
  approval-request.json \
  approval.json \
  scan.log \
  normalize.json \
  index.json \
  ask.json \
  assembly.json \
  field-acceptance.log \
  fetch.json \
  verification.json \
  timing.txt \
  field-verification.log
do
  [ -s "$run_dir/$file" ] || fail "missing or empty $run_dir/$file"
done

json_true "$run_dir/scope.json" || fail "scope.json is not ok"
json_string_field "$run_dir/approval-request.json" token || fail "approval-request.json missing approval token"
json_true "$run_dir/approval.json" || fail "approval.json is not ok"
json_true "$run_dir/normalize.json" || fail "normalize.json is not ok"
json_true "$run_dir/index.json" || fail "index.json is not ok"
json_has_citation "$run_dir/ask.json" || fail "ask.json has no citation"
json_true "$run_dir/assembly.json" || fail "assembly.json is not ok"
json_true "$run_dir/fetch.json" || fail "fetch.json is not ok"
json_string_field "$run_dir/fetch.json" bundleDir || fail "fetch.json missing bundleDir"
json_true "$run_dir/verification.json" || fail "verification.json is not ok"
timing_ok "$run_dir/timing.txt" || fail "field walkthrough timing exceeded max or is incomplete"

[ -d "$run_dir/fetched-bundle" ] || fail "fetched bundle missing"
[ -s "$run_dir/fetched-bundle/manifest.json" ] || fail "fetched bundle manifest missing"
[ -s "$run_dir/fetched-bundle/manifest.sig" ] || fail "fetched bundle signature missing"
[ -s "$run_dir/fetched-bundle/manifest.pub.json" ] || fail "fetched bundle public key missing"

"$field_verifier" "$run_dir/field-acceptance" >/dev/null
grep -q '^OK field acceptance artifact verified:' "$run_dir/field-verification.log" || fail "field verification log missing OK line"

[ -s "$demo_asset" ] || fail "demo asset missing or empty: $demo_asset"
cast_header_ok "$demo_asset" || fail "demo asset is not an asciinema cast: $demo_asset"
for cast_line in \
  'Kelp Pi demo: declaring fixture scope' \
  'Kelp Pi demo: running scoped Nuclei scan' \
  'Kelp Pi demo: asking cited local retrieval' \
  'Kelp Pi demo: exporting reviewer bundle' \
  'Kelp Pi demo: verifying launch evidence'
do
  cast_contains "$demo_asset" "$cast_line" || fail "demo asset missing cast proof: $cast_line"
done

pass "Pi launch evidence verified: $run_dir"
