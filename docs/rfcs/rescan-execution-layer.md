# RFC: Rescan Execution Layer

Status: accepted design

Issue: #94

## Problem

Piranesi is import-only by default. `rescan` adds an opt-in replay path for scans
that were already run manually and ingested into a workspace. It must not reposition
Piranesi as a scanner engine, scheduler, exploitation tool, or C2 system.

## Decision

`piranesi rescan --from-baseline <workspace>` is disabled unless the user explicitly
invokes it and the optional container runtime support is available. Replay is allowed
only when Piranesi can recover a supported tool invocation from baseline evidence and
can enforce the same tool, flags, target scope, and output evidence shape.

The default install and all import/report/retest/sign/serve workflows remain usable
without Docker or Docker Python dependencies.

## Boundary

Rescan is a replay layer over existing evidence, not a scan-planning layer.

Allowed:

- recover a supported baseline command from raw evidence;
- run the same supported tool in a locked-down container;
- write replay outputs in the same shape existing `ingest` commands already consume;
- record provenance for every replay input and output;
- feed replay outputs into `retest` when the user asks for that flow.

Not allowed:

- autonomous target selection;
- scheduling or background scanning;
- exploitation, payload generation, or implant/session control;
- AI-driven decisioning about what to run;
- new network egress outside the original ingested scope;
- guessing missing flags or expanding scope when evidence is ambiguous.

## Replay Contract

Each replay attempt produces a replay spec before execution:

```json
{
  "schema_version": "piranesi.replay-spec.v1",
  "tool": "nmap",
  "recovered_command": ["nmap", "-sV", "-oX", "-", "127.0.0.1"],
  "target_scope": ["127.0.0.1"],
  "input_evidence": [
    {
      "path": "raw/nmap/example.xml",
      "sha256": "..."
    }
  ],
  "confidence": "high",
  "unsupported_reason": null
}
```

Execution requires:

- a supported tool extractor;
- a non-empty recovered command;
- explicit target scope recovered from evidence;
- a digest-pinned container image accepted by policy;
- optional runtime dependencies installed;
- a derived network policy that records recovered scope and fails closed unless
  unsupported Docker egress allowlisting is explicitly acknowledged.

## Provenance Contract

Replay provenance extends chain-of-custody rather than replacing it. New manifest
fields should include:

- baseline workspace identifier and input evidence digests;
- replay spec digest;
- recovered command and arguments;
- normalized environment allowlist;
- container image reference and immutable digest;
- runtime name/version and enforcement mode;
- intended target scope and network policy;
- output evidence paths and SHA-256 digests;
- error/failure reason when replay is refused.

Existing signed manifests remain readable. Replay provenance is optional for
import-only workspaces and required only for evidence created by `rescan`.

## Image Trust Policy

The high-level image policy is fail closed:

- accept digest-pinned images only;
- reject mutable tags such as `latest`;
- reject tag-only references unless they also include a digest;
- record the accepted image digest separately from the user-facing image reference;
- do not auto-select, pull from arbitrary registries silently, or imply trust in a
  registry beyond the pinned digest.

## Fail-Closed Cases

`rescan` refuses execution when:

- optional runtime support is not installed;
- no supported extractor exists for the baseline evidence;
- evidence is malformed, incomplete, or ambiguous;
- target scope cannot be recovered;
- requested target scope would exceed the baseline scope;
- image reference is mutable or unpinned;
- network egress policy cannot be enforced or explicitly documented;
- recovered command targets exceed the baseline-derived target scope;
- output path would escape the workspace;
- runtime errors leave output evidence incomplete.

Failures should be actionable and should not mutate normalized findings unless valid
output evidence exists.

## Compatibility

- `ingest` remains import-only and keeps its current behavior.
- `report` renders from workspace artifacts and does not need Docker.
- `retest` continues comparing two workspaces; a later opt-in mode may call `rescan`
  first to build a current workspace.
- `sign` includes replay provenance when present and remains compatible with older
  manifests.
- `serve` reads workspace data only and never triggers replay.

## Follow-Up Work

- #103: optional dependency/runtime wiring.
- #97: baseline replay extractors for `nmap` and `nuclei` (documented in
  [rescan replay extractors](../rescan-extractors.md)).
- #98: concrete image pinning and verification policy (documented in
  [rescan image policy](../rescan-image-policy.md)).
- #95: user-facing `piranesi rescan --from-baseline <workspace>` (documented in
  [rescan CLI](../rescan-cli.md)).
- #99: enforced network egress policy for replay containers.
