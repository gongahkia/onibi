# KelpClaw SIFT Sentinel

## What This Is

KelpClaw SIFT Sentinel turns Protocol SIFT output into a defensible autonomous DFIR record: claim-to-evidence verification, hostile-evidence firewall, spoliation check, and signed audit trail.

KelpClaw is the verification and containment harness around Claude Code and Protocol SIFT for the SANS Find Evil! hackathon. Claude Code provides the required agentic framework. Protocol SIFT provides the SIFT Workstation MCP bridge. KelpClaw makes the agent's incident-response output defensible by checking claims against evidence, blocking evidence-borne instructions, proving original evidence hashes still match, and preserving a signed audit trail.

## What Is Novel In This Submission

The following work is scoped as post-2026-04-15 hackathon contribution:

- `packages/findevil/` - Phase 1 package for claim schema, report extraction, evidence linking, verifier rules, repair prompts, taint tracking, instruction firewall, and spoliation guard.
- `examples/findevil-sift-sentinel/` - Phase 1 runnable SIFT Sentinel example with case manifest, hostile-evidence fixtures, and demo commands.
- `fixtures/protocol-sift-baseline/` - Phase 1 captured Protocol SIFT baseline output for repeatable offline judging and regression tests.
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

These commands were run against the current repository state. They use the deterministic offline Protocol SIFT-style fixture and write fresh outputs to `/tmp/kelpclaw-findevil-sentinel` so the committed `.kelpclaw/findevil/sentinel/` run stays unchanged for review.

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
$ wc -l /tmp/kelpclaw-findevil-sentinel/{agent-execution,repair-trace,firewall-events,taint-ledger}.jsonl
$ test -s /tmp/kelpclaw-findevil-sentinel/accuracy-report.md && test -s /tmp/kelpclaw-findevil-sentinel/audit-bundle/index.html
$ ./node_modules/.bin/kelp-claw verify-audit-bundle /tmp/kelpclaw-findevil-sentinel/audit-bundle --profile reviewer
```

Expected high-level result:

- The sentinel command returns `ok: true`, `status: "succeeded"`, `policyDenials: 1`, and `uncorrectedPolicyDenials: 0`.
- The accuracy report shows one baseline claim, one repaired claim, one repair prompt, one repair result, one successful status change, and one firewall block.
- The spoliation check shows `ok: true` with zero changed, added, or removed files.
- The audit-bundle verification returns `ok: true` with a valid reviewer signature and thirteen checked files.

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
