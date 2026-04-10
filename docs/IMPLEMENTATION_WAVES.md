# Implementation Waves — Agent Delegation Guide

Assumes Wave 0 (Phase 0 scaffolding + Phase 5 ground truth curation) is complete or in progress.

---

## Wave 1 — After Phase 0 is done

### Agent 3: Phase 1 Joern Validation Spike

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 2)

**Prompt:**
> You are implementing the Joern validation spike for Project Piranesi — a cybersecurity analysis tool for TypeScript/JavaScript. This is the FIRST task of Phase 1 and determines whether Joern is viable as the taint analysis backend.
>
> Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 2 for full details. Your task:
>
> 1. Verify Joern is installed (`joern --version`) and JVM 11+ is present (`java -version`). If not, install via `brew install joern` and `brew install openjdk@11`.
> 2. Select 5 public TypeScript Express projects from GitHub (varying complexity — simple REST API, Prisma-backed API, auth middleware app, Next.js API routes, async-heavy microservice).
> 3. Clone each, transpile to JS: `npx tsc --outDir /tmp/spike-<name> --declaration false --allowJs --target ES2020 --module commonjs --skipLibCheck`. Use a Piranesi-generated tsconfig (NEVER the target repo's tsconfig — security invariant).
> 4. Import each into Joern and write CPGQL queries to detect data flows for: `req.body` → `query()`/`$queryRaw()` (SQLi), `req.query` → `exec()`/`spawn()` (command injection), `req.params` → `res.send()`/`res.render()` (XSS), `req.body` → `readFile()`/`writeFile()` (path traversal).
> 5. Measure detection rate (target >= 80%), latency (target < 60s for 500 files), and false positive count.
> 6. Produce a spike report as `docs/SPIKE_JOERN_REPORT.md` with: per-project detection results, latency measurements, undetectable patterns with root causes, and a go/no-go recommendation.
>
> If detection rate >= 80%: recommend GO. If 60-80%: document gaps and whether CPGQL query improvements could close them. If < 60%: recommend NO-GO and flag for escalation.

---

### Agent 4: Phase 4 LiteLLM Wrapper + Router + Trace

**Docs:** `docs/PHASE_4_LLM_ORCHESTRATION.md` (Sections 2, 3, 7)

**Prompt:**
> You are implementing the LLM provider abstraction layer for Project Piranesi. Read `docs/PHASE_4_LLM_ORCHESTRATION.md` Sections 2, 3, and 7. The existing Phase 0 codebase is in `src/piranesi/` — use its config system (`piranesi.config`) and trace logging (`piranesi.trace`).
>
> Implement:
> 1. `src/piranesi/llm/provider.py` — LiteLLM wrapper. All LLM calls in the codebase go through this wrapper. Adds: trace logging (write TraceEntry to the trace writer), cost tracking, retry with exponential backoff + jitter (use tenacity), timeout handling. Structured output via LiteLLM's JSON mode / function calling. NEVER log API keys.
> 2. `src/piranesi/llm/router.py` — Per-stage model routing. Reads `[models]` section from `piranesi.toml`. Resolves model for each stage (scanner, detector, triage, skeptic, patcher, legal_memo). Tracks cumulative cost. Warns at `budget.warn_at_usd`. Raises `BudgetExceededError` at `budget.max_cost_usd`. Fallback logic: if primary model fails (rate limit, timeout), use `[models.fallback].default`.
> 3. `src/piranesi/llm/trace.py` — JSONL trace logging integrated with the provider. Every call logs: timestamp, stage, model, prompt_hash (SHA-256), response_hash, prompt_tokens, response_tokens, cost_usd, duration_ms, cache_hit. Optional full prompt/response when `trace.log_prompts = true`.
> 4. `src/piranesi/llm/__init__.py` — re-export provider, router.
> 5. Tests in `tests/test_llm/test_provider.py`, `test_router.py`, `test_trace.py`. Use LiteLLM's mock provider for tests. Test: model selection per stage, budget tracking, fallback, trace entry format, cost accumulation.
>
> Do NOT implement ensemble, skeptic, or cost-aware optimizer yet — those come in Wave 2.

---

### Agent 5: Phase 4 Prompt Templates + Adversarial Hardening

**Docs:** `docs/PHASE_4_LLM_ORCHESTRATION.md` (Sections 7b, 8)

**Prompt:**
> You are implementing the prompt engineering and adversarial input hardening layer for Project Piranesi. Read `docs/PHASE_4_LLM_ORCHESTRATION.md` Sections 7b and 8.
>
> Implement:
> 1. `src/piranesi/llm/sanitize.py` — Comment stripping for code sent to LLMs. Strip single-line `//`, multi-line `/* */`, and JSDoc `/** */` comments from JavaScript/TypeScript code snippets. MUST preserve line numbers (replace comments with empty lines, do not delete lines). Also implement canary detection: check if LLM response contains fragments of known system prompt templates.
> 2. `src/piranesi/llm/prompts/` — Versioned prompt templates for all 5 stages:
>    - `scanner_augment.py` — source/sink discovery prompt
>    - `triage_classify.py` — TP/FP classification prompt (structured output: verdict, confidence, explanation)
>    - `skeptic_challenge.py` — adversarial challenge prompt
>    - `patcher_fix.py` — patch generation prompt
>    - `legal_memo_draft.py` — regulatory impact prompt
>    Each module exports `VERSION: str` and `render(**kwargs) -> list[dict]` (returns LLM message array).
> 3. Tests in `tests/test_llm/test_sanitize.py` — test comment stripping preserves line numbers, handles edge cases (strings containing `//`, regex literals, template literals with `/*`), and canary detection.
> 4. Tests in `tests/test_llm/test_prompts.py` — test each prompt template renders with expected fields.
>
> SECURITY: All prompts must use structured output (function calling / tool use schemas). Never accept free-form text as the primary LLM response. Code snippets passed to prompts must go through `sanitize.strip_comments()` first.

---

## Wave 2 — After Joern spike passes + LLM wrapper done

### Agent 6: Phase 1 TypeScript Transpilation

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 3)

**Prompt:**
> You are implementing the TypeScript transpilation pipeline for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 3.
>
> Implement `src/piranesi/scan/transpile.py`:
> 1. Generate Piranesi's own minimal `tsconfig.json` in a temp directory. SECURITY INVARIANT: NEVER use the target repo's tsconfig.json (can contain compiler plugins that execute code on the host). Also ignore `.npmrc`, `.node-version`, `.nvmrc`, `.tool-versions` from the target repo.
> 2. Run `tsc --project /tmp/piranesi-tsconfig-XXXX/tsconfig.json` via `run_subprocess()` from `piranesi.ui` (or equivalent subprocess wrapper with full logging).
> 3. If `tsc` not found: try `npx tsc`. If that fails: raise with clear installation instructions.
> 4. If `tsc` fails on type errors: retry with `--skipLibCheck --noEmit false`. Log which files failed.
> 5. If > 20% of files fail: log WARNING with summary.
> 6. Parse source map `.map` files to build bidirectional line mapping: `(transpiled_file, transpiled_line) <-> (original_file, original_line)`. Implement as a `SourceMap` class with `resolve(js_file, js_line) -> (ts_file, ts_line)` method.
> 7. Tests in `tests/test_scan/test_transpile.py`: test tsconfig generation, source map parsing, line mapping accuracy (spot-check against known transformations), error handling for missing tsc, failed transpilation logging.
>
> Use the logging standards from Phase 0 — every subprocess call logged with command, exit code, stdout/stderr.

---

### Agent 7: Phase 1 Joern Server Management

**Docs:** `docs/PHASE_1_TAINT_ANALYSIS.md` (Section 4)

**Prompt:**
> You are implementing the Joern server lifecycle manager for Project Piranesi. Read `docs/PHASE_1_TAINT_ANALYSIS.md` Section 4.
>
> Implement `src/piranesi/scan/joern.py`:
> 1. `JoernServer` context manager class:
>    - `__enter__`: start Joern in server mode (`joern --server --server-host 127.0.0.1 --server-port <port>`). SECURITY: bind to 127.0.0.1 ONLY, never 0.0.0.0. Start as subprocess with stdout/stderr capture. Wait for readiness (poll health endpoint with exponential backoff, max 30s). If Joern binary not found: raise with clear installation instructions.
>    - `__exit__`: shut down server (SIGTERM, wait 5s, SIGKILL if needed). Always clean up.
>    - `import_project(path)`: POST to Joern REST API to import the transpiled JS project.
>    - `query(cpgql: str) -> dict`: execute a CPGQL query via HTTP, parse JSON response.
>    - Handle: server crash (restart once, then fail), port conflicts (try ports 8080-8089), query timeout (configurable, default 60s).
> 2. Configuration from `piranesi.toml` `[joern]` section: `binary_path`, `server_port`, `startup_timeout_seconds`, `query_timeout_seconds`, `jvm_memory` (passed as `-Xmx` to JVM).
> 3. Tests in `tests/test_scan/test_joern.py`: test server lifecycle (start/stop), port conflict handling, timeout handling. Mark integration tests with `@pytest.mark.joern` (skip if Joern not installed).
>
> Use `run_subprocess()` and structured logging throughout. Every Joern interaction logged.

---

### Agent 8: Phase 2 Docker Sandbox Runner

**Docs:** `docs/PHASE_2_EXPLOIT_VERIFICATION.md` (Section 5), `docs/ARCHITECTURE.md` (Section 8.2)

**Prompt:**
> You are implementing the Docker sandbox runner for Project Piranesi. Read `docs/PHASE_2_EXPLOIT_VERIFICATION.md` Section 5 and `docs/ARCHITECTURE.md` Section 8.2 for security invariants.
>
> Implement `src/piranesi/verify/sandbox.py`:
> 1. `build_image(target_path: str) -> str`: auto-generate a Dockerfile (NEVER use the target repo's Dockerfile or docker-compose.yml — security invariant). The generated Dockerfile: `FROM node:20-slim`, delete `.npmrc`/`.env`/`Dockerfile*`/`docker-compose*`, `npm install --production --ignore-scripts --registry https://registry.npmjs.org/`, copy source, `CMD ["npm", "start"]`. Detect start command from `package.json` `scripts.start`. Detect port from source patterns or default to 3000. Build via `docker-py`.
> 2. `start_container(image: str) -> Container`: create internal-only Docker network (`internal=True`), run container with: `read_only=True`, `tmpfs={"/tmp": "size=64m"}`, `cap_drop=["ALL"]`, `security_opt=["no-new-privileges"]`, `mem_limit="512m"`, `cpu_quota=100000`, `pids_limit=256`, `user="node"`, `log_config={"type": "json-file", "config": {"max-size": "10m", "max-file": "1"}}`. Assert: no Docker socket mount, no host volume mounts.
> 3. `wait_for_ready(host_port: int, max_wait: float = 30.0) -> bool`: poll with exponential backoff.
> 4. `fire_payload(payload, host_port) -> ExploitResult`: send HTTP request via `requests`.
> 5. `capture_results(container, exploit_result) -> SandboxCapture`: capture container logs, filesystem diff, timing.
> 6. `run_in_sandbox(target_path, payloads) -> list[SandboxCapture]`: orchestrate the full lifecycle (build → start → wait → fire → capture → teardown). Always clean up containers and networks in `finally` block.
> 7. Tests in `tests/test_verify/test_sandbox.py`: test Dockerfile generation, container security config assertions, teardown. Mark with `@pytest.mark.docker`.
>
> This module is independent of the taint engine. It just needs a target path and a list of payloads. Use structured logging for all Docker operations.

---

### Agent 9: Phase 4 Ensemble + Skeptic Agent

**Docs:** `docs/PHASE_4_LLM_ORCHESTRATION.md` (Sections 5, 6, 7b)

**Prompt:**
> You are implementing the ensemble voter and skeptic agent for Project Piranesi. Read `docs/PHASE_4_LLM_ORCHESTRATION.md` Sections 5, 6, and 7b. Build on top of the LLM provider/router from Agent 4.
>
> Implement:
> 1. `src/piranesi/triage/ensemble.py` — Calibrated ensemble voter:
>    - Run N models (configurable, default 3) in parallel on each finding.
>    - Each model produces: verdict (true_positive/false_positive), confidence (0.0-1.0), explanation.
>    - Calibration: temperature scaling per model. If calibration data not available, fall back to majority vote.
>    - Aggregation: weighted average of calibrated confidences. Weights from per-CWE historical precision (uniform if unavailable).
>    - Decision thresholds: >= 0.7 → TP, <= 0.3 → FP, 0.3-0.7 → escalate to more expensive model.
>    - Use structured output (function calling) for all model calls. Code snippets passed through `sanitize.strip_comments()` before inclusion in prompts.
> 2. `src/piranesi/triage/skeptic.py` — Adversarial skeptic agent:
>    - Uses a DIFFERENT model than the detector (configured in `piranesi.toml` `[models].skeptic`).
>    - Prompt: argue why the finding is NOT a real vulnerability (consider sanitization, framework protections, dead code, type constraints).
>    - Produces: verdict (genuine/false_positive/uncertain) + reasoning.
>    - Reasoning is included in `TriagedFinding.skeptic_analysis` for auditability.
> 3. SECURITY INVARIANT: LLM triage cannot suppress Z3-verified findings. If a finding has `sandbox_result.confirmed = True`, triage verdict is forced to "confirmed" regardless of LLM opinion. LLM triage is a PRE-filter (before verify), not a POST-filter.
> 4. Tests in `tests/test_triage/`: test majority vote, calibration math, escalation logic, skeptic prompt construction, the Z3-override invariant.

---

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

---

## Wave 8 — CI Green + Stub Resolution (COMPLETED)

All CI gates fixed (ruff, mypy, pytest), all stubs resolved. See `docs/WAVE_8_CI_GREEN.md`.

---

## Wave 9 — Individual CLI Stage Commands (COMPLETED)

Wired `piranesi scan/detect/triage/verify/legal/patch/report` to execute their respective pipeline stages independently.

---

## Wave 10 — Live E2E Integration Test (COMPLETED)

Added `tests/test_e2e.py` with `@pytest.mark.e2e` that runs the full pipeline against taint_app fixtures.

---

## Wave 11 — Ground Truth Expansion (COMPLETED)

Expanded from 20 to 50 entries (36 TPs + 14 FPs) across 5 CWE categories.

---

## Wave 12 — SARIF Output + Docker Image + CI Docs (COMPLETED)

SARIF 2.1.0 report generator (`report/sarif.py`, 349 lines), CWE metadata (`report/cwe.py`, 121 lines), Docker runtime image, CI integration docs. Tests: 261 lines.

---

## Wave 13 — False Positive Reduction (COMPLETED)

SSRF sink specs split, framework sanitizer specs added for NestJS/Next.js/Fastify, taint path confidence scoring.

---

## Wave 14 — Ground Truth Research (COMPLETED)

149 entries (113 TPs + 36 FPs) across 7 CWE categories. 80+ synthetic TypeScript fixtures in `eval/synthetic/`.

---

## Wave 15 — Multi-Framework TS/JS (COMPLETED)

NestJS (specs + tests 132 lines), Next.js (specs + tests 210 lines), Fastify (specs + tests 99 lines). All via plugin system.

---

## Wave 16 — Regulatory Expansion (COMPLETED)

CCPA/CPRA (7 rules, 126-line TOML, 82-line loader), HIPAA (5 rules), GDPR (9 rules, 154-line TOML), NIS2 (6 rules). All wired into default engine. Tests: 694 lines.

---

## Wave 17 — Community Rules + Contributing Docs (COMPLETED)

Rule auto-discovery (`rules/community/`), template TOML, `docs/contributing-rules.md` (76 lines).

---

## Wave 18 — Plugin System (COMPLETED)

`plugin.py` (558 lines) with 13 built-in framework plugins, ABC interfaces for FrameworkPlugin/RulePlugin/ReporterPlugin, entry-point discovery. Tests: 518 lines.

---

## Wave 19 — Multi-Language Shallow (COMPLETED)

Python (Flask/Django/FastAPI) specs + tests (282 lines), Go (Gin/Echo/Chi) specs, Java (Spring Boot) specs. Framework detection (`scan/framework.py`, 273 lines). Joern frontend mapping defined but not yet wired (see Wave 25).

---

## Wave 20 — v0.2.0 Release Prep (COMPLETED)

Release checklist passed: CHANGELOG, eval harness, `uv build`, version bumped to 0.2.0.

---

## Wave 21 — Incremental Scanning + CPG Caching (COMPLETED)

`scan/incremental.py` (170 lines): FileManifest, diff_manifests, `--incremental` CLI flag. CPG cache key derivation. Parallel legal+patch stages via ThreadPoolExecutor.

---

## Wave 22 — Finding Suppression + Baselines (COMPLETED)

`detect/suppression.py` (345 lines): .piranesi-ignore, inline `// piranesi:suppress CWE-XX`, stable fingerprinting. `diff.py`: baseline diff, `piranesi diff`, `piranesi baseline save`, `--fail-on-new`.

---

## Wave 23 — OWASP Coverage: Secrets + Misconfiguration (COMPLETED)

`detect/secrets.py` (337 lines): regex + Shannon entropy secret detection, CWE-798. `detect/misconfigurations.py` (370 lines): CORS, security headers, cookie settings. `detect/dependencies.py` (711 lines): npm audit + pip-audit + SBOM (SPDX/CycloneDX).

---

## Wave 24 — Additional Output Formats (COMPLETED)

`report/junit.py` (141 lines), `report/csv.py` (99 lines). `piranesi init` scaffolding. `--fail-severity`, `--no-fail`, exit codes 0-4.

---

## Wave 25 — Multi-Language Joern Frontends (COMPLETED)

`scan/joern.py` LANGUAGE_TO_JOERN_FRONTEND mapping: pysrc2cpg, gosrc2cpg, javasrc2cpg. Language-specific CPGQL patterns. Framework plugins for Flask, Django, FastAPI, Gin, Echo, Chi, Spring Boot.

---

## Wave 26 — Cross-Language Taint Tracking (COMPLETED)

`detect/cross_language.py` (349 lines): API boundary detection, cross-language CandidateFinding synthesis, TS→Python/Go/Java flow tracking.

---

## Wave 27 — Ensemble Calibration (COMPLETED)

`eval/calibrate.py` (399 lines): Platt scaling, per-CWE correction, optimal threshold search. `eval/calibration/` directory seeded (awaiting live calibration run with API keys).

---

## Wave 28 — OWASP Additional Patterns (COMPLETED)

CWE-502 (deserialization), CWE-601 (open redirect), CWE-434 (file upload) specs + sanitizers for TS/JS, Python, Go, Java. Ground truth entries added.

---

## Wave 29 — Monorepo + Workspace Scanning (COMPLETED)

`scan/monorepo.py` (933 lines): npm/Yarn/pnpm/Turborepo/Nx/Lerna/Go/Maven/Gradle workspace detection, package dependency graph, per-package parallel scanning, `--package` and `--changed-packages` flags. Cross-package taint flow merging.

---

## Wave 30 — Custom Rule Authoring + Rule Marketplace (COMPLETED)

`rules/engine.py` (1013 lines): TOML-based rule DSL, CPGQL/regex pattern compilation, rule inheritance. `rules/testing.py` (425 lines): inline test annotations, `piranesi rules test-all`. `rules/registry.py` (606 lines): git-based rule distribution, install/update/remove lifecycle, GPG signature verification.

---

## Wave 31 — Advanced Taint Analysis (COMPLETED)

`detect/interprocedural.py` (2218 lines): callback chains, promise/await, event emitter, HOF taint propagation, call-graph summaries. `detect/alias.py` (552 lines): property assignment, destructuring, spread tracking. `detect/prototype_pollution.py` (421 lines): CWE-1321 detection, recursive merge tracking. `detect/sanitizer_validation.py` (185 lines): context-sensitive sanitizer-CWE matrix, bypass pattern detection.

---

## Wave 32 — Reachability + Dead Code Pruning (COMPLETED)

`detect/reachability.py` (461 lines): call-graph BFS from entry points, dead code report, `--include-unreachable` flag. `detect/dep_reachability.py` (799 lines): import/require graph, dependency-level reachability, SCA noise reduction.

---

## Wave 33 — IDE + Editor Integration (COMPLETED)

`lsp/server.py` (820 lines): pygls-based LSP adapter, diagnostics on save, code actions, hover info. `watch.py` (525 lines): file watcher with debounce, Rich live display, `--on-finding` hook. `hooks/pre_commit.py` (165 lines): `piranesi hook install/uninstall`, `.pre-commit-hooks.yaml`, staged-only scanning.

---

## Wave 34 — Advanced Reporting + Trend Analysis (COMPLETED)

`report/trends.py` (383 lines): historical metrics, fix rate, sparkline charts, trend alerts. `report/tui.py` (741 lines): Textual TUI with vim-style navigation, taint path visualization, filter/search/export. `report/compliance.py` (583 lines): regulatory coverage matrix, gap analysis, attestation generation.

---

## Parallelization Summary (Waves 1-34)

| Wave | Milestone | Agents | Parallel? | Target |
|------|-----------|--------|-----------|--------|
| 1-7 | Core pipeline (Phases 0-6) | 3-24 | Mixed | v0.1.0 |
| 8-19 | Hardening + expansion | 25-45 | Mixed | v0.1.x |
| 20 | v0.2.0 release | — | Sequential | v0.2.0 |
| 21-28 | Incremental, OWASP, multi-lang | 46-60 | Mixed | v0.2.x |
| 29 | Monorepo support | 61-62 | 2 parallel | v1.0 |
| 30 | Custom rules | 63-65 | 3 parallel | v1.0 |
| 31 | Advanced taint | 66-68 | 3 parallel | v1.0 |
| 32 | Reachability | 69-70 | 2 parallel | v1.0 |
| 33 | IDE integration | 71-73 | 3 parallel | v1.1 |
| 34 | Advanced reporting | 74-76 | 3 parallel | v1.1 |

All waves 1-34 (phases 0-25) are **COMPLETE**. Current stats: 99 source files, ~101K LOC, 523 tests, 186 ground truth entries across 12 CWE classes, 11 CLI commands, 7 regulatory frameworks.

---

# Next Phases — v1.0 → v2.0 Roadmap

**Priority order:** Web vuln gap closure → Depth → Ground truth → Compliance → Language breadth

**Known gap (OWASP Top 10 2021):** Current 12 CWEs are strong on injection/traversal but miss A01 (Broken Access Control: IDOR, CSRF, privilege escalation), A02 (Cryptographic Failures: weak crypto, cleartext transport), A07 (Auth Failures: broken auth, session fixation), and injection variants (NoSQL, SSTI, ReDoS, LDAP). Phases 26-28 close these gaps.

---

## Wave 35 — Web Vulnerability Gap Closure (v1.0)

**Deps:** Wave 28 (OWASP additional patterns)
**Parallel agents:** Yes — all 3 phases are independent (different CWE families)
**Docs:** `docs/PHASE_26_WEB_VULN_AUTH_ACCESS.md`, `docs/PHASE_27_WEB_VULN_INJECTION_VARIANTS.md`, `docs/PHASE_28_WEB_VULN_CRYPTO_TRANSPORT.md`

### Agent 77: Auth & Access Control Detection (Phase 26)

**Prompt:**
> You are implementing authentication and access control vulnerability detection for Project Piranesi. Read `docs/PHASE_26_WEB_VULN_AUTH_ACCESS.md`.
>
> Implement `src/piranesi/detect/auth_access.py`:
> 1. CSRF detection (CWE-352): identify POST/PUT/DELETE handlers without CSRF middleware in the middleware chain. Framework-specific: Express `csurf`, NestJS `@Guard`, Django `csrf_exempt` detection, Flask WTForms, Spring `CsrfFilter`.
> 2. IDOR detection (CWE-639): heuristic — route param `:id` flows to `db.find(id)` without ownership check (`WHERE user_id = session.user`). Confidence 0.5-0.6 due to high FP risk.
> 3. Broken auth (CWE-287): JWT `alg=none` acceptance, symmetric/asymmetric confusion, missing expiry validation, password comparison without timing-safe equality, cookie without `httpOnly`/`secure`/`sameSite`.
> 4. Session fixation (CWE-384): session not regenerated after auth (`req.session.regenerate` absence after login).
> 5. Mass assignment (CWE-915): `Model.create(req.body)` without field allowlist — Sequelize, Mongoose, Prisma, Django ModelForm, Spring `@RequestBody`.
> 6. Privilege escalation (CWE-269): admin routes without auth middleware, heuristic `/admin/*` path matching.
> 7. Missing auth for critical function (CWE-306): sensitive endpoints (user deletion, password reset, payment) without auth middleware.
> 8. Add all specs to `scan/specs.py`. Tests: 35+ GT entries (5 per CWE), both vulnerable and safe patterns.

### Agent 78: Injection Variant Detection (Phase 27)

**Prompt:**
> You are implementing injection variant detection for Project Piranesi. Read `docs/PHASE_27_WEB_VULN_INJECTION_VARIANTS.md`.
>
> Implement:
> 1. `src/piranesi/detect/injection_variants.py` — taint-based detection for:
>    - CWE-943 NoSQL injection: user input → MongoDB `find({field: input})`, `$where`, `$regex` operator injection. Sinks: Mongoose `Model.find(req.body)`, MongoDB native driver, PyMongo, mgo, Spring Data MongoDB. Sanitizer: mongo-sanitize, schema validation.
>    - CWE-1336 SSTI: user input → template engine `compile(userInput)` or `Template(user_input).render()`. Cover Handlebars, EJS, Pug, Nunjucks, Jinja2, Mako, Thymeleaf, Freemarker. Key: `Template(user_string)` is vulnerable, `template.render(context={key: user_string})` is safe.
>    - CWE-90 LDAP injection: user input concatenated into LDAP filter string. Sinks: `ldap.search(filter)`, `ldapjs`, `python-ldap`, `go-ldap`.
>    - CWE-113 HTTP header injection: user input → `res.setHeader()`, `res.redirect()` without CRLF stripping.
>    - CWE-917 Expression language injection: SpEL, OGNL, MVEL — `ExpressionParser.parseExpression(userInput)`.
>    - CWE-643 XPath injection: user input concatenated into XPath query.
> 2. `src/piranesi/detect/redos.py` — static regex analysis for CWE-1333:
>    - Scan all regex literals and `new RegExp()` calls.
>    - Detect catastrophic backtracking: nested quantifiers `(a+)+`, overlapping alternations.
>    - Also taint: `new RegExp(userInput)` = regex injection.
> 3. All specs in `scan/specs.py`. Tests: 35+ GT entries total.

### Agent 79: Crypto & Transport Detection (Phase 28)

**Prompt:**
> You are implementing cryptographic and transport security detection for Project Piranesi. Read `docs/PHASE_28_WEB_VULN_CRYPTO_TRANSPORT.md`.
>
> Implement `src/piranesi/detect/crypto_transport.py`:
> 1. Weak crypto (CWE-327/328): pattern scan for MD5, SHA1, DES, 3DES, RC4, ECB mode in security contexts. JS: `crypto.createHash('md5')`, `CryptoJS.MD5()`. Python: `hashlib.md5()`. Go: `crypto/md5`. Java: `MessageDigest.getInstance("MD5")`. Context-sensitive: MD5 for file checksums should NOT trigger.
> 2. Inadequate key length (CWE-326): RSA < 2048, EC < 256, AES < 128.
> 3. Cleartext transmission (CWE-319): `http://` in fetch/axios/request (excluding localhost), TLS < 1.2, `rejectUnauthorized: false`.
> 4. Improper cert validation (CWE-295): `rejectUnauthorized: false`, `verify=False`, `InsecureSkipVerify: true`, custom TrustManager accepting all certs.
> 5. Weak PRNG (CWE-338): `Math.random()` flowing to token/key/nonce generation. Taint: `Math.random()` → security-sensitive sink.
> 6. JWT verification issues (CWE-347): `alg=none`, missing `algorithms` option, symmetric/asymmetric confusion, hardcoded secret.
> 7. All specs in `scan/specs.py`. Tests: 40+ GT entries. Both vulnerable and safe patterns per CWE.

---

## Wave 36 — Ground Truth Research III: CVE Mining (v1.0)

**Deps:** Wave 35 (new CWE specs must exist before GT for them)
**Parallel agents:** Yes — 3 agents mining different CWE tiers simultaneously
**Docs:** `docs/PHASE_29_GROUND_TRUTH_CVE_MINING.md`

### Agent 80: Tier 1 CVE Mining — Core Injection CWEs

**Prompt:**
> You are expanding ground truth for Project Piranesi's core injection CWEs. Read `docs/PHASE_29_GROUND_TRUTH_CVE_MINING.md` Section 3 Tier 1.
>
> Tasks:
> 1. Mine NVD/GHSA for real-world CVEs affecting TypeScript/JavaScript web apps for CWE-89 (SQLi), CWE-79 (XSS), CWE-78 (CmdInj), CWE-22 (PathTraversal).
> 2. For each CVE: locate the fix commit, extract vulnerable code (parent commit), create minimal fixture (< 100 lines).
> 3. Target: 40 new entries (10 per CWE) — focus on complex, multi-step, and inter-procedural patterns the existing GT doesn't cover.
> 4. Include `cve_id`, `ghsa_id`, `fix_commit` fields in YAML entries.
> 5. Both TP and FP entries (correctly patched versions = FP entries).
> 6. Write entries as `eval/ground_truth/gt-187.yaml` through `gt-226.yaml`. Fixtures in `eval/cve_fixtures/`.
> 7. Validate each fixture compiles and is self-contained.

### Agent 81: Tier 2 CVE Mining — New CWE Categories

**Prompt:**
> You are creating ground truth for newly added CWE categories in Project Piranesi. Read `docs/PHASE_29_GROUND_TRUTH_CVE_MINING.md` Section 3 Tier 2.
>
> Tasks:
> 1. Mine NVD/GHSA for CVEs matching: CWE-352 (CSRF), CWE-943 (NoSQL injection), CWE-327 (Weak crypto), CWE-502 (Deserialization), CWE-287 (Broken auth), CWE-1336 (SSTI), CWE-338 (Weak PRNG), CWE-295 (Cert validation).
> 2. Target: 50 new entries (~6 per CWE).
> 3. Focus on real CVEs from popular npm packages, Python libs, Go modules, Java Spring apps.
> 4. Include CVE references, fix commits, minimal fixtures.
> 5. Write entries as `eval/ground_truth/gt-227.yaml` through `gt-276.yaml`.

### Agent 82: Tier 3 CVE Mining — Complex Patterns + Tooling

**Prompt:**
> You are creating complex-pattern ground truth and mining tooling for Project Piranesi. Read `docs/PHASE_29_GROUND_TRUTH_CVE_MINING.md` Sections 3 (Tier 3), 5.
>
> Tasks:
> 1. Create 30 GT entries for complex patterns: multi-step vulns (3+ taint steps), cross-module flows, second-order injection (stored → retrieved → sink), framework-specific patterns (10 per framework: Express, NestJS, Django).
> 2. Write entries as `eval/ground_truth/gt-277.yaml` through `gt-306.yaml`.
> 3. Implement `eval/mine_cves.py`: NVD API v2 query script, filter by CWE + language, output candidate list as JSON.
> 4. Implement `eval/extract_fixture.py`: given GitHub repo + commit range, extract minimal vulnerable code.
> 5. Implement `eval/validate_fixture.py`: run Piranesi against a fixture, check detection matches GT.
> 6. Implement `eval/validate_all.py`: batch validate all GT entries, report per-CWE detection rate.

---

## Wave 37 — Ground Truth Research IV: Multi-Language (v1.0)

**Deps:** Wave 25 (multi-language Joern frontends)
**Parallel agents:** Yes — 3 agents per language ecosystem
**Docs:** `docs/PHASE_30_GROUND_TRUTH_MULTI_LANG.md`

### Agent 83: Python Ground Truth (Phase 30)

**Prompt:**
> You are creating Python web security ground truth for Project Piranesi. Read `docs/PHASE_30_GROUND_TRUTH_MULTI_LANG.md` Section 2.
>
> Create 50 GT entries:
> - 15 Flask: SQLi via `db.execute(f"...")`, XSS via `render_template_string()`, SSTI, CmdInj via `subprocess.run(f"...", shell=True)`, SSRF via `requests.get(user_url)`
> - 15 Django: ORM bypass via `extra()`, `raw()`, mass assignment via ModelForm without `fields`, CSRF exempt views, debug mode detection
> - 10 FastAPI: request body injection, Pydantic bypass, dependency injection misuse
> - 10 General Python: `pickle.loads()`, `yaml.load()`, `eval()`, `exec()`, `os.system()`
> - Each entry: self-contained fixture in `eval/fixtures/python/{framework}/`, YAML in `eval/ground_truth/gt-307.yaml` through `gt-356.yaml`

### Agent 84: Go Ground Truth (Phase 30)

**Prompt:**
> You are creating Go web security ground truth for Project Piranesi. Read `docs/PHASE_30_GROUND_TRUTH_MULTI_LANG.md` Section 3.
>
> Create 40 GT entries:
> - 12 Gin: SQLi via `db.Raw("..." + c.Query("id"))`, XSS via `c.Writer.Write([]byte(input))`, path traversal via `c.File(c.Param("path"))`, SSRF
> - 10 Echo: similar patterns, middleware bypass
> - 8 Chi: auth gaps, middleware analysis
> - 10 net/http stdlib: direct handler vulns, template injection
> - Each entry: fixture in `eval/fixtures/go/{framework}/`, YAML in `gt-357.yaml` through `gt-396.yaml`

### Agent 85: Java + Advanced Patterns Ground Truth (Phase 30)

**Prompt:**
> You are creating Java and advanced-pattern ground truth for Project Piranesi. Read `docs/PHASE_30_GROUND_TRUTH_MULTI_LANG.md` Sections 4, 5.
>
> Create 70 GT entries:
> - 20 Spring Boot: SQLi via JdbcTemplate, SpEL injection, mass assignment, SSRF via RestTemplate, deserialization via ObjectInputStream
> - 10 Servlet: direct `request.getParameter()` → sink
> - 10 General Java: `Runtime.exec()`, `ProcessBuilder`, `XMLDecoder`, `ObjectInputStream`
> - 30 Advanced patterns (any language): race conditions (TOCTOU), second-order injection, multi-step chains (3+ steps), business logic flaws (price manipulation), timing attacks, prototype chain attacks
> - YAML in `gt-397.yaml` through `gt-466.yaml`

---

## Wave 38 — Analysis Depth: Field-Sensitive Taint (v1.0)

**Deps:** Wave 31 (advanced taint — existing alias/interprocedural analysis)
**Parallel agents:** 2 — core engine + GT/testing are independent
**Docs:** `docs/PHASE_31_FIELD_SENSITIVE_TAINT.md`

### Agent 86: Field-Sensitive Taint Engine (Phase 31)

**Prompt:**
> You are implementing field-sensitive taint tracking for Project Piranesi. Read `docs/PHASE_31_FIELD_SENSITIVE_TAINT.md` Sections 2-6.
>
> Implement `src/piranesi/detect/field_taint.py`:
> 1. Replace binary `tainted: bool` with `taint_labels: dict[str, TaintLabel]` per variable.
> 2. Propagation rules: property read (`obj.x` → label for "x"), destructuring (`const {x,y} = obj` → separate labels), spread (`{...obj}` → all labels), computed property (`obj[key]` → conservative all).
> 3. Post-process Joern flows: for each flow step, query AST for property access / destructuring, build field-level flow overlay, prune flows where specific field is NOT tainted at sink.
> 4. Integration: `detect/flows.py` calls `prune_untainted_fields()` on each CandidateFinding.
> 5. Performance: only analyze flows where source is multi-field (req.body, req.query). Skip single-field sources.
> 6. Caching: field summaries per function, reuse across call sites.

### Agent 87: Field-Sensitive GT + Testing (Phase 31)

**Prompt:**
> You are writing ground truth and tests for field-sensitive taint analysis. Read `docs/PHASE_31_FIELD_SENSITIVE_TAINT.md` Sections 7-8.
>
> Tasks:
> 1. 20+ fixtures in `tests/fixtures/typescript/field_sensitive/`: destructuring with mixed safe/tainted fields, spread with override, nested property access, computed properties, JSON.parse/stringify propagation.
> 2. 15 new GT entries specifically for field-sensitive patterns in `eval/ground_truth/`.
> 3. FP reduction benchmark: fixtures that current analysis flags but field-sensitive should not.
> 4. TP preservation: fixtures that field-sensitive must still detect.
> 5. Test suite: `tests/test_detect/test_field_taint.py` — verify propagation rules, pruning logic, integration with flows.py.

---

## Wave 39 — Analysis Depth: Symbolic Execution + ML + Incremental CPG (v1.0)

**Deps:** Wave 38 (field-sensitive taint — benefits from precision)
**Parallel agents:** Yes — all 3 are independent analysis improvements
**Docs:** `docs/PHASE_32_SYMBOLIC_EXECUTION.md`, `docs/PHASE_33_ML_FP_REDUCTION.md`, `docs/PHASE_34_INCREMENTAL_CPG.md`

### Agent 88: Concolic Execution Engine (Phase 32)

**Prompt:**
> You are implementing a concolic execution engine for Project Piranesi. Read `docs/PHASE_32_SYMBOLIC_EXECUTION.md`.
>
> Implement `src/piranesi/verify/concolic.py`:
> 1. Input: CandidateFinding with taint path + AST. Execute symbolically along taint path: concrete values where known, Z3 symbolic variables where tainted.
> 2. At each branch: fork state, add path constraint. At sink: solve all constraints with Z3.
> 3. JS/TS symbolic semantics: string concat → `Z3.Concat`, `.slice()` → `Z3.SubString`, `.indexOf()` → `Z3.IndexOf`, numeric ops → Z3 arithmetic, type coercion approximation.
> 4. Loop handling: bounded unrolling (k=3 default), havoc for remaining iterations.
> 5. Path explosion mitigation: prioritize sink-reaching paths (guided by Joern taint path), merge states at join points with ITE, timeout per finding (120s), max 100 paths.
> 6. Integration: `solver.py` (fast, simple) → `concolic.py` (slow, thorough) for UNKNOWN findings.
> 7. Tests: 15+ fixtures with complex path conditions, infeasible path pruning, timeout handling.

### Agent 89: ML FP Classifier (Phase 33)

**Prompt:**
> You are implementing an ML-based false positive classifier for Project Piranesi. Read `docs/PHASE_33_ML_FP_REDUCTION.md`.
>
> Implement:
> 1. `src/piranesi/triage/ml_classifier.py`: feature extraction (18 features: cwe_id, confidence, taint_path_length, has_sanitizer, source_type, sink_type, framework, etc.), model loading, prediction with calibrated probability.
> 2. `eval/train_classifier.py`: load GT, extract features, train Random Forest + Logistic Regression + GBT, stratified k-fold CV, optimize for recall >= 95%, serialize to `models/fp_classifier.pkl`.
> 3. `eval/active_learn.py`: run classifier on unlabeled findings, select uncertain (0.4-0.6 confidence), prompt user for label, retrain.
> 4. Pipeline integration: ML pre-filter runs BEFORE LLM triage. Config: `[triage] ml_prefilter = true`.
> 5. `piranesi model info` and `piranesi model train` CLI commands.
> 6. scikit-learn as dev dependency only — not required for scanning.
> 7. Tests: feature extraction from mock findings, model train/predict on GT subset, integration test.

### Agent 90: Incremental CPG Engine (Phase 34)

**Prompt:**
> You are implementing incremental CPG updates for Project Piranesi. Read `docs/PHASE_34_INCREMENTAL_CPG.md`.
>
> Implement:
> 1. `src/piranesi/scan/cpg_diff.py`: function-level AST diffing for changed files, selective Joern re-query for changed functions only.
> 2. `PiranesiCPG`: lightweight Python graph of {functions, calls, dataflows} extracted from Joern. Cache in `.piranesi-cache/cpg/`. On incremental: update PiranesiCPG, only invoke Joern for new/changed function taint queries.
> 3. Call graph invalidation: when function F changes, invalidate edges from/to F, taint flows through F, taint summaries for F. Transitive invalidation max depth 3.
> 4. Cache management: `.piranesi-cache/cpg/{project_hash}/`, LRU eviction at 500MB. `piranesi cache info` and `piranesi cache clear` CLI commands.
> 5. Correctness invariant: `piranesi scan --verify-incremental` runs both modes and compares. If mismatch: use full scan, invalidate cache.
> 6. Performance targets: single file change < 3s, 5 files < 10s, 20+ files → full scan fallback.
> 7. Tests: fixture project, modify 1/5/20 files, verify identical results to full scan, benchmark timing.

---

## Wave 40 — Compliance Expansion (v1.0)

**Deps:** Wave 35 (new CWE detections feed into compliance mappings)
**Parallel agents:** Yes — all 3 are independent framework mappings
**Docs:** `docs/PHASE_35_COMPLIANCE_SOC2_PCIDSS.md`, `docs/PHASE_36_COMPLIANCE_ISO_NIST.md`, `docs/PHASE_37_VULN_DB_SYNC.md`

### Agent 91: SOC 2 + PCI-DSS Compliance (Phase 35)

**Prompt:**
> You are implementing SOC 2 and PCI-DSS compliance mapping for Project Piranesi. Read `docs/PHASE_35_COMPLIANCE_SOC2_PCIDSS.md`.
>
> Implement:
> 1. `src/piranesi/legal/rules/soc2.py` + `rules/soc2.toml`: 15 rules mapping CWE findings to SOC 2 Trust Services Criteria (CC6.1 logical access, CC6.6 external threats, CC6.7 asset restriction, CC6.8 malware, CC7.1 monitoring, CC8.1 change management).
> 2. `src/piranesi/legal/rules/pci_dss.py` + `rules/pci_dss.toml`: 20 rules mapping to PCI-DSS v4.0 requirements (Req 3.4 stored data, Req 4.1 transit, Req 6.2-6.5 secure dev, Req 8.3 auth, Req 10.2 logs, Req 11.3 scanning).
> 3. PCI-DSS scope detection: heuristic for payment processing code (keywords: stripe, payment, card, checkout, billing).
> 4. Evidence generation: `piranesi compliance evidence --framework soc2 --output evidence/`. JSON bundles with finding count, affected files, scan date.
> 5. Update `report/compliance.py` with SOC 2 and PCI-DSS sections.
> 6. Tests: 20+ cases mapping findings to correct controls.

### Agent 92: ISO 27001 + NIST CSF + CIS Compliance (Phase 36)

**Prompt:**
> You are implementing ISO 27001, NIST CSF 2.0, and CIS Benchmarks compliance for Project Piranesi. Read `docs/PHASE_36_COMPLIANCE_ISO_NIST.md`.
>
> Implement:
> 1. `src/piranesi/legal/rules/iso27001.py` + `rules/iso27001.toml`: 18 rules mapping to ISO 27001:2022 Annex A controls (A.8.3 access restriction, A.8.7 malware, A.8.8 vuln management, A.8.24 cryptography, A.8.25 SDL, A.8.28 secure coding).
> 2. `src/piranesi/legal/rules/nist_csf.py` + `rules/nist_csf.toml`: 15 rules mapping to NIST CSF 2.0 functions (IDENTIFY, PROTECT, DETECT, RESPOND) and subcategories (PR.DS, PR.AC, PR.IP, DE.CM).
> 3. `src/piranesi/legal/rules/cis.py` + `rules/cis.toml`: 8 rules for CIS Controls v8 (Control 16 Application Security).
> 4. Maturity scoring (0-5) per framework: `piranesi compliance maturity` CLI. Historical maturity via trends.py.
> 5. Unified compliance dashboard: update `report/compliance.py` for 10+ total frameworks.
> 6. Tests: per-framework rule tests, maturity scoring tests.

### Agent 93: Vulnerability Database Live Sync (Phase 37)

**Prompt:**
> You are implementing a unified vulnerability database for Project Piranesi. Read `docs/PHASE_37_VULN_DB_SYNC.md`.
>
> Implement:
> 1. `src/piranesi/advisory/db.py`: SQLite at `.piranesi-cache/advisory.db`. Schema: advisories (advisory_id, cve_id, ghsa_id, cwe_ids, affected_packages, affected_versions, severity, cvss_score, epss_score, exploit_available, fix_version, source). Incremental sync.
> 2. Advisory sources: NVD REST API v2, GHSA GraphQL API, OSV REST API, Go vuln DB. Normalize all to common `Advisory` model.
> 3. EPSS integration: query `api.first.org/data/v1/epss`, enrich advisories. EPSS > 0.5 = "actively exploited risk" label.
> 4. Exploit availability: check Metasploit modules, PoC-in-GitHub. Labels: none/poc_available/weaponized/in_the_wild.
> 5. Enhanced dep scanning: parse lockfiles (package-lock.json, yarn.lock, Pipfile.lock, go.sum, pom.xml), query local advisory DB instead of npm audit.
> 6. Offline mode: `piranesi advisory export/import` for air-gapped environments.
> 7. `piranesi advisory sync` CLI command.
> 8. Tests: mock API responses, version range matching, EPSS enrichment.

---

## Wave 41 — Threat Modeling + Context-Sensitive Analysis (v1.1)

**Deps:** Wave 40 (compliance data enriches threat models), Wave 38 (field-sensitive taint)
**Parallel agents:** Yes — threat modeling and k-CFA are independent
**Docs:** `docs/PHASE_38_THREAT_MODELING.md`, `docs/PHASE_41_CONTEXT_SENSITIVE_CFA.md`

### Agent 94: Threat Modeling Engine (Phase 38)

**Prompt:**
> You are implementing automated threat modeling for Project Piranesi. Read `docs/PHASE_38_THREAT_MODELING.md`.
>
> Implement:
> 1. `src/piranesi/threat/stride.py`: CWE → STRIDE mapping table. Classify each finding (Spoofing: CWE-287/306/352, Tampering: CWE-89/79/78/915, Repudiation: CWE-384, Info Disclosure: CWE-22/918/327, DoS: CWE-1333, Elevation: CWE-639/269/502).
> 2. `src/piranesi/threat/dread.py`: DREAD scoring per finding (Damage, Reproducibility, Exploitability, Affected Users, Discoverability). Normalize 0-10.
> 3. `src/piranesi/threat/attack_tree.py`: generate attack trees for high-severity findings. Root = attacker goal, branches = steps, leaves = techniques. Output: Markdown, JSON, Mermaid.
> 4. `src/piranesi/threat/dfd.py`: extract data flow diagrams from Joern call graph. Elements: external entities, processes, data stores, trust boundaries. Output: Mermaid flowchart.
> 5. `piranesi threat model` CLI: STRIDE classify → DREAD score → top-5 attack trees → DFD → Markdown report.
> 6. Tests: STRIDE mapping, DREAD scoring, attack tree structure, DFD elements.

### Agent 95: Context-Sensitive Call Graph (Phase 41)

**Prompt:**
> You are implementing k-CFA context-sensitive call graph analysis for Project Piranesi. Read `docs/PHASE_41_CONTEXT_SENSITIVE_CFA.md`.
>
> Implement `src/piranesi/detect/cfa.py`:
> 1. 1-CFA: separate function summary per call site. Context string: `(caller_method, call_site_line)`.
> 2. Per-context taint summaries: `TaintSummary` keyed by context. When analyzing `f` called from `a`, only propagate `a`'s taint.
> 3. Dynamic dispatch resolution: JavaScript `obj.method()` resolved by constructor analysis. TypeScript leverages type annotations. Python MRO approximation.
> 4. Performance: context budget (max 1000 per function), hot function fallback to 0-CFA, memoization for identical taint states, lazy context splitting.
> 5. Config: `[detect] context_sensitivity = 1` (0=current, 1=1-CFA, 2=2-CFA). Fallback on time budget exceeded.
> 6. Integration: replace call graph in `detect/interprocedural.py`.
> 7. Tests: 15+ fixtures, FP comparison (0-CFA vs 1-CFA), performance benchmark, TP regression check.

---

## Wave 42 — Web Language Breadth: PHP + Ruby (v1.1)

**Deps:** Wave 35 (CWE specs defined), Wave 25 (Joern multi-language)
**Parallel agents:** Yes — PHP and Ruby are fully independent
**Docs:** `docs/PHASE_39_PHP_WEB_SECURITY.md`, `docs/PHASE_40_RUBY_RAILS_SECURITY.md`

### Agent 96: PHP Web Security (Phase 39)

**Prompt:**
> You are implementing PHP web security analysis for Project Piranesi. Read `docs/PHASE_39_PHP_WEB_SECURITY.md`.
>
> Implement:
> 1. PHP source/sink specs in `scan/specs.py`: Sources (`$_GET`, `$_POST`, `$_REQUEST`, `$_COOKIE`, Laravel `$request->input()`, Symfony `$request->get()`). Sinks by CWE: `mysqli_query()`, `echo`, `exec()`, `include()`, `unserialize()`, `curl_exec()`.
> 2. Framework plugins: Laravel (middleware chain, `$guarded`/`$fillable`, `@csrf`), Symfony (security.yaml, voters), WordPress (`esc_html()`, `wp_nonce_field()`, `$wpdb->prepare()`).
> 3. PHP-specific: variable variables `$$var`, type juggling `==` vs `===` (CWE-1289), `extract()` mass assignment, deserialization gadget chains (`__wakeup`, `__destruct`).
> 4. Sanitizer specs: `htmlspecialchars()`, `addslashes()`, PDO prepared statements, `filter_var()`, Laravel `e()`, WordPress `esc_html()`/`esc_attr()`/`esc_url()`.
> 5. Joern PHP frontend: `php2cpg`. Update `scan/joern.py` LANGUAGE_TO_JOERN_FRONTEND.
> 6. 25+ GT entries. Fixtures: Laravel app, WordPress plugin, Symfony controller, raw PHP.

### Agent 97: Ruby/Rails Web Security (Phase 40)

**Prompt:**
> You are implementing Ruby/Rails web security analysis for Project Piranesi. Read `docs/PHASE_40_RUBY_RAILS_SECURITY.md`.
>
> Implement:
> 1. Ruby source/sink specs: Sources (`params`, `request.body`, `cookies`, `ENV`). Sinks: ActiveRecord `.where("name = '#{params}'")`, `.find_by_sql()`, `raw()` helper, `html_safe`, `system()`, `IO.popen()`, `File.read(params[:path])`, `Marshal.load()`, `YAML.load()`.
> 2. Rails-specific: Strong Parameters analysis (`params.require().permit()` vs `permit!`), CSRF `protect_from_forgery` checks, `skip_before_action :verify_authenticity_token` detection, render injection `render params[:action]`.
> 3. Sanitizers: ERB auto-escaping `<%= %>`, `sanitize()`, `ActiveRecord::Base.sanitize_sql()`, `permit()`.
> 4. Gem vulnerability scanning: parse `Gemfile.lock`, query advisory DB (Phase 37) + Ruby Advisory Database.
> 5. Joern Ruby frontend: `rubysrc2cpg`. Update `scan/joern.py`.
> 6. 20+ GT entries. Fixtures: Rails controller, Sinatra app, raw Ruby.

---

## Parallelization Summary (Full Roadmap)

| Wave | Milestone | Agents | Parallel? | Target |
|------|-----------|--------|-----------|--------|
| 1-7 | Core pipeline (Phases 0-6) | 3-24 | Mixed | v0.1.0 |
| 8-19 | Hardening + expansion | 25-45 | Mixed | v0.1.x |
| 20 | v0.2.0 release | — | Sequential | v0.2.0 |
| 21-28 | Incremental, OWASP, multi-lang | 46-60 | Mixed | v0.2.x |
| 29-34 | Monorepo, rules, taint, LSP, reporting | 61-76 | Mixed | v1.0/1.1 |
| **35** | **Web vuln gap closure** | **77-79** | **3 parallel** | **v1.0** |
| **36** | **Ground truth CVE mining** | **80-82** | **3 parallel** | **v1.0** |
| **37** | **Ground truth multi-language** | **83-85** | **3 parallel** | **v1.0** |
| **38** | **Field-sensitive taint** | **86-87** | **2 parallel** | **v1.0** |
| **39** | **Symbolic exec + ML + incr CPG** | **88-90** | **3 parallel** | **v1.0** |
| **40** | **Compliance expansion** | **91-93** | **3 parallel** | **v1.0** |
| **41** | **Threat modeling + k-CFA** | **94-95** | **2 parallel** | **v1.1** |
| **42** | **PHP + Ruby** | **96-97** | **2 parallel** | **v1.1** |

**Maximum parallelism for next sprints:**
- **Sprint 1 (v1.0 foundation):** Wave 35 — 3 agents (web vuln gaps). Independent, no deps.
- **Sprint 2 (v1.0 GT + depth):** Waves 36+37+38 can run simultaneously (8 agents). Wave 36/37 depend on Wave 35 specs. Wave 38 depends on Wave 31.
- **Sprint 3 (v1.0 analysis + compliance):** Waves 39+40 run simultaneously (6 agents). Wave 39 benefits from Wave 38 precision. Wave 40 benefits from Wave 35 CWEs.
- **Sprint 4 (v1.1 ecosystem):** Waves 41+42 run simultaneously (4 agents). Both independent.

**Critical path to v1.0:** Wave 35 (web vuln gaps) → Wave 36/37 (GT) → Wave 38 (field taint) → Wave 39 (depth). Compliance (Wave 40) runs in parallel with depth.

**Total new agents:** 21 (agents 77-97)
**Total new phases:** 16 (phases 26-41)
**Target CWE expansion:** 12 → 25+ CWE classes
**Target GT expansion:** 186 → 466+ entries
**Target compliance frameworks:** 7 → 12+
**Target languages:** 4 → 6 (adding PHP, Ruby)