![](https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg)

# `Piranesi`

Piranesi is an alpha, local-first AppSec analysis CLI that turns source code into explainable security, exploitability, and compliance artifacts. It uses Joern-backed taint discovery to surface candidate vulnerabilities, can verify exploits in Docker, and can attach legal and patch context when LLM credentials are configured.

## Status

`v0.2.0` is an alpha release. The stable center of gravity is TypeScript/JavaScript web application analysis, especially small Express targets. Broader language, framework, compliance, verification, and workflow features are present at different maturity levels; see [docs/capabilities.md](docs/capabilities.md). The verify stage is validated on the bundled XSS fixture, and the example docs include real runs on both a hand-crafted vulnerable app and OWASP NodeGoat. Real-world projects still produce misses and false positives, so the example writeups call those out explicitly.

## What It Does

- Transpiles JS/TS projects into a Joern-friendly analysis workspace.
- Extracts tainted source-to-sink flows for SQLi, XSS, path traversal, command injection, SSRF, and related classes.
- Generates stage artifacts for `scan`, `detect`, `triage`, `verify`, `legal`, `patch`, and `report`.
- Verifies exploitable findings in Docker when execution is enabled.
- Runs static scan/detect/report in deterministic mode without LLM credentials.
- Supports BYOK LLM routing for model-assisted triage, patch generation, and legal memo generation.

## Requirements

- Python 3.12+
- `uv` for source-checkout development, or `pip install piranesi` for packaged use
- Joern plus a working JVM
- TypeScript compiler (`tsc`)
- Docker for the verify stage
- Optional LLM API key for model-assisted triage, patch generation, and legal memo drafting

The full installation walkthrough is in [docs/getting-started.md](docs/getting-started.md).

## Quick Start

```bash
uv sync
uv run piranesi --version
uv run piranesi init

brew install joern openjdk@17
npm install --global typescript
open -a Docker

uv run piranesi doctor .

cd examples/vuln-express
npm install
cd ../..

uv run piranesi run examples/vuln-express \
  --authorized \
  --yes \
  --output .piranesi-out/vuln-express \
  --no-execute
```

`piranesi doctor .` reports whether the local machine is ready for deterministic scanning, LLM-assisted triage/patching, and Docker-backed verification.

`--no-execute` skips Docker exploit execution. Without an LLM credential, `piranesi run` uses deterministic mode: static scan/detect/report still run, triage preserves reachable findings without model-backed false-positive discrimination, and patch generation is skipped.

Set one LiteLLM-compatible credential to enable LLM-assisted stages: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, `AZURE_OPENAI_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, or `LITELLM_API_KEY`.

Use `--fail-severity high` to fail CI only on `high` or `critical` findings, or `--no-fail` to always exit `0` for findings while still writing artifacts.

## Real Output

The compact summary below was produced from a real run against [`examples/vuln-express`](examples/vuln-express):

```text
$ uv run python docs/examples/run_detect_summary.py examples/vuln-express
Piranesi Detect Summary
Target: /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express
Transpile failures tolerated: 0
Candidate findings: 4
By CWE:
  CWE-22: 1
  CWE-78: 1
  CWE-79: 1
  CWE-918: 1
Findings:
  - CWE-78 | source=cmd | sink=execSync | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:49
  - CWE-79 | source=q | sink=res.send | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:31
  - CWE-22 | source=file | sink=fs.readFileSync | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:43
  - CWE-918 | source=url | sink=fetch | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:55
```

## Example Results

| Target | Invocation used | What Piranesi found | Misses / noise |
| --- | --- | --- | --- |
| `examples/vuln-express` | `uv run piranesi run ... --no-execute` | 4 candidate findings: XSS, path traversal, command injection, SSRF | Missed the planted SQLi, 0 false positives in the current sample |
| OWASP NodeGoat | `uv run python docs/examples/run_detect_summary.py workspace/nodegoat/app --show-limit 16` | 32 candidates, including `eval(req.body.*)` and several `res.render` flows | 17 clear SSRF false positives, missed the `$where` NoSQL injection |

Full writeups:

- [Hand-Crafted Vulnerable Express App](docs/examples/vuln-express.md)
- [OWASP NodeGoat](docs/examples/nodegoat.md)
- [Getting Started](docs/getting-started.md)
- [Configuration Reference](docs/configuration.md)

## SARIF Output

Generate SARIF 2.1.0 reports with `--format sarif`:

```bash
uv run piranesi run examples/vuln-express \
  --format sarif \
  --authorized \
  --yes \
  --output .piranesi-out/vuln-express
```

This writes `report.sarif.json` alongside the standard JSON and Markdown reports. The SARIF output includes taint-flow `codeFlows`, inline `fixes` from patch diffs, and regulatory metadata.

## GitHub Actions

Add Piranesi to your CI pipeline and upload results to GitHub code scanning:

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
        run: pip install piranesi
      - name: Run Piranesi
        id: piranesi
        continue-on-error: true
        run: |
          piranesi run . \
            --format sarif \
            --authorized \
            --yes \
            --output .piranesi-output
      - name: Upload SARIF
        if: always() && hashFiles('.piranesi-output/report.sarif.json') != ''
        uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: .piranesi-output/report.sarif.json
          category: piranesi
      - name: Fail on findings
        if: steps.piranesi.outcome == 'failure'
        run: exit 1
```

See [docs/ci-integration.md](docs/ci-integration.md) for GitLab CI, Docker-based, and generic CI examples.

## Development

```bash
uv sync
uv build
uv run piranesi --help
uv run pytest
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Capability Matrix](docs/capabilities.md)
- [Getting Started](docs/getting-started.md)
- [Configuration Reference](docs/configuration.md)
- [CI Integration](docs/ci-integration.md)

## License

Apache 2.0

<div align="center">
    <img src="./asset/logo/imaginary-prisons.jpg" width="50%">
</div>
