# Track A TODO: Security And Data Boundary Hardening

## Goal
Eliminate immediate confidentiality and artifact-integrity risks in scanning, triage, verify, and compliance output paths.

## Priority
P0. Must complete before broad internal rollout.

## Work Items
1. Fix LLM redaction regex defects in prompt sanitization.
2. Expand redaction patterns for realistic token/key formats.
3. Add residual-secret regression tests for nested and header/body payloads.
4. Fix verify evidence inline secret redaction regex defects.
5. Enforce safe artifact filename normalization for verification evidence paths.
6. Add path safety checks to ensure artifact writes stay within output root.
7. Harden compliance bundle text redaction for quoted and assignment-based key formats.
8. Add a regression suite covering:
- `Authorization: Bearer ...`
- API key assignment patterns (`KEY = "..."`, `key: ...`)
- common provider token patterns
- nested dict/list artifacts.
9. Document redaction scope, guarantees, and known limitations in docs.

## Deliverables
1. Patched code in:
- `src/piranesi/llm/sanitize.py`
- `src/piranesi/verify/evidence.py`
- `src/piranesi/legal/evidence.py`
2. New/updated tests in:
- `tests/test_llm/test_sanitize.py`
- `tests/test_legal/test_evidence.py`
- `tests/test_verify/test_evidence.py` (new)
3. Updated security note in `SECURITY.md` and/or relevant docs.

## Acceptance Criteria
1. Redaction tests pass and prevent known secret leaks.
2. Evidence artifact path traversal attempts cannot escape output directory.
3. Existing report/CLI compatibility preserved for safe finding IDs.
4. No regression in deterministic non-LLM runs.

## Metrics
1. Secret redaction false negatives in regression corpus: target `0`.
2. Path traversal safety tests: target `100%` pass.
3. Track A test suite pass rate: target `100%`.

## Status
- [x] Planned
- [x] In progress
- [x] Completed
