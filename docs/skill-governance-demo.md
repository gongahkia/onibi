# SKILL.md Governance Demo

This demo shows the adoption path for KelpClaw as an agent skill governance framework: compatibility, enforced live execution, replay comparison, and signed audit handoff.

The SG/APAC governance layer is evidence-oriented. It maps KelpClaw controls to practical governance review areas such as bounded autonomy, human accountability, traceability, data and third-party risk, and residual risk. It does not certify legal or regulatory compliance.

## Passing Skill

```console
$ kelp-claw compat fixtures/skills-corpus/local-file-audit/SKILL.md --policy baseline
$ kelp-claw policy explain fixtures/skills-corpus/local-file-audit/SKILL.md --policy baseline
$ kelp-claw governance report fixtures/skills-corpus/local-file-audit/SKILL.md --region sg --framework agentic-ai --policy sg-agentic-ai-baseline
$ kelp-claw run-skill fixtures/skills-corpus/local-file-audit/SKILL.md --input input.json --run-id skill-run.local-demo
$ kelp-claw governance report skill-run.local-demo --region sg --framework agentic-ai
$ kelp-claw export-audit-bundle skill-run.local-demo --include-governance --region sg --framework agentic-ai
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/skill-run.local-demo
```

Expected result: compatibility is runnable, the governance tier is low or moderate depending on tools, the run succeeds, and the audit bundle verifies with a valid Ed25519 signature that covers the governance report.

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

## Enforced Codex Wrapper

```console
$ kelp-claw run-skill ./SKILL.md --input input.json --agent codex-cli --wrapper --enforce-policy
```

Wrapper mode runs Codex in a materialized workspace, normalizes Codex JSONL tool events into KelpClaw hook events, applies policy to observed tool actions, and fails closed when an enforced tool event cannot be classified.

## Recorded Replay Diff

```console
$ kelp-claw replay-diff --recorded --skill ./SKILL.md --input input.json --agents codex-cli,custom-agent --agent-command ./agent-wrapper
```

Expected result: KelpClaw stores one run per agent, compares normalized tool sequence, step hashes, output hashes, and policy decisions, then reports whether the agents behaved equivalently.

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

The action writes annotations for policy findings, adds a PR summary, exports a static signed audit bundle, verifies the bundle signature, and uploads the bundle as an artifact.
