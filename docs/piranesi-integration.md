# Piranesi Integration

Piranesi was imported into KelpClaw with history preserved, then reduced to the concepts that strengthen KelpClaw's agent governance direction.

## What Carried Forward

- Local evidence vault for operator notes, SARIF outputs, agent artifacts, screenshots, transcripts, and other files.
- Normalized findings with source references, evidence snippets, affected instances, and deterministic IDs.
- Append-only audit log and chain-of-custody manifest verification.
- Evidence QA for missing raw files, digest drift, unsigned workspaces, and findings without source references.
- Retest lifecycle diff for `new`, `open`, `closed`, `changed`, `regressed`, and `ambiguous` findings.

## What Was Cut

The standalone Python product surface was intentionally removed from KelpClaw HEAD after history import: Typer CLI, Python packaging, local preview server, report/PDF renderer, active rescan runtime, Slack/email/GitHub exporters, and scanner-specific adapters beyond SARIF.

This keeps KelpClaw focused on agent skill governance with policy, sandboxing, replay, audit bundles, inventory, and evidence handoff.

## CLI Flow

```console
$ kelp-claw evidence init --workspace .kelpclaw/evidence --client "Example Client" --project "Agent Review"
$ kelp-claw evidence add --workspace .kelpclaw/evidence --file operator-note.txt --kind note
$ kelp-claw evidence import-sarif --workspace .kelpclaw/evidence findings.sarif
$ kelp-claw evidence sign --workspace .kelpclaw/evidence
$ kelp-claw evidence verify --workspace .kelpclaw/evidence
$ kelp-claw governance report <runId> --include-evidence .kelpclaw/evidence
$ kelp-claw export-audit-bundle <runId> --include-evidence .kelpclaw/evidence --include-governance
```
