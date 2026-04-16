# CI Integration

Piranesi is a provider-agnostic local CLI. It does not ship GitHub Actions, GitLab templates, PR bots, or any other provider-specific integration code in the repository. You integrate it by invoking `piranesi run` in your own pipeline and then publishing the generated artifacts however your CI provider expects.

## Standard invocation

Every CI integration follows the same basic shape:

```bash
export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run ./target --authorized --yes --output ./piranesi-output
```

`piranesi run` can execute static scan/detect/report in deterministic mode without LLM credentials. Set one LiteLLM-compatible credential (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, `AZURE_OPENAI_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, or `LITELLM_API_KEY`) when you want model-assisted triage, patch generation, or legal memo drafting in CI.

Useful outputs from that directory:

- `report.json`: machine-readable combined report
- `report.md`: human-readable summary
- `pr_body.md`: markdown summary per finding
- `report.sarif.json`: SARIF 2.1.0 output when you run with `--format sarif`

## Exit codes

- `0`: no findings met the current fail policy, or `--no-fail` was set
- `1`: findings at or above `--fail-severity` were detected
- `2`: configuration or required-flag error
- `3`: runtime failure
- `4`: the trace budget was exceeded

Useful CI knobs:

- `--fail-severity high`: only fail for `high` or `critical` findings
- `--fail-severity medium`: fail for `medium`, `high`, or `critical`
- `--no-fail`: always exit `0` for findings while still writing artifacts

## Generic CI

Use this in any CI system that gives you Python and a checked-out repository:

```bash
python -m pip install --upgrade pip
python -m pip install piranesi

export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run . \
  --authorized \
  --yes \
  --output .piranesi-output
```

To emit SARIF as well:

```bash
export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run . \
  --format sarif \
  --authorized \
  --yes \
  --output .piranesi-output
```

## GitHub Actions

This keeps the Piranesi integration provider-agnostic at the tool layer: install the CLI, run it, publish artifacts, and optionally upload SARIF into GitHub code scanning.

```yaml
name: piranesi

on:
  pull_request:
  push:
    branches: [main]

jobs:
  scan:
    runs-on: ubuntu-latest
    env:
      OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    permissions:
      contents: read
      actions: read
      security-events: write
    steps:
      - uses: actions/checkout@v5

      - uses: actions/setup-python@v5
        with:
          python-version: "3.12"

      - name: Install Piranesi
        run: |
          python -m pip install --upgrade pip
          python -m pip install piranesi

      - name: Run Piranesi
        id: piranesi
        continue-on-error: true
        run: |
          piranesi run . \
            --format sarif \
            --authorized \
            --yes \
            --output .piranesi-output

      - name: Upload Piranesi artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: piranesi-output
          path: .piranesi-output/

      - name: Upload SARIF
        if: always() && hashFiles('.piranesi-output/report.sarif.json') != ''
        uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: .piranesi-output/report.sarif.json
          category: piranesi

      - name: Fail workflow if findings were detected
        if: steps.piranesi.outcome == 'failure'
        run: exit 1
```

Notes:

- There is no first-party GitHub Action in this repository. The workflow above is just a normal CLI invocation plus optional artifact upload steps.
- GitHub code scanning requires a repository that supports GitHub Code Security and a workflow with `security-events: write`.
- If you only want JSON and Markdown, remove `--format sarif` and the SARIF upload step.

## GitLab CI

Use the published container image when you do not want to install Python, Node, Joern, and Piranesi on every runner.

```yaml
stages:
  - security

piranesi:
  stage: security
  image: ghcr.io/gongahkia/piranesi:latest
  variables:
    OPENAI_API_KEY: $OPENAI_API_KEY
  script:
    - piranesi run . --format sarif --authorized --yes --output piranesi-output
  artifacts:
    when: always
    paths:
      - piranesi-output/
    reports:
      sarif: piranesi-output/report.sarif.json
```

Notes:

- `artifacts:reports:sarif` is the GitLab-native SARIF ingestion path. If your GitLab instance does not support SARIF reports, keep `artifacts:paths` and download the SARIF file as a normal artifact instead.
- There is no first-party GitLab integration in Piranesi itself. This is a standard containerized CLI job.

## Docker-based CI

If your CI system can run Docker but you do not want a package install on the runner, mount the checked-out repository into the Piranesi image.

```bash
docker run --rm \
  -e OPENAI_API_KEY \
  -v "$PWD":/workspace \
  ghcr.io/gongahkia/piranesi:latest \
  run /workspace --authorized --yes
```

To emit SARIF in the mounted workspace:

```bash
docker run --rm \
  -e OPENAI_API_KEY \
  -v "$PWD":/workspace \
  ghcr.io/gongahkia/piranesi:latest \
  run /workspace --format sarif --authorized --yes --output /workspace/piranesi-output
```

The image ships with Joern, JVM 17, Node.js, TypeScript, Python 3.12, and Piranesi pre-installed. It uses the bundled default `piranesi.toml`, while outputs default to `/workspace/piranesi-output`.

## Fail-on-findings

The default CLI contract fails on any unsuppressed finding:

```bash
export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run . --authorized --yes --output piranesi-output
```

- Exit code `0`: no unsuppressed findings
- Exit code `1`: one or more unsuppressed findings

If you want to fail only on higher-severity issues:

```bash
export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run . \
  --fail-severity high \
  --authorized \
  --yes \
  --output piranesi-output
```

If you want to keep uploading artifacts even when Piranesi exits `1`, capture the exit code, publish `piranesi-output/`, then re-raise the failure:

```bash
set +e
export OPENAI_API_KEY="$OPENAI_API_KEY"
piranesi run . --authorized --yes --output piranesi-output
status=$?
set -e

# publish piranesi-output/ here

exit "$status"
```

If you never want findings to fail the job, use `--no-fail`. Configuration and runtime errors still exit non-zero.

## SARIF consumers

Common ways to consume `report.sarif.json`:

- VS Code: open the file with a SARIF viewer extension for local triage.
- GitHub code scanning: upload the SARIF file from your own workflow with `github/codeql-action/upload-sarif`.
- GitLab: publish it as an `artifacts:reports:sarif` report when your instance supports SARIF ingestion.
- DefectDojo: import the file as a SARIF scan type.
- SonarQube: import the file during analysis with `sonar.sarifReportPaths=./piranesi-output/report.sarif.json`.

## Design boundary

Keep provider-specific wiring in your own pipeline repository:

- Piranesi core should stay a local CLI plus standard output formats.
- CI jobs, artifact upload steps, code-scanning upload steps, and notification hooks belong in user-owned pipeline configuration.
- The Piranesi repository should not grow GitHub-only or GitLab-only runtime behavior just to support CI.
