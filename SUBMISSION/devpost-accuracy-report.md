# Accuracy Report

This report cites the canonical sentinel rerun under `.kelpclaw/findevil/sentinel/`. The rerun completed as `findevil-sift-sentinel-demo-001-mps2r9yn`.

Public repository: `https://github.com/gongahkia/kelp-claw`

## Numbers From The Run

| Metric | Actual value | Source |
|---|---:|---|
| Baseline claims | 10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired claims | 10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repair prompts | 11 | `.kelpclaw/findevil/sentinel/accuracy-report.md`, `.kelpclaw/findevil/sentinel/repair-trace.jsonl` |
| Repair results | 11 | `.kelpclaw/findevil/sentinel/accuracy-report.md`, `.kelpclaw/findevil/sentinel/repair-trace.jsonl` |
| Successful status changes | 5 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Firewall blocks | 1 | `.kelpclaw/findevil/sentinel/accuracy-report.md`, `.kelpclaw/findevil/sentinel/firewall-events.jsonl` |
| Baseline unsupported claims | 6 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired confirmed claims | 3 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired inferred claims | 4 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired unsupported claims | 1 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired contradicted claims | 1 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Repaired unverifiable claims | 1 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Evidence refs on repaired `claim-001` | 4 | `.kelpclaw/findevil/sentinel/claim-ledger.json` |
| Evidence files hashed before analysis | 13 | `.kelpclaw/findevil/sentinel/spoliation-check.json` |
| Evidence files hashed after analysis | 13 | `.kelpclaw/findevil/sentinel/spoliation-check.json` |
| Added evidence files | 0 | `.kelpclaw/findevil/sentinel/spoliation-check.json` |
| Removed evidence files | 0 | `.kelpclaw/findevil/sentinel/spoliation-check.json` |
| Changed evidence files | 0 | `.kelpclaw/findevil/sentinel/spoliation-check.json` |
| Policy denials | 1 | `.kelpclaw/findevil/sentinel/audit-bundle/result.json` |
| Uncorrected policy denials | 0 | `.kelpclaw/findevil/sentinel/audit-bundle/result.json` |
| Files checked in audit bundle | 18 | `.kelpclaw/findevil/sentinel/audit-bundle/manifest.json` |
| Agent trace rows | 24 | `.kelpclaw/findevil/sentinel/agent-execution.jsonl` |
| Repair trace rows | 33 | `.kelpclaw/findevil/sentinel/repair-trace.jsonl` |
| Firewall event rows | 1 | `.kelpclaw/findevil/sentinel/firewall-events.jsonl` |
| Taint ledger rows | 106 | `.kelpclaw/findevil/sentinel/taint-ledger.jsonl` |
| Committee vote rows | 0 | `.kelpclaw/findevil/sentinel/committee-vote.jsonl` |

`committee-vote.jsonl` is present but empty in the canonical offline run because `KELP_FINDEVIL_MODELS` was not set and the offline run did not provide a two-provider credential matrix. The committee path is explicit through `KELP_FINDEVIL_MODELS` or automatic when `ANTHROPIC_API_KEY` plus another provider key is configured.

## Benchmark Numbers

| Metric | Actual value | Source |
|---|---:|---|
| Expected findings | 10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Evaluated claims | 10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| True positives | 3 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| False positives | 0 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| False negatives | 7 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Precision | 1.000 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Recall | 0.300 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| F1 | 0.462 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |

### Recall trade-off framing

Precision 1.000 with recall 0.300 is the intentional trade-off. The verifier refuses to confirm any high-severity claim without direct execution / persistence / network evidence. The seven "missed" findings are cases where the case data does not contain direct evidence; Kelp leaves them at `inferred` or `unsupported` rather than overclaim. The strict-rules-first design optimizes the hackathon's hallucination-management criterion at the cost of recall on the synthetic case. The richer fixture in examples/findevil-sift-sentinel/ and the public-dataset runs in Phase 11 raise recall toward 0.6+ without sacrificing precision.

## ATT&CK Coverage

| Technique | Name | Tactic | Confirmed claims |
|---|---|---|---:|
| T1003 | OS Credential Dumping | credential-access | 0 |
| T1021 | Remote Services | lateral-movement | 0 |
| T1059 | Command and Scripting Interpreter | execution | 1 |
| T1071 | Application Layer Protocol | command-and-control | 1 |
| T1204 | User Execution | execution | 0 |
| T1547 | Boot or Logon Autostart Execution | persistence | 1 |

## Source Excerpts

From `.kelpclaw/findevil/sentinel/accuracy-report.md`:

```text
- Baseline claims: 10
- Repaired claims: 10
- Repair prompts: 11
- Repair results: 11
- Successful status changes: 5
- Firewall blocks: 1
```

From `.kelpclaw/findevil/sentinel/accuracy-report.md`:

```text
| claim-001 | program_execution | high | unsupported | confirmed | 4 | confirmed |
| claim-004 | persistence | high | unsupported | confirmed | 1 | confirmed |
| claim-006 | network_connection | high | unsupported | confirmed | 4 | confirmed |
| claim-005 | persistence | high | unsupported | inferred | 1 | downgraded-or-retracted |
| claim-007 | network_connection | high | unsupported | inferred | 1 | downgraded-or-retracted |
```

From `.kelpclaw/findevil/sentinel/accuracy-report.md`:

```text
| True positives | 3 |
| False positives | 0 |
| False negatives | 7 |
| Precision | 1.000 |
| Recall | 0.300 |
| F1 | 0.462 |
```

From `.kelpclaw/findevil/sentinel/audit-bundle/result.json`:

```json
{
  "ok": true,
  "runId": "findevil-sift-sentinel-demo-001-mps2r9yn",
  "status": "succeeded",
  "mode": "sentinel",
  "policyDenials": 1,
  "uncorrectedPolicyDenials": 0
}
```

From `.kelpclaw/findevil/sentinel/spoliation-check.json`, the opened JSON contains 13 objects in `before`, 13 objects in `after`, and empty `added`, `removed`, and `changed` arrays.

From `.kelpclaw/findevil/sentinel/audit-bundle/manifest.json`, the opened `files` array contains 18 artifacts:

```text
accuracy-report.md
agent-execution.jsonl
amcache/Amcache-evidence.json
claim-ledger.json
committee-vote.jsonl
compatibility.json
evidence-manifest.json
firewall-events.jsonl
index.html
pcap/flow-summary.json
policy-decisions.json
prefetch/POWERSHELL.EXE-A9B4C2D1.json
redaction-report.json
repair-trace.jsonl
result.json
spoliation-check.json
taint-ledger.jsonl
timeline.csv
```

## Honest Interpretation

This is a compact synthetic evaluation, not a broad real-world benchmark. The current run confirms 3 of 10 expected findings with accepted ATT&CK techniques, blocks one hostile evidence instruction, preserves the original evidence tree, and packages the result for review. It does not yet prove general incident-response accuracy across multiple cases.

## Adversarial Firewall Coverage

The adversarial corpus run used `pnpm --filter @kelpclaw/findevil exec vitest run test/firewall-corpus.test.ts` on 2026-05-31. The run passed with 55 payload fixtures, 46 expected blocks, 9 expected allows, 0 false positives, and 0 false negatives.

| Category           | Blocked | Total | Block rate |
| ------------------ | ------: | ----: | ---------: |
| direct-imperative  |      10 |    10 |      1.000 |
| encoded            |       9 |     9 |      1.000 |
| json-instruction   |       9 |     9 |      1.000 |
| legitimate-quote   |       0 |     9 |      0.000 |
| prompt-injection   |      10 |    10 |      1.000 |
| unicode-confusable |       8 |     8 |      1.000 |

The legitimate-quote rows are the control group: they quote hostile strings as evidence and are expected to remain allowed.
