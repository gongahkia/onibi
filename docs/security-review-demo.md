# Security Review Demo

This demo shows how to hand KelpClaw evidence to a security or governance reviewer without asking them to run KelpClaw locally.

## Static Findings

```console
$ kelp-claw export-sarif fixtures/skills-corpus/destructive-shell/SKILL.md \
  --policy baseline \
  --out findings.sarif

$ kelp-claw governance controls fixtures/skills-corpus/destructive-shell/SKILL.md \
  --policy baseline \
  --region sg \
  --framework agentic-ai \
  --out controls.md
```

Expected result: `findings.sarif` contains an `error` for the destructive shell denial, and `controls.md` gives a compact matrix of control area, status, evidence files, residual risk, and reviewer action.

## Signed Bundle Handoff

```console
$ kelp-claw run-skill fixtures/skills-corpus/local-file-audit/SKILL.md \
  --input input.json \
  --run-id skill-run.security-demo

$ kelp-claw export-audit-bundle skill-run.security-demo \
  --include-governance \
  --include-controls \
  --include-sarif \
  --region sg \
  --framework agentic-ai

$ kelp-claw verify-audit-bundle .kelpclaw/audit-bundles/skill-run.security-demo --strict
```

Expected result: the bundle includes `governance-report.json`, `controls.md`, `findings.sarif`, `manifest.json`, `manifest.sig`, `attestation.json`, and `attestation.sig`. Strict verification checks the manifest signature, file hashes, attestation signature, attestation manifest hash, and referenced evidence files.

## GitHub Action

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

The action emits annotations, uploads `findings.sarif` to code scanning, writes a PR summary with governance tier and controls status, strict-verifies the signed attestation, and uploads the static audit bundle as an artifact.
