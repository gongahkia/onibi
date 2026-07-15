#!/usr/bin/env bash
set -euo pipefail

out="${1:-artifacts/fleet-budget-smoke}"
umask 077
mkdir -p "$out"
chmod 0700 "$out"
printf '{"schema":"onibi.fleet-budget-smoke.v1","command":"go test -race -count=1 ./internal/budget ./internal/adapters ./internal/daemon ./internal/web"}\n' >"$out/metadata.json"

if go test -race -count=1 ./internal/budget ./internal/adapters ./internal/daemon ./internal/web >"$out/test.log" 2>&1; then
  status=0
else
  status=$?
fi
if ((status == 0)); then
  result="pass"
else
  result="fail"
fi
printf '{"schema":"onibi.fleet-budget-smoke.v1","result":"%s","exit_code":%d,"log":"test.log"}\n' "$result" "$status" >"$out/summary.json"
cat "$out/test.log"
exit "$status"
