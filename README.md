![](https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg)

# `Piranesi`

Piranesi is an alpha CLI for security analysis of TypeScript and JavaScript codebases. It uses Joern-backed taint discovery to surface candidate vulnerabilities, can verify exploits in Docker, and can attach legal and patch context when LLM credentials are configured.

## Status

`v0.1.0` is an alpha release. The end-to-end CLI works on small Express targets, the verify stage is validated on the bundled XSS fixture, and the example docs include real runs on both a hand-crafted vulnerable app and OWASP NodeGoat. Real-world projects still produce misses and false positives, so the example writeups call those out explicitly.

## What It Does

- Transpiles JS/TS projects into a Joern-friendly analysis workspace.
- Extracts tainted source-to-sink flows for SQLi, XSS, path traversal, command injection, SSRF, and related classes.
- Generates stage artifacts for `scan`, `detect`, `triage`, `verify`, `legal`, `patch`, and `report`.
- Verifies exploitable findings in Docker when execution is enabled.
- Supports BYOK LLM routing for triage, patch generation, and legal memo generation.

## Requirements

- Python 3.12+
- `uv`
- Joern plus a working JVM
- TypeScript compiler (`tsc`)
- Docker for the verify stage
- Optional LLM API key for triage and patch generation

The full installation walkthrough is in [docs/getting-started.md](docs/getting-started.md).

## Quick Start

```bash
uv sync
uv run piranesi --version

brew install joern openjdk@17
npm install --global typescript
open -a Docker

cd examples/vuln-express
npm install
cd ../..

uv run piranesi run examples/vuln-express \
  --authorized \
  --yes \
  --output .piranesi-out/vuln-express \
  --no-execute
```

`--no-execute` skips Docker exploit execution. That makes the first run deterministic and does not require an LLM API key.

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

## Development

```bash
uv sync
uv build
uv run piranesi --help
uv run pytest
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Getting Started](docs/getting-started.md)
- [Configuration Reference](docs/configuration.md)
- [Phase 6: Integration and Release](docs/PHASE_6_INTEGRATION_AND_RELEASE.md)

## License

Apache 2.0

<div align="center">
    <img src="./asset/logo/imaginary-prisons.jpg" width="50%">
</div>
