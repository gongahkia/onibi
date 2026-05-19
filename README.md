<p align="center">
  <a href="https://github.com/gongahkia/piranesi">
    <img alt="Piranesi" src="asset/logo/imaginary-prisons.jpg" width="180">
  </a>
</p>

<h1 align="center">Piranesi</h1>

<p align="center">
  <strong>Local-first red-team engagement workspace.</strong>
</p>

<p align="center">
  <a href="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg" /></a>
  <a href="https://github.com/gongahkia/piranesi/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" /></a>
  <a href="https://github.com/gongahkia/piranesi"><img alt="Status" src="https://img.shields.io/badge/status-alpha-orange?style=flat-square" /></a>
</p>

---

Piranesi turns authorized red-team engagement artifacts into local, reviewable
deliverables: preserved evidence, normalized findings, report artifacts, retest diffs,
and signed chain-of-custody manifests. It is not a scanner, C2 platform, SaaS portal,
fleet manager, or automated compliance engine. You bring operator artifacts and tool
output; Piranesi keeps the evidence local and produces artifacts a team can inspect,
sign, preview, and hand off.

`v0.2.0` is the pivot release. The documented Phase 1 workflow is intentionally
small, with scanner imports retained as one evidence source:

```text
piranesi evidence
piranesi ingest
piranesi report
piranesi retest
piranesi sign
piranesi serve
```

Historical host-posture, source-code scanning, infrastructure, and workflow docs are
retained only as legacy context. They are not current product guidance unless a
future roadmap issue explicitly reintroduces them.

## Why Piranesi

Consultants already run tools such as nmap, nuclei, Burp, ZAP, Nessus, ffuf, and
sqlmap. The slow work comes later: preserving evidence, deduplicating findings,
writing reports, tracking retests, proving provenance, and handing off artifacts
without leaking client data.

Piranesi focuses on that artifact layer:

- **Import-only:** Phase 1 does not run active scans or payloads.
- **Operator-evidence aware:** screenshots, transcripts, logs, and other artifacts can
  be preserved in the local evidence vault.
- **Evidence-bound:** findings cite raw tool exports, source digests, and locators.
- **Local-first:** workspaces, reports, signatures, and previews stay on disk.
- **Deterministic:** normalized IDs and contract snapshots make reports reproducible.
- **Reviewable:** Markdown, JSON, PDF, retest output, and signatures are inspectable.

## Quick Start

From a source checkout:

```bash
uv sync
uv run piranesi ingest init --workspace ./workspace \
  --client "Example Client" \
  --project "Loopback Lab" \
  --scope 127.0.0.1
printf "Initial operator note\n" > operator-note.txt
uv run piranesi evidence add \
  --file operator-note.txt \
  --kind note \
  --workspace ./workspace \
  --title "Initial operator note"
uv run piranesi ingest nmap \
  --input tests/fixtures/pentest/nmap/localhost-http.xml \
  --workspace ./workspace
uv run piranesi ingest nuclei \
  --input tests/fixtures/pentest/nuclei/localhost-http.jsonl \
  --workspace ./workspace
uv run piranesi report --workspace ./workspace --format md
uv run piranesi sign --workspace ./workspace
uv run piranesi serve --workspace ./workspace
```

Generate a PDF with the deterministic fallback renderer:

```bash
uv run piranesi report \
  --workspace ./workspace \
  --format pdf \
  --pdf-backend reportlab
```

Compare two workspace snapshots after a retest:

```bash
uv run piranesi retest \
  --baseline ./workspace-before \
  --current ./workspace-after \
  --output retest.json
```

Verify a signed workspace manifest:

```bash
uv run piranesi sign --workspace ./workspace --verify
```

## Current Capabilities

Implemented Phase 1 pieces:

- Pentest workspace contract with raw evidence, normalized findings, reports,
  signatures, and append-only audit log.
- Red-team evidence inventory for operator artifacts such as screenshots, notes,
  transcripts, payload metadata, detection artifacts, and C2 logs.
- Real fixture policy and provenance validation for parser fixtures.
- nmap XML ingestion.
- nuclei JSONL ingestion.
- Pentest report rendering to JSON, Markdown, and PDF.
- Chain-of-custody manifest creation and verification.
- Retest lifecycle diff with `new`, `open`, `closed`, `changed`, `regressed`, and
  `ambiguous` statuses.
- Local loopback report preview via `piranesi serve`.

See [docs/capabilities.md](docs/capabilities.md) for the detailed Phase 1 matrix and
[docs/known-limitations.json](docs/known-limitations.json) for tracked limitations.

## Workspace Layout

```text
workspace/
  workspace.json
  audit-log.jsonl
  evidence/
    index.json
  raw/
    nmap/
    nuclei/
    screenshot/
    transcript/
    c2-log/
  normalized/
    findings.json
  timeline/
  objectives/
  procedures/
  detections/
  reports/
  signatures/
```

Piranesi copies imported files under `raw/<tool>/`, records the original digest, and
normalizes report-ready findings under `normalized/findings.json`. Operator artifacts
added with `piranesi evidence add` are also copied under `raw/<kind>/` and indexed in
`evidence/index.json`.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Workspace contract](docs/pentest-workspace.md)
- [Report schema](docs/pentest-report-schema.md)
- [Nuclei ingestion](docs/nuclei-ingest.md)
- [Retest workflow](docs/retest-workflow.md)
- [Chain of custody](docs/chain-of-custody.md)
- [Local preview UI](docs/local-ui.md)
- [Privacy and data handling](docs/privacy-data-handling.md)
- [CI examples](docs/ci-integration.md)

## Non-Goals In Phase 1

- No hosted SaaS, auth, teams, or client portal.
- No new scanner engine or active target interaction.
- No C2 operation, implant management, or payload execution.
- No autonomous exploitation, payload generation, or AI writeups.
- No fleet management or live SSH probing.
- No compliance certification claims.

Future work is tracked in GitHub roadmap issues and must be implemented behind
separate acceptance criteria before it becomes public guidance.

## Development

Quality gates used for Phase 1 changes:

```bash
uv run python scripts/validate_pentest_fixtures.py
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/piranesi/
uv run pytest -q -m "not integration and not joern and not docker and not e2e and not slow"
```

When CLI help changes, update and review the contract snapshot:

```bash
uv run python scripts/update_contract_snapshots.py
uv run pytest -q tests/test_contract_snapshots.py
```

## License

Apache-2.0. See [LICENSE](LICENSE).
