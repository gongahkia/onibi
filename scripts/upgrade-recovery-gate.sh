#!/usr/bin/env bash
set -euo pipefail

out="${1:-artifacts/upgrade-recovery-gate}"
umask 077
mkdir -p "$out"
chmod 0700 "$out"
printf '{"schema":"onibi.upgrade-recovery-gate.v1","command":"go test -race -count=1 ./internal/store ./internal/updatecheck ./internal/doctor ./internal/cli"}\n' >"$out/metadata.json"

if go test -race -count=1 ./internal/store ./internal/updatecheck ./internal/doctor ./internal/cli >"$out/test.log" 2>&1; then
  status=0
else
  status=$?
fi
if ((status == 0)); then
  result="pass"
else
  result="fail"
fi
printf '{"schema":"onibi.upgrade-recovery-gate.v1","result":"%s","exit_code":%d,"log":"test.log"}\n' "$result" "$status" >"$out/summary.json"
cat "$out/test.log"
exit "$status"
