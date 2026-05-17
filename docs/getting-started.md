# Getting Started

This guide uses the Phase 1 pentest report engine commands only.

## Install For Development

```bash
uv sync
uv run piranesi --help
```

## Create A Workspace

```bash
uv run piranesi ingest init --workspace ./workspace \
  --client "Example Client" \
  --project "Loopback Lab" \
  --scope 127.0.0.1
```

## Ingest Real Fixture Data

```bash
uv run piranesi ingest nmap \
  --input tests/fixtures/pentest/nmap/localhost-http.xml \
  --workspace ./workspace

uv run piranesi ingest nuclei \
  --input tests/fixtures/pentest/nuclei/localhost-http.jsonl \
  --workspace ./workspace
```

## Render Reports

```bash
uv run piranesi report --workspace ./workspace --format json
uv run piranesi report --workspace ./workspace --format md
uv run piranesi report --workspace ./workspace --format pdf --pdf-backend reportlab
```

`reportlab` is the reliable local PDF backend. The default WeasyPrint backend can produce
more polished HTML/CSS PDFs, but it needs native system libraries installed on the machine.

## Sign And Verify

```bash
uv run piranesi sign --workspace ./workspace
uv run piranesi sign --workspace ./workspace --verify
```

## Preview Locally

```bash
uv run piranesi serve --workspace ./workspace
```

## Retest

```bash
uv run piranesi retest \
  --baseline ./workspace-before \
  --current ./workspace-after \
  --output retest.json
```

## Next Reading

- [Workspace contract](pentest-workspace.md)
- [Report schema](pentest-report-schema.md)
- [Nuclei ingestion](nuclei-ingest.md)
- [Retest workflow](retest-workflow.md)
- [Chain of custody](chain-of-custody.md)
- [Local preview UI](local-ui.md)
