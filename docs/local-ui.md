# Local Preview UI

`piranesi serve` previews a pentest workspace on a local HTTP server:

```bash
piranesi serve --workspace ./workspace
```

The server loads real workspace data and renders engagement metadata, severity
summary, affected assets, findings, evidence, retest status, chain-of-custody status,
and links to report artifacts.

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
- `GET /api/report/json`
- `GET /api/report/markdown`
- `GET /api/report/pdf?backend=reportlab`
- `GET /api/report/pdf?backend=weasyprint`

The PDF routes render from the report model. ReportLab is the deterministic fallback
when WeasyPrint system dependencies are unavailable.

## Non-Goals

The Phase 1 preview UI has no uploads, client portal, authentication, hosted mode,
team workflow, or scanner execution.
