# Chain Of Custody Manifests

`piranesi sign --workspace ./workspace` creates a deterministic local SHA-256 manifest under
`workspace/signatures/manifest-<manifest-id>.json`.

The Phase 1 manifest covers:

- `workspace.json`;
- `normalized/findings.json`;
- `audit-log.jsonl`;
- copied raw inputs under `raw/`;
- report artifacts under `reports/`;
- imported tool metadata such as version and command arguments when available;
- workspace and findings schema versions.

The manifest also stores an audit-chain head. Each audit-log JSONL event is canonicalized and
hashed with the previous event hash, starting from 64 zeroes. Removing, reordering, or editing
events changes the chain head and causes verification to fail.

When a workspace contains evidence produced by `piranesi rescan`, the manifest also includes
optional replay provenance entries. Each entry records the replay spec digest, recovered command,
normalized environment allowlist, intended target scope, input evidence digests, digest-pinned
image reference and image digest, network policy, and output evidence digest. Older manifests that
do not include replay provenance remain readable and verifiable.

Verify a workspace with:

```bash
piranesi sign --workspace ./workspace --verify
```

## Trust Limits

This is an artifact integrity manifest, not an identity signature. It does not provide RFC3161
trusted timestamps, Sigstore/keyless identity, public-key signatures, or auditor attestation.
Those are follow-up trust-upgrade candidates once the base manifest format is stable.
