# KelpClaw

KelpClaw is a reproducible AppSec agent harness. It wraps AI-assisted security triage with scoped execution, policy gates, passive scanner evidence, SARIF output, replayable logs, and signed audit bundles.

The goal is not to be an autonomous exploit bot. KelpClaw is built for source and container owners who want AI-assisted vulnerability triage that can be reviewed, reproduced, and handed to security teams without trusting an opaque chat transcript.

## What It Does

- Builds a declared Dockerfile target and records build metadata.
- Imports scanner outputs from SARIF, Nuclei, ZAP, Nmap, Burp, and Nessus.
- Runs a scoped AppSec triage assistant through `--agent-command`.
- Forbids exploit execution by default under `appsec-agent-baseline`.
- Emits normalized findings, SARIF, logs, policy decisions, and signed evidence.
- Produces a static audit bundle that can be opened without running KelpClaw.

## Quickstart

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm --filter @kelpclaw/cli build
$ pnpm --filter @kelpclaw/cli exec kelp-claw doctor
```

Create an agent command that reads `KELPCLAW_APPSEC_INPUT` and writes JSON to `KELPCLAW_APPSEC_OUTPUT`, then run:

```console
$ kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command ./appsec-agent.sh \
  --sarif findings.sarif \
  --out .kelpclaw/appsec/local
```

Verify the signed bundle:

```console
$ kelp-claw verify-audit-bundle .kelpclaw/appsec/local/audit-bundle
```

See [`docs/appsec-harness.md`](docs/appsec-harness.md) for the agent I/O contract and evidence layout.

## Existing Primitives

- `packages/evidence`: evidence workspaces, passive scanner imports, QA, signing, and verification.
- `packages/policy`: policy evaluator and built-in policy packs including `appsec-agent-baseline`.
- `packages/agent-hooks`: agent tool-event normalization.
- `packages/nanoclaw`: deterministic execution, Docker runner, replay data, hashes, and policy decisions.
- `packages/codegen`: artifact store and replay helpers.
- `packages/cli`: AppSec audit, skill audit, evidence, policy, inventory, SARIF, and audit bundle commands.
- `packages/workflow-spec`: shared workflow IR types, schemas, fixtures, and validators.
- `packages/testing`: deterministic harnesses and regression fixtures.

## CLI Surface

```console
$ kelp-claw help
$ kelp-claw appsec audit --context . --dockerfile Dockerfile --agent-command ./appsec-agent.sh
$ kelp-claw evidence import-sarif findings.sarif --workspace .kelpclaw/evidence
$ kelp-claw inventory scan --root . --policy appsec-agent-baseline
$ kelp-claw export-audit-bundle <runId> --include-sarif
```

## Safety Boundary

KelpClaw v1 does not execute exploits. Scanner execution can be done outside KelpClaw and imported as evidence. The AppSec assistant can correlate evidence and recommend validation, but active exploit validation is intentionally outside the default harness.

## Developer Setup

```console
$ pnpm -r build
$ pnpm -r test
```

## License

MIT. See [`LICENSE`](LICENSE).
