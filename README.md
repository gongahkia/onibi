# KelpClaw SIFT Sentinel

## What This Is

KelpClaw SIFT Sentinel turns Protocol SIFT output into a defensible autonomous DFIR record: claim-to-evidence verification, hostile-evidence firewall, spoliation check, ATT&CK Navigator export, deterministic replay, RFC3161 timestamping, and a signed audit trail.

KelpClaw is the verification and containment harness around Claude Code and Protocol SIFT for the SANS Find Evil! hackathon. Claude Code provides the agentic framework. Protocol SIFT provides the SIFT Workstation bridge. KelpClaw makes the agent's incident-response output reviewable by checking claims against evidence, blocking evidence-borne instructions, proving original evidence hashes still match, and preserving a signed audit trail.

Public repository: https://github.com/gongahkia/kelp-claw

Demo video artifact: `SUBMISSION/kelpclaw-sift-sentinel-demo.mp4`

## v3 Benchmark Anchors

| Anchor | Output | Precision | Recall | F1 |
|---|---|---:|---:|---:|
| Synthetic Sentinel | `.kelpclaw/findevil/sentinel-synthetic/` | 1.000 | 0.500 | 0.667 |
| CFReDS Forensics Image Test | `.kelpclaw/findevil/sentinel-cfreds/` | 0.000 | 0.000 | 0.000 |
| DFIR-Metric subset-10 | `.kelpclaw/findevil/benchmark/dfir-metric/` | 1.000 | 1.000 | 1.000 |

The CFReDS score is intentionally conservative: the container anchor verifies the pinned E01 and emits 25 official worksheet prompts, but confirms none without recovered artifact proof.

## What Is Novel In This Submission

The following work is scoped as post-2026-04-15 hackathon contribution:

- `packages/findevil/` - claim schema, report extraction, evidence linking, verifier rules, repair prompts, taint tracking, instruction firewall, spoliation guard, benchmark scoring, ATT&CK tagging, Sigma mapping, Navigator export, deterministic replay, and TSA timestamping.
- `examples/findevil-sift-sentinel/` - runnable synthetic case with hostile-evidence fixtures and demo commands.
- `examples/findevil-cfreds-image-test/` - public CFReDS image manifest and expected worksheet findings.
- `fixtures/protocol-sift-baseline/` - captured Protocol SIFT-style baseline output for repeatable offline judging and regression tests.
- `Dockerfile.kelp` - packaged v3 CLI runtime.
- `.github/workflows/ci.yml` - CI build and test verification.

These pieces are documented separately from the pre-existing governance foundation.

## What Pre-Existed

These packages pre-date the Find Evil work and are retained as the foundation allowed by the hackathon rules:

- `packages/evidence` - Ed25519 signing, audit bundles, attestation profiles, and evidence workspace verification.
- `packages/policy` - policy evaluator and policy-pack machinery.
- `packages/agent-hooks` - Claude Code hook normalization into structured JSONL events.
- `packages/nanoclaw` - run manifests, replay data, per-node IO, hashes, and policy decisions.
- `packages/codegen` - content-addressed SHA-256 artifact store and replay policy helpers.
- `packages/cli` - `kelp-claw` command surface for audit bundles, replay diff, evidence, policy, and governance workflows.
- `packages/workflow-spec` - shared workflow IR types, schemas, fixtures, and validators.
- `packages/testing` - deterministic harnesses and regression fixtures.
- `packages/adapters/src/mcp-adapter.ts` - retained MCP adapter used at the Protocol SIFT client boundary.

## Try It Out

Build the local container:

```console
$ docker build -f Dockerfile.kelp -t kelp:v3 .
```

Run the synthetic sentinel anchor:

```console
$ docker run --rm \
  -v "$PWD/.kelpclaw/findevil/sentinel-synthetic:/data/out" \
  kelp:v3 findevil sentinel \
  --case /app/examples/findevil-sift-sentinel/case.yml \
  --evidence-root /app/examples/findevil-sift-sentinel/case-data \
  --trace /app/fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out /data/out
```

Run the DFIR-Metric subset-10 anchor:

```console
$ mkdir -p .kelpclaw/datasets/dfir-metric .kelpclaw/findevil/benchmark/dfir-metric
$ docker run --rm \
  -v "$PWD/.kelpclaw:/app/.kelpclaw" \
  -v "$PWD/.kelpclaw/findevil/benchmark/dfir-metric:/data/out" \
  kelp:v3 findevil benchmark \
  --dataset dfir-metric \
  --subset-size 10 \
  --out /data/out
```

Verify the reviewer bundle:

```console
$ ./node_modules/.bin/kelp-claw verify-audit-bundle \
  .kelpclaw/findevil/sentinel-synthetic/audit-bundle --profile reviewer
```

Verify the RFC3161 timestamp token:

```console
$ openssl ts -verify \
  -in .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.tsr \
  -content .kelpclaw/findevil/sentinel-synthetic/audit-bundle/evidence-manifest.json \
  -CAfile freetsa-cacert.pem
```

Drag `.kelpclaw/findevil/sentinel-synthetic/attack-navigator-layer.json` into https://mitre-attack.github.io/attack-navigator/ to inspect technique coverage.

## Developer Setup

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm -r build
$ pnpm -r test
```

For deterministic replay:

```console
$ pnpm --filter @kelpclaw/findevil exec vitest run test/determinism.test.ts
```

Expected hash:

```text
sha256:8f99da2da7cb45a9e28d0c6db0c89fe6d08cbcf36fa0d2a710cd9552a10ee666
```

## Configuration

Set `KELP_FINDEVIL_MODELS` to a comma-separated committee to make claim extraction require cross-model agreement. Entries use `provider:model` and may include an optional weight suffix. Supported providers are `anthropic`, `openai`, `openai-azure`, and `gemini`.

Provider environment:

- Anthropic: `ANTHROPIC_API_KEY`
- OpenAI hosted endpoint: `OPENAI_API_KEY`
- Azure OpenAI: `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_DEPLOYMENT`
- Gemini: `GOOGLE_API_KEY`

Claude Code remains the agentic framework for the hackathon submission. The multi-vendor committee uses other providers only for claim-extraction voting, not for tool execution.

## License

MIT. See `LICENSE`.
