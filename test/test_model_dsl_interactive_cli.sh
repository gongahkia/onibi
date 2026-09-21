#!/bin/sh
set -eu

executable=${1:?missing zenbu executable}
valid=${2:?missing valid DSL fixture}
invalid=${3:?missing invalid DSL fixture}
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT

missing="$workspace/missing.zenmodel"
if "$executable" --model-dsl "$missing" </dev/null >"$workspace/missing.out" 2>&1; then
  echo "missing --model-dsl path unexpectedly succeeded" >&2
  exit 1
fi
grep -F "cannot read $missing:" "$workspace/missing.out" >/dev/null

if "$executable" --model-dsl "$invalid" </dev/null >"$workspace/invalid.out" 2>&1; then
  echo "invalid --model-dsl grammar unexpectedly succeeded" >&2
  exit 1
fi
grep -F "$invalid:9:15: error: unknown state \`missing\`" "$workspace/invalid.out" >/dev/null

if "$executable" --model vim --model-dsl "$valid" </dev/null >"$workspace/conflict.out" 2>&1; then
  echo "contradictory model selection unexpectedly succeeded" >&2
  exit 1
fi
grep -F "choose only one of --model and --model-dsl" "$workspace/conflict.out" >/dev/null

if "$executable" --model dsl </dev/null >"$workspace/unknown.out" 2>&1; then
  echo "--model dsl unexpectedly succeeded" >&2
  exit 1
fi
grep -F "unknown model: dsl" "$workspace/unknown.out" >/dev/null
