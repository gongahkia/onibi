# Getting Started

This guide uses the current local-first red-team workspace flow. Scanner imports are
one evidence source; operator artifacts, timeline entries, detection handoff notes,
reports, and signatures live in the same workspace.

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

Add operator evidence first. This preserves the original file under `raw/<kind>/`
and records metadata in `evidence/index.json`:

```bash
printf "Initial operator note\n" > operator-note.txt
uv run piranesi evidence add \
  --workspace ./workspace \
  --file operator-note.txt \
  --kind note \
  --title "Initial operator note" \
  --source "operator-1"
```

Scanner exports are imported as normalized findings:

```bash
uv run piranesi ingest nmap \
  --input tests/fixtures/pentest/nmap/localhost-http.xml \
  --workspace ./workspace

uv run piranesi ingest nuclei \
  --input tests/fixtures/pentest/nuclei/localhost-http.jsonl \
  --workspace ./workspace

uv run piranesi ingest burp \
  --input tests/fixtures/pentest/burp/lab-issues.xml \
  --workspace ./workspace

uv run piranesi ingest c2 \
  --input tests/fixtures/redteam/c2/mock-c2-events.jsonl \
  --workspace ./workspace \
  --title "Mock C2 event log"
```

## Render Reports

```bash
uv run piranesi report --workspace ./workspace --format json
uv run piranesi report --workspace ./workspace --format md
uv run piranesi report --workspace ./workspace --format pdf --pdf-backend reportlab
uv run piranesi report --workspace ./workspace --type red-team --format json
uv run piranesi report --workspace ./workspace --type red-team --format md
uv run piranesi report --workspace ./workspace --type red-team --format pdf --pdf-backend reportlab
uv run piranesi report --workspace ./workspace --type red-team --format archive
```

`reportlab` is the reliable local PDF backend. The default WeasyPrint backend can produce
more polished HTML/CSS PDFs, but it needs native system libraries installed on the machine.

Red-team archive export writes a ZIP with report artifacts and workspace indexes. It
does not include raw evidence files unless `--include-raw-evidence` is passed; raw
evidence marked `secret` additionally requires `--include-secret-raw-evidence`.

## Sign And Verify

```bash
uv run piranesi sign --workspace ./workspace
uv run piranesi sign --workspace ./workspace --verify
```

## Preview Locally

```bash
uv run piranesi serve --workspace ./workspace
```

The local app can initialize engagement metadata, show guided empty states, capture
typed notes, and upload evidence files from the browser while keeping the server
bound to loopback by default.

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
- [Burp ingestion](burp-ingest.md)
- [C2 log import](c2-log-import.md)
- [Nuclei ingestion](nuclei-ingest.md)
- [Retest workflow](retest-workflow.md)
- [Chain of custody](chain-of-custody.md)
- [Local preview UI](local-ui.md)
