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

## Wave 20 — v0.2.0 Release Prep

**Status:** Pending
**Deps:** Waves 12-19 complete

Release checklist: update CHANGELOG, run full eval harness, verify `uv build`, tag release.
---

## Wave 21 — Incremental Scanning + CPG Caching (v0.3.0)

**Deps:** Wave 20 (v0.2.0 release)
**Parallel agents:** Yes — incremental and caching are independent

### Agent 46: Incremental Scan Engine

**Docs:** `docs/PHASE_14_INCREMENTAL_AND_PERFORMANCE.md` (Sections 2, 5)

**Prompt:**
> You are implementing incremental scanning for Project Piranesi. Read `docs/PHASE_14_INCREMENTAL_AND_PERFORMANCE.md` Section 2.
>
> Implement `src/piranesi/scan/incremental.py`:
> 1. `FileManifest` dataclass: maps file paths to SHA-256 hashes + mtime.
> 2. `write_manifest(target_dir, output_dir)` — scan all source files, compute hashes, write `{output_dir}/_manifest.json`.
> 3. `diff_manifests(previous_manifest, current_manifest) -> IncrementalResult` — classify files as added/modified/deleted/unchanged.
> 4. Update `scan/transpile.py` to accept `changed_files: set[Path] | None`. When set, only transpile changed files.
> 5. Add `--incremental` flag to `piranesi run` and `piranesi scan` in cli.py.
> 6. When `--incremental`: load previous manifest from output_dir, diff against current files, pass changed_files to transpile.
> 7. Carry forward findings for unchanged files from previous scan's detect artifact.
> 8. Tests: create a fixture with 5 files, run full scan, modify 1 file, run incremental, verify only 1 file re-transpiled.
>
> Read `pipeline.py` (scan stage, lines 366-414) and `scan/transpile.py` for context.

### Agent 47: CPG Caching + Parallel Stages

**Docs:** `docs/PHASE_14_INCREMENTAL_AND_PERFORMANCE.md` (Sections 3, 4)

**Prompt:**
> You are implementing CPG caching and stage parallelism for Project Piranesi. Read `docs/PHASE_14_INCREMENTAL_AND_PERFORMANCE.md` Sections 3 and 4.
>
> Implement:
> 1. CPG cache key: SHA-256 of sorted file hashes + config hash. Store cached data at `{output_dir}/_cpg_cache/`.
> 2. `--no-cache` flag to force full re-scan.
> 3. Parallelize legal + patch stages in `pipeline.py` using `concurrent.futures.ThreadPoolExecutor`. Both consume verify output independently.
> 4. Add `--profile` flag that prints per-stage timing breakdown table to stderr.
> 5. Tests: verify cache hit skips transpile, verify parallel legal+patch produces same results as sequential.
>
> Read `scan/joern.py` (import_project method) and `pipeline.py` (stage runner functions).

---

## Wave 22 — Finding Suppression + Baselines (v0.3.0)

**Deps:** Wave 20
**Parallel agents:** Yes — suppression and diff are independent

### Agent 48: Suppression System

**Docs:** `docs/PHASE_15_SUPPRESSION_AND_BASELINES.md` (Section 2)

**Prompt:**
> You are implementing finding suppression for Project Piranesi. Read `docs/PHASE_15_SUPPRESSION_AND_BASELINES.md` Section 2.
>
> Implement:
> 1. `src/piranesi/detect/suppression.py`:
>    - `load_ignore_file(project_root) -> list[SuppressionRule]` — parse `.piranesi-ignore` YAML file.
>    - `parse_inline_suppressions(source_file) -> list[InlineSuppression]` — scan source for `// piranesi:suppress CWE-XX reason:"..."` comments.
>    - `apply_suppressions(findings, rules, inline) -> list[CandidateFinding]` — match findings against rules, mark `suppressed=True` on matches.
> 2. Add `suppressed: bool = False` and `suppression_reason: str | None = None` fields to `CandidateFinding` model.
> 3. Integrate into detect stage in `pipeline.py` — apply suppressions after finding extraction.
> 4. `piranesi suppress <finding-id> --reason "..." --ticket SEC-123` CLI command — appends to `.piranesi-ignore`.
> 5. Report shows: "6 findings (2 suppressed)" with suppressed findings in a separate section.
> 6. Implement stable fingerprinting: hash vuln_class + source_function_name + sink_function_name + path_length (instead of line numbers).
> 7. Tests: verify file-based suppression, inline suppression, stable fingerprints survive line shifts.

### Agent 49: Baseline Comparison + `piranesi diff`

**Docs:** `docs/PHASE_15_SUPPRESSION_AND_BASELINES.md` (Section 3)

**Prompt:**
> You are implementing baseline comparison for Project Piranesi. Read `docs/PHASE_15_SUPPRESSION_AND_BASELINES.md` Section 3.
>
> Implement:
> 1. `src/piranesi/diff.py`:
>    - `load_findings(artifact_path) -> list[Finding]` — load findings from a scan's detect/verify JSON artifact.
>    - `diff_findings(baseline, current) -> DiffResult` — match by stable fingerprint, classify as new/fixed/unchanged.
>    - `render_diff(diff_result) -> str` — human-readable diff output.
> 2. `piranesi diff <baseline_dir> <current_dir>` CLI command — compare two scan outputs, print new/fixed/unchanged.
> 3. `piranesi baseline save --from <results_dir> --to <baseline.json>` CLI command.
> 4. `--baseline <path>` flag on `piranesi run` — auto-diff against baseline after scan.
> 5. `--fail-on-new` flag — exit 1 only if NEW findings exist (unchanged findings don't fail).
> 6. Tests: create baseline with 5 findings, modify to add 1 and fix 1, verify diff shows 1 new + 1 fixed + 4 unchanged.

---

## Wave 23 — OWASP Coverage: Secrets + Misconfiguration (v0.4.0)

**Deps:** Wave 20
**Parallel agents:** Yes — secrets, CORS/headers, and dependencies are independent

### Agent 50: Secret Detection

**Docs:** `docs/PHASE_16_OWASP_COVERAGE.md` (Section 2)

**Prompt:**
> You are implementing hardcoded secret detection for Project Piranesi. Read `docs/PHASE_16_OWASP_COVERAGE.md` Section 2.
>
> Implement `src/piranesi/detect/secrets.py`:
> 1. Regex patterns for: AWS access keys (`AKIA[0-9A-Z]{16}`), Stripe keys (`sk_live_`), GitHub tokens (`ghp_`), Slack tokens (`xox[bpors]-`), SendGrid keys (`SG\.`), PEM private keys (`-----BEGIN.*PRIVATE KEY-----`).
> 2. Shannon entropy analysis: flag strings with entropy > 4.5 and length > 20 as potential secrets.
> 3. Exclusions: skip `node_modules/`, `vendor/`, `.git/`, test files (unless `--include-tests`), `.env.example`.
> 4. Produce `CandidateFinding` with `vuln_class: "CWE-798"`. Severity: CRITICAL for private keys + cloud keys, HIGH for API tokens.
> 5. Integrate as sub-stage within detect (not separate pipeline stage).
> 6. Tests: fixture files with known secrets (redacted), verify detection. Test entropy analysis. Test exclusion patterns.

### Agent 51: CORS + Security Header Detection

**Docs:** `docs/PHASE_16_OWASP_COVERAGE.md` (Section 3)

**Prompt:**
> You are implementing CORS and security header misconfiguration detection for Project Piranesi. Read `docs/PHASE_16_OWASP_COVERAGE.md` Section 3.
>
> Implement:
> 1. Add CORS source/sink specs to `scan/specs.py`: source `req.headers.origin`, sink `res.setHeader('Access-Control-Allow-Origin', origin)` — reflected origin without validation → CWE-942.
> 2. Detect wildcard CORS (`Access-Control-Allow-Origin: *`) with credentials → CWE-942.
> 3. Detect missing security headers: absence-of-pattern detection. Scan for `res.send()`/`res.render()` without upstream `res.setHeader('X-Frame-Options', ...)`. Flag missing X-Frame-Options (CWE-1021), CSP (CWE-693), HSTS (CWE-319).
> 4. Detect insecure cookie settings: `cookie: { secure: false }` (CWE-614), `httpOnly: false` (CWE-1004).
> 5. Detect missing `helmet()` middleware in Express apps.
> 6. Tests for each pattern.

### Agent 52: Dependency Vulnerability Scanning

**Docs:** `docs/PHASE_16_OWASP_COVERAGE.md` (Section 4)

**Prompt:**
> You are implementing dependency vulnerability scanning for Project Piranesi. Read `docs/PHASE_16_OWASP_COVERAGE.md` Section 4.
>
> Implement `src/piranesi/detect/dependencies.py`:
> 1. Run `npm audit --json` for Node.js projects, parse JSON output.
> 2. Run `pip-audit --format json` for Python projects (if available).
> 3. Map each advisory to a `CandidateFinding` with `vuln_class: "CWE-1395"`. Include CVE ID, affected package, version, patched version, severity.
> 4. Integrate as sub-stage within scan (runs after transpilation, before detect).
> 5. Optional `--sbom spdx` / `--sbom cyclonedx` flags to generate SBOM.
> 6. Tests: mock `npm audit` JSON output, verify findings extracted. Test graceful fallback when audit tools unavailable.

---

## Wave 24 — Additional Output Formats (v0.3.0)

**Deps:** Wave 20
**Parallel agents:** Yes — JUnit, CSV, and init are independent

### Agent 53: JUnit XML + CSV Output

**Docs:** `docs/PHASE_17_OUTPUT_FORMATS.md` (Sections 2, 3)

**Prompt:**
> You are adding JUnit XML and CSV output formats to Project Piranesi. Read `docs/PHASE_17_OUTPUT_FORMATS.md` Sections 2 and 3.
>
> Implement:
> 1. `src/piranesi/report/junit.py` — JUnit XML report. Each confirmed finding → `<testcase>` with `<failure>`. Suppressed findings → `<skipped>`. Include taint path + severity + exploit in failure message.
> 2. `src/piranesi/report/csv.py` — CSV export with columns: id, cwe_id, cwe_name, severity, source_file, source_line, sink_file, sink_line, taint_source, taint_sink, exploit_payload, regulatory_frameworks, suppressed, suppression_reason.
> 3. Add `--format junit` and `--format csv` to CLI format enum.
> 4. Wire into `report/renderer.py`.
> 5. Tests: validate JUnit XML against schema, verify CSV is importable.

### Agent 54: `piranesi init` + Configurable Exit Codes

**Docs:** `docs/PHASE_17_OUTPUT_FORMATS.md` (Sections 4, 5)

**Prompt:**
> You are adding the `piranesi init` command and configurable exit codes to Project Piranesi. Read `docs/PHASE_17_OUTPUT_FORMATS.md` Sections 4 and 5.
>
> Implement:
> 1. `piranesi init` CLI command: detect framework via `scan/framework.py`, generate `piranesi.toml` from template with framework-appropriate defaults, generate empty `.piranesi-ignore` with format comments, print next steps.
> 2. `--fail-severity {low,medium,high,critical}` flag on `piranesi run` — only exit 1 if findings at or above threshold exist.
> 3. `--no-fail` flag — always exit 0 regardless of findings.
> 4. Document exit codes 0-4 in CLI help and docs.
> 5. Tests: verify init scaffolds correct config, verify exit codes.

---

## Wave 25 — Multi-Language Joern Frontends (v1.0)

**Deps:** Wave 18 (plugin system), Wave 19 (shallow specs)
**Parallel agents:** Yes — each language is independent

### Agent 55: Python Joern Frontend Integration

**Docs:** `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` (Sections 2, 3)

**Prompt:**
> You are wiring the Python Joern frontend (`pysrc2cpg`) into Project Piranesi's pipeline. Read `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` Sections 2 and 3.
>
> Implement:
> 1. Update `scan/joern.py` `import_project()` to accept a `language` parameter and select the correct Joern frontend (`pysrc2cpg` for Python).
> 2. Update `pipeline.py` scan stage: if detected language is Python, skip tsc transpilation, pass source dir directly to Joern with pysrc2cpg.
> 3. Add Python-specific CPGQL query patterns to `scan/queries.py`: `cursor.execute()` calls, `os.system()`, `subprocess.run(shell=True)`, `eval()`, Django `Model.objects.raw()` vs safe `filter()`.
> 4. Exclude `venv/`, `.venv/`, `site-packages/` from Python scans.
> 5. Test: create a Flask fixture app with known SQLi, run full pipeline with pysrc2cpg, verify finding detected.
>
> Read `scan/joern.py` (import_project, lines 120-140) and the LANGUAGE_TO_JOERN_FRONTEND mapping.

### Agent 56: Go Joern Frontend Integration

**Docs:** `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` (Section 4)

**Prompt:**
> You are wiring the Go Joern frontend (`gosrc2cpg`) into Project Piranesi's pipeline. Read `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` Section 4.
>
> Implement:
> 1. Update `scan/joern.py` to support `gosrc2cpg`.
> 2. Skip transpilation for Go projects.
> 3. Go-specific CPGQL patterns: `db.Query(fmt.Sprintf(...))`, `exec.Command()`, `template.HTML()`, `os.Open()`.
> 4. Handle Go modules: exclude `vendor/` directory.
> 5. Test with a Gin fixture app.

### Agent 57: Java Joern Frontend Integration

**Docs:** `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` (Section 5)

**Prompt:**
> You are wiring the Java Joern frontend (`javasrc2cpg`) into Project Piranesi's pipeline. Read `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` Section 5.
>
> Implement:
> 1. Update `scan/joern.py` to support `javasrc2cpg`.
> 2. Skip transpilation for Java projects.
> 3. Spring-specific CPGQL patterns: `@RequestBody`/`@RequestParam` annotation sources, `jdbcTemplate.query(sql+input)` sinks, `@Query(nativeQuery=true)` with concatenation.
> 4. Recognize `@PreAuthorize`/`@Secured` as access control (not sinks).
> 5. Exclude `src/test/` from scans.
> 6. Test with a Spring Boot fixture app.

---

## Wave 26 — Cross-Language Taint Tracking (v1.0)

**Deps:** Wave 25 (multi-language frontends working)

### Agent 58: Cross-Language Finding Detection

**Docs:** `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` (Section 6)

**Prompt:**
> You are implementing cross-language taint tracking for Project Piranesi. Read `docs/PHASE_18_MULTI_LANGUAGE_DEPTH.md` Section 6.
>
> Implement `src/piranesi/detect/cross_language.py`:
> 1. Detect API boundaries: TypeScript `fetch('/api/endpoint')` → Python Flask route `/api/endpoint`. Match by URL path pattern.
> 2. For multi-language projects, after scanning each language independently, cross-reference: if a TS frontend sends tainted data to a URL matching a Python/Go/Java backend route with a known sink, create a cross-language `CandidateFinding`.
> 3. The finding's taint path spans both languages with a "cross-language API call" step in the middle.
> 4. Add 5+ cross-language ground truth entries.
> 5. Tests: TypeScript fixture calling a Flask API, verify cross-language finding created.

---

## Wave 27 — Ensemble Calibration (v1.0)

**Deps:** Wave 14 (ground truth), Wave 18 (ensemble voter)

### Agent 59: Calibration Pipeline + Cost Optimization

**Docs:** `docs/PHASE_19_CALIBRATION_AND_LLM_OPT.md` (Sections 2, 3, 4)

**Prompt:**
> You are implementing ensemble calibration and cost-aware model routing for Project Piranesi. Read `docs/PHASE_19_CALIBRATION_AND_LLM_OPT.md`.
>
> Implement:
> 1. `eval/calibrate.py` — run triage against all ground truth entries with each model. Record reported confidence vs actual correctness. Output `eval/calibration/{model}.json`.
> 2. Fit Platt scaling (logistic regression) per model. Compute per-CWE correction factors when n >= 10.
> 3. Update `triage/ensemble.py` to load and apply calibration data. Fall back to uncalibrated if no data.
> 4. Cost-aware routing in `llm/router.py`: estimate finding difficulty (CWE class, path length, sanitizer count), route easy findings to cheap model, hard findings to expensive model.
> 5. Optimal threshold search: find TP/FP thresholds that maximize F1 on ground truth.
> 6. Tests: verify calibration improves accuracy on held-out ground truth split, verify cost reduction.
>
> NOTE: This requires real LLM API calls (~$5-20). Run with `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` set.

---

## Wave 28 — OWASP Additional Patterns (v0.4.0)

**Deps:** Wave 23 (secrets + CORS done)
**Parallel agents:** Yes — deserialization, redirect, file upload are independent

### Agent 60: Unsafe Deserialization + Open Redirect + File Upload

**Docs:** `docs/PHASE_16_OWASP_COVERAGE.md` (Sections 5, 6, 7)

**Prompt:**
> You are adding detection for unsafe deserialization, open redirect, and unrestricted file upload to Project Piranesi. Read `docs/PHASE_16_OWASP_COVERAGE.md` Sections 5, 6, and 7.
>
> Implement source/sink specs and ground truth for:
> 1. **Unsafe deserialization** (CWE-502): `JSON.parse(userInput)` without schema validation, `yaml.load()` unsafe loader (Python), `pickle.loads()` (Python), `ObjectInputStream.readObject()` (Java).
> 2. **Open redirect** (CWE-601): `res.redirect(req.query.url)`, `Location` header from user input.
> 3. **Unrestricted file upload** (CWE-434): `multer()` without file type validation, `req.file.originalname` used in path without extension check.
> 4. Add 5+ ground truth entries per CWE class.
> 5. Tests for each pattern.

---

## Parallelization Summary (Updated)

| Wave | Milestone | Agents | Parallel? |
|------|-----------|--------|-----------|
| 20 | v0.2.0 release | — | Sequential |
| 21 | Incremental + caching | 46-47 | 2 parallel |
| 22 | Suppression + baselines | 48-49 | 2 parallel |
| 23 | OWASP secrets + misconfig | 50-52 | 3 parallel |
| 24 | Output formats | 53-54 | 2 parallel |
| 25 | Multi-language depth | 55-57 | 3 parallel |
| 26 | Cross-language taint | 58 | Sequential |
| 27 | Ensemble calibration | 59 | Sequential |
| 28 | OWASP additional | 60 | Sequential |

**Maximum parallelism per sprint:**
- v0.3.0: Waves 21+22+24 can all run simultaneously (6 agents)
- v0.4.0: Waves 23+28 can run simultaneously (4 agents)
- v1.0: Waves 25+26+27 — Wave 25 parallel (3 agents), then 26+27 sequential

