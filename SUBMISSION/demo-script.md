# Demo Script

Target length: 5 minutes or less.

## 0:00 Framing

Spoken: "This is KelpClaw SIFT Sentinel. Claude Code and Protocol SIFT perform the investigation. KelpClaw wraps the run with claim verification, hostile-evidence containment, spoliation checks, ATT&CK Navigator export, deterministic replay, RFC3161 timestamping, and a signed audit bundle."

On screen:

```console
$ docker build -f Dockerfile.kelp -t kelp:v3 .
$ sed -n '1,120p' SUBMISSION/architecture-diagram.md
```

## 0:25 CFReDS Case Anchor

Spoken: "The public CFReDS Forensics Image Test is mounted read-only. The fetch script verifies the 309 MB E01 against its pinned SHA-256 before Sentinel touches it."

On screen:

```console
$ node scripts/fetch-cfreds-case.mjs
$ sed -n '1,120p' examples/findevil-cfreds-image-test/case.yml
$ docker run --rm \
  -v "$PWD/.kelpclaw/datasets/cfreds/forensics-image-test:/data/cfreds:ro" \
  -v "$PWD/.kelpclaw/findevil/sentinel-cfreds:/data/out" \
  kelp:v3 findevil sentinel \
  --case /app/examples/findevil-cfreds-image-test/case.yml \
  --evidence-root /data/cfreds \
  --sift-command "<Protocol SIFT JSONL command>" \
  --max-iterations 3 \
  --out /data/out
```

## 1:15 Conservative CFReDS Result

Spoken: "The CFReDS container anchor emits the official worksheet prompts as 25 claims, but confirms none without recovered artifact evidence. That is intentional: the tool refuses to turn prompts into findings."

On screen:

```console
$ jq '{files:.files}' .kelpclaw/findevil/sentinel-cfreds/evidence-manifest.json
$ jq -r '.claims[] | [.id,.type,.status,(.evidenceRefs|length)] | @tsv' \
  .kelpclaw/findevil/sentinel-cfreds/claim-ledger.json | head
$ jq '{ok,before:(.before|length),after:(.after|length),changed:(.changed|length)}' \
  .kelpclaw/findevil/sentinel-cfreds/spoliation-check.json
```

## 1:55 Synthetic Self-Correction

Spoken: "The synthetic case shows self-correction. v3 starts with weak claims, links direct artifacts, and repairs 5 findings to confirmed while keeping weak evidence unconfirmed."

On screen:

```console
$ sed -n '1,90p' .kelpclaw/findevil/sentinel-synthetic/accuracy-report.md
$ jq -r '.claims[] | select(.status=="confirmed") | [.id,.type,(.evidenceRefs|length)] | @tsv' \
  .kelpclaw/findevil/sentinel-synthetic/claim-ledger.json
```

## 2:35 Hostile-Evidence Firewall

Spoken: "The evidence includes hostile strings. The firewall blocks tainted case text when it crosses into a tool argument, then injects a safe reanalysis prompt that quotes the string as evidence only."

On screen:

```console
$ sed -n '1p' examples/findevil-sift-sentinel/case-data/ransom_note.txt
$ jq '{eventType, source, blockedUse, policyDecision, correctionTask}' \
  .kelpclaw/findevil/sentinel-synthetic/firewall-events.jsonl
$ pnpm --filter @kelpclaw/findevil exec vitest run test/firewall-corpus.test.ts
```

## 3:10 Navigator Drag-And-Drop

Spoken: "Every run exports an ATT&CK Navigator layer. I drag `attack-navigator-layer.json` into MITRE ATT&CK Navigator and the reviewer sees exactly which techniques were confirmed, weak, or unconfirmed."

On screen:

```console
$ jq '{name,domain,techniques:(.techniques|length)}' \
  .kelpclaw/findevil/sentinel-synthetic/attack-navigator-layer.json
$ open https://mitre-attack.github.io/attack-navigator/
```

Visual beat: drag `.kelpclaw/findevil/sentinel-synthetic/attack-navigator-layer.json` into the Navigator page.

## 3:45 Audit Bundle And TSA Verification

Spoken: "The audit bundle is signed, and the evidence manifest is timestamped. A reviewer can verify both the Kelp signature and the RFC3161 timestamp token."

On screen:

```console
$ ./node_modules/.bin/kelp-claw verify-audit-bundle \
  .kelpclaw/findevil/sentinel-synthetic/audit-bundle --profile reviewer
$ openssl ts -verify \
  -in .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.tsr \
  -data .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.json \
  -CAfile freetsa-cacert.pem
$ open .kelpclaw/findevil/sentinel-synthetic/audit-bundle/index.html
```

## 4:30 Three-Anchor Accuracy Close

Spoken: "The v3 submission reports three anchors: synthetic precision 1.000, recall 0.500, F1 0.667; CFReDS precision 0.000, recall 0.000, F1 0.000 because no worksheet answer is promoted without direct artifact proof; and DFIR-Metric blind subset-10 precision 0.000, recall 0.000, F1 0.000 because benchmark answers are scorer-only."

On screen:

```console
$ sed -n '1,180p' SUBMISSION/devpost-accuracy-report.md
```
