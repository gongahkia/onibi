# Judge Reproduction Guide

This guide gives two reproducible paths:

- Synthetic anchor: fastest path, no SIFT VM required.
- CFReDS Hacking Case pilot: real public disk image, intended to run inside a SIFT Workstation VM.

KelpClaw is a Node 20 / pnpm 10 TypeScript monorepo. The live SIFT path uses
Sleuth Kit and libewf tools through a constrained DFIR workflow.

## Repository Setup

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm -r build
$ pnpm -r test
```

Expected source verification: all workspace builds and tests pass on Node 20.

## Fast Synthetic Anchor

This path exercises the verifier, repair loop, hostile-evidence firewall,
spoliation check, ATT&CK export, and signed audit bundle without downloading a
disk image.

```console
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --timestamp skip \
  --out .kelpclaw/findevil/sentinel-synthetic
```

Verify the generated audit bundle:

```console
$ ./node_modules/.bin/kelp-claw verify-audit-bundle \
  .kelpclaw/findevil/sentinel-synthetic/audit-bundle \
  --profile reviewer
```

Useful files:

- `.kelpclaw/findevil/sentinel-synthetic/claim-ledger.json`
- `.kelpclaw/findevil/sentinel-synthetic/agent-execution.jsonl`
- `.kelpclaw/findevil/sentinel-synthetic/repair-trace.jsonl`
- `.kelpclaw/findevil/sentinel-synthetic/spoliation-check.json`
- `.kelpclaw/findevil/sentinel-synthetic/accuracy-report.md`
- `.kelpclaw/findevil/sentinel-synthetic/audit-bundle/`

## Real CFReDS Hacking Case Pilot

Run this path inside SIFT Workstation. SIFT provides the expected forensic
dependencies: Sleuth Kit, libewf/ewfmount, ewfverify, mmls, fsstat,
tsk_recover, and RegRipper where available.

Prerequisites inside the VM:

- Node.js 20.19 or newer
- pnpm through Corepack
- Network access to download the public CFReDS image parts
- Sufficient disk space for the image and recovered artifacts

Fetch the public dataset:

```console
$ node scripts/fetch-cfreds-hacking-case.mjs
```

The script downloads:

- `4Dell Latitude CPi.E01`
- `4Dell Latitude CPi.E02`
- `TestAnswers.pdf`

The expected acquisition MD5 from the official answer PDF is:

```text
AEE4FCD9301C03B3B054623CA261959A
```

Verify the EWF image directly on SIFT:

```console
$ cd .kelpclaw/datasets/cfreds/hacking-case
$ ewfverify "4Dell Latitude CPi.E01"
```

Seed the extracted-claim cache and recover SIFT artifacts:

```console
$ node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
```

Run the deterministic Sentinel pass:

```console
$ node packages/cli/dist/index.js findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --sift-command "node scripts/run-cfreds-hacking-case-triage.mjs --dataset .kelpclaw/datasets/cfreds/hacking-case --out .kelpclaw/findevil/cfreds-hacking-case/triage --mode emit-trace" \
  --max-iterations 3 \
  --repair-runner evidence-linked \
  --timestamp skip \
  --deterministic \
  --out .kelpclaw/findevil/cfreds-hacking-case/sentinel
```

Verify the audit bundle using the printed `auditBundle` path or the default
location:

```console
$ node packages/cli/dist/index.js verify-audit-bundle \
  .kelpclaw/findevil/cfreds-hacking-case/sentinel/audit-bundle \
  --profile reviewer
```

## Traceability Check

The main auditability claim is that confirmed findings can be traced back to
the exact tool execution that produced their evidence.

From the Sentinel output directory:

```console
$ jq '.claims[] | select(.severity=="high" or .severity=="critical") |
    {claim: .text, status: .status, refs: .evidenceRefs}' claim-ledger.json | head -40
```

Pick a `toolUseId` from the evidence reference, then locate the producing tool
event:

```console
$ jq 'select(.toolUseId=="<PASTE_toolUseId_HERE>")' agent-execution.jsonl
```

If a recovered artifact was linked from a normalized tool event, the second
command returns the tool call/result that produced the evidence.

## Expected Output Set

Preserve these files from the final CFReDS run:

- `agent-execution.jsonl`
- `claim-ledger.json`
- `repair-trace.jsonl`
- `taint-ledger.jsonl`
- `firewall-events.jsonl`
- `spoliation-check.json`
- `evidence-manifest.json`
- `accuracy-report.md`
- `attack-navigator-layer.json`
- `committee-vote.jsonl`
- `audit-bundle/`

These files are the submission evidence for execution logs, self-correction,
evidence integrity, accuracy, and reviewer verification.
