# Prompt: Strengthen Report Evidence Semantics

You are working in the Piranesi repository, a Python 3.12+ AppSec CLI that scans source code, detects tainted flows, triages findings, verifies exploitability, optionally patches issues, and renders reports. The current pipeline conceptually distinguishes candidate, triaged, confirmed, unreachable, and suppressed findings, but the report language and machine-readable output can still blur those evidence levels.

Goal: make report semantics explicit so users can immediately tell whether a finding is a static candidate, an LLM-triaged active candidate, a dynamically verified issue, an unreachable candidate, or a suppressed item.

Implementation requirements:

- Audit `src/piranesi/report/renderer.py`, Markdown templates under `src/piranesi/templates/`, and CLI report/explain behavior in `src/piranesi/cli.py`.
- Add a first-class evidence/status field where needed, without breaking existing report consumers unnecessarily.
- Ensure `report.json`, `report.md`, executive summaries, and `piranesi explain` consistently expose the evidence level.
- Avoid language that implies a candidate is proven exploitable unless verification evidence exists.
- Add a concise status legend to Markdown output.
- Keep deterministic/no-LLM mode coherent: reachable static findings should remain candidates, not confirmed vulnerabilities.
- Add tests that cover at least confirmed findings, active candidates, unreachable candidates, and suppressed findings.
- Update relevant docs, likely `docs/capabilities.md` and `docs/getting-started.md`.

Acceptance criteria:

- A user reading either JSON or Markdown can tell what evidence supports each issue.
- Existing tests pass or are updated intentionally.
- New tests fail before the change and pass after it.
- The report remains backward-friendly: prefer additive fields unless there is a documented reason to rename/remove fields.

Validation suggestions:

- Run targeted report renderer tests.
- Run `python3 -m compileall -q src tests`.
- Run `python3 -m ruff check src tests docs` if ruff is available.
