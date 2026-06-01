# Accuracy Report

Public repository: `https://github.com/gongahkia/kelp-claw`

This v3 report is anchored by three reruns from 2026-05-31:

- Synthetic Sentinel: `.kelpclaw/findevil/sentinel-synthetic/`
- CFReDS Forensics Image Test: `.kelpclaw/findevil/sentinel-cfreds/`
- DFIR-Metric subset-10: `.kelpclaw/findevil/benchmark/dfir-metric/`

A fourth public-image pilot is implemented for the NIST CFReDS Hacking Case at `examples/findevil-cfreds-hacking-case/`, but its precision / recall / F1 must be filled from the SIFT VM output after `scripts/run-cfreds-hacking-case-triage.mjs` runs against the downloaded E01/E02 image.

## Precision / Recall / F1

### 1. Synthetic Sentinel Case

Source: `.kelpclaw/findevil/sentinel-synthetic/accuracy-report.md`

| Metric            | Value |
| ----------------- | ----: |
| Expected findings |    10 |
| Evaluated claims  |    10 |
| True positives    |     5 |
| False positives   |     0 |
| False negatives   |     5 |
| Precision         | 1.000 |
| Recall            | 0.500 |
| F1                | 0.667 |

Run details:

| Metric                                   |                                      Value |
| ---------------------------------------- | -----------------------------------------: |
| Run ID                                   | `findevil-sift-sentinel-demo-001-mpt8ou9e` |
| Baseline claims                          |                                         10 |
| Repaired claims                          |                                         10 |
| Repair prompts                           |                                         11 |
| Repair results                           |                                         11 |
| Successful status changes                |                                          5 |
| Firewall blocks                          |                                          1 |
| Evidence files before / after            |                                    13 / 13 |
| Added / removed / changed evidence files |                                  0 / 0 / 0 |
| Audit-bundle manifest files              |                                         20 |
| Agent trace / repair trace rows          |                                    24 / 33 |
| Taint-ledger rows                        |                                        106 |
| ATT&CK Navigator techniques              |                                          6 |

The synthetic fixture intentionally rewards strictness. v3 confirms PowerShell execution, two persistence claims, C2 flow evidence, and the YARA-backed malware-identification claim. It leaves weak file-presence, DNS-only, credential-access, and lateral-movement assertions unconfirmed.

### 2. CFReDS Forensics Image Test

Sources: `.kelpclaw/findevil/sentinel-cfreds/claim-ledger.json`, `.kelpclaw/findevil/sentinel-cfreds/evidence-manifest.json`, `examples/findevil-cfreds-image-test/case.yml`

| Metric            | Value |
| ----------------- | ----: |
| Expected findings |    25 |
| Evaluated claims  |    25 |
| True positives    |     0 |
| False positives   |     0 |
| False negatives   |    25 |
| Precision         | 0.000 |
| Recall            | 0.000 |
| F1                | 0.000 |

Run details:

| Metric                                   |                                                              Value |
| ---------------------------------------- | -----------------------------------------------------------------: |
| Run ID                                   |                          `findevil-cfreds-image-test-001-mpt8qvj2` |
| Cached evidence file                     |                                              `2020JimmyWilson.E01` |
| Pinned SHA-256                           | `6c18f662744d55e2769d9510f6173f04dab668c42b67ef27b675d22e628b4ed5` |
| Evidence bytes                           |                                                        309,818,835 |
| Baseline / repaired claims               |                                                            25 / 25 |
| Confirmed claims                         |                                                                  0 |
| Unsupported / unverifiable claims        |                                                             4 / 21 |
| Evidence files before / after            |                                                              1 / 1 |
| Added / removed / changed evidence files |                                                          0 / 0 / 0 |
| Policy denials                           |                                                                  0 |
| ATT&CK Navigator techniques              |                                                                  5 |

This run is intentionally conservative. The container anchor verifies the pinned E01 and emits the 25 official worksheet prompts as claims, but it does not claim recovered email, VHD, SAM, recycle-bin, browser, autorun, or hash answers without a live Protocol SIFT artifact parser. The generated sentinel `accuracy-report.md` now scores against the active CFReDS case manifest.

### 3. DFIR-Metric Blind Subset-10

Source: `.kelpclaw/findevil/benchmark/dfir-metric/aggregate-accuracy-report.md`

| Metric            | Value |
| ----------------- | ----: |
| Cases selected    |    10 |
| Expected findings |    14 |
| Evaluated claims  |    14 |
| True positives    |     0 |
| False positives   |     0 |
| False negatives   |    14 |
| Precision         | 0.000 |
| Recall            | 0.000 |
| F1                | 0.000 |

Per-category detail:

| Category      | Cases | Expected | Claims | Precision | Recall |    F1 |
| ------------- | ----: | -------: | -----: | --------: | -----: | ----: |
| nss-count     |     2 |        0 |      0 |     0.000 |  0.000 | 0.000 |
| nss-file-list |     8 |       14 |     14 |     0.000 |  0.000 | 0.000 |

The first ten pinned DFIR-Metric rows include two upstream rows with empty `answer` arrays. They are retained in the subset count, but contribute 0 expected findings and 0 evaluated claims. The 14 non-empty expected answers are used only by the scorer; the evidence scaffolds and trace claims withhold answer values, so no answer is confirmed without recovered artifact proof.

### 4. CFReDS Hacking Case Pilot

Implemented path:

```console
$ node scripts/fetch-cfreds-hacking-case.mjs
$ node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --trace .kelpclaw/findevil/cfreds-hacking-case/triage/trace.jsonl \
  --max-iterations 3 \
  --timestamp skip \
  --out .kelpclaw/findevil/sentinel-cfreds-hacking-case
```

The pilot manifest scores 8 artifact-backed findings from the official 31-question worksheet: EWF acquisition MD5, Windows XP, registered owner, computer/domain identity, Mr. Evil account/profile, Look@LAN identity config, hacking-tool indicators, and the Interception captured-traffic artifact. Do not report this as a completed metric until `.kelpclaw/findevil/sentinel-cfreds-hacking-case/accuracy-report.md` exists from the SIFT VM run.

## Firewall Corpus

Phase 12B rerun: `pnpm --filter @kelpclaw/findevil exec vitest run test/firewall-corpus.test.ts`

| Category           | Blocked | Total malicious/control rows | Block rate | False positives | False negatives |
| ------------------ | ------: | ---------------------------: | ---------: | --------------: | --------------: |
| direct-imperative  |      10 |                           10 |      1.000 |               0 |               0 |
| encoded            |       9 |                            9 |      1.000 |               0 |               0 |
| json-instruction   |       9 |                            9 |      1.000 |               0 |               0 |
| legitimate-quote   |       0 |                            9 |      0.000 |               0 |               0 |
| prompt-injection   |      10 |                           10 |      1.000 |               0 |               0 |
| unicode-confusable |       8 |                            8 |      1.000 |               0 |               0 |

Across malicious categories, the firewall blocked 46 of 46 payloads for a malicious-corpus block rate of 1.000. The 9 legitimate-quote controls remained allowed, with 0 false positives and 0 false negatives.

## MCP Constraint Surface

`kelp-claw findevil mcp` is the purpose-built MCP server for the hackathon's architecture pattern 2. It exposes only:

- `findevil.case_inventory`
- `findevil.hash_evidence_file`
- `findevil.get_partition_table`
- `findevil.list_filesystem`
- `findevil.extract_file_by_inode`
- `findevil.search_recovered_artifacts`

The server does not expose arbitrary shell, file writes, deletes, package installation, or remount operations. Unit tests cover tool listing, path-escape rejection, and Sleuth Kit command allowlisting.

## Determinism

Phase 12C rerun: `pnpm --filter @kelpclaw/findevil exec vitest run test/determinism.test.ts`

Deterministic replay mode logged the same repaired claim-ledger hash twice:

```text
sha256:8f99da2da7cb45a9e28d0c6db0c89fe6d08cbcf36fa0d2a710cd9552a10ee666
```

The deterministic test seeds the fixture extractor cache, runs `runSentinel` twice with `deterministic: true`, and asserts the two `computeClaimLedgerHash` values match.

## Audit And Timestamp Anchors

Both sentinel anchors emitted RFC3161 timestamp-response files in their audit bundles:

| Run       | TSA token                                                                  |
| --------- | -------------------------------------------------------------------------- |
| Synthetic | `.kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.tsr` |
| CFReDS    | `.kelpclaw/findevil/sentinel-cfreds/audit-bundle/evidence-manifest.tsr`    |

Reviewer verification command:

```console
$ openssl ts -verify -in audit-bundle/evidence-manifest.tsr \
  -data audit-bundle/evidence-manifest.json \
  -CAfile freetsa-cacert.pem
```
