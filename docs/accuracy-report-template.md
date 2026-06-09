# Accuracy Report Template

Use this template as the final human-reviewed report after the CFReDS Hacking
Case Sentinel run completes. Replace bracketed values with data from the run
output.

## Run Summary

- Case: NIST CFReDS Hacking Case
- Run directory: `[.kelpclaw/findevil/cfreds-hacking-case/sentinel]`
- Run mode: deterministic Sentinel with evidence-linked repair runner
- Timestamp mode: skip
- Dataset source: NIST CFReDS Hacking Case
- Acquisition MD5: `AEE4FCD9301C03B3B054623CA261959A`
- Audit bundle: `[path to audit-bundle]`
- Verification result: `[ok true/false, checked file count, failures]`

## Metrics

Populate from `accuracy-report.md`, benchmark output, and `claim-ledger.json`.

| Metric                  |     Value |
| ----------------------- | --------: |
| Expected pilot findings |         8 |
| Confirmed claims        | `[value]` |
| True positives          | `[value]` |
| False positives         | `[value]` |
| False negatives         | `[value]` |
| Precision               | `[value]` |
| Recall                  | `[value]` |
| F1                      | `[value]` |
| Hallucination count     | `[value]` |
| Hallucination rate      | `[value]` |

Hallucination definition used by KelpClaw: a hallucinated finding is a
confirmed claim with no ground-truth support. Inferred claims are excluded.

## Confirmed Findings

Summarize each confirmed finding from `claim-ledger.json`.

| Claim ID     | Finding          | Severity     | Evidence refs                   | ATT&CK        |
| ------------ | ---------------- | ------------ | ------------------------------- | ------------- |
| `[claim-id]` | `[finding text]` | `[severity]` | `[artifact/supports/toolUseId]` | `[technique]` |

## Self-Correction

Use `repair-trace.jsonl`.

- Iterations used: `[value]`
- Claims changed by repair: `[value]`
- Unsupported claims removed or downgraded: `[value]`
- Example: `[claim ID before/after and why]`

The repair loop should be described as a verify-then-correct control: Sentinel
extracts claims, checks them against artifact-backed rules, and records repair
iterations when claims are unsupported, contradicted, or missing evidence.

## Traceability Proof

Pick one high-severity finding and show its producing tool event.

```console
$ jq '.claims[] | select(.id=="[claim-id]") |
    {claim: .text, refs: .evidenceRefs}' claim-ledger.json
$ jq 'select(.toolUseId=="[toolUseId]")' agent-execution.jsonl
```

Result to document:

- Claim ID:
- Evidence artifact:
- `toolUseId`:
- Tool name:
- Tool result summary:

## Evidence Integrity

Use `spoliation-check.json`, `evidence-manifest.json`, and the audit bundle.

Architectural guardrails:

- Custom read-only MCP server exposes typed forensic tools instead of arbitrary shell.
- SIFT command path uses allowlisted forensic commands, argument arrays,
  runtime caps, and output caps.
- MCP evidence path handling uses lexical containment and realpath containment
  against the real evidence root.

Detective / post-exec controls:

- Spoliation check hashes evidence before and after the run.
- Hostile-evidence firewall scans normalized events and repair inputs for
  tainted instructions.
- Signed audit bundle records manifests, signatures, attestation, and reviewer
  artifacts.

State plainly: the current firewall scan is a detective control in the
Sentinel flow unless an external hook enforces it before tool execution. It is
not a full runtime sandbox.

## Residual Gaps

- Firewall classification runs after `runAgent()` returns in the current
  Sentinel path unless external hook enforcement is active.
- `checkReadOnlyMount()` exists but is not wired into the live Sentinel or MCP
  path.
- `uncorrectedPolicyDenials` is currently hardcoded to 0.
- `toolUseId` and `toolName` provenance are attached only when normalized tool
  events contain path-like artifact values that resolve inside `evidenceRoot`.

## Reviewer Files

Include or link:

- `agent-execution.jsonl`
- `claim-ledger.json`
- `repair-trace.jsonl`
- `taint-ledger.jsonl`
- `firewall-events.jsonl`
- `spoliation-check.json`
- `evidence-manifest.json`
- `attack-navigator-layer.json`
- `committee-vote.jsonl`
- `audit-bundle/`
