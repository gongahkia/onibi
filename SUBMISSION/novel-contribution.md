# Novel Contribution

This table separates pre-existing KelpClaw foundation code from work created for the Find Evil hackathon window, 2026-04-15 through 2026-06-15. All cited paths exist in the Phase 3 repository state.

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

The hackathon-specific contribution is the DFIR sentinel behavior: claim-to-evidence verification, targeted repair, hostile-evidence taint containment, spoliation checking, Find Evil CLI commands, demo evidence, fixture traces, and submission documentation.
