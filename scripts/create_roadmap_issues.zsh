#!/usr/bin/env zsh
set -euo pipefail

repo="gongahkia/yeokcham"
count=0
previous_gate=""
typeset -A stage_gates
existing_issues='[]'

if [[ "${DRY_RUN:-0}" != "1" ]]; then
  existing_issues="$(gh issue list --repo "$repo" --state all --limit 250 --json number,title)"
fi

ensure_label() {
  [[ "${DRY_RUN:-0}" == "1" ]] && return
  gh label create "$1" --repo "$repo" --color "$2" --description "$3" --force >/dev/null
}

ensure_milestone() {
  local title="$1"
  [[ "${DRY_RUN:-0}" == "1" ]] && return
  if ! gh api "repos/$repo/milestones?state=all&per_page=100" --paginate --jq ".[] | select(.title == \"$title\") | .number" | grep -q .; then
    gh api --method POST "repos/$repo/milestones" -f title="$title" >/dev/null
  fi
}

create_stage() {
  local milestone="$1"
  local labels="$2"
  shift 2
  local gate=""
  local title dependency body url number label
  for title in "$@"; do
    dependency="$gate"
    [[ -z "$dependency" ]] && dependency="$previous_gate"
    body="## Scope
Implement: $title.

## Acceptance
- Add focused automated coverage for the changed behavior.
- Preserve protocol/profile security constraints and fail closed on invalid input.
- cargo test --workspace passes for the affected crate(s)."
    [[ -n "$dependency" ]] && body+="

## Dependency
Blocked by #$dependency."
    local -a label_args
    label_args=()
    for label in ${(s:,:)labels}; do
      label_args+=(--label "$label")
    done
    if [[ "${DRY_RUN:-0}" == "1" ]]; then
      number="dry-$((count + 1))"
    elif number="$(jq -r --arg title "$title" '.[] | select(.title == $title) | .number' <<<"$existing_issues" | head -n1)"; [[ -n "$number" ]]; then
      :
    else
      url=$(gh issue create --repo "$repo" --milestone "$milestone" "${label_args[@]}" --title "$title" --body "$body")
      number="${url##*/}"
      existing_issues="$(jq --arg title "$title" --argjson number "$number" '. + [{title: $title, number: $number}]' <<<"$existing_issues")"
    fi
    [[ -z "$gate" ]] && gate="$number"
    ((++count))
    print -r -- "#$number $title"
  done
  stage_gates[$milestone]="$gate"
  previous_gate="$gate"
}

ensure_label "kind:foundation" "1d76db" "workspace, build, and engineering tooling"
ensure_label "kind:protocol" "5319e7" "wire protocol and policy"
ensure_label "kind:security" "b60205" "cryptographic or security-sensitive work"
ensure_label "kind:transport" "0e8a16" "network transport work"
ensure_label "kind:relay" "006b75" "maildrop relay work"
ensure_label "kind:storage" "fbca04" "durable state and blob storage"
ensure_label "kind:ffi" "d93f0b" "C ABI and integration boundary"
ensure_label "kind:client" "c5def5" "CLI and TUI client"
ensure_label "kind:mesh" "bfd4f2" "local mesh transport"
ensure_label "kind:testing" "ededed" "testing, fuzzing, and hardening"
ensure_label "risk:high" "b60205" "requires security-focused review"
ensure_label "risk:cross-platform" "0052cc" "must work on macOS, Linux, and Windows"

for milestone in \
  "M0 Foundation" \
  "M1 Wire Protocol" \
  "M2 Identity and Sessions" \
  "M3 Direct Transport" \
  "M4 Tor Maildrop Relay" \
  "M5 Delivery and Files" \
  "M6 Daemon, ABI, and TUI" \
  "M7 Local Mesh" \
  "M8 Hardening and Release"; do
  ensure_milestone "$milestone"
done

create_stage "M0 Foundation" "kind:foundation,risk:cross-platform" \
  "Configure Rust workspace and MSRV" \
  "Pin reproducible Rust toolchain and Cargo lock policy" \
  "Create crate layout for core protocol relay daemon ABI and CLI" \
  "Define workspace error taxonomy" \
  "Add structured logging with secret redaction" \
  "Define Cargo feature-flag governance" \
  "Enforce formatting and Clippy warnings in CI" \
  "Add macOS Linux and Windows CI build matrix" \
  "Add dependency vulnerability audit in CI" \
  "Add dependency license policy verification" \
  "Emit reproducible release build metadata" \
  "Create deterministic test fixture utilities" \
  "Add criterion benchmark harness" \
  "Add test-secret isolation utilities"

create_stage "M1 Wire Protocol" "kind:protocol,risk:high" \
  "Define protocol version value type" \
  "Implement deterministic CBOR encoder" \
  "Enforce CBOR decoder size and depth limits" \
  "Create cryptographic domain-separation registry" \
  "Define encrypted message envelope schema" \
  "Define message payload schema" \
  "Define versioned delivery-profile schema" \
  "Define mailbox capability token schema" \
  "Implement canonical signing-input construction" \
  "Define extension-frame schema" \
  "Implement protocol version-negotiation handshake" \
  "Define unsupported-version failure behavior" \
  "Define stable protocol error code space" \
  "Enforce wire-frame length boundaries" \
  "Define unknown-field compatibility behavior" \
  "Implement delivery-profile constraint validator" \
  "Expose typed profile-policy decision API" \
  "Encode direct-profile configuration" \
  "Encode Tor-maildrop-profile configuration" \
  "Encode local-mesh-profile configuration" \
  "Implement recipient capability encoding" \
  "Implement encrypted-header encoding" \
  "Generate protocol golden vectors" \
  "Build protocol interoperability harness"

create_stage "M2 Identity and Sessions" "kind:security,risk:high" \
  "Generate Ed25519 identity keys" \
  "Derive self-certifying identity identifiers" \
  "Implement identity serialization format" \
  "Define OS-keystore abstraction" \
  "Implement macOS keystore backend" \
  "Implement Windows keystore backend" \
  "Implement Linux keystore backend" \
  "Encrypt durable session state database" \
  "Implement encrypted identity export and import" \
  "Create signed contact invitations" \
  "Validate signed contact invitations" \
  "Compute safety-number fingerprints" \
  "Encode and decode QR verification payloads" \
  "Persist verified-contact state" \
  "Create and validate signed relay invitations" \
  "Generate X25519 prekeys" \
  "Rotate signed prekeys" \
  "Store one-time prekeys safely" \
  "Publish and parse prekey bundles" \
  "Implement X3DH session establishment" \
  "Implement durable Double Ratchet state" \
  "Handle skipped-message keys and replay detection" \
  "Implement identity key rotation and contact revocation"

create_stage "M3 Direct Transport" "kind:transport,risk:cross-platform" \
  "Implement direct QUIC transport adapter" \
  "Authenticate direct transport peer identity" \
  "Bridge QUIC streams to protocol frames" \
  "Validate advertised peer endpoints" \
  "Add UPnP port-mapping support" \
  "Add NAT-PMP port-mapping support" \
  "Support optional user-supplied STUN servers" \
  "Add LAN mDNS peer discovery" \
  "Implement bounded direct connection attempts" \
  "Expose direct-profile IP-disclosure warning" \
  "Implement direct retry and backoff policy" \
  "Require explicit direct-profile selection" \
  "Reject silent direct-to-Tor profile downgrade" \
  "Handle direct connection migration and reconnect" \
  "Enforce direct connection resource limits" \
  "Implement direct transport frame multiplexing" \
  "Add direct two-node end-to-end integration test" \
  "Add direct transport network-fault tests"

create_stage "M4 Tor Maildrop Relay" "kind:relay,risk:high" \
  "Implement external Tor SOCKS connector" \
  "Validate onion-service relay endpoints" \
  "Implement relay TLS endpoint pinning" \
  "Create opaque mailbox capabilities" \
  "Validate mailbox capabilities at relay ingress" \
  "Enforce recipient mailbox quotas" \
  "Enforce relay retention TTL policy" \
  "Create SQLite relay schema migrations" \
  "Implement durable relay envelope insertion" \
  "Implement mailbox envelope retrieval" \
  "Implement acknowledgement-driven relay deletion" \
  "Issue signed relay storage receipts" \
  "Verify relay storage receipts in clients" \
  "Manage relay identity keys" \
  "Implement maildrop replication scheduler" \
  "Select explicit relay replicas by profile" \
  "Retry failed replica writes safely" \
  "Validate self-hosted relay configuration" \
  "Expose relay health administration endpoint" \
  "Emit redacted relay operational metrics" \
  "Reject non-synthetic traffic on project test relay" \
  "Garbage-collect expired relay mailboxes" \
  "Rate-limit relay ingress by capability" \
  "Add relay abuse and quota tests" \
  "Add Tor maildrop integration test topology" \
  "Test relay receipt expiry and key rotation"

create_stage "M5 Delivery and Files" "kind:storage,risk:high" \
  "Implement durable sender outbox queue" \
  "Implement durable recipient inbox deduplication" \
  "Generate idempotent message identifiers" \
  "Create recipient signed delivery acknowledgements" \
  "Verify recipient delivery acknowledgements" \
  "Expose unknown delivered and expired message states" \
  "Enforce sender-selected message expiry bounds" \
  "Implement background delivery scheduler" \
  "Derive attachment encryption keys" \
  "Encrypt fixed-size attachment chunks" \
  "Validate attachment chunk hashes" \
  "Implement resumable attachment upload" \
  "Implement resumable attachment download" \
  "Define encrypted attachment manifest" \
  "Enforce configurable 100 MiB reference attachment cap" \
  "Account attachment storage against relay quota" \
  "Garbage-collect expired attachment chunks" \
  "Make attachment retries idempotent" \
  "Detect and quarantine corrupted relay blobs" \
  "Add delivery and attachment end-to-end tests"

create_stage "M6 Daemon, ABI, and TUI" "kind:ffi,risk:cross-platform" \
  "Implement daemon startup shutdown and lock lifecycle" \
  "Implement typed daemon configuration loader" \
  "Validate daemon configuration before network startup" \
  "Publish versioned C ABI header" \
  "Define opaque C ABI handle ownership" \
  "Define stable C ABI error codes" \
  "Implement C ABI allocation and release functions" \
  "Implement C ABI asynchronous completion callbacks" \
  "Implement C ABI version negotiation" \
  "Document and enforce C ABI thread-safety rules" \
  "Zeroize C ABI secret buffers" \
  "Build a C consumer ABI conformance test" \
  "Implement CLI identity commands" \
  "Implement CLI contact invitation commands" \
  "Implement CLI relay-profile commands" \
  "Implement CLI message and attachment send commands" \
  "Implement TUI inbox outbox and delivery-state views"

create_stage "M7 Local Mesh" "kind:mesh,risk:cross-platform" \
  "Define common transport abstraction" \
  "Build in-memory transport conformance harness" \
  "Implement LAN direct transport adapter" \
  "Implement local Wi-Fi hotspot transport adapter" \
  "Implement Wi-Fi Direct transport adapter" \
  "Define Bluetooth transport abstraction" \
  "Implement macOS Bluetooth transport backend" \
  "Implement Windows Bluetooth transport backend" \
  "Implement Linux Bluetooth transport backend" \
  "Handle local transport permission and availability states" \
  "Create proximity contact-invitation exchange" \
  "Validate local-mesh profile policy" \
  "Require explicit local transport selection" \
  "Implement local transport reconnect behavior" \
  "Add LAN Wi-Fi Direct end-to-end test" \
  "Add Bluetooth end-to-end test harness" \
  "Benchmark local transport reliability and throughput"

create_stage "M8 Hardening and Release" "kind:testing,risk:high" \
  "Fuzz deterministic CBOR parser" \
  "Fuzz encrypted envelope parser" \
  "Fuzz delivery-profile parser" \
  "Fuzz relay ingress API" \
  "Fuzz attachment manifest and chunk parser" \
  "Fuzz C ABI invalid-handle behavior" \
  "Property-test Double Ratchet state transitions" \
  "Property-test mailbox capability authorization" \
  "Model-test delivery state machine" \
  "Run Miri over core secret-state tests" \
  "Pin and audit cryptographic dependency graph" \
  "Verify no secrets enter logs or metrics" \
  "Inject direct transport partition and retry faults" \
  "Exercise Tor maildrop integration in CI" \
  "Test clock-skew handling for expiry and receipts" \
  "Inject SQLite storage faults and recovery" \
  "Stress concurrent delivery and deduplication" \
  "Run end-to-end tests on all supported platforms" \
  "Benchmark protocol throughput latency and memory ceilings" \
  "Verify reproducible release artifacts" \
  "Sign and verify release artifact manifest" \
  "Add regression vectors for security fixes" \
  "Verify synthetic-only test-relay admission policy" \
  "Build two-node demo topology integration test"

if (( count != 183 )); then
  print -u2 -- "expected 183 issues, created $count"
  exit 1
fi

print -r -- "created $count roadmap issues"
