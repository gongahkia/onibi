# Kelp Pi Threat Model

## Scope

Kelp Pi is a Raspberry Pi 5 field data plane for scoped AppSec triage. It runs local
scanner jobs, stores evidence, serves offline cited retrieval, and exports signed audit
bundles to the KelpClaw control plane over SSH-tunneled stdio. v1 does not claim
tamper-resistant hardware custody, covert operation, exploit execution, or scanning
outside an operator-declared scope.

## Physical Capture

The Pi private key is an encrypted file under `/var/lib/kelp-pi/keys/`, unlocked by an
operator passphrase at bootstrap or daemon start. Powered-off capture requires the
attacker to recover that passphrase or defeat the at-rest encryption before using the
Pi key. Powered-on capture of an unlocked agent is treated as key compromise: the
attacker may sign envelopes until the process stops or the key is revoked. The control
plane must revoke the Pi key ID after suspected capture and distrust envelopes after
the last operator-confirmed good timestamp.

## Hostile LAN

The Pi assumes the local network may be monitored or hostile. v1 remote control uses
SSH-tunneled stdio, so the agent does not expose a custom TCP control port. Every
protocol envelope is still signed at the Kelp Pi layer; SSH only carries the transport.
The Pi should deny outbound traffic except declared control-plane endpoints, isolate AP
clients, sinkhole captive-portal DNS checks locally, and refuse selfcheck targets
outside loopback, the Pi AP CIDR, or the configured allowlist.

## Malicious Corpus

Corpus files, scanner sidecars, and imported prior bundles are untrusted input. Ingest
must refuse binaries and executable files, cap upload/corpus/index sizes, derive chunk
IDs from canonical path plus content hash, and avoid executing corpus content. PDF
ingest uses external text sidecars only. Retrieval answers must cite stored chunks; if
retrieval score is below threshold, the result is `no_answer`.

## Audit-Log Tamper Attempts

Every scanner invocation, policy decision, evidence append, `/ask` query, and bundle
export is appended to a hash-chained audit log. Each entry includes the previous entry
hash; rotation emits signed segment manifests. Verification must fail at the first
modified, deleted, reordered, or inserted entry. Pi-produced audit bundles include the
relevant audit-log slice, the manifest, signatures, public key metadata, and enough
context for `kelp-claw verify-audit-bundle` to verify the bundle without Pi-specific
flags.
