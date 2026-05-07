# TODO 1: Add Host PDF And Dashboard Outputs

## Goal

Close the output gap between the VM vulnerability proposal and the current host posture alpha by adding first-class PDF and dashboard output support for the `piranesi assess` host workflow.

The current host flow writes only `host-report.json` and `host-report.md`. The proposal expects report-only Phase 1 delivery with PDF plus dashboard outputs. This task should add those outputs without disturbing the older SAST/report pipeline.

## Current State

Relevant files:

- `src/piranesi/cli.py`
- `src/piranesi/host/report.py`
- `src/piranesi/host/models.py`
- `tests/test_host_posture.py`
- `README.md`
- `docs/host-posture.md`

Current host report format enum:

- `HostReportFormat.JSON`
- `HostReportFormat.MARKDOWN`
- `HostReportFormat.BOTH`

Current host writer:

- `write_host_report_outputs(report, output_dir, report_format="both")`
- Writes `host-report.json` and/or `host-report.md`.

The general, legacy `ReportFormat` enum includes many more formats, but host mode does not use them.

## Desired Behavior

Add host report formats:

- `pdf`
- `dashboard`
- `all`

Expected CLI examples:

```bash
uv run piranesi assess piranesi-evidence --output piranesi-output --format pdf
uv run piranesi assess piranesi-evidence --output piranesi-output --format dashboard
uv run piranesi assess piranesi-evidence --output piranesi-output --format all
```

Expected files:

- `--format json`: `host-report.json`
- `--format markdown`: `host-report.md`
- `--format both`: `host-report.json`, `host-report.md`
- `--format pdf`: `host-report.pdf`
- `--format dashboard`: dashboard assets under `host-dashboard/`
- `--format all`: JSON, Markdown, PDF, and dashboard

## Implementation Notes

### PDF

Prefer a lightweight, deterministic implementation. Do not require a browser or heavyweight native dependency if avoidable.

Reasonable approaches:

1. Use an optional Python PDF dependency if already acceptable in project dependency policy.
2. Build a simple PDF writer locally if the report layout is intentionally plain.
3. If introducing a dependency, make it optional if possible and return a clear CLI error when `--format pdf` is requested without it.

PDF content should include:

- Report title
- Target
- Generated timestamp
- Analysis modes
- Posture score
- Summary by severity
- Host metadata
- Top actions
- Collection health
- Findings with severity, category, confidence, evidence, remediation
- Known limitations

The PDF does not need to be visually elaborate in this task. It must be readable, deterministic enough for tests, and useful for analyst handoff.

### Dashboard

Implement a static, local-first dashboard. Do not start a server from `assess`.

Suggested output:

```text
piranesi-output/
  host-dashboard/
    index.html
    host-report.json
    assets/
      host-dashboard.css
      host-dashboard.js
```

The dashboard should load the copied JSON report from the same directory and render:

- Score and finding counts
- Severity/category summaries
- Top actions
- Evidence completeness
- Collection health
- Filterable findings table
- Finding detail panel or expandable rows

Keep it usable as a local static file opened in a browser. Avoid external CDN dependencies. The dashboard should not transmit report data.

## Suggested Code Changes

1. Extend `HostReportFormat` in `src/piranesi/cli.py`.

2. Extend `write_host_report_outputs()` in `src/piranesi/host/report.py`.

3. Add helper functions in `src/piranesi/host/report.py`, or split into small modules if the file becomes too large:

```python
def render_host_pdf(report: HostPostureReport) -> bytes: ...
def write_host_dashboard(report: HostPostureReport, output_dir: Path) -> None: ...
```

4. Keep `render_host_markdown()` behavior unchanged for existing tests.

5. Update README and host docs.

6. Add tests around:

- Format enum accepts new formats.
- `--format pdf` writes a non-empty `%PDF` file.
- `--format dashboard` writes `host-dashboard/index.html` and dashboard JSON.
- `--format all` writes all expected outputs.
- Existing `--format both` behavior remains unchanged.

## Acceptance Criteria

- `uv run piranesi assess tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-out --format pdf` exits 0 and writes `host-report.pdf`.
- `uv run piranesi assess tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-out --format dashboard` exits 0 and writes a usable static dashboard.
- `--format both` still writes only JSON and Markdown.
- `--format all` writes JSON, Markdown, PDF, and dashboard.
- Tests cover new writer paths.
- Documentation no longer says host mode lacks PDF/dashboard once implemented.

## Out Of Scope

- Fleet-wide dashboard aggregation.
- Web server mode.
- Authentication for dashboard access.
- Rich charting libraries.
- Ticket generation.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_cli.py
uv run piranesi assess tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-host-out --format all
```

