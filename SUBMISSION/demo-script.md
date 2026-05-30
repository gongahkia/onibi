# Demo Script

## 0:00 Framing

Spoken: "This is KelpClaw SIFT Sentinel. Claude Code and Protocol SIFT perform the investigation. KelpClaw wraps the run with claim verification, hostile-evidence containment, spoliation checks, and a signed audit bundle."

On screen:

```console
$ sed -n '1,40p' examples/findevil-sift-sentinel/case.yml
$ sed -n '1,80p' SUBMISSION/architecture-diagram.md
```

## 0:30 Baseline Overclaim

Spoken: "The baseline report intentionally overclaims execution. It sees `evil.exe` present in Public Downloads and concludes that it executed. The verifier should not accept file presence alone as execution."

On screen:

```console
$ sed -n '1,120p' fixtures/protocol-sift-baseline/baseline-report.md
$ sed -n '1,14p' .kelpclaw/findevil/sentinel/accuracy-report.md
```

## 1:30 Verifier Flags

Spoken: "The baseline claim starts unsupported. The report shows one unsupported baseline claim and zero confirmed baseline claims."

On screen:

```console
$ sed -n '14,24p' .kelpclaw/findevil/sentinel/accuracy-report.md
```

## 2:00 Repair Pass

Spoken: "The sentinel generates one targeted repair prompt. It asks the agent to prove, retract, or downgrade the claim using direct execution artifacts such as Prefetch, Amcache, ShimCache, or Sysmon."

On screen:

```console
$ sed -n '1,3p' .kelpclaw/findevil/sentinel/repair-trace.jsonl
$ jq '.claims[0] | {id, text, status, evidenceRefs: (.evidenceRefs | length)}' .kelpclaw/findevil/sentinel/claim-ledger.json
```

## 2:45 Hostile-Evidence Block

Spoken: "The case also contains hostile text. The baseline copies the ransom-note command into an operational next step, but the firewall blocks that as tainted case data."

On screen:

```console
$ sed -n '1p' examples/findevil-sift-sentinel/case-data/ransom_note.txt
$ sed -n '1p' .kelpclaw/findevil/sentinel/firewall-events.jsonl
```

## 3:30 Safe Reanalysis

Spoken: "Instead of following hostile text, the sentinel creates a safe reanalysis task. The text is quoted as evidence only."

On screen:

```console
$ jq '.correctionTask' .kelpclaw/findevil/sentinel/firewall-events.jsonl
```

## 4:00 Spoliation Check Passes

Spoken: "The original evidence tree is hashed before and after the run. This run has nine files before, nine files after, and no added, removed, or changed evidence files."

On screen:

```console
$ jq '{ok, before:(.before|length), after:(.after|length), added:(.added|length), removed:(.removed|length), changed:(.changed|length)}' .kelpclaw/findevil/sentinel/spoliation-check.json
```

## 4:30 Signed Audit Bundle Opens

Spoken: "The result is a signed audit bundle with a reviewer UI. I open the bundle, click one claim to show its linked evidence row, then click the firewall block to show the safe-reanalysis prompt that quoted the hostile text as evidence only."

On screen:

```console
$ ls .kelpclaw/findevil/sentinel/audit-bundle
$ node packages/cli/dist/index.js verify-audit-bundle .kelpclaw/findevil/sentinel/audit-bundle --profile reviewer
$ open .kelpclaw/findevil/sentinel/audit-bundle/index.html
```
