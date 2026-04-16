# CI Integration

Piranesi is a provider-agnostic CLI. The tool itself does not require GitHub- or
GitLab-specific runtime APIs, but this repository provides first-party CI template
files for quick onboarding.

## First-Party CI Templates

- GitHub Actions: `docs/ci-templates/github-actions-piranesi.yml`
- GitLab CI: `docs/ci-templates/gitlab-ci-piranesi.yml`

Both templates default to deterministic mode (`--no-execute`, no LLM key required),
upload artifacts, emit SARIF, and fail according to severity/baseline policy.

## Provider-Agnostic Command

Use this as the baseline command in any CI provider:

```bash
piranesi run . \
  --authorized \
  --yes \
  --no-execute \
  --format sarif \
  --fail-severity high \
  --output .piranesi-output
```

Optional baseline gate (if `.piranesi-baseline.json` exists):

```bash
piranesi run . \
  --authorized \
  --yes \
  --no-execute \
  --format sarif \
  --fail-severity high \
  --baseline .piranesi-baseline.json \
  --fail-on-new \
  --fail-on-new-severity high \
  --output .piranesi-output
```

## Expected Artifacts

- `scan.json`
- `detect.json`
- `triage.json`
- `verify.json`
- `legal.json`
- `patch.json`
- `report.json`
- `report.md`
- `pr_body.md`
- `report.sarif.json` (when `--format sarif`)
- `baseline-diff.md` and `baseline-diff.json` (when `--baseline` is used)

## Exit Codes

- `0`: scan policy passed (or `--no-fail` used)
- `1`: findings matched fail policy (`--fail-severity` and/or baseline gating)
- `2`: configuration or required-flag error
- `3`: runtime failure
- `4`: budget exceeded

## LLM Environment Variables (Optional)

Deterministic mode does not require LLM credentials.
Only set keys if you want model-assisted stages:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENROUTER_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `GOOGLE_API_KEY`
- `LITELLM_API_KEY`

## Docker-Based CI

If your runner supports containers, use the published image:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$PWD":/workspace \
  -w /workspace \
  ghcr.io/gongahkia/piranesi:latest \
  run . --authorized --yes --no-execute --format sarif --output /workspace/piranesi-output
```

See `docs/docker.md` for mount, config-path, and permission guidance.
