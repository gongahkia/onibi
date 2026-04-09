# Implementation Waves — Agent Delegation Guide

## Wave 3 — After transpilation + Joern server done

### Agent 10: Phase 1 CPGQL Queries + Source/Sink Specs

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Sections 5, 10)

**Prompt:**
> You are implementing the CPGQL query layer for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Sections 5 and 10. Build on top of the Joern server manager (Agent 7) and transpilation pipeline (Agent 6).
>
> Implement:
> 1. `src/piranesi/scan/specs.py` — Source and sink specifications as CPGQL query patterns. Define all v1 sources (Express `req.body`, `req.query`, `req.params`, `req.headers`, `req.cookies`, `process.env`, URL/URLSearchParams) and sinks (SQL `query`/`$queryRaw`/`$executeRaw`/`raw`, `exec`/`execSync`/`spawn`/`spawnSync`, `eval`/`Function`, `dangerouslySetInnerHTML`/`send`/`render`/`write`, `readFile`/`writeFile`/`readFileSync`/`writeFileSync`, `fetch`/`get`/`post`/`request`). Each spec: CPGQL pattern string, source_type/sink_type enum, CWE ID for sinks.
> 2. `src/piranesi/scan/queries.py` — CPGQL query templates. Functions that take a `JoernServer` instance and source/sink specs, and execute the queries. Core query: `sink.reachableByFlows(source).l`. Also: known sanitizer patterns (escape, parameterize, normalize) — flows through these are filtered.
> 3. Extensibility: custom source/sink patterns from `piranesi.toml` `[scan.custom_sources]` and `[scan.custom_sinks]`.
> 4. Tests in `tests/test_scan/test_queries.py`: hand-crafted JS fixture files with known sources/sinks. Verify each query detects expected patterns. Mark integration tests with `@pytest.mark.joern`.

---

### Agent 11: Phase 3 Regulatory Engine Core + Taxonomy

**Docs:** `docs/PHASE_3_REGULATORY_ENGINE.md` (Sections 2, 3, 4)

**Prompt:**
> You are implementing the regulatory rule engine for Project Piranesi. Read `docs/PHASE_3_REGULATORY_ENGINE.md` Sections 2, 3, and 4. This module is independent of the taint engine — develop against mock `ConfirmedFinding` objects.
>
> Implement:
> 1. `src/piranesi/legal/engine.py` — Minimal forward-chaining inference engine. A `Fact` is a Pydantic model with `predicate: str` and `args: dict`. A `Rule` has `preconditions: list[FactPattern]` and `conclusions: list[Fact]`. The engine maintains a fact set, iterates rules until fixed point (no new facts derived). Provide `add_rule()`, `add_fact()`, `run()`, `query(predicate) -> list[Fact]` methods. Hand-rolled, not PyDatalog or Souffle (see Section 3 for justification).
> 2. `src/piranesi/legal/taxonomy.py` — Personal data category taxonomy. 4 tiers: Tier 1 (NRIC, biometric, genetic, health), Tier 2 (financial, employment, criminal), Tier 3 (contact, DOB, nationality, race, religion), Tier 4 (name, username, public). Function `classify_field(field_name: str) -> list[str]` using heuristic rules (field name patterns → data categories). Function `tier_for_category(category: str) -> int`.
> 3. Tests in `tests/test_legal/test_engine.py`: test forward chaining (add facts + rules, verify derived facts), fixed-point termination, no infinite loops. `test_taxonomy.py`: test field name classification heuristics.
>
> Do NOT implement specific PDPA/MAS TRM rules yet — that's Wave 4.

---

### Agent 12: Phase 5 Scoring Methodology

**Docs:** `docs/PHASE_5_EVALUATION_HARNESS.md` (Section 3)

**Prompt:**
> You are implementing the evaluation scoring pipeline for Project Piranesi. Read `docs/PHASE_5_EVALUATION_HARNESS.md` Section 3. The ground truth dataset (from Agent 2) must exist in `eval/ground_truth/` as YAML files.
>
> Implement:
> 1. `eval/scoring.py` — CLI scoring script. Input: `--pipeline-output results.json` (Piranesi's output) + `--ground-truth eval/ground_truth/` (YAML entries). Output: `eval/scores/latest.json` (machine-readable) + human-readable table to stdout.
> 2. Matching logic: a finding matches a ground truth entry when: same file (at least one affected_file in common), same CWE ID, taint source and sink match (normalized string comparison). Partial match (0.5 weight) when source matches but sink differs in same file.
> 3. Metrics: precision, recall, F1 (overall + per-CWE). Per-stage metrics: scan recall, detect precision/recall, triage FP filter rate, verify confirmation rate.
> 4. Output format: the concrete table shown in Section 3.5.
> 5. Tests in `tests/eval/test_scoring.py`: test matching logic (exact, partial, no match), precision/recall calculation with known inputs, edge cases (zero TP, zero FP, empty ground truth).

---

## Wave 4 — After CPGQL queries done + engine core done

### Agent 13: Phase 1 Data Flow Extraction + Attack Surface

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Sections 6, 9)

**Prompt:**
> You are implementing the core data flow extraction layer for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Sections 6 and 9. Build on top of the Joern server (Agent 7), transpilation (Agent 6), and CPGQL queries (Agent 10).
>
> Implement:
> 1. `src/piranesi/detect/flows.py` — Data flow extraction. For each source-sink pair from `specs.py`, query Joern via `sink.reachableByFlows(source).l`. Map each Joern flow to Piranesi's `list[TaintStep]` (see ARCHITECTURE.md Section 4.3 for the TaintStep model). Use the SourceMap from Agent 6 to convert Joern's JS line numbers back to original TS locations. Classify operations from Joern nodeType (CALL → "call_arg", IDENTIFIER → "assignment", METHOD_PARAMETER_IN → "call_arg", RETURN → "return", FIELD_IDENTIFIER → "property_access"). Check for sanitizer functions on the flow path — if found, mark as sanitized for that specific path. Generate `CandidateFinding` for each flow: deterministic ID (SHA-256 of vuln_class + source location + sink location), vuln_class from sink CWE, confidence 0.7, severity from CWE class.
> 2. `src/piranesi/scan/surface.py` — Attack surface mapping. Build `ScanResult` from Joern CPG: files_scanned, call_graph (via `cpg.method.callOut`), entry_points (Express route handlers via CPGQL), attack_surface (combine entry points with source detection).
> 3. Tests: hand-crafted TS fixtures with known taint flows. Verify CandidateFinding output matches expected. Mark integration tests `@pytest.mark.joern`.

---

### Agent 14: Phase 3 PDPA + MAS TRM Rule Encoding

**Docs:** `docs/PHASE_3_REGULATORY_ENGINE.md` (Sections 5a, 5b)

**Prompt:**
> You are encoding regulatory rules for Project Piranesi's compliance engine. Read `docs/PHASE_3_REGULATORY_ENGINE.md` Sections 5a and 5b. Build on top of the engine core (Agent 11).
>
> Implement:
> 1. `src/piranesi/legal/rules/pdpa.py` — 5 rules for Singapore PDPA Section 24 (Protection Obligation):
>    - Rule 1: any confirmed vuln (SQLi/XSS/CmdInj/PathTrav) affecting personal data → S24 breach
>    - Rule 2: Tier 1 data (NRIC/biometric) in vulnerable path → aggravated S24 (higher penalty)
>    - Rule 3: no encryption on personal data in vulnerable path → additional S24 finding
>    - Rule 4: > 500 individuals affected → mandatory notification under S26D
>    - Rule 5: third-party processor in vulnerable path → S24 + S25 obligations
>    Include PDPC enforcement precedent references (SingHealth, Grab decisions).
> 2. `src/piranesi/legal/rules/mas_trm.py` — 3 rules for MAS TRM Section 11:
>    - Rule 1: confirmed vuln in financial system → TRM 11.1 gap
>    - Rule 2: CmdInj/PathTrav in financial system → TRM 11.2 concern
>    - Rule 3: any injection vuln → TRM 11.0.5 concern (inadequate controls)
>    Note MAS TRM is guidelines (supervisory action), not statute (direct financial penalty).
> 3. `rules/pdpa.toml` and `rules/mas_trm.toml` — TOML representations of the rules loaded at runtime.
> 4. Tests: given mock findings with specific data categories and vuln classes, verify correct rules fire and correct obligations are derived.

---

## Wave 5 — After data flow extraction done

### Agent 15: Phase 1 Path Condition Extraction

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 7)

**Prompt:**
> You are implementing path condition extraction from Joern's CPG for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 7. Build on top of data flow extraction (Agent 13).
>
> Implement `src/piranesi/detect/conditions.py`:
> 1. For each data flow path in a CandidateFinding, identify branch points (if/else, switch, ternary) along the path using Joern's CPG control flow edges.
> 2. Query Joern for branch conditions: `cpg.method.name("X").ast.isControlStructure.condition.code.l`.
> 3. Parse condition text into `PathCondition` models:
>    - `typeof x === "string"` → TypeCheck(var="x", type="string")
>    - `x.length > 5` → StringLength(var="x", op="gt", n=5)
>    - `x.includes("admin")` → StringContains(var="x", substr="admin")
>    - `x === "expected"` → StringEq(var="x", val="expected")
>    - `x > 0` → IntBound(var="x", op="gt", n=0)
> 4. Determine `required_value` (true/false) — which branch the flow takes.
> 5. For unparseable conditions: store raw expression, set `symbolic_constraint = None`. Z3 (Phase 2) will skip these.
> 6. Tests: hand-crafted JS with known branch conditions. Verify extracted PathConditions.

---

### Agent 16: Phase 1 Data Category Classification

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 8)

**Prompt:**
> You are implementing data category classification for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 8. Build on top of data flow extraction (Agent 13).
>
> Implement `src/piranesi/detect/categories.py`:
> 1. For each `TaintSource` in a CandidateFinding, classify data categories using:
>    - Field name heuristics: `nric`/`ic_number` → ["nric"], `email` → ["contact_email"], `credit_card`/`cc_number` → ["financial_credit_card"], `password` → ["credentials"], etc.
>    - Route context heuristics: `/api/users/:id` → likely personal data.
>    - LLM classification (via Phase 4 provider): ask "What type of personal data is likely stored in field '{field_name}' in context of '{route_pattern}'?" Use structured output.
> 2. Populate `TaintSource.data_categories` for each source.
> 3. Category taxonomy aligned with Phase 3 PDPA tiers (Tier 1-4 from `taxonomy.py`).
> 4. Tests: verify heuristics correctly classify common field names. Test LLM fallback with mock provider.

---

### Agent 17: Phase 2 Constraint Extraction + Z3 Solver

**Docs:** `docs/PHASE_2_EXPLOIT_VERIFICATION.md` (Sections 2, 3, 4)

**Prompt:**
> You are implementing the SMT-based exploit payload generator for Project Piranesi. Read `docs/PHASE_2_EXPLOIT_VERIFICATION.md` Sections 2, 3, and 4. Input is `CandidateFinding` with `path_conditions` from Phase 1.
>
> Implement:
> 1. `src/piranesi/verify/constraints.py` — Extract exploit template from CandidateFinding: identify payload slots (which input fields carry tainted data), extract path conditions, normalize constraints. Build an `ExploitTemplate` with: vuln_class, http_method, endpoint, payload_slots, path_conditions.
> 2. `src/piranesi/verify/solver.py` — Z3 solver wrapper. Translate PathConditions to Z3 assertions: StringEq → `z3.String ==`, StringContains → `z3.Contains`, StringLength → `z3.Length` with comparison, IntBound → `z3.Int` with comparison, TypeCheck → sort constraints, LogicalAnd/Or/Not → `z3.And/Or/Not`. Add vulnerability-specific constraints per CWE (SQLi: input contains `'`; XSS: input contains `<script>`; CmdInj: input contains `;`/`|`). Timeout 30s. Return concrete payload values on SAT, UNVERIFIABLE on UNSAT/UNKNOWN.
> 3. Payload synthesis: from Z3 model, construct full HTTP request (`SynthesizedPayload`). Handle JSON bodies, URL-encoded, query params, headers. Apply encoding post-Z3.
> 4. Tests: test Z3 translation for each constraint type, test payload synthesis for SQLi/XSS/CmdInj/PathTraversal, test timeout handling, test UNSAT graceful handling.

---

### Agent 18: Phase 3 Legal Memo Generation

**Docs:** `docs/PHASE_3_REGULATORY_ENGINE.md` (Sections 6, 7)

**Prompt:**
> You are implementing the legal memo generation pipeline for Project Piranesi. Read `docs/PHASE_3_REGULATORY_ENGINE.md` Sections 6 and 7. Build on top of the engine core (Agent 11) and rule encoding (Agent 14).
>
> Implement:
> 1. `src/piranesi/legal/memo.py` — For each ConfirmedFinding: extract vuln class, data categories, severity. Assert facts into the inference engine. Run inference. Collect derived obligations. Group by framework. Render a Markdown legal memo with: finding reference, regulatory frameworks, per-framework sections (section triggered, obligation text, data categories, penalty range, notification timeline, enforcement precedents), risk assessment, recommended actions.
> 2. Every memo MUST include: `"DISCLAIMER: This analysis is informational only. It is not legal advice. Consult qualified legal counsel for regulatory compliance decisions."`
> 3. Integration layer: `assess_finding(finding: ConfirmedFinding, engine: ForwardChainingEngine) -> LegalAssessment`.
> 4. Tests: given a mock SQLi finding affecting NRIC data in a Singapore fintech context, verify the memo triggers PDPA S24 + MAS TRM 11, includes correct penalty range and notification timeline.

---

## Wave 6 — Testing wave

### Agent 19: Phase 1+2 Integration Testing

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 11), `docs/PHASE_2_EXPLOIT_VERIFICATION.md` (Section 9)

**Prompt:**
> You are writing integration tests for Piranesi's taint analysis (Phase 1) and exploit verification (Phase 2) pipelines. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 11 and `docs/PHASE_2_EXPLOIT_VERIFICATION.md` Section 9.
>
> Create test fixtures in `tests/fixtures/typescript/` — TypeScript files with comment annotations marking expected findings:
> ```typescript
> // @piranesi-expect: CWE-89, source=req.body.userId, sink=db.query
> // @piranesi-expect-clean: this parameterized query is safe
> ```
>
> Test categories (all must pass):
> 1. Simple direct taint flow (req.body → query())
> 2. Taint through function calls (req.body → helper() → query())
> 3. Sanitization (req.body → escape() → query() — should NOT be flagged)
> 4. Inter-procedural (taint crosses module boundaries)
> 5. False positive tests (parameterized queries, sanitized input, dead code)
> 6. Z3 constraint solving (given a CandidateFinding with path conditions, verify Z3 produces a valid payload)
> 7. Docker sandbox (build container for a minimal Express app, fire a payload, verify confirmation — mark `@pytest.mark.docker`)
>
> Also implement `src/piranesi/verify/reproducer.py` — generate standalone bash+curl reproducer scripts for each confirmed finding. Include safety header warning.

---

### Agent 20: Phase 3+4 Testing

**Docs:** `docs/PHASE_3_REGULATORY_ENGINE.md` (Section 8), `docs/PHASE_4_LLM_ORCHESTRATION.md` (Section 9)

**Prompt:**
> You are writing tests for Piranesi's regulatory engine (Phase 3) and LLM orchestration (Phase 4). Read `docs/PHASE_3_REGULATORY_ENGINE.md` Section 8 and `docs/PHASE_4_LLM_ORCHESTRATION.md` Section 9.
>
> Phase 3 tests (`tests/test_legal/`):
> - Forward-chaining engine: rule matching, fact derivation, fixed-point, no infinite loops.
> - Each rule set: given mock findings with specific properties, verify correct obligations derived.
> - Edge cases: no personal data (no PDPA rules fire), multiple data categories (multiple rules fire).
> - Integration: ConfirmedFinding → full memo output, verify structure and content.
>
> Phase 4 tests (`tests/test_llm/`, `tests/test_triage/`):
> - Router: model selection, budget tracking, fallback, BudgetExceededError.
> - Ensemble: majority vote, calibration math, escalation at 0.3-0.7 range.
> - Skeptic: prompt construction, verdict parsing, different-model enforcement.
> - Z3-override invariant: if finding.sandbox_result.confirmed, triage cannot downgrade.
> - Full triage pipeline (mock models): finding → ensemble → skeptic → decision.
> - Trace: JSONL output, nondeterminism detection.

---

### Agent 21: Phase 5 Baseline Integrations

**Docs:** `docs/PHASE_5_EVALUATION_HARNESS.md` (Sections 4, 5)

**Prompt:**
> You are implementing baseline comparisons for Piranesi's evaluation harness. Read `docs/PHASE_5_EVALUATION_HARNESS.md` Sections 4 and 5. Requires a working Piranesi pipeline and the ground truth dataset.
>
> Implement:
> 1. `eval/baselines/opengrep_runner.py` — run OpenGrep (preferred, LGPL-2.1 safe as subprocess) or Semgrep CE (fallback) against ground truth projects. Capture JSON output. OpenGrep: `opengrep --config=p/typescript --config=p/javascript --json`. Falls back to `semgrep` if OpenGrep unavailable.
> 2. `eval/baselines/opengrep_normalizer.py` — map rule IDs to CWE IDs, extract file/line/description, output in Piranesi's normalized finding format.
> 3. `eval/baselines/llm_only_runner.py` — single-model LLM-only baseline. Send full source files to an LLM with "identify security vulnerabilities" prompt. Parse structured JSON output. Use same model as Piranesi's detector for fair comparison.
> 4. Combined comparison report: side-by-side table (Piranesi vs OpenGrep vs LLM-only) showing precision, recall, F1, cost, FP count, regulatory mapping capability.
> 5. Tests: use pre-recorded baseline outputs (checked into fixtures) to test normalizers and scorer without requiring OpenGrep/Semgrep installation.

---

### Agent 22: Phase 2 Confirmation Logic

**Docs:** `docs/PHASE_2_EXPLOIT_VERIFICATION.md` (Sections 6, 7, 8)

**Prompt:**
> You are implementing exploit confirmation heuristics and reproducer scripts for Project Piranesi. Read `docs/PHASE_2_EXPLOIT_VERIFICATION.md` Sections 6, 7, and 8.
>
> Implement:
> 1. `src/piranesi/verify/confirm.py` — Confirmation heuristics per vulnerability class:
>    - SQLi: response contains SQL error messages, row count differs from baseline, UNION data extracted, or timing-based (> 5s for SLEEP payload) → CONFIRMED. Ambiguous response difference → LIKELY.
>    - XSS: response body contains unescaped injected script/event handler → CONFIRMED. HTML-encoded → NOT_VULNERABLE.
>    - Command injection: response contains `uid=` or `/root:` or command output → CONFIRMED.
>    - Path traversal: response contains file content from traversed path → CONFIRMED.
>    Produce `ConfirmationResult` with `level: CONFIRMED | LIKELY | UNVERIFIABLE` and `evidence: str`.
> 2. Baseline request: before firing the exploit, send a benign request to the same endpoint to capture baseline response. Compare exploit response against baseline to detect differences.
> 3. Safety: payloads are read-only (no DROP TABLE, no rm). Use `id`/`whoami`/`cat` for command injection, `OR 1=1`/`UNION SELECT` for SQLi.
> 4. Tests: mock HTTP responses for each vuln class (confirmed, likely, not vulnerable). Verify heuristics produce correct confirmation levels.

---

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
