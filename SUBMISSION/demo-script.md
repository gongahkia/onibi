# Demo Script

Target length: 5 minutes or less.

## 0:00 Framing

Spoken: "This is KelpClaw SIFT Sentinel. Claude Code and Protocol SIFT perform the investigation. KelpClaw wraps the run with claim verification, hostile-evidence containment, spoliation checks, and a signed audit bundle."

On screen:

```console
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --out .kelpclaw/findevil/sentinel
$ sed -n '1,40p' examples/findevil-sift-sentinel/case.yml
$ sed -n '1,90p' SUBMISSION/architecture-diagram.md
```

## 0:30 Baseline Overclaim x3

Spoken: "The baseline report intentionally overclaims three things for the demo: PowerShell execution before direct artifact linking, Run-key persistence before registry proof, and DailyUpdater persistence from a TaskCache-only reference."

On screen:

```console
$ sed -n '1,120p' fixtures/protocol-sift-baseline/baseline-report.md
$ rg "F-001|F-004|F-005|Analyst conclusion" fixtures/protocol-sift-baseline/baseline-report.md
```

## 1:30 Verifier Flags All 3

Spoken: "The verifier does not accept those conclusions on confidence alone. In the baseline ledger, all three selected claims start unsupported."

On screen:

```console
$ sed -n '1,34p' .kelpclaw/findevil/sentinel/accuracy-report.md
$ rg "claim-001|claim-004|claim-005" .kelpclaw/findevil/sentinel/accuracy-report.md
```

## 2:00 Repair Pass Succeeds On 2, Retracts 1

Spoken: "The repair pass asks for proof, retraction, or downgrade. It confirms PowerShell from Prefetch, confirms UpdaterRun from a Run-key, and downgrades DailyUpdater because the task evidence is still not authoritative."

On screen:

```console
$ jq -r 'select(.event=="repair_result" and (.claimId=="claim-001" or .claimId=="claim-004" or .claimId=="claim-005")) | [.claimId,.status,.output] | @tsv' .kelpclaw/findevil/sentinel/repair-trace.jsonl
$ jq -r '.claims[] | select(.id=="claim-001" or .id=="claim-004" or .id=="claim-005") | [.id,.status,(.evidenceRefs|length)] | @tsv' .kelpclaw/findevil/sentinel/claim-ledger.json
```

## 2:45 Hostile-Evidence Block

Spoken: "The case also contains hostile text. The baseline copies the ransom-note command into an operational next step, but the firewall blocks that as tainted case data."

On screen:

```console
$ sed -n '1p' examples/findevil-sift-sentinel/case-data/ransom_note.txt
$ jq '{eventType, source, blockedUse, policyDecision, correctionTask}' .kelpclaw/findevil/sentinel/firewall-events.jsonl
```

## 3:15 Spoliation Check Passes

Spoken: "The original evidence tree is hashed before and after the run. This run has 13 files before, 13 files after, and no added, removed, or changed evidence files."

On screen:

```console
$ jq '{ok, before:(.before|length), after:(.after|length), added:(.added|length), removed:(.removed|length), changed:(.changed|length)}' .kelpclaw/findevil/sentinel/spoliation-check.json
```

## 3:45 Reviewer UI Walkthrough

Spoken: "The result is a signed audit bundle with a reviewer UI. I open the bundle, click a confirmed claim to show linked evidence, then click the firewall block to show the safe reanalysis prompt."

On screen:

```console
$ ls .kelpclaw/findevil/sentinel/audit-bundle
$ ./node_modules/.bin/kelp-claw verify-audit-bundle .kelpclaw/findevil/sentinel/audit-bundle --profile reviewer
$ open .kelpclaw/findevil/sentinel/audit-bundle/index.html
```

## 4:30 ATT&CK Coverage And Benchmark Table

Spoken: "The final accuracy report shows ATT&CK coverage and a benchmark against ground truth: 10 expected findings, 3 true positives, 0 false positives, 7 false negatives, precision 1.000, recall 0.300, and F1 0.462."

On screen:

```console
$ sed -n '/## MITRE ATT&CK Coverage/,$p' .kelpclaw/findevil/sentinel/accuracy-report.md
```
