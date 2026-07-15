#!/usr/bin/env bash
set -euo pipefail

out="${1:-artifacts/macos-scenario-gate}"
umask 077
mkdir -p "$out"
chmod 0700 "$out"
host_os="$(go env GOOS)"
cat >"$out/matrix.json" <<'EOF'
{"schema":"onibi.release-scenario-matrix.v1","platform":"macos","release_blocking":true,"transports":["lan","tailscale","tailscale-private","wireguard","zerotier","cloudflare-quick","cloudflare-named","ngrok"],"iphone_evidence":{"required_for_certification":true,"runner":"scripts/iphone-transport-smoke.sh","checks":["setup_health","pairing","approval","intervention","reconnect","teardown","failure_diagnostics"]},"scenarios":[{"id":"macos-platform"},{"id":"multi-host-enrollment"},{"id":"web-transports"},{"id":"session-recovery"},{"id":"approvals"},{"id":"intervention"},{"id":"iphone-evidence-contract"}]}
EOF
printf '{"schema":"onibi.macos-scenario-gate.v1","host_os":"%s","matrix":"matrix.json"}\n' "$host_os" >"$out/metadata.json"

status=0
checks=()
run() {
  local id="$1"
  shift
  {
    printf '\n[%s] $' "$id"
    printf ' %q' "$@"
    printf '\n'
  } >>"$out/test.log"
  if "$@" >>"$out/test.log" 2>&1; then
    checks+=("{\"id\":\"$id\",\"result\":\"pass\",\"log\":\"test.log\"}")
    return
  fi
  status=1
  checks+=("{\"id\":\"$id\",\"result\":\"fail\",\"log\":\"test.log\"}")
}

if [[ "$host_os" != "darwin" ]]; then
  printf 'macOS scenario gate requires GOOS=darwin, got %s\n' "$host_os" >"$out/test.log"
  status=2
  checks+=("{\"id\":\"macos-platform\",\"result\":\"fail\",\"log\":\"test.log\"}")
else
  run macos-platform scripts/macos-release-gate.sh "$out/macos-release"
  run multi-host-enrollment go test -race -count=1 ./internal/cli ./internal/fleetnode ./internal/web ./internal/daemon -run '^(TestFleetEnrollRelayUsesOwnerAuthorizedHubFlow|TestEnrollmentPersistsVerifiedFleetLink|TestFleetEnrollmentSupportsIndependentHosts|TestFleetLinkAuthenticatesHeartbeatAndDeliversControl|TestFleetLinkReconnectsAndVerifiesHubControls)$'
  run web-transports go test -race -count=1 ./internal/web/transport -run '^(TestTransportConformance|TestLifecycleCoversStaticLANWithoutProviderBypass)$'
  run session-recovery go test -race -count=1 ./internal/daemon ./internal/web -run '^(TestRestoreSessionsRecoversTmuxWithoutDuplicates|TestRestoreSessionsOrphansMissingTmuxWithoutCancellingApproval|TestRelaySessionReattachAfterTunnelReconnect|TestSessionsStatusWSEventReplay)$'
  run approvals go test -race -count=1 ./internal/daemon ./internal/web -run '^(TestClaudeApprovalDenyIsEnforcingAndAudited|TestCodexApprovalDenyIsEnforcingAndAudited|TestPiApprovalDenyIsEnforcingAndAudited|TestWSEventsStreamsApprovalRequestAndDecision|TestApprovalPostDecidesQueue)$'
  run intervention go test -race -count=1 ./internal/web ./internal/daemon -run '^(TestControlCommandRetryExecutesOnceAndReturnsStatus|TestControlCommandsCoverInputHandoverAndKill|TestFleetControlDuplicateDoesNotReplay)$'
  run iphone-evidence-contract go test -race -count=1 ./internal/e2e -run '^TestIPhoneTransportSmokeRedactsPairURL$'
fi

if ((status == 0)); then
  result="pass"
else
  result="fail"
fi
checks_json="$(IFS=,; printf '%s' "${checks[*]}")"
printf '{"schema":"onibi.macos-scenario-gate.v1","result":"%s","exit_code":%d,"log":"test.log","matrix":"matrix.json","checks":[%s]}\n' "$result" "$status" "$checks_json" >"$out/summary.json"
cat "$out/test.log"
exit "$status"
