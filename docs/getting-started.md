# Getting Started

This guide gets a fresh machine to the first reproducible Piranesi report and
local review workflow. The shortest path is: run the no-credentials demo, inspect
the output in the local workbench, then collect real host evidence when the target
machine is ready.

## Try Piranesi In 10 Minutes

Install the CLI with pipx and run the no-credentials demo:

```bash
pipx install piranesi
piranesi quickstart
piranesi demo --output piranesi-demo-output
piranesi ui piranesi-demo-output --open
piranesi doctor --host
```

The demo assesses bundled Debian/Ubuntu-style host evidence and writes:

```text
piranesi-demo-output/
  host-report.json
  host-report.md
```

No osquery, Trivy, Linux VM, or LLM API key is required for the demo.
The UI opens the same local report artifacts without uploading them anywhere.
`piranesi doctor --host` then reports only host-posture collection dependencies
and next steps for real evidence collection.

For the full storage, retention, LLM, and outbound export model, see
[`docs/privacy-data-handling.md`](privacy-data-handling.md).

## Prerequisites

- Python 3.12+
- `pipx` for packaged use, or `uv` for source-checkout development

Only real host collection needs Linux host tooling:

- osquery for `piranesi collect`
- Trivy for optional package CVE evidence
- Lynis for optional hardening baseline evidence
- OpenSCAP for optional XCCDF baseline evidence

The legacy source-code scanner also needs:

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

If you are consuming a packaged release instead of hacking on the repository, use
`pipx install piranesi`, then run `piranesi quickstart`.

For a new target repository, scaffold the local config before the first run:

```bash
uv run piranesi init
```

That writes `piranesi.toml` plus an empty `.piranesi-ignore` template in the current directory.
The generated config is tuned from detected frameworks and languages, and the
command prints next steps for dependency setup, `piranesi doctor .`, and the
first safe `--no-execute` scan.

If you prefer not to install the host toolchain, use the container workflow in
`docs/docker.md`.

Suppression lifecycle best practices:

- Always include `reason`, `reason_code`, and `owner`.
- Set `created` and `expires` so suppressions are time-bound.
- Link a ticket/reference (`ticket`, `reference`) for auditability.
- Run `piranesi suppressions validate --findings <detect.json|output-dir>` in CI.
- Use `[suppression]` config flags (`fail_on_invalid`, `fail_on_expired`, `fail_on_stale`) to enforce policy.

Custom rule authoring starter packs are available at `examples/rule-packs/`. See
`docs/custom-rule-packs.md` for enable/copy/customize workflows.

## Runtime Validation

Run these once before the first scan:

```bash
uv run piranesi quickstart
uv run piranesi demo --output piranesi-demo-output
uv run piranesi doctor --host
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
- `piranesi doctor --host` is the fastest way to see what host collection can run
  on the current machine.
- Piranesi can run static scan/detect/report in deterministic mode without an LLM API key.

## Command Model

Piranesi uses progressive disclosure:

- Start with `piranesi quickstart`, `piranesi demo`, and `piranesi ui` for the
  local evidence workbench path.
- Start with `piranesi run ...` for the default end-to-end workflow.
- Use grouped advanced commands only when you need fine-grained control:
  - `piranesi pipeline ...` for stage-level operations (`scan`, `detect`, `triage`, `verify`, `legal`, `patch`, `report`)
  - `piranesi baseline diff ...` for baseline comparison workflows
  - `piranesi suppressions add ...` for suppression creation
  - `piranesi dev ...` for editor/watch workflows (`lsp`, `watch`)

Backward-compatible top-level command forms still work.

Product surfaces use the same model: host, fleet, source-code, container, and
Kubernetes workflows produce local JSON/Markdown reports; the workbench reads
those reports and keeps raw evidence on the local machine.

## Optional LLM Configuration

Piranesi uses LiteLLM-compatible credentials for model-assisted triage, patch generation, and legal memo generation. Static scan/detect/report can run without these credentials. The runtime checks for at least one of these environment variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENROUTER_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `GOOGLE_API_KEY`
- `LITELLM_API_KEY`

LLM stages also honor `budget.max_tokens` from `piranesi.toml`. When the budget is tight, Piranesi trims prompt context and caps completion tokens. If exhausted, triage falls back to deterministic pass-through for remaining findings and patch generation skips remaining items with warnings.

Model-assisted stages may send finding summaries and code snippets to your configured provider. Piranesi strips comments and redacts common secret patterns before calls, but this is best-effort. If policy requires zero outbound model data, keep all LLM credentials unset and run deterministic mode.

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

Golden-path first run sequence (the same flow is covered by a CLI regression test):

1. `uv run piranesi init`
2. `uv run piranesi doctor .`
3. `uv run piranesi run . --authorized --yes --no-execute --output .piranesi-out/first-run`
4. `uv run piranesi explain <finding-id> --output .piranesi-out/first-run`

## Understanding the Output

After the first run, the output directory contains:

- `scan.json`: file list, call graph, entry points, attack-surface summary, and `query_quality` metrics for loaded/matched source and sink specs.
- `detect.json`: candidate findings from the taint analysis stage.
- `triage.json`: triage verdicts generated via the configured LLM provider.
- `verify.json`: confirmed findings plus per-finding verification attempts with precondition status (`satisfied`, `missing`, `inferred`, `user_provided`), selected `proof_mode`, machine-readable evidence strings, and explicit skip/inconclusive reasons.
- `legal.json`: regulatory obligations for confirmed findings.
- `patch.json`: generated fixes for confirmed findings.
- `report.json`: machine-readable combined report, including the `query_quality` block copied from `scan.json`, per-finding `evidence_status` values, active `known_limitations` registry entries, and optional ownership metadata (`service`, `system`, `team`, `owner`, `repository`, `environment`, `control_owner`) when `[ownership]` config is set.
- `report.md`: human-readable markdown report.
- `pr_body.md`: per-finding GitHub-flavored markdown.
- `baseline-diff.md` / `baseline-diff.json` (when `--baseline` is used): PR-focused delta summary with `new`, `changed`, `fixed`, and `existing` classifications.

`report.json` also includes `finding_clusters`, which group repeated flows that
share the same CWE and sink location while preserving each individual finding.

Evidence statuses in `report.json` and `report.md`:

- `confirmed`: dynamically verified exploitability evidence exists.
- `triaged_active_candidate`: retained after model-assisted triage.
- `static_candidate`: static-only candidate without dynamic proof.
- `unreachable_candidate`: candidate not reachable from entry points.
- `suppressed`: candidate intentionally suppressed with documented reason.

Suppression lifecycle is also reported in `report.json` and `report.md`, including:

- total/active/expired/stale/invalid rule counts
- stale selector list (when detect findings were available)
- expired selector list

Compliance obligations are reported as **compliance support evidence**, not certification:

- each obligation includes mapping metadata (`framework_version`, `control_id`, rationale, `last_reviewed`, reviewer/source, confidence)
- framework-level metadata is versioned and should be reviewed/updated when standards or internal mappings change
- use legal/compliance review processes outside Piranesi for formal attestations

To package artifacts for audit handoff:

```bash
uv run piranesi compliance bundle \
  --framework all \
  --artifacts-dir .piranesi-out/vuln-express \
  --output .piranesi-out/vuln-express/compliance-bundle
```

The bundle includes redacted artifact snapshots, per-control evidence files (including control owner metadata when configured), and a `manifest.json` checksum index.

Use `query_quality` to tune specs over time:

- `source_specs` / `sink_specs`: candidate volume by spec with descriptor metadata (`spec_id`, category, and definition file/origin).
- `unmatched_*_specs`: specs that never generated candidates in the run.
- `noisy_*_specs`: high-cardinality specs based on the configured threshold in `query_quality.noisy_candidate_threshold`.

To inspect one finding without opening the full JSON artifact:

```bash
piranesi explain <finding-id> --output .piranesi-out/vuln-express
```

`piranesi explain` prints both the status code and human-readable evidence label so
you can quickly distinguish static candidates from dynamically verified findings.
It now also prints structured explanation metadata:

- matched source/sink specs (including custom/built-in status)
- sanitizers considered vs sanitizers actually observed on the path
- propagation summary (source to sink, operations, sanitizer steps)
- verification state (candidate, unreachable, suppressed, or verified), including attempt outcome, skip/inconclusive reason, missing preconditions, and actionable next steps
- verification proof mode and evidence captured by the verifier
- selected target launch profile, startup errors, and launch log path (when profiles are used)
- ownership attribution (service/system/team/owner/control owner/repository/environment)
- confidence contributors with a documented `v1` weighted component model

Advisory DB workflow (optional but recommended for dependency workflows):

```bash
uv run piranesi advisory status --project-root .
uv run piranesi advisory update --project-root .
```

For offline/air-gapped environments, import a prepared DB snapshot:

```bash
uv run piranesi advisory import ./artifacts/advisory.db --project-root .
```

See `docs/advisory-db-workflow.md` for full online/offline workflows and search examples.

Confidence model (`v1`) components shown in `report.json` and `piranesi explain`:

- `static_reachability`
- `source_quality`
- `sink_quality`
- `sanitizer_signal`
- `triage_signal`
- `verification_signal`
- `suppression_signal`

`final_confidence` remains the pipeline confidence value for backward compatibility;
the component scores provide transparent contributor-level context.

Composite risk model (`v1`) is also included per finding as:

- `composite_risk_score`: numeric score in `[0, 100]`
- `composite_risk_band`: `low`, `medium`, `high`, or `critical`
- `composite_risk`: additive component breakdown and rationale

Formula:

```
score = clamp(
  severity
  + confidence
  + source_exposure
  + sink_criticality
  + ownership_signal
  + verification_signal
  + exploitability_signal
  + advisory_signal
  + reachable_path_signal
  + suppression_signal,
  0, 100
)
```

Caveats:

- The score is a prioritization aid, not an absolute likelihood/impact guarantee.
- Severity fields remain unchanged for backward compatibility with existing workflows.
- Missing metadata (for example ownership or advisories) is treated explicitly in the breakdown so ranking decisions stay auditable.

For the bundled vulnerable app, the real run on 2026-04-09 produced four candidate findings:

- `CWE-79` on `/search`
- `CWE-22` on `/files`
- `CWE-78` on `/shell`
- `CWE-918` on `/proxy`

It missed the planted SQLi in `/users`. That miss is documented in [docs/examples/vuln-express.md](examples/vuln-express.md).

## Editor And Watch Workflow

For fast local iteration, use the LSP server in your editor and `watch` in a terminal.

Start LSP (stdio, default):

```bash
uv run piranesi dev lsp --config ./piranesi.toml
```

Start watch mode with a tighter debounce window:

```bash
uv run piranesi dev watch . \
  --debounce 300 \
  --filter "**/*.{ts,tsx,js,jsx}" \
  --authorized \
  --yes
```

Behavior:

- Save events trigger incremental scan/detect invalidation scoped to changed files where feasible.
- LSP diagnostics include stable IDs (`stable_id`), severity, evidence level, and an actionable remediation hint.
- If incremental state is unavailable (for example missing manifests), LSP/watch falls back to a full project scan.

## Verification and LLM-Assisted Runs

When you are ready to exercise Docker-backed verification, remove `--no-execute`. That requires:

- Docker to be running
- The target directory to contain a runnable Node app with a `package.json`
- Explicit authorization via `--authorized`

Verification currently uses structured exploit/probe templates with safe defaults for:

- `CWE-89` SQL injection
- `CWE-78` command injection
- `CWE-918` SSRF (loopback-only probes)
- `CWE-22` path traversal
- `CWE-601` open redirect
- `CWE-79` reflected XSS
- `CWE-502` insecure deserialization (marker payloads only)
- weak crypto classes (`CWE-327`, `CWE-326`, `CWE-319`) when tainted input controls algorithm/cipher choice

By default (`--proof-mode safe`) these templates avoid destructive payloads and
do not require external network callback infrastructure.

If you explicitly set `--proof-mode unsafe` (or `[verify].proof_mode = "unsafe"`),
Piranesi may select higher-risk templates intended for disposable test targets.
Unsafe mode can perform intrusive/destructive probes and should not be used on
production systems.

Reusable target launch profiles can remove repeated startup flags when you verify local services.

Minimal Express example:

```toml
[verify]
target_profile = "express_local"

[verify.target_profiles.express_local]
command = "npm run dev"
cwd = "examples/vuln-express"
base_url = "http://127.0.0.1:{port}"
readiness_url = "/"
startup_timeout_seconds = 45
teardown = "on_success"
logs_path = "./piranesi-output/verify-express.log"

[verify.target_profiles.express_local.env]
PORT = "4010"
```

Run with:

```bash
uv run piranesi pipeline verify .piranesi-out/vuln-express/triage.json \
  --authorized \
  --yes \
  --target-profile express_local
```

Python/FastAPI example:

```toml
[verify]
target_profile = "fastapi_local"

[verify.target_profiles.fastapi_local]
command = "uvicorn app:app --host 127.0.0.1 --port 8000"
cwd = "examples/fastapi-app"
base_url = "http://127.0.0.1:8000"
readiness_url = "/docs"
startup_timeout_seconds = 60
teardown = "always"
logs_path = "./piranesi-output/verify-fastapi.log"
```

If startup or readiness fails, `verify.json` includes `startup_error` and
`launch_log_path` for the attempt so failures are actionable.

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
