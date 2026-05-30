# Novel Contribution

This table separates pre-existing KelpClaw foundation code from work created for the Find Evil hackathon window, 2026-04-15 through 2026-06-15. Public repository: `https://github.com/gongahkia/kelp-claw`.

| Pre-existing KelpClaw subsystem | Find Evil work created during 2026-04-15 to 2026-06-15 |
|---|---|
| Evidence signing and audit primitives: `packages/evidence/src/index.ts` | Sentinel audit export that packages Find Evil outputs: `packages/findevil/src/sentinel/index.ts` |
| Policy evaluator and pack machinery: `packages/policy/src/evaluator.ts`, `packages/policy/src/packs.ts` | DFIR spoliation policy pack: `packages/policy/src/packs/dfir-spoliation-strict.ts` |
| Claude Code hook normalization: `packages/agent-hooks/src/index.ts` | Find Evil agent trace normalization and sentinel CLI wiring: `packages/cli/src/findevil/sentinel.ts` |
| Run replay and manifest foundation: `packages/nanoclaw/src/replay.ts`, `packages/nanoclaw/src/types.ts` | Repair loop and repair trace for unsupported claims: `packages/findevil/src/repair/loop.ts` |
| SHA-256 artifact and storage helpers: `packages/codegen/src/artifacts.ts`, `packages/codegen/src/storage.ts` | Evidence hashing and spoliation check: `packages/findevil/src/spoliation/index.ts`, `packages/findevil/src/spoliation/hashing.ts` |
| Shared schema and stable JSON helpers: `packages/workflow-spec/src/schema.ts`, `packages/workflow-spec/src/stable-json.ts` | Claim schema and claim status taxonomy: `packages/findevil/src/types/claim.ts` |
| Existing CLI command framework: `packages/cli/src/index.ts` | New `findevil` subcommands: `packages/cli/src/findevil/index.ts`, `packages/cli/src/findevil/verify.ts`, `packages/cli/src/findevil/firewall.ts`, `packages/cli/src/findevil/sentinel.ts` |
| Existing test harness package: `packages/testing/src/harness.ts` | Find Evil verifier, firewall, taint, repair, spoliation, and integration tests: `packages/findevil/test/sentinel.integration.test.ts`, `packages/findevil/test/verifier-rules.test.ts`, `packages/findevil/test/firewall-policy.test.ts`, `packages/findevil/test/spoliation.test.ts` |
| Pre-existing policy pack index structure: `packages/policy/src/packs/index.ts` | Tainted-instruction firewall pack: `packages/policy/src/packs/tainted-instruction-block.ts` |
| No prior DFIR example case in the retained repo foundation | Synthetic Find Evil case and offline SIFT fixture: `examples/findevil-sift-sentinel/case.yml`, `examples/findevil-sift-sentinel/case-data/timeline.csv`, `fixtures/protocol-sift-baseline/baseline.jsonl`, `fixtures/protocol-sift-baseline/baseline-report.md` |

## Phase 7 And v2 Files

These v2 files are new Find Evil contribution files and are called out because they implement the ATT&CK, benchmark, committee, live SIFT, and reviewer-UI features cited in the submission:

| Feature area | New file |
|---|---|
| MITRE ATT&CK tagging | `packages/findevil/src/attack/catalog.ts` |
| MITRE ATT&CK tagging | `packages/findevil/src/attack/index.ts` |
| Ground-truth benchmark scoring | `packages/findevil/src/benchmark/benchmark.ts` |
| Ground-truth benchmark scoring | `packages/findevil/src/benchmark/scorer.ts` |
| Ground-truth benchmark scoring | `packages/findevil/src/benchmark/types.ts` |
| Multi-model committee extraction | `packages/findevil/src/extractor/committee.ts` |
| Static reviewer UI | `packages/findevil/src/sentinel/reviewer-html.ts` |
| Live Protocol SIFT runner | `packages/findevil/src/sentinel/sift-runner.ts` |

## Phase 6 Artifact Linkers

The v2 artifact-linker expansion also added or extended these Find Evil-specific linkers:

- `packages/findevil/src/linker/sysmon.ts` for Sysmon Event IDs 1, 3, 11, and 13.
- `packages/findevil/src/linker/eventlog.ts` for Security/System EVTX-style JSON records, including Event IDs 4688, 4624, 4625, 4698, 4702, and 7045.
- `packages/findevil/src/linker/shimcache.ts` for ShimCache CSV/JSON-style rows.
- `packages/findevil/src/linker/srum.ts` for SRUM network-activity rows.
- `packages/findevil/src/linker/pcap.ts` for PCAP, flow-summary, and Zeek-style flow records.

The hackathon-specific contribution is the DFIR sentinel behavior: claim-to-evidence verification, ATT&CK tagging, benchmark scoring, targeted repair, optional committee verification, hostile-evidence taint containment, spoliation checking, Find Evil CLI commands, live SIFT integration, reviewer UI, demo evidence, fixture traces, and submission documentation.
