# Sample Gallery

The sample gallery gives new users and CI jobs deterministic targets for trying
Piranesi without bringing private evidence first. Samples are intentionally small,
local, and explicit about which paths need optional tools.

## Bundled Host Posture Demo

The fastest deterministic path is the packaged Debian-style host evidence demo:

```bash
piranesi demo --output piranesi-demo-output
piranesi ui piranesi-demo-output --open
```

Expected artifacts:

- `piranesi-demo-output/host-report.json`
- `piranesi-demo-output/host-report.md`

Reference fixture and report paths in the source checkout:

- `src/piranesi/fixtures/host/debian-vulnerable/`
- `tests/fixtures/reports/host-report/host-report.json`

This path does not require osquery, Trivy, Lynis, OpenSCAP, Joern, Docker, or LLM
credentials because it replays bundled evidence.

## Vulnerable Express ZIP Demo

The local workbench exposes a small vulnerable Express app as a downloadable ZIP
when started in workbench mode:

```bash
piranesi ui --workbench --open
```

Open the sample gallery, download `piranesi-vuln-express.zip`, then upload that
ZIP through the same workbench. The workbench also has a `Run bundled ZIP demo`
button that starts the same local sample without a manual download/upload step.
The demo contains intentionally vulnerable routes for reflected XSS, path
traversal, command injection, and SSRF. Expected artifacts after a successful
workbench scan:

- `report.json`
- `report.md`

The packaged sample source lives at
`src/piranesi/fixtures/app/vuln-express/`. The larger source-checkout example is
documented in [`docs/examples/vuln-express.md`](examples/vuln-express.md).

Optional tools:

- Joern
- Java
- Node.js
- npm

If those tools are missing, the workbench preflight panel marks the scan path as
not ready before upload.

## Container And Kubernetes Fixtures

Container and Kubernetes samples are source-checkout fixtures. They are local and
do not require a live Docker daemon or Kubernetes cluster for the fixture path.

Container review:

```bash
piranesi container assess \
  --image tests/fixtures/container/trivy-image.json \
  --output piranesi-container-output
```

Expected artifacts:

- `piranesi-container-output/container-report.json`
- `piranesi-container-output/container-report.md`

Kubernetes review:

```bash
piranesi k8s assess tests/fixtures/k8s --output piranesi-k8s-output
```

Expected artifacts:

- `piranesi-k8s-output/k8s-report.json`
- `piranesi-k8s-output/k8s-report.md`

These samples are deterministic fixture reviews. They complement, but do not
replace, dedicated container scanners, CNAPP platforms, admission control, or
cluster runtime monitoring.

## Validation

Small deterministic sample paths are covered by existing CLI and UI tests:

```bash
uv run pytest tests/test_cli.py::test_demo_writes_json_and_markdown_from_bundled_fixture
uv run pytest tests/test_ui_server.py::test_workbench_sample_gallery_lists_downloadable_app_zip
```

For a source checkout smoke pass:

```bash
uv run piranesi demo --output /tmp/piranesi-demo
uv run piranesi ui --workbench --port 0
uv run pytest tests/test_ui_server.py tests/test_cli.py -k "ui or demo"
```
