# KelpClaw SIFT Sentinel

## What This Is

KelpClaw SIFT Sentinel turns Protocol SIFT output into a defensible autonomous DFIR record: claim-to-evidence verification, hostile-evidence firewall, spoliation check, and signed audit trail.

KelpClaw is the verification and containment harness around Claude Code and Protocol SIFT for the SANS Find Evil! hackathon. Claude Code provides the required agentic framework. Protocol SIFT provides the SIFT Workstation MCP bridge. KelpClaw makes the agent's incident-response output defensible by checking claims against evidence, blocking evidence-borne instructions, proving original evidence hashes still match, and preserving a signed audit trail.

Public repository: https://github.com/gongahkia/kelp-claw

Demo video artifact: `SUBMISSION/kelpclaw-sift-sentinel-demo.mp4`

## What Is Novel In This Submission

The following work is scoped as post-2026-04-15 hackathon contribution:

- `packages/findevil/` - Phase 1 package for claim schema, report extraction, evidence linking, verifier rules, repair prompts, taint tracking, instruction firewall, and spoliation guard.
- `examples/findevil-sift-sentinel/` - Phase 1 runnable SIFT Sentinel example with case manifest, hostile-evidence fixtures, and demo commands.
- `fixtures/protocol-sift-baseline/` - Phase 1 captured Protocol SIFT baseline output for repeatable offline judging and regression tests.
- `packages/findevil/src/attack/` - Phase 7 MITRE ATT&CK tagging for claims.
- `packages/findevil/src/benchmark/` - Phase 7 ground-truth scoring with precision, recall, and F1.
- `packages/findevil/src/extractor/committee.ts` - Phase 7 optional multi-model committee claim extraction.
- `packages/findevil/src/sentinel/reviewer-html.ts` - Phase 7 static reviewer UI for the signed audit bundle.
- `packages/findevil/src/sentinel/sift-runner.ts` - Phase 5 live Protocol SIFT command runner.
- `packages/policy` policy pack `dfir-spoliation-strict` - Phase 1 policy pack for blocking writes into evidence roots.
- `packages/policy` policy pack `tainted-instruction-block` - Phase 1 policy pack for blocking hostile case-derived instructions from becoming tool arguments.

These pieces are intentionally documented as new work so the submission separates the Find Evil contribution from the pre-existing governance foundation.

## What Pre-Existed

These packages pre-date the Find Evil work and are retained as the foundation allowed by the hackathon rules:

- `packages/evidence` - Ed25519 signing, audit bundles, attestation profiles, and evidence workspace verification.
- `packages/policy` - policy evaluator and mature policy-pack machinery.
- `packages/agent-hooks` - Claude Code hook normalization into structured JSONL events.
- `packages/nanoclaw` - run manifests, replay data, per-node IO, hashes, and policy decisions.
- `packages/codegen` - content-addressed SHA256 artifact store and replay policy helpers.
- `packages/cli` - `kelp-claw` command surface for audit bundles, replay diff, evidence, policy, and governance workflows.
- `packages/workflow-spec` - shared workflow IR types, schemas, fixtures, and validators.
- `packages/testing` - deterministic harnesses and regression fixtures.
- `packages/adapters/src/mcp-adapter.ts` - the retained MCP adapter used as the Protocol SIFT client boundary.

The workflow editor, API server, web-intelligence package, skill registry, SaaS adapters, and MCP web gateway have been shelved under `legacy/` so the repository presents a DFIR CLI submission instead of a general workflow product.

## Try It Out

These commands were run against the current repository state. They use the deterministic offline Protocol SIFT-style fixture and write fresh outputs to `/tmp/kelpclaw-findevil-sentinel` so the canonical `.kelpclaw/findevil/sentinel/` run remains available for review.

Equivalent invocation: `pnpm exec kelp-claw ...`.

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm -r --if-present build
$ rm -rf /tmp/kelpclaw-findevil-sentinel
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out /tmp/kelpclaw-findevil-sentinel
$ sed -n '1,80p' /tmp/kelpclaw-findevil-sentinel/accuracy-report.md
$ jq '{ok, checkedAt, changed:(.changed|length), added:(.added|length), removed:(.removed|length)}' /tmp/kelpclaw-findevil-sentinel/spoliation-check.json
$ wc -l /tmp/kelpclaw-findevil-sentinel/{agent-execution,committee-vote,repair-trace,firewall-events,taint-ledger}.jsonl
$ test -s /tmp/kelpclaw-findevil-sentinel/accuracy-report.md && test -s /tmp/kelpclaw-findevil-sentinel/audit-bundle/index.html
$ ./node_modules/.bin/kelp-claw verify-audit-bundle /tmp/kelpclaw-findevil-sentinel/audit-bundle --profile reviewer
```

Expected high-level result:

- The sentinel command returns `ok: true`, `status: "succeeded"`, `policyDenials: 1`, and `uncorrectedPolicyDenials: 0`.
- The accuracy report shows 10 baseline claims, 10 repaired claims, 11 repair prompts, 11 repair results, 5 successful status changes, and 1 firewall block.
- The benchmark table shows 10 expected findings, 10 evaluated claims, 3 true positives, 0 false positives, 7 false negatives, precision 1.000, recall 0.300, and F1 0.462.
- The ATT&CK table covers T1003, T1021, T1059, T1071, T1204, and T1547.
- The spoliation check shows `ok: true` with 13 files before, 13 files after, and zero changed, added, or removed files.
- The audit-bundle verification returns `ok: true` with a valid reviewer signature and 18 checked files.

Drag `.kelpclaw/findevil/sentinel/attack-navigator-layer.json` into https://mitre-attack.github.io/attack-navigator/ to see the technique coverage map.

## Configuration

### Multi-vendor committee

Set `KELP_FINDEVIL_MODELS` to a comma-separated committee to make claim extraction require cross-model agreement. Entries use `provider:model` and may include an optional weight suffix. Supported providers are `anthropic`, `openai`, `openai-azure`, and `gemini`.

Provider environment:

- Anthropic: `ANTHROPIC_API_KEY`
- OpenAI hosted endpoint: `OPENAI_API_KEY`
- Azure OpenAI: `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_DEPLOYMENT`
- Gemini: `GOOGLE_API_KEY`

When `KELP_FINDEVIL_MODELS` is unset and at least one provider is configured, extraction uses the configured subset of the default committee: `anthropic:claude-opus-4-7`, `openai-azure:gpt-5`, and `gemini:gemini-2.5-pro`. If only one provider is configured, the extractor falls back to that single model.

```console
$ KELP_FINDEVIL_MODELS=anthropic:claude-opus-4-7,openai-azure:gpt-5,gemini:gemini-2.5-pro \
  ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out /tmp/kelpclaw-findevil-sentinel
```

The run writes `committee-vote.jsonl` beside the claim ledger with one row per model vote, then lowers confidence or marks claims inferred/unverifiable when the committee disagrees.

Claude Code remains the agentic framework for the hackathon submission. The multi-vendor committee uses other providers only for claim-extraction voting, not for tool execution.

## Development

```console
$ corepack enable
$ pnpm install
$ pnpm -r --filter '!./legacy/**' build
$ pnpm test
```

The active workspace is intentionally limited to the retained DFIR foundation packages. Shelved code remains in `legacy/` for provenance but is no longer part of the pnpm workspace.

## License

MIT. See `LICENSE`.
