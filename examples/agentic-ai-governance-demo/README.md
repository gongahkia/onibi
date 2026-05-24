# Agentic AI Governance Demo

This example is the compact public demo path for KelpClaw.

```console
$ kelp-claw doctor
$ kelp-claw compat examples/agentic-ai-governance-demo/skills/passing/SKILL.md --policy sg-agentic-ai-baseline
$ kelp-claw run-skill examples/agentic-ai-governance-demo/skills/passing/SKILL.md --input examples/agentic-ai-governance-demo/inputs/basic.json --run-id skill-run.example-passing
$ kelp-claw export-audit-bundle skill-run.example-passing --include-governance --include-controls --include-sarif --region sg --framework agentic-ai
$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/skill-run.example-passing --profile reviewer
```

Use `skills/blocked/SKILL.md` to show fail-closed destructive shell policy behavior, `skills/web-evidence/SKILL.md` to show governed Exa/TinyFish evidence attachment, and `skills/replay-diff/SKILL.md` to show cross-agent replay comparison.
