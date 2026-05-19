# Local Web App

`piranesi serve` starts a local HTTP server for the Piranesi workspace app:

```bash
piranesi serve --workspace ./workspace
```

The server creates the workspace if it does not exist yet. The app can initialize
engagement metadata, add typed operator-note evidence, show guided empty states,
and render current workspace data: evidence, timeline, objectives, procedures,
findings, detection handoff records, report artifacts, and chain-of-custody status.

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
- `GET /api/workspace`
- `POST /api/workspace/init`
- `POST /api/evidence/note`
- `GET /api/report/json`
- `GET /api/report/markdown`
- `GET /api/report/pdf?backend=reportlab`
- `GET /api/report/pdf?backend=weasyprint`

The PDF routes render from the report model. ReportLab is the deterministic fallback
when WeasyPrint system dependencies are unavailable.

## Non-Goals

The Phase 1 app has no arbitrary file upload UI, client portal, authentication,
hosted mode, team workflow, or scanner execution. Scanner import remains available
through the CLI and imported findings are represented in the app.
