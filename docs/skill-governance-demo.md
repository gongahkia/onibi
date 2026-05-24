# SKILL.md Governance Demo

This demo shows the adoption path for KelpClaw as an agent skill governance framework: compatibility, enforced live execution, replay comparison, and signed audit handoff.

The SG/APAC governance layer is evidence-oriented. It maps KelpClaw controls to practical governance review areas such as bounded autonomy, human accountability, traceability, data and third-party risk, and residual risk. It does not certify legal or regulatory compliance.

## One-Command Demo

```console
$ kelp-claw doctor
$ kelp-claw demo governance --out .kelpclaw/demo/governance
$ kelp-claw verify-audit-bundle .kelpclaw/demo/governance/audit-bundle --profile reviewer
```

Expected result: `doctor` reports local readiness and optional live integration gaps. `demo governance` creates a runnable sample skill, input file, signed evidence workspace, SG-oriented governance report, controls matrix, SARIF export, static audit bundle, and strict verification result under `.kelpclaw/demo/governance`.

## Passing Skill

```console
$ kelp-claw compat fixtures/skills-corpus/local-file-audit/SKILL.md --policy baseline
$ kelp-claw policy explain fixtures/skills-corpus/local-file-audit/SKILL.md --policy baseline
$ kelp-claw governance report fixtures/skills-corpus/local-file-audit/SKILL.md --region sg --framework agentic-ai --policy sg-agentic-ai-baseline
$ kelp-claw run-skill fixtures/skills-corpus/local-file-audit/SKILL.md --input input.json --run-id skill-run.local-demo
$ kelp-claw governance report skill-run.local-demo --region sg --framework agentic-ai
$ kelp-claw governance controls skill-run.local-demo --region sg --framework agentic-ai --out controls.md
$ kelp-claw export-audit-bundle skill-run.local-demo --include-governance --include-controls --include-sarif --region sg --framework agentic-ai
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/skill-run.local-demo --strict
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/skill-run.local-demo --profile reviewer
```

Expected result: compatibility is runnable, the governance tier is low or moderate depending on tools, the run succeeds, and the audit bundle verifies with valid Ed25519 manifest and attestation signatures.

## Blocked Skill

```console
$ kelp-claw policy explain fixtures/skills-corpus/destructive-shell/SKILL.md --policy baseline
$ kelp-claw governance report fixtures/skills-corpus/destructive-shell/SKILL.md --region sg --framework agentic-ai --policy sg-agentic-ai-baseline
$ kelp-claw run-skill fixtures/skills-corpus/destructive-shell/SKILL.md --input input.json --policy baseline
```

Expected result: `sg-agentic-deny-destructive-shell` marks the governance tier high, and `baseline-deny-destructive-shell` blocks the run before any live agent is launched.

## PDPA-Oriented Skill

```console
$ kelp-claw compat fixtures/skills-corpus/pii-file-write/SKILL.md --policy sg-pdpa-strict
$ kelp-claw governance report fixtures/skills-corpus/pii-file-write/SKILL.md --region sg --framework agentic-ai --policy sg-pdpa-strict
```

Expected result: the skill remains runnable but requires privacy-reviewer approval and appears as moderate governance risk because it writes customer data.

## Web Evidence

```console
$ EXA_API_KEY=... kelp-claw web search "Singapore agentic AI governance" \
  --provider exa \
  --policy sg-web-research \
  --domain mas.gov.sg \
  --out .kelpclaw/web-evidence/sg-agentic-ai

$ kelp-claw governance report ./SKILL.md \
  --region sg \
  --framework agentic-ai \
  --policy sg-web-research \
  --include-web-evidence .kelpclaw/web-evidence/sg-agentic-ai

$ kelp-claw export-audit-bundle skill-run.local-demo \
  --include-web-evidence .kelpclaw/web-evidence/sg-agentic-ai \
  --include-governance \
  --region sg \
  --framework agentic-ai
```

Expected result: the web command evaluates policy before calling Exa/TinyFish, writes `web-evidence.json`, `web-events.jsonl`, `web-bom.json`, and `web-evidence.html`, and the governance report marks third-party web evidence as attached.

## Evidence Workspace

```console
$ kelp-claw evidence init --workspace .kelpclaw/evidence --client "Example Client" --project "Agent Review"
$ kelp-claw evidence add --workspace .kelpclaw/evidence --file operator-note.txt --kind note --title "Operator note"
$ kelp-claw evidence import-sarif --workspace .kelpclaw/evidence findings.sarif
$ kelp-claw evidence sign --workspace .kelpclaw/evidence
$ kelp-claw evidence verify --workspace .kelpclaw/evidence
$ kelp-claw governance report skill-run.local-demo --include-evidence .kelpclaw/evidence
```

Expected result: KelpClaw preserves local evidence, imports normalized findings with source references, signs a chain-of-custody manifest, and marks the evidence workspace as attached in the governance report.

## Enforced Agent Wrappers

```console
$ kelp-claw run-skill ./SKILL.md --input input.json --agent codex-cli --wrapper --enforce-policy
$ kelp-claw run-skill ./SKILL.md --input input.json --agent claude-code --wrapper --enforce-policy
$ kelp-claw run-skill ./SKILL.md --input input.json --agent goose --wrapper --enforce-policy
```

Wrapper mode runs the agent in a materialized workspace, normalizes JSONL tool events into KelpClaw hook events, applies policy to observed tool actions, and fails closed when an enforced tool event cannot be classified.

## Recorded Replay Diff

```console
$ kelp-claw replay-diff --recorded --skill ./SKILL.md --input input.json --agents codex-cli,custom-agent --agent-command ./agent-wrapper
$ kelp-claw replay-diff --recorded --skill ./SKILL.md --input input.json --agents claude-code,goose --wrapper --enforce-policy --agent-command ./agent-wrapper
```

Expected result: KelpClaw stores one run per agent, compares normalized tool sequence, step hashes, output hashes, and policy decisions, then reports whether the agents behaved equivalently.

## Agent Inventory

```console
$ kelp-claw inventory scan --root . --policy sg-agentic-ai-baseline --out .kelpclaw/inventory/agent-inventory.json
$ kelp-claw inventory graph --root . --format markdown --out .kelpclaw/inventory/permissions.md
$ kelp-claw inventory coverage --root . --format markdown --fail-on high --out .kelpclaw/inventory/coverage.md
```

Expected result: the inventory links SKILL.md files to detected tools, policies, runs, signed bundles, attestations, web evidence, GitHub workflows, and MCP gateways. Coverage fails when high-risk evidence is missing, such as an unrunnable skill or a run without a signed audit bundle.

## PR Workflow

```yaml
- uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
  with:
    skill: ./SKILL.md
    policy: sg-agentic-ai-baseline
    governance: "true"
    region: sg
    framework: agentic-ai
    fail-on-unrunnable: "true"
```

The action writes annotations for policy/governance findings, adds a PR summary, exports SARIF, exports a static signed audit bundle, strict-verifies the bundle attestation, uploads SARIF to code scanning, and uploads the bundle as an artifact.

## Golden Example

```console
$ kelp-claw compat examples/agentic-ai-governance-demo/skills/passing/SKILL.md --policy sg-agentic-ai-baseline
$ kelp-claw policy explain examples/agentic-ai-governance-demo/skills/blocked/SKILL.md --policy sg-agentic-ai-baseline
$ kelp-claw replay-diff --skill examples/agentic-ai-governance-demo/skills/replay-diff/SKILL.md --agents claude-code,codex-cli,goose
```

The example folder gives public demo material for a passing skill, blocked skill, web-evidence skill, and cross-agent replay comparison.

For repository-level checks, switch the same action to inventory mode:

```yaml
- uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
  with:
    mode: inventory
    inventory-root: .
    policy: sg-agentic-ai-baseline
    fail-on-coverage: high
```
