# Accuracy Report

This report uses the latest sentinel run under `.kelpclaw/findevil/sentinel/`. The only run directory present under `.kelpclaw/findevil/` is `.kelpclaw/findevil/sentinel/`.

## Numbers From The Run

| Metric                                | Actual value | Source                                                                                                   |
| ------------------------------------- | -----------: | -------------------------------------------------------------------------------------------------------- |
| Baseline claims                       |           10 | `.kelpclaw/findevil/sentinel/accuracy-report.md`                                                         |
| Repaired claims                       |           10 | `.kelpclaw/findevil/sentinel/accuracy-report.md`                                                         |
| Repair prompts                        |           11 | `.kelpclaw/findevil/sentinel/accuracy-report.md` and `.kelpclaw/findevil/sentinel/repair-trace.jsonl`    |
| Repair results                        |           11 | `.kelpclaw/findevil/sentinel/accuracy-report.md` and `.kelpclaw/findevil/sentinel/repair-trace.jsonl`    |
| Successful status changes             |            5 | `.kelpclaw/findevil/sentinel/accuracy-report.md`                                                         |
| Firewall blocks                       |            1 | `.kelpclaw/findevil/sentinel/accuracy-report.md` and `.kelpclaw/findevil/sentinel/firewall-events.jsonl` |
| Baseline unsupported claims           |            6 | `.kelpclaw/findevil/sentinel/accuracy-report.md`                                                         |
| Repaired confirmed claims             |            3 | `.kelpclaw/findevil/sentinel/accuracy-report.md`                                                         |
| Evidence refs on repaired `claim-001` |            4 | `.kelpclaw/findevil/sentinel/accuracy-report.md` and `.kelpclaw/findevil/sentinel/claim-ledger.json`     |
| Evidence files hashed before analysis |           13 | `.kelpclaw/findevil/sentinel/spoliation-check.json`                                                      |
| Evidence files hashed after analysis  |           13 | `.kelpclaw/findevil/sentinel/spoliation-check.json`                                                      |
| Added evidence files                  |            0 | `.kelpclaw/findevil/sentinel/spoliation-check.json`                                                      |
| Removed evidence files                |            0 | `.kelpclaw/findevil/sentinel/spoliation-check.json`                                                      |
| Changed evidence files                |            0 | `.kelpclaw/findevil/sentinel/spoliation-check.json`                                                      |
| Policy denials                        |            1 | `.kelpclaw/findevil/sentinel/audit-bundle/result.json`                                                   |
| Uncorrected policy denials            |            0 | `.kelpclaw/findevil/sentinel/audit-bundle/result.json`                                                   |
| Files checked in audit bundle         |           14 | `.kelpclaw/findevil/sentinel/audit-bundle/manifest.json`                                                 |

## Benchmark Numbers

| Metric            | Actual value | Source                                           |
| ----------------- | -----------: | ------------------------------------------------ |
| Expected findings |           10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Evaluated claims  |           10 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| True positives    |            3 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| False positives   |            0 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| False negatives   |            7 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Precision         |        1.000 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| Recall            |        0.300 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |
| F1                |        0.462 | `.kelpclaw/findevil/sentinel/accuracy-report.md` |

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

From `.kelpclaw/findevil/sentinel/repair-trace.jsonl`:

```json
{
  "timestamp": "2026-05-30T07:45:38.475Z",
  "iteration": 1,
  "claimId": "claim-001",
  "event": "repair_result",
  "status": "confirmed",
  "output": "iteration 1: repaired with 4 linked evidence refs"
}
```

From `.kelpclaw/findevil/sentinel/firewall-events.jsonl`:

```text
"eventType":"tainted_instruction_blocked"
"policyDecision":{"action":"deny","matchedRuleIds":["block-tainted-instruction-text"],"reason":"Case-derived text cannot become an operational instruction."}
```

From `.kelpclaw/findevil/sentinel/spoliation-check.json`:

```text
"ok": true
"before": [
"after": [
"added": [],
"removed": [],
"changed": []
```

The opened spoliation JSON contains 13 objects in `before`, 13 objects in `after`, and empty `added`, `removed`, and `changed` arrays.

From `.kelpclaw/findevil/sentinel/audit-bundle/result.json`:

```json
{
  "ok": true,
  "runId": "findevil-sift-sentinel-demo-001-mps1qirn",
  "status": "succeeded",
  "mode": "sentinel",
  "policyDenials": 1,
  "uncorrectedPolicyDenials": 0
}
```

From `.kelpclaw/findevil/sentinel/audit-bundle/manifest.json`, the opened `files` array contains fourteen artifacts:

```text
accuracy-report.md
agent-execution.jsonl
claim-ledger.json
committee-vote.jsonl
compatibility.json
evidence-manifest.json
firewall-events.jsonl
index.html
policy-decisions.json
redaction-report.json
repair-trace.jsonl
result.json
spoliation-check.json
taint-ledger.jsonl
```

## Honest Interpretation

This is a compact synthetic evaluation, not a broad benchmark. The current run confirms 3 of 10 expected findings with accepted ATT&CK techniques, blocks one hostile evidence instruction, preserves the original evidence tree, and packages the result for review. It does not yet prove general incident-response accuracy across multiple real-world cases.
