# Getting Started

This guide gets a fresh machine to the first reproducible Piranesi scan. It covers the host dependencies, the repository setup, and the artifacts you should expect from the first run.

## Prerequisites

- Python 3.12+
- `uv` for source-checkout development, or `pip install piranesi` for packaged use
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

If you are consuming a packaged release instead of hacking on the repository,
install with `python -m pip install piranesi` and run `piranesi doctor .` inside
the target repository.

For a new target repository, scaffold the local config before the first run:

```bash
uv run piranesi init
```

That writes `piranesi.toml` plus an empty `.piranesi-ignore` template in the current directory.
The generated config is tuned from detected frameworks and languages, and the
command prints next steps for dependency setup, `piranesi doctor .`, and the
first safe `--no-execute` scan.

## Runtime Validation

Run these once before the first scan:

```bash
uv run piranesi doctor .
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
- `piranesi doctor .` is the fastest way to see what will work on the current machine.
- Piranesi can run static scan/detect/report in deterministic mode without an LLM API key.

## Optional LLM Configuration

Piranesi uses LiteLLM-compatible credentials for model-assisted triage, patch generation, and legal memo generation. Static scan/detect/report can run without these credentials. The runtime checks for at least one of these environment variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENROUTER_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `GOOGLE_API_KEY`
- `LITELLM_API_KEY`

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

This does three useful things for a first run:

- Exercises the real `piranesi run` entry point.
- Avoids Docker-side exploit execution while you are still validating the host.
- Produces all stage artifacts in a single output directory.

If no LLM credential is configured, the run uses deterministic mode:

- `triage.json` preserves reachable static findings with an explicit deterministic-mode note.
- `patch.json` is empty because patch generation is LLM-backed.
- `report.json` and `report.md` are still generated from the static and verification artifacts.

For a compact human-readable summary, run:

```bash
uv run python docs/examples/run_detect_summary.py examples/vuln-express
```

## Understanding the Output

After the first run, the output directory contains:

- `scan.json`: file list, call graph, entry points, and attack-surface summary.
- `detect.json`: candidate findings from the taint analysis stage.
- `triage.json`: triage verdicts generated via the configured LLM provider.
- `verify.json`: confirmed findings. With `--no-execute`, this stays empty by design.
- `legal.json`: regulatory obligations for confirmed findings.
- `patch.json`: generated fixes for confirmed findings.
- `report.json`: machine-readable combined report.
- `report.md`: human-readable markdown report.
- `pr_body.md`: per-finding GitHub-flavored markdown.

For the bundled vulnerable app, the real run on 2026-04-09 produced four candidate findings:

- `CWE-79` on `/search`
- `CWE-22` on `/files`
- `CWE-78` on `/shell`
- `CWE-918` on `/proxy`

It missed the planted SQLi in `/users`. That miss is documented in [docs/examples/vuln-express.md](examples/vuln-express.md).

## Verification and LLM-Assisted Runs

When you are ready to exercise Docker-backed verification, remove `--no-execute`. That requires:

- Docker to be running
- The target directory to contain a runnable Node app with a `package.json`
- Explicit authorization via `--authorized`

When you are ready to exercise LLM-assisted triage and patch generation, set one LiteLLM-compatible API key before running the pipeline.

An end-to-end verified example already exists in [`tests/fixtures/verify/xss_app`](../tests/fixtures/verify/xss_app). On the test machine used for this release pass, it produced one confirmed XSS finding with payload `<script>alert(1)</script>`.

## Exit Codes

`piranesi run` uses these exit codes:

- `0`: no findings met the current fail policy, or `--no-fail` was set
- `1`: findings at or above `--fail-severity` were detected
- `2`: configuration or required-flag error
- `3`: runtime error
- `4`: budget exceeded

Examples:

```bash
piranesi run . --fail-severity high --authorized --yes
piranesi run . --no-fail --authorized --yes
```

## Common Issues

- `error TS5055 ... would overwrite input file`: Piranesi retries transpilation with forced emit flags, but noisy projects can still produce TypeScript warnings.
- Joern port conflicts on `8080`: the runtime automatically walks to the next candidate port.
- NodeGoat and other larger apps: the most stable current evaluation path is the direct transpile-plus-detect helper in `docs/examples/run_detect_summary.py`. The full `piranesi run` path is still brittle on NodeGoat-sized apps in `v0.2.0`.
