# Kelp Pi Threat Model

## Scope

Kelp Pi is a Raspberry Pi 5 local AppSec triage runtime. The active pivot target is a single Zig `kelp-pi` binary with local model inference, policy-gated tool use, offline evidence retrieval, and signed bundles.

Current Zig scaffold is not a hardened release. Treat it as a contract testbed.

## Current Enforced Controls

- `policy check` parses `policies/appsec-agent-baseline.toml`.
- Policy precedence is fail-closed by severity: deny, require approval, log-only, allow.
- `keygen` creates an Ed25519 keypair JSON under the data directory.
- `scope set` writes the active scope file.
- `scan` refuses targets outside active scope.
- `scan` requires an approval token for active scanner commands matched by policy.
- `index ingest` refuses inputs containing NUL bytes and writes content hashes.
- `model warm` checks model manifest membership before any load attempt.
- `verify-bundle` requires manifest, result, and static HTML files.

## Not Yet Enforced

- GGUF hash verification.
- `llama.cpp` load and prompt execution.
- SQLite FTS5 retrieval.
- Actual scanner execution.
- systemd/nftables scanner sandboxing.
- Append-only signed transcript.
- Hash-chained audit log.
- Bundle Ed25519 signature.
- Private-key file mode hardening.
- At-rest key encryption.
- Real Pi thermal/storage gates.

## Threats

### Physical Capture

Private-key compromise is a device compromise. Current scaffold writes a local key JSON and does not harden permissions beyond process defaults. Release target must set restrictive file mode, support revocation, and document operator storage protection.

### Malicious Scanner Output

Scanner output and imported evidence are untrusted. Prompt-injection content inside scanner output must not bypass policy. Policy gates run before tool dispatch regardless of model output.

### Hostile LAN

The Pi should assume the local LAN is monitored or hostile. Release target uses SSH/direct TTY for operator access and should avoid exposing a custom unauthenticated TCP control port.

### Scope Breakout

Active scanners must only reach declared targets. Current scaffold does string-based scope checks only. Release target needs OS-level enforcement around scanner network access.

### Audit Tamper

Current minimal bundle verification is not tamper evidence. Release target needs hash-chained logs and signed manifests covering transcript, evidence, policy decisions, and generated findings.

## Refusal Boundary

Default policy denies exploit execution, destructive commands, secret exfiltration, persistence, and lateral movement. Active scanning requires approval. No auto-approve-all mode.
