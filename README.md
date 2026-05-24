<p align="center">
  <a href="https://github.com/gongahkia/piranesi">
    <img alt="Piranesi" src="asset/logo/imaginary-prisons.jpg" width="180">
  </a>
</p>

KelpClaw is an Agent Skill Governance Framework with policy, sandboxing, replay, evidence, and audit.

Its core adoption path is simple: run any `SKILL.md`, evaluate it against policy packs, capture replayable execution evidence, and export a static audit bundle that security, compliance, and platform teams can review without running KelpClaw.

KelpClaw remains the editable workflow planner. The frontend now opens on Skill Governance for `SKILL.md` compatibility, policy, replay, and audit-bundle handoff; the Workflow Graph mode is still available for drag-and-drop workflow composition. NanoClaw remains the deterministic runtime that compiles approved workflow revisions and executes nodes through a Docker-per-node contract. The Piranesi-derived code is used as KelpClaw's local evidence subsystem, not as a separate product direction.

## Workspace Layout

| Workspace                 | Ownership                                                                                 |
| ------------------------- | ----------------------------------------------------------------------------------------- |
| `apps/kelpclaw`           | React UI for Skill Governance, replay review, policy editing, and workflow graph planning |
| `apps/api`                | HTTP API for planning, workflow persistence, validation, approval, and execution control  |
| `packages/workflow-spec`  | Shared workflow IR types, Zod schemas, JSON Schema, fixtures, and validation errors       |
| `packages/skill-registry` | Built-in deterministic skills, metadata, metaprompts, and lookup rules                    |
| `packages/nanoclaw`       | DAG compiler, production runner, Docker command runner, and deterministic test runner     |
| `packages/codegen`        | Generated artifact contracts, checksums, and replay policy helpers                        |
| `packages/adapters`       | Live provider adapters, generic connectors, and deterministic test mocks                  |
| `packages/web-intel`      | Governed Exa/TinyFish web search, fetch, answer, browser, and evidence normalization      |
| `packages/evidence`       | Piranesi-derived local evidence vault, normalized findings, custody, QA, and retest diff  |
| `packages/testing`        | Shared fixtures, mock providers, and deterministic execution harnesses                    |

## Development

Quality gates used for Phase 1 changes:

```bash
uv run python scripts/validate_pentest_fixtures.py
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/piranesi/
uv run pytest -q -m "not integration and not joern and not docker and not e2e and not slow"
```

When CLI help changes, update and review the contract snapshot:

```console
$ pnpm --filter @kelpclaw/api test
$ pnpm --filter @kelpclaw/kelpclaw dev
$ pnpm --filter @kelpclaw/workflow-spec test
$ pnpm validate:fixtures
$ pnpm check:codegen-policy
```

Quickstart, deployment notes for durable SQLite mode, Docker Compose, and production readiness live in
[`docs/quickstart.md`](docs/quickstart.md),
[`docs/deployment.md`](docs/deployment.md),
[`docs/agent-runtime-demo.md`](docs/agent-runtime-demo.md),
[`docs/skill-governance-demo.md`](docs/skill-governance-demo.md),
[`docs/security-review-demo.md`](docs/security-review-demo.md),
[`docs/agent-inventory.md`](docs/agent-inventory.md),
[`docs/piranesi-integration.md`](docs/piranesi-integration.md),
[`docs/web-intel.md`](docs/web-intel.md),
[`docs/product-hardening-roadmap.md`](docs/product-hardening-roadmap.md), and
[`docs/production-readiness.md`](docs/production-readiness.md).

## Workflow V1 Model

KelpClaw uses the canonical workflow JSON IR with `schemaVersion: "1.0.0"`. The top-level workflow fields are `id`, `schemaVersion`, `name`, `prompt`, `revision`, `nodes`, `edges`, `approval`, `createdAt`, and `updatedAt`.

Workflow nodes use `kind` instead of the earlier planner `type` shape. Supported node kinds are `trigger`, `skill`, `codegen`, `transform`, `approval`, and `delivery`. Each node declares a human description, JSON-Schema-compatible input and output ports, config, runtime settings, and determinism metadata.

Edges are port-aware: each edge connects `source.nodeId/source.port` to `target.nodeId/target.port`. Validation reports stable error codes for duplicate nodes, missing node references, invalid ports, DAG cycles, unapproved execution, unsupported schema versions, and missing codegen provenance or replay metadata.

Canonical serialization keeps object keys and collections stable for snapshots, review diffs, and DAG hashing. The migration harness currently passes through v1 workflows and rejects unsupported schema versions so future IR upgrades can be added without changing callers.

## Approval And Execution

Approving a workflow freezes the current revision into `workflow.approval`, including the approver, approval timestamp, frozen DAG hash, and compiled node order. NanoClaw compiles only approved workflow revisions and emits a v1 `execution_result` envelope for both mock and Docker-backed runners.

Editing an approved workflow creates a new draft revision. Execution remains blocked until that current revision is approved.

## NanoClaw Runtime Controls

API runs use the production runner by default. Adapter nodes invoke canonical live adapters (`adapter.gmail`, `adapter.sheets`, `adapter.email`, `adapter.whatsapp`, `adapter.telegram`, `adapter.github`, `adapter.slack`, `adapter.discord`, `adapter.notion`, `adapter.linear`, `adapter.jira`, `adapter.airtable`, `adapter.webhook`), deterministic built-in nodes run in-process, and custom/codegen nodes fall back to Docker. Set `NANOCLAW_RUNNER=mock` only for tests and offline demos. Optional controls are `NANOCLAW_DOCKER_BIN` for a non-default Docker binary and `NANOCLAW_HOST_WORKSPACE` for command-construction compatibility.

NanoClaw writes each run under a preserved workspace in the OS temp directory unless callers pass `workspaceRoot`. The workspace contains `workflow.json`, per-node `input.json` and `output.json`, `stdout.log`, `stderr.log`, an `artifacts/` directory, and `run-manifest.json` for replay.

Docker nodes receive only declared runtime environment variables plus NanoClaw paths:

- `NANOCLAW_WORKFLOW_SPEC`
- `NANOCLAW_NODE_INPUT`
- `NANOCLAW_NODE_OUTPUT`
- `NANOCLAW_ARTIFACTS_DIR`
- `NANOCLAW_NODE_ID`
- `NANOCLAW_ATTEMPT`

Containers mount the frozen workflow spec read-only and the node attempt workspace read-write. Network mode is `none` unless the node declares adapter or external API access. CPU, memory, timeout, retry count, backoff, logs, artifacts, attempts, skipped downstream nodes, and replay metadata are captured in the execution result.

## Skill Registry

The built-in skill registry records input and output schemas, required secrets, live adapter dependencies, runtime templates, metaprompts, validation rules, and example fixtures. Deterministic matching returns scored `SkillMatch` results with explainable reasons. Registry skills are preferred over codegen when the top match reaches the fixed reuse threshold.

## SKILL.md Audit Runner

KelpClaw can analyze and run agent skills in an audit-first mode:

```console
$ kelp-claw help
$ kelp-claw version --json
$ kelp-claw doctor
$ kelp-claw demo governance --out .kelpclaw/demo/governance
$ kelp-claw release manifest --out .kelpclaw/release
$ kelp-claw verify-release .kelpclaw/release
$ kelp-claw compat ./SKILL.md --policy baseline
$ kelp-claw policy explain ./SKILL.md --policy baseline
$ kelp-claw governance report ./SKILL.md --region sg --framework agentic-ai --policy sg-agentic-ai-baseline
$ kelp-claw governance controls ./SKILL.md --region sg --framework agentic-ai --out controls.md
$ kelp-claw export-sarif ./SKILL.md --policy baseline --out findings.sarif
$ kelp-claw run-skill ./SKILL.md --input input.json
$ kelp-claw run-skill ./SKILL.md --input input.json --agent codex-cli --wrapper --enforce-policy
$ kelp-claw run-skill github:owner/repo/path/SKILL.md --input input.json
$ kelp-claw governance report <runId> --region sg --framework agentic-ai
$ kelp-claw export-audit-bundle <runId> --include-governance --include-controls --include-sarif --region sg --framework agentic-ai
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/<runId> --strict
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/<runId> --profile reviewer
$ kelp-claw replay-diff --skill ./SKILL.md --agents claude-code,codex-cli,goose
$ kelp-claw replay-diff --recorded --skill ./SKILL.md --input input.json --agents codex-cli,custom-agent
$ kelp-claw web search "Singapore agentic AI governance" --provider exa --policy sg-web-research --out .kelpclaw/web-evidence/sg-ai
$ kelp-claw web fetch https://example.com/source --provider tinyfish --out .kelpclaw/web-evidence/source
$ kelp-claw export-audit-bundle <runId> --include-web-evidence .kelpclaw/web-evidence/sg-ai --include-governance
$ kelp-claw evidence init --workspace .kelpclaw/evidence --client "Example Client" --project "Agent Review"
$ kelp-claw evidence add --workspace .kelpclaw/evidence --file operator-note.txt --kind note --title "Operator note"
$ kelp-claw evidence import-sarif --workspace .kelpclaw/evidence findings.sarif
$ kelp-claw evidence import-nmap --workspace .kelpclaw/evidence nmap.xml
$ kelp-claw evidence import-nuclei --workspace .kelpclaw/evidence nuclei.jsonl
$ kelp-claw evidence sign --workspace .kelpclaw/evidence
$ kelp-claw evidence verify --workspace .kelpclaw/evidence
$ kelp-claw export-audit-bundle <runId> --include-evidence .kelpclaw/evidence --include-governance
$ kelp-claw inventory scan --root . --policy sg-agentic-ai-baseline --out .kelpclaw/inventory/agent-inventory.json
$ kelp-claw inventory graph --root . --format markdown --out .kelpclaw/inventory/permissions.md
$ kelp-claw inventory coverage --root . --format markdown --fail-on high --out .kelpclaw/inventory/coverage.md
```

`help` returns the major workflows and command groups as JSON for CLI, docs, and wrappers. `version --json` reports build/runtime metadata plus policy pack versions. `doctor` checks local readiness for demos and live integrations, including Node.js, writable workspace access, built-in policy packs, Git, optional Codex CLI, and Exa/TinyFish environment configuration. `demo governance` creates a complete local handoff in one command: demo skill, input, evidence workspace, imported SARIF finding, signed governance audit bundle, and strict verification result. `release manifest` writes signed release metadata, a CycloneDX-style SBOM, and SLSA-inspired provenance; `verify-release` checks those hashes and signatures.

`compat` reports detected tools, required secrets, network posture, sandbox profile, and policy findings. `run-skill` writes deterministic local artifacts under `.kelpclaw/runs/<runId>/`, including `skill.json`, `workflow.json`, `bom.json`, `audit.jsonl`, and `policy-decisions.json`. With `--agent codex-cli`, `--agent claude-code`, or `--agent goose`, KelpClaw materializes a temporary workspace, captures stdout/stderr, installs a local hook command for compatible agents, records hook-derived `PreToolUse`/`PostToolUse` events when available, evaluates policy, and stores generated artifact metadata. Planned policy denials block before launch; hook-denied pre-tool events block the run under `--enforce-policy`. `export-audit-bundle` creates a static bundle with an offline reviewer `index.html`, redacts secret-like and email-like content before signing by default, and writes `redaction-report.json`.

`policy explain` shows the exact planned tool steps and policy decisions for a skill. `governance report` emits SG/APAC-oriented evidence for autonomy tier, tool/data/network risk, human approval points, auditability, replay evidence, residual risks, and IMDA Agentic AI-oriented framework mappings. `governance controls` produces a reviewer-facing controls matrix, and `export-sarif` converts policy/governance/web findings into SARIF 2.1.0 for GitHub code scanning and security review. `--wrapper` adds stricter JSONL handling by normalizing Codex/Claude/Goose-style tool events into KelpClaw hook events and failing closed on unclassified enforced tool events. `export-audit-bundle` signs a manifest and attestation with a local Ed25519 key by default; use `kelp-claw audit-key init` to create the key explicitly and `verify-audit-bundle --profile reviewer|regulator|ci` before forwarding the static bundle.

`kelp-claw web` adds governed Exa/TinyFish web intelligence. `search`, `fetch`, `answer`, and `research` evaluate a policy pack before the provider call, normalize sources into KelpClaw web evidence, hash source content, redact obvious secrets and emails, and optionally write `web-evidence.json`, `web-events.jsonl`, `web-bom.json`, and `web-evidence.html`. Set `EXA_API_KEY` and/or `TINYFISH_API_KEY` for live calls. Attach the evidence to `governance report` or `export-audit-bundle` with `--include-web-evidence <dir-or-json>`.

`kelp-claw evidence` ports the useful Piranesi concepts into KelpClaw: a local evidence vault, normalized findings, SARIF/Nmap/Nuclei/Burp/ZAP/Nessus passive imports, append-only audit log, Ed25519-signed chain-of-custody manifest, delivery QA, retest diff, and a static evidence viewer inside audit bundles. Attach it to governance reports or audit bundles with `--include-evidence <workspace>`.

`kelp-claw inventory` scans a repository for SKILL.md files, recorded runs, signed audit bundles, governed web evidence, evidence workspaces, KelpClaw GitHub Action workflows, and MCP web gateways. `inventory graph` renders a permission graph of skills, tools, secrets, policies, bundles, attestations, web evidence, and evidence workspaces; `inventory coverage` reports missing signed bundles, missing attestations, networked skills without web evidence, unsigned evidence workspaces, and CI coverage gaps.

Built-in policy packs are available without writing YAML on day one:

```console
$ kelp-claw policy use baseline
$ kelp-claw policy use finance-sg
$ kelp-claw policy use pii-strict
$ kelp-claw policy use no-destructive-shell
$ kelp-claw policy use github-pr-safe
$ kelp-claw policy use sg-agentic-ai-baseline
$ kelp-claw policy use sg-pdpa-strict
$ kelp-claw policy use sg-financial-ai
$ kelp-claw policy use asean-genai-baseline
$ kelp-claw policy use web-search-safe
$ kelp-claw policy use sg-web-research
$ kelp-claw policy use browser-automation-strict
```

Expose the same governed web gateway to MCP clients with:

```console
$ kelp-claw mcp web-gateway --policy sg-web-research
$ kelp-claw mcp web-gateway --policy browser-automation-strict --allow-browser-tools
```

Use the bundled GitHub Action in PR workflows:

```yaml
- uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
  with:
    skill: ./SKILL.md
    policy: sg-agentic-ai-baseline
    governance: "true"
    region: sg
    framework: agentic-ai
    fail-on-unrunnable: "true"
    upload-sarif: "true"
```

Use repository inventory mode for periodic or PR-level agent estate checks:

```yaml
- uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
  with:
    mode: inventory
    inventory-root: .
    policy: sg-agentic-ai-baseline
    fail-on-coverage: high
```

The compatibility corpus in `fixtures/skills-corpus` contains representative public-style skills and expected reports for regression tests.

The golden demo in `examples/agentic-ai-governance-demo` contains a passing skill, blocked skill, web-evidence skill, replay-diff skill, and sample input for public walkthroughs.

## Auth, Secrets, And Integrations

The API server requires `KELPCLAW_ADMIN_TOKEN` outside test construction. KelpClaw sends it as a Bearer token from its integration panel or `VITE_KELPCLAW_ADMIN_TOKEN`.

Production secrets use encrypted local SQLite storage with `KELPCLAW_SECRET_MASTER_KEY`. Workflow specs store only `secret:<name>` refs; raw values are written through `/api/secrets` or the KelpClaw setup panel and are never returned by list APIs.

Google uses OAuth web flow endpoints under `/api/integrations/google/*`. SMTP email, WhatsApp Cloud API, Telegram Bot API, GitHub, Slack, Discord, Notion, Linear, Jira Cloud, Airtable, generic webhook delivery, and database adapters use encrypted provider secrets. The built-in database runtime supports SQLite directly and exposes a `DatabaseClient` contract for Postgres, MySQL, and other engines. Mock adapters and `.fake` ids remain test helpers only.

## Phase 5 Codegen

`POST /api/workflows/plan` now uses the registry-backed draft planner instead of the legacy fixture mock. The planner checks built-in and promoted skill metadata first. When no deterministic skill reaches the reuse threshold, it creates an explicit codegen node with planner rationale, generated source and dependency artifact references, sandbox policy, review state, and replay metadata.

Live code generation can use the Anthropic Agent SDK, OpenAI Responses API, or an OpenAI-compatible open-weight chat-completions endpoint. Set `KELPCLAW_PLANNER_PROVIDER=anthropic` with `ANTHROPIC_API_KEY`, `KELPCLAW_PLANNER_PROVIDER=openai` with `OPENAI_API_KEY`, or `KELPCLAW_PLANNER_PROVIDER=openweight` with `KELPCLAW_OPENWEIGHT_BASE_URL`. `KELPCLAW_CODEGEN_PROVIDER` can override the planner provider for generated-node build roles. Tests and local deterministic harnesses inject fake generators, so CI does not need live provider credentials.

Generated artifacts are written to a content-addressed local store. Set `KELPCLAW_ARTIFACT_STORE` to override the default `.kelpclaw/artifacts` path. Runtime-generated artifacts are ignored by git.

Codegen nodes must be reviewed before approval:

```console
$ curl -X POST /api/workflows/:id/codegen/:nodeId/review
$ curl -X POST /api/workflows/:id/codegen/:nodeId/promote
```

NanoClaw is the deterministic workflow runtime, not a model provider. It verifies stored artifact hashes before execution, materializes reviewed generated source into the isolated node workspace, and never regenerates code during approved runs. Promotion writes a reusable skill record to the artifact store and registers it so future matching can reuse the promoted skill instead of creating another codegen node.

## Validation Guarantees

- Workflow specs are diffable and validated with stable error codes.
- KelpClaw renders the shared v1 fixtures with schema version, revision, prompt, node kind, port-aware edges, and approval state.
- NanoClaw execution is covered through an approved-workflow mock runner and Docker command-construction tests.
- Integration adapters are production-capable; missing live secrets fail as structured run output.
- CI runs TypeScript format, lint, typecheck, tests, and builds.
