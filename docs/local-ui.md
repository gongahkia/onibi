# Local Web App

`piranesi serve` starts a local HTTP server for the Piranesi workspace app:

```bash
piranesi serve --workspace ./workspace
```

The server creates the workspace if it does not exist yet. The app can initialize
engagement metadata, add typed operator-note evidence, upload local evidence files,
add timeline events, add objectives and procedures, add detection handoff notes and
IOCs, show guided empty states, and render current workspace data: evidence,
timeline, objectives, procedures, findings, detection handoff records, report
artifacts, and chain-of-custody status.

The product decision behind this interface is documented in
[Product Interface Decision](product-interface-decision.md).

## Bind Safety

The default bind is loopback:

```bash
piranesi serve --workspace ./workspace --host 127.0.0.1 --port 8765
```

Binding to a non-loopback address can expose pentest evidence to the local network.
It is rejected unless explicitly acknowledged:

```bash
piranesi serve --workspace ./workspace --host 0.0.0.0 --unsafe-bind
```

The command prints a warning when `--unsafe-bind` is used.

## Routes

Routes are fixed and do not expose arbitrary workspace file paths:

- `GET /`
- `GET /api/health`
- `GET /api/workspace`
- `POST /api/workspace/init`
- `POST /api/evidence/note`
- `POST /api/evidence/file`
- `POST /api/timeline/event`
- `POST /api/objectives/objective`
- `POST /api/objectives/procedure`
- `POST /api/detections/ioc`
- `POST /api/detections/note`
- `GET /api/report/json`
- `GET /api/report/markdown`
- `GET /api/report/pdf?backend=reportlab`
- `GET /api/report/pdf?backend=weasyprint`

The PDF routes render from the report model. ReportLab is the deterministic fallback
when WeasyPrint system dependencies are unavailable.

The health route returns lightweight JSON for local smoke checks and does not
generate report content or call external services.

## Non-Goals

The Phase 1 app has no client portal, authentication, hosted mode, team workflow,
or scanner execution. Report generation, signing, scanner imports, PFF import/export,
and bulk automation remain CLI-first so they stay deterministic and scriptable. File
upload is limited to explicit evidence preservation; the server still does not serve
arbitrary workspace paths. Scanner import remains available through the CLI and
imported findings are represented in the app.
