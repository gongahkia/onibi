# Devpost Submission Draft

Use this as the working copy for the Devpost story. Replace bracketed values
after the CFReDS run and demo video are final.

## Project Name

KelpClaw SIFT Sentinel

## Elevator Pitch

KelpClaw SIFT Sentinel is a constrained autonomous DFIR agent for SIFT
Workstation. It lets an agent triage a real forensic image while keeping the
dangerous parts narrow: typed read-only forensic tools, evidence-backed claim
verification, self-correction, spoliation checks, hostile-evidence detection,
and signed audit logs that trace findings back to tool executions.

## What It Does

KelpClaw runs a Sentinel workflow over forensic case data. The agent can use a
custom read-only MCP server and a SIFT-side triage wrapper to inspect evidence,
but findings are not accepted just because the model says them. Reports are
converted into structured claims, linked to recovered artifacts, checked by
verifier rules, and repaired when unsupported or contradicted.

The output is designed for reviewers:

- `claim-ledger.json` for evidence-backed findings
- `agent-execution.jsonl` for normalized tool events
- `repair-trace.jsonl` for self-correction
- `spoliation-check.json` for evidence integrity
- `attack-navigator-layer.json` for ATT&CK coverage
- Signed `audit-bundle/` for external verification

## How It Addresses Autonomous Execution

Self-correction: Sentinel runs a verify-then-repair loop. Unsupported,
contradicted, or missing-evidence claims are fed back through the repair path
and recorded in `repair-trace.jsonl`.

Accuracy: The benchmark scorer reports precision, recall, F1, and a
hallucination metric. A hallucinated finding is defined as a confirmed claim
with no ground-truth support; inferred claims are excluded.

Auditability: Evidence references can carry `toolUseId` and `toolName`, so a
confirmed finding can be traced to the normalized tool call or result that
produced the artifact.

Constraint enforcement: The custom MCP server exposes typed forensic
operations instead of arbitrary shell. Evidence path resolution uses lexical
and realpath containment. The SIFT command path is kept narrow through
allowlisted forensic commands and explicit arguments.

Evidence integrity: KelpClaw writes an evidence manifest, performs before and
after spoliation hashing, exports ATT&CK Navigator coverage, and signs an
audit bundle with Ed25519 signatures and attestation metadata.

## How We Built It

The project is a Node 20 / pnpm 10 TypeScript ESM monorepo.

Key packages:

- `packages/findevil`: Sentinel, MCP server, verifier rules, evidence linkers,
  firewall, repair loop, benchmark scoring, deterministic replay, and ATT&CK
  export.
- `packages/cli`: `kelp-claw findevil` command surface and reviewer commands.
- `packages/evidence`: Ed25519 signing, audit bundles, manifests, and
  verification profiles.
- `packages/workflow-spec`: shared workflow schemas and validation.
- `packages/policy`, `packages/agent-hooks`, `packages/nanoclaw`, and
  `packages/codegen`: pre-existing governance and execution foundation.

The real-image pilot uses the public NIST CFReDS Hacking Case. The SIFT triage
wrapper downloads the image, verifies the acquisition hash, mounts it with
`ewfmount`, exposes a raw view, runs Sleuth Kit tools, recovers artifacts, and
feeds a deterministic trace into Sentinel.

## What Is Novel

The Find Evil work adds a purpose-built autonomous DFIR layer on top of the
existing KelpClaw governance foundation:

- Typed read-only MCP surface for forensic evidence operations
- Claim schema and evidence-reference provenance with `toolUseId`
- Evidence linkers for recovered artifacts and forensic outputs
- Verify-repair loop for unsupported or contradicted findings
- Hostile-evidence firewall and taint ledger
- Spoliation check and signed reviewer audit bundle
- DFIR benchmark scoring with hallucination rate
- CFReDS Hacking Case pilot path for a real public image

## Challenges

The main challenge was drawing a precise line between architectural guardrails
and detective controls. The typed MCP server, command allowlists, and realpath
containment are hard boundaries. The firewall scan and spoliation hashing are
detective controls unless an external hook enforces them before execution.

Another challenge was making deterministic judging practical. The CFReDS path
uses a seeding run to recover artifacts and emit claims, then a deterministic
Sentinel pass with the evidence-linked repair runner so judges can reproduce
the run without live model calls.

A third challenge was avoiding overclaiming. The CFReDS Hacking Case has 31
worksheet questions, but this pilot only scores the subset where KelpClaw can
point to recovered artifact proof.

## What We Learned

Autonomous DFIR is strongest when the agent is not the final authority. The
agent can accelerate triage, but the system needs structured claims,
artifact-backed evidence, provenance, and repair traces so a human reviewer can
challenge the result.

The project also reinforced that security controls need honest labels.
Detective controls are still useful, especially in a reviewer workflow, but
they should not be described as sandboxes.

## What's Next

- Wire `checkReadOnlyMount()` into the live Sentinel or MCP path.
- Move hostile-evidence firewall decisions earlier into pre-execution
  enforcement where the host integration supports it.
- Broaden the CFReDS Hacking Case scored set beyond the current 8
  artifact-backed findings.
- Improve the claim-to-tool graph so every accepted evidence reference carries
  complete producing-tool metadata.
- Add more public DFIR cases and richer benchmark anchors.

## Links

- Repository: `[public GitHub URL]`
- Demo video: `[video URL]`
- Judge guide: `JUDGES.md`
- Architecture diagram: `docs/architecture.mmd`
- Dataset documentation: `docs/dataset-cfreds-hacking-case.md`
- Accuracy report: `[final run accuracy-report.md or docs/accuracy-report-template.md]`

## Final Metrics To Fill

- CFReDS run directory:
- Confirmed claims:
- Precision:
- Recall:
- F1:
- Hallucination rate:
- Audit bundle verification:
- Demo video length:
