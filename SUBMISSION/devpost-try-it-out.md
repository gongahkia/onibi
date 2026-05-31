# Try It Out

Public repository: `https://github.com/gongahkia/kelp-claw`

Demo video artifact: `SUBMISSION/kelpclaw-sift-sentinel-demo.mp4`

## Build The v3 Container

```console
$ docker build -f Dockerfile.kelp -t kelp:v3 .
```

## Synthetic Sentinel Anchor

```console
$ rm -rf .kelpclaw/findevil/sentinel-synthetic
$ docker run --rm \
  -v "$PWD/.kelpclaw/findevil/sentinel-synthetic:/data/out" \
  kelp:v3 findevil sentinel \
  --case /app/examples/findevil-sift-sentinel/case.yml \
  --evidence-root /app/examples/findevil-sift-sentinel/case-data \
  --trace /app/fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out /data/out
```

Expected high-level result:

- `ok: true`, `status: "succeeded"`, `policyDenials: 1`, `uncorrectedPolicyDenials: 0`.
- Precision 1.000, recall 0.500, F1 0.667.
- 13 evidence files before, 13 after, 0 added, 0 removed, 0 changed.
- `attack-navigator-layer.json` contains 6 ATT&CK techniques.
- `audit-bundle/evidence-manifest.tsr` exists for RFC3161 verification.

## CFReDS Anchor

```console
$ node scripts/fetch-cfreds-case.mjs
$ rm -rf .kelpclaw/findevil/sentinel-cfreds
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

Expected high-level result:

- The fetch script returns `ok .kelpclaw/datasets/cfreds/forensics-image-test/2020JimmyWilson.E01`.
- The evidence manifest contains one file, SHA-256 `6c18f662744d55e2769d9510f6173f04dab668c42b67ef27b675d22e628b4ed5`.
- The conservative container anchor emits 25 worksheet claims and confirms 0 without recovered artifact proof.
- The spoliation check reports 1 file before, 1 after, 0 changed.

## DFIR-Metric Subset-10

```console
$ rm -rf .kelpclaw/findevil/benchmark/dfir-metric
$ mkdir -p .kelpclaw/datasets/dfir-metric .kelpclaw/findevil/benchmark/dfir-metric
$ docker run --rm \
  -v "$PWD/.kelpclaw:/app/.kelpclaw" \
  -v "$PWD/.kelpclaw/findevil/benchmark/dfir-metric:/data/out" \
  kelp:v3 findevil benchmark \
  --dataset dfir-metric \
  --subset-size 10 \
  --out /data/out
```

Expected high-level result:

- Cases: 10.
- Expected findings: 14.
- True positives: 14.
- False positives: 0.
- False negatives: 0.
- Precision 1.000, recall 1.000, F1 1.000.

The first ten pinned DFIR-Metric rows include two rows with empty answer arrays, so they contribute 0 expected findings.

## Verify Audit Bundle

```console
$ ./node_modules/.bin/kelp-claw verify-audit-bundle \
  .kelpclaw/findevil/sentinel-synthetic/audit-bundle --profile reviewer
```

## Verify RFC3161 TSA Token

```console
$ openssl ts -verify \
  -in .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.tsr \
  -data .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.json \
  -CAfile freetsa-cacert.pem
```

Judges need the FreeTSA CA certificate from `https://freetsa.org/files/`.

## Open Reviewer UI And Navigator Layer

```console
$ open .kelpclaw/findevil/sentinel-synthetic/audit-bundle/index.html
$ open https://mitre-attack.github.io/attack-navigator/
```

Drag `.kelpclaw/findevil/sentinel-synthetic/attack-navigator-layer.json` into ATT&CK Navigator.
