# Getting Started

This guide gets a fresh machine to the first reproducible Piranesi scan. It covers the host dependencies, the repository setup, and the artifacts you should expect from the first run.

## Prerequisites

- Python 3.12+
- `uv`
- Joern
- A working JVM for Joern
- Node.js and npm
- TypeScript compiler (`tsc`)
- Docker

### macOS

```bash
brew install joern openjdk@17
npm install --global typescript
open -a Docker
```

If your shell does not automatically pick up Homebrew's JVM, export the path that `brew --prefix openjdk@17` prints for your machine.

### Linux

Install a recent OpenJDK build, install Joern from the upstream binary release, install Node.js/npm, then install TypeScript globally:

```bash
npm install --global typescript
```

Docker is only required for the verify stage, but it should be installed before release verification.

## Repository Setup

```bash
git clone https://github.com/gongahkia/piranesi.git
cd piranesi
uv sync
```

## Runtime Validation

Run these once before the first scan:

```bash
joern --help
java -version
npx tsc --version
docker info
uv run piranesi --version
uv run piranesi --help
```

Notes:

- Some Joern installs do not support `joern --version`. `joern --help` plus a successful Joern-backed scan is the practical validation path in this repository.
- The first `docker info` may fail if Docker Desktop is still starting.
- Without an LLM API key, Piranesi still runs scan and detect. Triage falls back to pass-through mode and patch generation is skipped.

## Optional LLM Configuration

Piranesi will use LiteLLM-compatible credentials when they are present. The current runtime checks look for these environment variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENROUTER_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `GOOGLE_API_KEY`
- `LITELLM_API_KEY`

You do not need any of them for the first detect-only walkthrough below.

## First Scan Walkthrough

The most reproducible first run is the bundled vulnerable Express app.

```bash
cd examples/vuln-express
npm install
cd ../..

uv run piranesi run examples/vuln-express \
  --authorized \
  --yes \
  --output .piranesi-out/vuln-express \
  --no-execute
```

This does four useful things for a first run:

- Exercises the real `piranesi run` entry point.
- Avoids Docker-side exploit execution while you are still validating the host.
- Avoids any LLM dependency.
- Produces all stage artifacts in a single output directory.

For a compact human-readable summary, run:

```bash
uv run python docs/examples/run_detect_summary.py examples/vuln-express
```

## Understanding the Output

After the first run, the output directory contains:

- `scan.json`: file list, call graph, entry points, and attack-surface summary.
- `detect.json`: candidate findings from the taint analysis stage.
- `triage.json`: triage verdicts. Without an API key, the current fallback marks findings as `true_positive` with a note that LLM triage was skipped.
- `verify.json`: confirmed findings. With `--no-execute`, this stays empty by design.
- `legal.json`: regulatory obligations for confirmed findings.
- `patch.json`: generated fixes for confirmed findings when an LLM is configured.
- `report.json`: machine-readable combined report.
- `report.md`: human-readable markdown report.
- `pr_body.md`: per-finding GitHub-flavored markdown.

For the bundled vulnerable app, the real run on 2026-04-09 produced four candidate findings:

- `CWE-79` on `/search`
- `CWE-22` on `/files`
- `CWE-78` on `/shell`
- `CWE-918` on `/proxy`

It missed the planted SQLi in `/users`. That miss is documented in [docs/examples/vuln-express.md](examples/vuln-express.md).

## Verification and Full Pipeline Runs

When you are ready to exercise the verify stage, remove `--no-execute`. That requires:

- Docker to be running
- The target directory to contain a runnable Node app with a `package.json`
- Explicit authorization via `--authorized`

An end-to-end verified example already exists in [`tests/fixtures/verify/xss_app`](../tests/fixtures/verify/xss_app). On the test machine used for this release pass, it produced one confirmed XSS finding with payload `<script>alert(1)</script>`.

## Common Issues

- `error TS5055 ... would overwrite input file`: Piranesi retries transpilation with forced emit flags, but noisy projects can still produce TypeScript warnings.
- Joern port conflicts on `8080`: the runtime automatically walks to the next candidate port.
- NodeGoat and other larger apps: the most stable current evaluation path is the direct transpile-plus-detect helper in `docs/examples/run_detect_summary.py`. The full `piranesi run` path is still brittle on NodeGoat-sized apps in `v0.1.0`.
