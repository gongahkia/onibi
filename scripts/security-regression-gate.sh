#!/usr/bin/env bash
set -euo pipefail

out="${1:-artifacts/security-regression-gate}"
fuzz_time="${ONIBI_SECURITY_FUZZ_TIME:-10s}"
umask 077
mkdir -p "$out"
chmod 0700 "$out"
printf '{"schema":"onibi.security-regression-gate.v1","fuzz_time":"%s"}\n' "$fuzz_time" >"$out/metadata.json"

status=0
run() {
  {
    printf '\n$'
    printf ' %q' "$@"
    printf '\n'
  } >>"$out/test.log"
  if "$@" >>"$out/test.log" 2>&1; then
    return
  fi
  status=1
}

run go test -race -count=1 ./internal/approval ./internal/envelope ./internal/web ./internal/store
run go test -parallel=1 -run=^$ -fuzz=FuzzDecide -fuzztime="$fuzz_time" ./internal/approval
run go test -parallel=1 -run=^$ -fuzz=FuzzCodecOpen -fuzztime="$fuzz_time" ./internal/envelope
run go test -parallel=1 -run=^$ -fuzz=FuzzOpenRelayFrame -fuzztime="$fuzz_time" ./internal/envelope
run go test -parallel=1 -run=^$ -fuzz=FuzzSequencedWSDecrypt -fuzztime="$fuzz_time" ./internal/web
run go test -parallel=1 -run=^$ -fuzz=FuzzPairConfirmRejectsAdversarialFrames -fuzztime="$fuzz_time" ./internal/web
run go test -parallel=1 -run=^$ -fuzz=FuzzCryptBoxOpen -fuzztime="$fuzz_time" ./internal/store

if ((status == 0)); then
  result="pass"
else
  result="fail"
fi
printf '{"schema":"onibi.security-regression-gate.v1","result":"%s","exit_code":%d,"log":"test.log"}\n' "$result" "$status" >"$out/summary.json"
cat "$out/test.log"
exit "$status"
