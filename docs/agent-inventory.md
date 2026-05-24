# Agent Inventory And Permission Graph

KelpClaw can inventory an agent-skill repository and produce a reviewer-friendly view of what can run, which policies protect it, and which evidence exists.

## Local Scan

```console
$ kelp-claw inventory scan \
  --root . \
  --policy sg-agentic-ai-baseline \
  --out .kelpclaw/inventory/agent-inventory.json

$ kelp-claw inventory graph \
  --root . \
  --format markdown \
  --out .kelpclaw/inventory/permissions.md

$ kelp-claw inventory coverage \
  --root . \
  --format markdown \
  --fail-on high \
  --out .kelpclaw/inventory/coverage.md
```

`inventory scan` detects SKILL.md files, recorded `.kelpclaw/runs`, signed audit bundles, governed web evidence, evidence workspaces, KelpClaw GitHub Action workflows, and MCP web gateway commands. The JSON output includes normalized permission edges such as skill-to-tool, skill-to-policy, run-to-bundle, bundle-to-attestation, skill-to-web-evidence, and skill-to-evidence-workspace.

`inventory graph` writes either a Markdown table or Mermaid flowchart. `inventory coverage` flags the main operational gaps: unrunnable skills, deny-level policy findings, runs without signed bundles, bundles without attestations, networked skills without web evidence, unsigned evidence workspaces, findings without source references, and workflows that use KelpClaw without SARIF upload evidence.

## CI Mode

```yaml
- uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
  with:
    mode: inventory
    inventory-root: .
    policy: sg-agentic-ai-baseline
    fail-on-coverage: high
```

The action writes a PR summary, uploads the inventory output folder as an artifact, and fails according to `fail-on-coverage`: `high`, `moderate`, or `none`.

## Useful Inputs

- `--runs-dir`: override the run artifact directory when runs are not under `.kelpclaw/runs`.
- `--bundles-dir`: override the audit bundle directory when bundles are not under `.kelpclaw/audit-bundles`.
- `--web-evidence-dir`: override the governed web evidence directory.
- `--evidence-dir`: override the local evidence workspace directory.
- `--format mermaid`: generate a graph that can be pasted into GitHub Markdown or docs.
