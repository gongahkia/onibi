# Implementation Waves — Agent Delegation Guide

## Wave 7 — Integration

### Agent 23: Phase 6 Pipeline Orchestrator + Report Renderer

**Docs:** `docs/PHASE_6_INTEGRATION_AND_RELEASE.md` (Sections 2, 3)

**Prompt:**
> You are implementing the end-to-end pipeline orchestrator and report renderer for Project Piranesi. Read `docs/PHASE_6_INTEGRATION_AND_RELEASE.md` Sections 2 and 3. All individual pipeline stages exist — this agent wires them together.
>
> Implement:
> 1. Update `src/piranesi/cli.py` — the `piranesi run` command. Execute stages sequentially: scan → detect → triage → verify → legal → patch → report. Each stage function takes config + prior stage output → stage output. Write intermediate JSON artifacts to the output directory. Progress via `ui.py` (stage headers, progress bars, summary table). Error handling: if a stage fails, save partial results, report which stage failed, suggest `--resume` to continue from last successful stage.
> 2. `--resume` flag: if intermediate artifacts exist from a prior run, skip completed stages.
> 3. `--dry-run` flag: show what would be scanned without executing (for cost estimation).
> 4. `src/piranesi/report/renderer.py` — Combined report generation. For each confirmed finding, merge: technical report (CWE, taint path, exploit, reproducer), legal memo (obligations, penalties), patch (unified diff). Output formats: JSON (`report.json`), Markdown (`report.md`), PR body (`pr_body.md`). Use Jinja2 templates for markdown rendering. Include executive summary (N findings, M confirmed, top regulatory concerns, total LLM cost, duration).
> 5. Tests: mock a full pipeline run with fixture data. Verify report output structure.

---

### Agent 24: Phase 6 Example Runs + Docs + Release

**Docs:** `docs/PHASE_6_INTEGRATION_AND_RELEASE.md` (Sections 4-9)

**Prompt:**
> You are preparing Project Piranesi for release. Read `docs/PHASE_6_INTEGRATION_AND_RELEASE.md` Sections 4-9.
>
> Tasks:
> 1. Run Piranesi against OWASP NodeGoat and a hand-crafted vulnerable Express app (3-5 known vulns). Document: setup steps, full CLI invocation, representative output, what was found, what was missed, any false positives. Write to `docs/examples/`.
> 2. Update `README.md` with real output from the example runs (replace the mock demo).
> 3. Write `docs/getting-started.md`: installation (uv, Joern, JVM, tsc, Docker), first scan walkthrough, understanding output.
> 4. Write `docs/configuration.md`: full piranesi.toml reference.
> 5. Write `CHANGELOG.md` for v0.1.0.
> 6. Write `SECURITY.md`: vulnerability reporting policy, 48h ack, 7d triage, 90d disclosure.
> 7. Verify release checklist from Section 6: Joern runtime validated, eval harness passes, CI green, pyproject.toml complete, `uv build` clean, `piranesi --version` works, no secrets in codebase.
> 8. License: verify Apache 2.0 LICENSE file exists and is correct.
