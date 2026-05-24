# Trivy Import Gate

Date: 2026-05-20

Status: parked behind real JSON evidence and report-value proof.

Trivy filesystem and container JSON can be useful when an engagement report needs
dependency, package, image, IaC, or secret-detection evidence from an operator-run
tool. Piranesi should import those exports only when they improve a concrete
local report or handoff workflow. It should not become a Trivy runner,
compliance dashboard, or container security platform.

## Accepted Evidence

An implementation issue may start only when the intake record includes:

- real `trivy fs`, `trivy image`, `trivy config`, or `trivy rootfs` JSON output
  from an authorized lab or engagement;
- Trivy version, database timestamp if available, target type, and command flags;
- target authorization and sanitization notes;
- fixture digest and secret-scan confirmation;
- proof that the imported findings improve a red-team, pentest, or assessment
  report that cannot be handled by attaching the export as raw evidence only.

## Mapping Expectations

The first adapter should preserve raw JSON and normalize conservative findings:

- CVE/package evidence with installed version, fixed version, package type, and
  primary reference when available;
- IaC misconfiguration evidence only when Trivy supplies stable identifiers and
  source locations;
- secret findings only as redacted evidence unless a reportable risk can be
  stated without exposing the secret;
- warnings for unsupported result classes, missing package metadata, or dropped
  vulnerability fields.

## Report-Value Gate

Before implementation, the GitHub issue must explain why normalized import is
better than preserving the Trivy JSON as a raw evidence artifact. Acceptable
reasons include deduplicated report findings, retest comparison, PFF export, or
client handoff summaries that need stable finding IDs.

## Out of Scope

- Running Trivy, pulling images, or accessing registries.
- Reintroducing pre-pivot host or container posture workflows.
- Compliance scoring, policy enforcement, admission control, or fleet inventory.
- Claiming coverage from synthetic or hand-authored Trivy-like JSON.
