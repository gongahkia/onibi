# Wave 8 — CI Green + Stub Resolution

**Status:** In progress
**Prerequisite:** Waves 1-7 complete (all pipeline stages implemented)
**Goal:** Get all CI gates passing and resolve remaining stubs/dead code

---

## Context

All 7 pipeline stages (scan, detect, triage, verify, legal, patch, report) are implemented and the orchestrator works end-to-end with mocked stage boundaries. However, CI is fully red:

| Gate | Current State | Target |
|------|--------------|--------|
| `ruff check .` | 160 errors (27 auto-fixable) | 0 errors |
| `ruff format --check` | 36 files need reformatting | 0 files |
| `mypy src tests` | 153 errors in 34 files | 0 errors |
| `pytest -q` | 4 failing tests | 0 failures |

### Failing Tests

1. `tests/eval/test_baselines.py::test_llm_only_runner_uses_detector_model_and_normalizes_findings`
2. `tests/test_detect/test_phase1_integration.py::test_phase1_fixture_annotations_match_real_findings`
3. `tests/test_scan/test_surface.py::test_build_scan_result_maps_entry_points_and_attack_surface`
4. `tests/test_scan/test_transpile.py::test_prepare_transpile_workspace_writes_isolated_tsconfig`

### Stubs / Dead Code

1. **`src/piranesi/patch/generator.py`** — 1-line Phase 0 placeholder. Patch logic is implemented inline in `pipeline.py:563-615`. This file is dead code.
2. **`cli.py:166` `_run_stubbed_stage()`** — Individual CLI commands (`piranesi scan`, `piranesi detect`, etc.) print "not implemented" and exit. Only `piranesi run` works.

---

## Parallel Work Streams

### Stream A: Ruff Lint + Format (independent)

1. Run `ruff check --fix .` to auto-fix 27 violations
2. Manually fix remaining ~133 lint violations
3. Run `ruff format src/ tests/` to reformat 36 files
4. Verify: `ruff check . && ruff format --check src/ tests/` returns 0

### Stream B: Mypy Type Errors (independent)

1. Run `mypy src tests` and categorize errors
2. Fix type annotations across 34 files
3. Verify: `mypy src tests` returns 0

### Stream C: Failing Tests (independent)

1. Diagnose each of the 4 failing tests
2. Fix root causes (likely fixture/assertion mismatches from recent refactors)
3. Verify: `pytest -q` all green

### Stream D: Stub Cleanup (independent)

1. Remove or populate `patch/generator.py` (currently dead code — pipeline uses inline implementation)
2. Optionally: wire individual CLI stage commands to their respective pipeline stage runners

---

## Acceptance Criteria

- [ ] `uv run ruff check .` — 0 errors
- [ ] `uv run ruff format --check src/ tests/` — 0 files to reformat
- [ ] `uv run mypy src tests` — 0 errors
- [ ] `uv run pytest -q` — all pass, 0 failures
- [ ] No stub files remaining in src/
- [ ] CI workflow passes end-to-end

---

## Future Waves (Post CI-Green)

### Wave 9 — Individual CLI Stage Commands
Wire `piranesi scan`, `piranesi detect`, `piranesi triage`, `piranesi verify`, `piranesi legal`, `piranesi patch`, `piranesi report` to execute their respective pipeline stages independently (currently stubbed).

### Wave 10 — Live E2E Integration Test
Add a pytest marker `@pytest.mark.e2e` test that runs the full pipeline against the bundled `examples/vuln-express` fixture with a real Joern server and Docker sandbox (no mocks). Validates the limitation noted in Wave 7.

### Wave 11 — Ground Truth Expansion
Expand eval ground truth from 20 to 50+ entries using NodeGoat and Juice Shop findings. Enables statistically meaningful per-CWE metrics.
