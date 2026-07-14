#!/usr/bin/env bash
set -euo pipefail

out="${1:-artifacts/linux-beta}"
mkdir -p "$out"
chmod 0700 "$out"
host_os="$(go env GOOS)"
printf '{"schema":"onibi.linux-beta-smoke.v1","host_os":"%s","command":"go test -race -count=1 ./internal/doctor ./internal/service"}\n' "$host_os" >"$out/metadata.json"

status=0
if [[ "$host_os" != "linux" ]]; then
  printf 'linux beta smoke requires GOOS=linux, got %s\n' "$host_os" >"$out/test.log"
  status=2
else
  go test -race -count=1 ./internal/doctor ./internal/service >"$out/test.log" 2>&1 || status=$?
fi

if ((status == 0)); then
  result="pass"
else
  result="fail"
fi
printf '{"schema":"onibi.linux-beta-smoke.v1","result":"%s","exit_code":%d,"log":"test.log"}\n' "$result" "$status" >"$out/summary.json"
cat "$out/test.log"
exit "$status"
