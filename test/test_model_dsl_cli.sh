#!/bin/sh
set -eu

executable=${1:?missing zenbu-headless executable}
valid=${2:?missing valid DSL fixture}
invalid=${3:?missing invalid DSL fixture}
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT

"$executable" model-check "$valid" >"$workspace/check.out"
grep -F "model-check: ok: $valid" "$workspace/check.out" >/dev/null

if "$executable" model-check "$invalid" >"$workspace/invalid.out" 2>&1; then
  echo "invalid model-check unexpectedly succeeded" >&2
  exit 1
fi
grep -F "$invalid:9:15: error: unknown state \`missing\`" "$workspace/invalid.out" >/dev/null

"$executable" model-describe "$valid" >"$workspace/describe-one.out"
"$executable" model-describe "$valid" >"$workspace/describe-two.out"
cmp "$workspace/describe-one.out" "$workspace/describe-two.out"
grep -F "prefix: d" "$workspace/describe-one.out" >/dev/null
grep -F "effect: apply selector \"current-word\" transform \"delete\"" "$workspace/describe-one.out" >/dev/null
