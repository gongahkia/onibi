# Kelp Pi Threat Model

## Scope

Kelp Pi is a Raspberry Pi 5 field data plane for scoped AppSec triage. Current code
implements the Rust agent foundation, signed wire primitives, local policy checks,
loopback `/ask`, deterministic retrieval fixtures, selfcheck posture checks,
hash-chained audit logs with signed rotation manifests, signed firmware-update
staging, and destructive data-dir wipe. Scanner execution, Pi-produced audit bundles,
AP/DNS/nftables hardening, quota enforcement, thermal scan refusal, and control-plane
bundle sync remain TODOs in [`pi-todo.md`](./pi-todo.md).

Kelp Pi does not claim tamper-resistant hardware custody, covert operation, exploit
execution, internet scanning, or scanning outside an operator-declared scope.

## Current Enforced Controls

- `kelp-pi-agent wire --stdio` verifies signed control-plane envelopes before handling
  `selfcheck.run`; malformed JSON, wrong sender, bad signatures, and unsupported kinds
  are refused.
- `policy.push` accepts only trusted control-plane signatures, verifies the embedded
  policy hash, and persists the accepted pack under `policy/current-policy.json`.
- `policy pull` emits signed current-pack requests; local CP sync rotates signed
  packs through `wire --stdio`, and `start` can poll signed policy-push files at a
  configured interval. Accepted pulls/pushes are logged.
- Signed Pi-originated envelopes can be queued in `outbox/queued` while disconnected
  and replayed in sequence order after reconnect; replayed envelopes are archived.
- Local policy evaluation covers scanner invocation, file operation, outbound network,
  and synthesis gates, and writes `policy-decision` audit events when called through
  the audited paths.
- `selfcheck --target` and signed `selfcheck.run` refuse targets outside loopback, the
  configured AP CIDR, or the configured allowlist; refusal is logged.
- `serve-ask` binds to loopback by default; non-loopback requires
  `--allow-non-loopback`.
- Retrieval returns `no_answer` below threshold. Optional synthesis is gated by an
  audited policy check and every generated sentence must cite retrieved chunks.
- Binary and executable ingest inputs are refused by `validate_ingest_source` and
  logged when the audited ingest validator is used.
- Audit entries are hash-chained. `verify-audit-log --data-dir` verifies rotated
  segment manifests, signatures, and the active log chain.
- `firmware-update` stages a bundle only after the manifest signature, signer key ID,
  relative payload path, and payload hash verify. Unsigned or wrong-key bundles are
  refused and logged.
- `wipe --force` zeros regular files before deleting the data dir; the agent then
  refuses to start until the data-dir layout is recreated.

## Physical Capture

Current `keygen` stores the Pi Ed25519 private key as a local JSON file with `0600`
permissions under the configured key directory. At-rest passphrase encryption is not
implemented yet, so powered-off capture of that file is a key-compromise event unless
the operator protects the storage layer externally. Powered-on capture of an unlocked
agent is also a key-compromise event: the attacker may sign envelopes until the
operator revokes the key or the process stops. The control plane must revoke the Pi
key ID after suspected capture and distrust envelopes after the last
operator-confirmed good timestamp.

## Hostile LAN

The Pi assumes the local network may be monitored or hostile. The implemented remote
control path is SSH-tunneled stdio with Kelp Pi Ed25519 envelope signatures at the
protocol layer. The agent does not expose a custom TCP control port. AP client
isolation, outbound nftables allowlisting, and captive-portal DNS sinkholing are
planned controls and are not enforced by current code.

## Malicious Corpus

Corpus files, scanner sidecars, and imported prior bundles are untrusted input.
Current code refuses binary and executable ingest inputs, derives chunk IDs from
canonical path plus content hash, avoids executing corpus content, and supports
PDF-derived text only through sidecar text. Upload/corpus/index quota enforcement is
planned and not enforced yet.

## Audit-Log Tamper Attempts

Current audited events include data-dir preflight, daemon start/stop, panic, local
policy decisions, approval requests, selfcheck target refusals, binary ingest refusals,
firmware update staging/refusal, and signed audit-log rotation. Verification fails on
modified, deleted, reordered, or inserted active-log entries and on tampered segment
files or manifests. Scanner invocation, evidence append, `/ask` query, bundle export,
and Pi-produced bundle verification are planned audit surfaces and are not fully
implemented yet.
