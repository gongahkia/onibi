# Red-Team Workspace MVP Local-Lab Validation

Date: 2026-05-20

GitHub issue: #114

Status: automated validation path added.

## Target And Inputs

This validation uses authorized local-lab artifacts only:

- Synthetic operator note: `tests/fixtures/redteam/notes/operator-note.md`
- Sanitized terminal transcript: `tests/fixtures/redteam/transcripts/operator-terminal.txt`
- Mock C2-style log: `tests/fixtures/redteam/c2/mock-c2-events.jsonl`
- Synthetic screenshot-style SVG: `tests/fixtures/redteam/screenshots/portal-login.svg`
- Existing localhost scanner exports from `tests/fixtures/pentest/`

No third-party target, client data, live C2 infrastructure, payload execution, or
real credentials are used.

## Automated Run

The validation is encoded in:

```bash
uv run pytest -q tests/test_redteam_validation.py
```

The test builds a complete workspace from scratch and exercises:

- `piranesi ingest init`
- `piranesi evidence add`
- `piranesi ingest nmap`
- `piranesi ingest nuclei`
- `piranesi timeline add`
- `piranesi objectives add`
- `piranesi procedures add`
- `piranesi detections add-ioc`
- `piranesi detections add-note`
- `piranesi report --type red-team --format json`
- `piranesi report --type red-team --format md`
- `piranesi sign`
- `piranesi sign --verify`

## Expected Results

The completed run must produce:

- `workspace.json`
- `evidence/index.json` with note, transcript, mock C2 log, and screenshot records
- `timeline/events.jsonl`
- imported scanner findings under `normalized/findings.json`
- red-team JSON and Markdown handoff reports under `reports/`
- a chain-of-custody manifest under `signatures/`
- a successful manifest verification result

The handoff report must include both operator timeline events and scanner findings.

## Validation Gaps Filed

- #116: browser file upload for evidence artifacts.
- #117: red-team PDF and handoff archive export.

These are follow-up gaps from the local-lab run, not blockers for the MVP validation
path itself.
