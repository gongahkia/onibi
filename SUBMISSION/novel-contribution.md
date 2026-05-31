# Novel Contribution

Public repository: `https://github.com/gongahkia/kelp-claw`

This document separates pre-existing KelpClaw foundation code from Find Evil hackathon work created during 2026-04-15 through 2026-06-15.

## Pre-Existing Foundation

| Pre-existing subsystem | Reused in Find Evil submission |
|---|---|
| `packages/evidence` | Ed25519 audit-bundle signing and reviewer verification |
| `packages/policy` | Policy evaluator and policy-pack machinery |
| `packages/agent-hooks` | Claude Code hook normalization into JSONL |
| `packages/nanoclaw` | Run manifest and replay concepts |
| `packages/codegen` | SHA-256 content-addressed artifact helpers |
| `packages/cli` | Existing command framework extended with `findevil` |
| `packages/workflow-spec` | Stable JSON helpers and shared schema style |
| `packages/testing` | Existing deterministic test harness patterns |

## Find Evil-Specific v3 Files

The following files are the v3 contribution surface called out by the submission.

### Artifact Linkers

- `packages/findevil/src/linker/amcache.ts`
- `packages/findevil/src/linker/eventlog.ts`
- `packages/findevil/src/linker/hashing.ts`
- `packages/findevil/src/linker/index.ts`
- `packages/findevil/src/linker/memory.ts`
- `packages/findevil/src/linker/mft.ts`
- `packages/findevil/src/linker/pcap.ts`
- `packages/findevil/src/linker/prefetch.ts`
- `packages/findevil/src/linker/registry.ts`
- `packages/findevil/src/linker/shimcache.ts`
- `packages/findevil/src/linker/srum.ts`
- `packages/findevil/src/linker/sysmon.ts`
- `packages/findevil/src/linker/timeline.ts`
- `packages/findevil/src/linker/yara.ts`

### Extractor Providers

- `packages/findevil/src/extractor/providers/anthropic.ts`
- `packages/findevil/src/extractor/providers/azure.ts`
- `packages/findevil/src/extractor/providers/gemini.ts`
- `packages/findevil/src/extractor/providers/openai.ts`
- `packages/findevil/src/extractor/providers/shared.ts`

### Benchmark

- `packages/findevil/src/benchmark/benchmark.ts`
- `packages/findevil/src/benchmark/dfir-metric.ts`
- `packages/findevil/src/benchmark/scorer.ts`
- `packages/findevil/src/benchmark/types.ts`

### Sigma

- `packages/findevil/src/sigma/index.ts`

### MITRE ATT&CK

- `packages/findevil/src/attack/catalog.ts`
- `packages/findevil/src/attack/index.ts`
- `packages/findevil/src/attack/navigator-layer.ts`

### Determinism

- `packages/findevil/src/sentinel/determinism.ts`

### RFC3161 Timestamping

- `packages/findevil/src/evidence/tsa.ts`

### Distribution And CI

- `Dockerfile.kelp`
- `.github/workflows/ci.yml`

## v3 Runtime Evidence

The v3 rerun produced three benchmark anchors:

| Anchor | Output | Precision | Recall | F1 |
|---|---|---:|---:|---:|
| Synthetic Sentinel | `.kelpclaw/findevil/sentinel-synthetic/` | 1.000 | 0.500 | 0.667 |
| CFReDS Forensics Image Test | `.kelpclaw/findevil/sentinel-cfreds/` | 0.000 | 0.000 | 0.000 |
| DFIR-Metric subset-10 | `.kelpclaw/findevil/benchmark/dfir-metric/` | 1.000 | 1.000 | 1.000 |

The novel behavior is not a generic wrapper around SIFT. It is the verification and containment layer: claim-to-evidence rules, ATT&CK tagging, Sigma/Navigator export, targeted repair, taint-aware firewalling, spoliation checks, deterministic claim-ledger hashing, RFC3161 timestamping, Docker packaging, and signed reviewer bundles.
