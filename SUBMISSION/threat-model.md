# Threat Model

Public repository: `https://github.com/gongahkia/kelp-claw`

Scope: STRIDE analysis for the KelpClaw SIFT Sentinel pipeline, covering the path from `case.yml` and evidence mounts through Protocol SIFT / Claude Code execution, claim extraction, evidence linking, firewalling, spoliation checks, benchmark scoring, ATT&CK export, and signed audit-bundle publication.

## Assets

| Asset | Why it matters |
|---|---|
| Original evidence | Must remain unchanged and must not be overwritten by an agent or tool. |
| Case manifest | Defines expected findings, protected boundaries, and scoring rules. |
| Agent execution trace | Reviewer source of truth for tool calls and model-visible events. |
| Claim ledger | Structured incident-response findings and evidence references. |
| Taint ledger and firewall events | Proof that hostile case text was tracked and blocked when needed. |
| Audit bundle | Reviewer package containing report, traces, manifests, signatures, and timestamp token. |

## STRIDE

| STRIDE risk | Sentinel-specific threat | v3 control | Residual risk |
|---|---|---|---|
| Spoofing | A generated finding pretends to cite evidence it never used. | Evidence refs carry artifact path, locator, support type, and SHA-256. Reviewer bundle copies linked preview artifacts. | A live parser can still mislabel a locator; reviewers must inspect source artifacts for high-stakes cases. |
| Tampering | Agent or forensic tool modifies mounted evidence. | Evidence tree is hashed before and after analysis. v3 synthetic run: 13 before / 13 after / 0 changed. v3 CFReDS run: 1 before / 1 after / 0 changed. Docker and SIFT flows mount evidence read-only. | OS-level read-only mounts must be configured correctly outside the container. |
| Repudiation | Agent output cannot be tied to a run. | `agent-execution.jsonl`, `repair-trace.jsonl`, `policy-decisions.json`, Ed25519 `manifest.sig`, and RFC3161 `evidence-manifest.tsr` are included in the audit bundle. | External TSA availability is a dependency for fresh timestamp tokens. |
| Information disclosure | Case data leaks through model prompts, traces, or reviewer exports. | Taint ledger marks case-derived text, and the reviewer bundle includes only linked preview artifacts rather than blindly copying the evidence root. | Operators must still avoid uploading sensitive real-world evidence to third-party model providers unless authorized. |
| Denial of service | Protocol SIFT or a parser hangs on malformed evidence. | `siftIntegration.maxRuntimeSeconds` bounds live SIFT execution, and Docker runs isolate the command. | Very large evidence can still exhaust local disk or CPU before the timeout if operators under-provision the VM. |
| Elevation of privilege | Prompt injection inside evidence becomes a tool instruction. | Instruction firewall blocks tainted imperative content when it appears in operational tool arguments and emits a safe reanalysis task. Phase 12B blocked 46 of 46 malicious corpus payloads and allowed all 9 legitimate quote controls, with 0 false positives and 0 false negatives. | The firewall is pattern and taint based; unknown prompt-injection forms need continued corpus expansion. |

## Trust Boundaries

| Boundary | Control |
|---|---|
| Evidence root to agent context | Taint extraction and evidence hashing. |
| Agent text to tool invocation | Instruction firewall and policy-denial event. |
| Claim text to confirmed finding | Claim-type-specific verifier rules and benchmark scorer. |
| Local run to reviewer package | Signed audit bundle and manifest hashing. |
| Evidence manifest to wall-clock proof | RFC3161 timestamp token verified with `openssl ts -verify`. |

## v3 Evidence

- Synthetic anchor: precision 1.000, recall 0.500, F1 0.667; 1 firewall block; 0 evidence changes.
- CFReDS anchor: pinned E01 SHA-256 verified; 25 worksheet prompts emitted; 0 confirmed findings without artifact proof; 0 evidence changes.
- DFIR-Metric subset-10: 14 non-empty expected answers, 14 confirmed, 0 false positives.
- Determinism: `sha256:8f99da2da7cb45a9e28d0c6db0c89fe6d08cbcf36fa0d2a710cd9552a10ee666`.
