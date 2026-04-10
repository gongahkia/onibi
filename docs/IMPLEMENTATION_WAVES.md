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

# Next Phases — v1.0 → v2.0 Roadmap

All waves 1-28 (phases 0-19) are implemented. The following phases define the next expansion arcs for production hardening, ecosystem reach, and analytical depth.

---

## Wave 29 — Monorepo + Workspace Scanning (v1.0)

**Deps:** Wave 21 (incremental scanning)
**Parallel agents:** Yes — monorepo detection and per-package scanning are independent
**Docs:** `docs/PHASE_20_MONOREPO_AND_WORKSPACES.md`

### Agent 61: Monorepo Detection + Package Graph

**Prompt:**
> You are implementing monorepo-aware scanning for Project Piranesi. Read `docs/PHASE_20_MONOREPO_AND_WORKSPACES.md` Section 2.
>
> Implement `src/piranesi/scan/monorepo.py`:
> 1. Detect monorepo structure: npm workspaces (`package.json` `workspaces` field), Yarn workspaces, pnpm workspaces (`pnpm-workspace.yaml`), Turborepo (`turbo.json`), Nx (`nx.json`), Lerna (`lerna.json`), Go multi-module (`go.work`), Maven multi-module (parent `pom.xml` with `<modules>`), Gradle multi-project (`settings.gradle` with `include`).
> 2. Build package dependency graph: for each workspace package, resolve internal dependencies (packages that import other workspace packages).
> 3. `MonorepoManifest` dataclass: root_path, packages (list of `WorkspacePackage`), dependency_edges, detected_tool.
> 4. Update `pipeline.py`: when monorepo detected, scan each package independently, then merge findings. Cross-package taint flows create inter-package findings.
> 5. `--package <name>` flag to scan a single package within a monorepo.
> 6. `--changed-packages` flag: use git diff to detect which packages changed, scan only those (combines with `--incremental`).
> 7. Report output: group findings by package, show cross-package findings separately.
> 8. Tests: create a fixture with 3-package npm workspace (shared-lib, api, frontend), verify per-package + cross-package detection.

### Agent 62: Per-Package Parallel Scanning

**Prompt:**
> You are implementing parallel per-package scanning for monorepo support. Read `docs/PHASE_20_MONOREPO_AND_WORKSPACES.md` Section 3.
>
> Implement:
> 1. Update `pipeline.py` to accept `MonorepoManifest`. For independent packages (no internal deps), scan in parallel using `concurrent.futures.ProcessPoolExecutor`.
> 2. For dependent packages, respect dependency order (topological sort).
> 3. Merge findings from all packages into a single `PiranesiReport`. Deduplicate cross-package findings.
> 4. `--max-parallel <n>` flag (default: CPU count) to control parallelism.
> 5. Progress display: per-package progress bars with Rich.
> 6. Tests: verify parallel scan produces same findings as sequential. Verify topological ordering.

---

## Wave 30 — Custom Rule Authoring + Rule Marketplace (v1.0)

**Deps:** Wave 17 (community rules), Wave 18 (plugin system)
**Parallel agents:** Yes — rule DSL, testing, and CLI are independent
**Docs:** `docs/PHASE_21_CUSTOM_RULES_AND_MARKETPLACE.md`

### Agent 63: Rule DSL + TOML Authoring

**Prompt:**
> You are implementing the custom rule authoring system for Project Piranesi. Read `docs/PHASE_21_CUSTOM_RULES_AND_MARKETPLACE.md` Section 2.
>
> Implement `src/piranesi/rules/engine.py`:
> 1. TOML-based rule format with fields: id, name, cwe_id, severity, description, source_pattern (CPGQL or regex), sink_pattern, sanitizer_patterns, message_template, tags, author, version.
> 2. `load_rules(rules_dir) -> list[CustomRule]` — auto-discover `rules/**/*.toml`.
> 3. `compile_rule(rule: CustomRule) -> CompiledRule` — validate CPGQL patterns, pre-compile regex patterns.
> 4. Integration: custom rules execute alongside built-in specs in the detect stage.
> 5. `piranesi rules validate <path>` CLI command — check rule syntax, pattern validity, required fields.
> 6. `piranesi rules test <path> --fixture <dir>` CLI command — run a rule against a fixture directory, show matches.
> 7. Rule inheritance: `extends: "builtin:sqli"` to modify a built-in rule's thresholds or add sanitizers.
> 8. Tests: write 3 custom rules (NoSQL injection, LDAP injection, XML injection), validate and test them.

### Agent 64: Rule Testing Framework

**Prompt:**
> You are implementing an inline testing framework for Piranesi custom rules. Read `docs/PHASE_21_CUSTOM_RULES_AND_MARKETPLACE.md` Section 3.
>
> Implement `src/piranesi/rules/testing.py`:
> 1. Inline test annotations in rule TOML files:
>    ```toml
>    [[tests]]
>    fixture = "tests/fixtures/nosql_injection.ts"
>    expect_finding = true
>    expect_cwe = "CWE-943"
>    expect_source_line = 12
>    expect_sink_line = 15
>
>    [[tests]]
>    fixture = "tests/fixtures/nosql_safe.ts"
>    expect_finding = false
>    ```
> 2. `piranesi rules test-all` — run all rule inline tests, report pass/fail.
> 3. Rule coverage report: which CWEs have custom rules, which ground truth entries are covered.
> 4. Tests: verify test runner catches true positives and true negatives.

### Agent 65: Rule Distribution via Git

**Prompt:**
> You are implementing rule distribution for Piranesi. Read `docs/PHASE_21_CUSTOM_RULES_AND_MARKETPLACE.md` Section 4.
>
> Implement `src/piranesi/rules/registry.py`:
> 1. `piranesi rules install <git-url>` — clone a git repo into `~/.piranesi/rules/<name>/`, validate all rules.
> 2. `piranesi rules update` — git pull all installed rule repos.
> 3. `piranesi rules remove <name>` — delete installed rule set.
> 4. Configuration in `piranesi.toml`:
>    ```toml
>    [rules]
>    paths = ["./rules", "~/.piranesi/rules/*"]
>    disabled_rules = ["noisy-rule-001"]
>    ```
> 5. Rule namespacing: `<repo-name>:<rule-id>` to avoid collisions.
> 6. Signature verification: optional GPG signature check on rule repos.
> 7. Tests: mock git clone, verify install/update/remove lifecycle.

---

## Wave 31 — Advanced Taint Analysis (v1.0)

**Deps:** Wave 25 (multi-language depth)
**Parallel agents:** Yes — inter-procedural, alias, and prototype pollution are independent
**Docs:** `docs/PHASE_22_ADVANCED_TAINT_ANALYSIS.md`

### Agent 66: Deep Inter-Procedural Analysis

**Prompt:**
> You are implementing deep inter-procedural taint analysis for Project Piranesi. Read `docs/PHASE_22_ADVANCED_TAINT_ANALYSIS.md` Section 2.
>
> Implement `src/piranesi/detect/interprocedural.py`:
> 1. Current taint tracking follows `reachableByFlows` which handles basic inter-procedural flow. Extend to handle:
>    - Callback chains: `fetch(url, (err, data) => sink(data))` — taint through callback parameters.
>    - Promise chains: `fetch(url).then(data => sink(data))` — taint through `.then()` / `await`.
>    - Event emitter patterns: `emitter.on('data', handler)` + `emitter.emit('data', tainted)` — taint through event channels.
>    - Higher-order functions: `arr.map(fn)` where `fn` contains a sink.
> 2. Build call-graph summary: for each function, compute `taint_summary: set[TaintTransfer]` — which parameters flow to which return values or sinks.
> 3. Use summaries to accelerate cross-module analysis without re-querying Joern for every call site.
> 4. Add 10+ ground truth entries for callback/promise/event patterns.
> 5. Tests: fixtures with each pattern, verify detection.

### Agent 67: Alias + Prototype Pollution Analysis

**Prompt:**
> You are implementing alias analysis and prototype pollution detection for Project Piranesi. Read `docs/PHASE_22_ADVANCED_TAINT_ANALYSIS.md` Section 3.
>
> Implement:
> 1. `src/piranesi/detect/alias.py` — alias-aware taint tracking:
>    - Detect when tainted values are assigned to object properties: `obj.x = tainted; sink(obj.x)`.
>    - Track through destructuring: `const {x} = tainted_obj; sink(x)`.
>    - Track through spread: `const merged = {...tainted, safe: 1}; sink(merged.key)`.
> 2. `src/piranesi/detect/prototype_pollution.py` — CWE-1321 detection:
>    - Source: user-controlled key in property assignment (`obj[req.body.key] = req.body.value`).
>    - Sink: recursive merge functions (`_.merge`, `Object.assign`, `lodash.defaultsDeep`).
>    - Detect `__proto__`, `constructor.prototype` traversal.
> 3. Add prototype pollution specs to `scan/specs.py`.
> 4. Ground truth: 5+ entries for alias tracking, 5+ for prototype pollution.
> 5. Tests: fixture apps with each pattern.

### Agent 68: Context-Sensitive Sanitizer Validation

**Prompt:**
> You are implementing context-sensitive sanitizer validation for Project Piranesi. Read `docs/PHASE_22_ADVANCED_TAINT_ANALYSIS.md` Section 4.
>
> Implement `src/piranesi/detect/sanitizer_validation.py`:
> 1. Current sanitizer detection is binary (present/absent). Upgrade to context-sensitive:
>    - HTML escape sanitizes XSS but NOT SQLi — don't suppress SQLi findings that pass through `escapeHtml()`.
>    - Parameterized queries sanitize SQLi but NOT XSS — don't suppress XSS if data passes through `db.query($1, [input])`.
>    - URL encoding sanitizes some path traversal but NOT SSRF.
> 2. Build a `SanitizerEffectiveness` matrix: sanitizer_name × CWE → {effective, ineffective, partial}.
> 3. Adjust finding confidence based on sanitizer-CWE match: effective → suppress, ineffective → no change, partial → reduce confidence by 0.3.
> 4. Detect sanitizer bypass patterns: double encoding, nested contexts (e.g., JSON inside HTML), charset tricks.
> 5. Tests: verify context-sensitive suppression for each sanitizer-CWE combination.

---

## Wave 32 — Reachability + Dead Code Pruning (v1.0)

**Deps:** Wave 31 (inter-procedural analysis)
**Docs:** `docs/PHASE_23_REACHABILITY_AND_DEAD_CODE.md`

### Agent 69: Reachability Analysis

**Prompt:**
> You are implementing reachability-based finding pruning for Project Piranesi. Read `docs/PHASE_23_REACHABILITY_AND_DEAD_CODE.md` Section 2.
>
> Implement `src/piranesi/detect/reachability.py`:
> 1. Build a call graph from Joern CPG: `cpg.method.callOut` edges.
> 2. Identify entry points: Express route handlers, exported functions, CLI entry points, test functions.
> 3. Compute reachable set: BFS from entry points over call graph.
> 4. For each CandidateFinding, check if the source function is reachable from an entry point.
> 5. Unreachable findings: mark as `reachability: "unreachable"`, reduce severity to `informational`, separate section in report.
> 6. `--include-unreachable` flag to include them in the main report anyway.
> 7. Dead code report: list functions never called from any entry point.
> 8. Tests: fixture with reachable and unreachable vulnerable code, verify pruning.

### Agent 70: Dependency Reachability

**Prompt:**
> You are implementing dependency-level reachability analysis. Read `docs/PHASE_23_REACHABILITY_AND_DEAD_CODE.md` Section 3.
>
> Implement `src/piranesi/detect/dep_reachability.py`:
> 1. For each dependency vulnerability from `detect/dependencies.py`, check if the vulnerable function/module is actually imported and called in the project.
> 2. Parse import statements and `require()` calls to build a module dependency graph.
> 3. Cross-reference with the vulnerable function name from the advisory.
> 4. Mark unreachable dependency vulnerabilities as `reachability: "dep_unreachable"`.
> 5. This dramatically reduces SCA (Software Composition Analysis) noise.
> 6. Tests: fixture with a vulnerable dep where the vulnerable function is unused vs used.

---

## Wave 33 — IDE + Editor Integration (v1.1)

**Deps:** Wave 24 (SARIF output)
**Parallel agents:** Yes — LSP, watch mode, and pre-commit are independent
**Docs:** `docs/PHASE_24_IDE_AND_EDITOR_INTEGRATION.md`

### Agent 71: Language Server Protocol (LSP) Adapter

**Prompt:**
> You are implementing an LSP adapter for Project Piranesi. Read `docs/PHASE_24_IDE_AND_EDITOR_INTEGRATION.md` Section 2.
>
> Implement `src/piranesi/lsp/server.py`:
> 1. LSP server using `pygls` (Python LSP library). Runs as a subprocess.
> 2. On `textDocument/didSave`: trigger incremental scan on the saved file.
> 3. Publish diagnostics: map findings to LSP `Diagnostic` objects with severity, CWE code, taint path in `relatedInformation`.
> 4. Code actions: for findings with patches, offer "Apply Piranesi fix" as a code action.
> 5. Hover information: on hover over a taint source/sink, show finding summary.
> 6. Configuration: `piranesi.toml` `[lsp]` section: `enabled`, `scan_on_save`, `debounce_ms`.
> 7. `piranesi lsp` CLI command to start the LSP server.
> 8. Tests: mock LSP client, verify diagnostics published on file save.
>
> Add `pygls>=1.3.0` to optional deps (`[project.optional-dependencies] lsp = [...]`).

### Agent 72: Watch Mode + File Watcher

**Prompt:**
> You are implementing watch mode for Project Piranesi. Read `docs/PHASE_24_IDE_AND_EDITOR_INTEGRATION.md` Section 3.
>
> Implement `src/piranesi/watch.py`:
> 1. `piranesi watch <dir>` CLI command: start a file watcher (using `watchfiles` library).
> 2. On file change: debounce (500ms default), trigger incremental scan on changed files.
> 3. Terminal UI (Rich Live display): show current findings count, last scan time, changed files.
> 4. `--filter <glob>` to only watch specific patterns.
> 5. `--on-finding <cmd>` hook: execute a shell command when new findings appear (e.g., desktop notification).
> 6. Ctrl+C graceful shutdown with summary.
> 7. Tests: mock file watcher events, verify incremental scan triggered.
>
> Add `watchfiles>=0.21.0` to optional deps.

### Agent 73: Git Pre-Commit Hook

**Prompt:**
> You are implementing a git pre-commit hook for Project Piranesi. Read `docs/PHASE_24_IDE_AND_EDITOR_INTEGRATION.md` Section 4.
>
> Implement `src/piranesi/hooks/pre_commit.py`:
> 1. `piranesi hook install` CLI command: write `.git/hooks/pre-commit` script that runs `piranesi run --incremental --fail-severity high` on staged files.
> 2. `piranesi hook uninstall` CLI command: remove the hook.
> 3. Pre-commit framework integration: generate `.pre-commit-hooks.yaml` for use with `pre-commit` tool.
> 4. Staged-only scanning: only scan files in `git diff --cached --name-only`.
> 5. `--hook-timeout <seconds>` config option (default 60s) — skip scan if it takes too long for a smooth dev experience.
> 6. `[hooks]` section in `piranesi.toml`: `pre_commit = true`, `fail_severity = "high"`, `timeout = 60`.
> 7. Tests: mock git staged files, verify only staged files scanned.

---

## Wave 34 — Advanced Reporting + Trend Analysis (v1.1)

**Deps:** Wave 22 (baselines), Wave 24 (output formats)
**Parallel agents:** Yes — trends, HTML, and compliance are independent
**Docs:** `docs/PHASE_25_ADVANCED_REPORTING.md`

### Agent 74: Historical Trend Analysis

**Prompt:**
> You are implementing historical trend analysis for Project Piranesi. Read `docs/PHASE_25_ADVANCED_REPORTING.md` Section 2.
>
> Implement `src/piranesi/report/trends.py`:
> 1. `piranesi trends <output_dir>` CLI command: scan all baseline artifacts in output_dir, compute historical metrics.
> 2. Metrics: findings over time (total, by severity, by CWE), fix rate (findings resolved per scan), mean time to fix (from first detection to resolution), new finding velocity.
> 3. Output: JSON time series (`trends.json`) + terminal sparkline chart (Rich).
> 4. `--since <date>` and `--until <date>` filters.
> 5. Trend alerts: if finding count increases >20% between scans, print warning.
> 6. Tests: create 5 baseline artifacts with varying findings, verify trend computation.

### Agent 75: TUI Report Viewer with Taint Visualization

**Prompt:**
> You are implementing an interactive TUI report viewer for Project Piranesi. Read `docs/PHASE_25_ADVANCED_REPORTING.md` Section 3.
>
> Implement `src/piranesi/report/tui.py`:
> 1. `--format tui` output option using `textual` (optional dep in `piranesi[tui]`).
> 2. Finding list with vim-style navigation (j/k scroll, / search, Enter expand, q quit).
> 3. Finding detail panel: severity, CWE, taint path (source → steps → sink), confidence, confirmation status.
> 4. Keybindings: p=patch, l=legal, r=reproducer, s=suppress, e=export markdown, f=filter.
> 5. Filter by severity, CWE, file — cycle with `f` key.
> 6. Non-TTY fallback: when stdout is piped, fall back to `--format markdown`.
> 7. When `textual` not installed, fall back to Rich tables (non-interactive).
> 8. Tests: verify non-TTY fallback, verify finding count, mock textual app for keybinding dispatch.

### Agent 76: Compliance Dashboard (CLI/TUI)

**Prompt:**
> You are implementing a compliance-focused CLI report for Project Piranesi. Read `docs/PHASE_25_ADVANCED_REPORTING.md` Section 4.
>
> Implement `src/piranesi/report/compliance.py`:
> 1. `--format compliance` output option — Rich tables to stdout (pipe-safe).
> 2. Regulatory coverage matrix: Rich table with findings × frameworks (GDPR, CCPA, HIPAA, NIS2, PDPA, EU AI Act, MAS TRM).
> 3. Per-framework Rich panel: total findings, severity breakdown, obligations, notification timelines, penalty exposure.
> 4. Gap analysis: OWASP Top 10 coverage table showing blind spots.
> 5. `--attestation` flag: output pre-filled Markdown attestation to stdout (redirect to file). Includes legal disclaimer.
> 6. `--tui` flag: interactive TUI compliance dashboard (navigate by framework, drill into findings). Requires `piranesi[tui]`.
> 7. Tests: verify compliance output includes all active regulatory frameworks, verify attestation metadata.

---

## Parallelization Summary (Full Roadmap)

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

**Maximum parallelism for next sprints:**
- v1.0: Waves 29+30+31+32 can all run simultaneously (10 agents). Wave 32 depends on 31 completing first.
- v1.1: Waves 33+34 can run simultaneously (6 agents). Both are independent of each other.

**Critical path to v1.0:** Wave 31 (advanced taint) → Wave 32 (reachability). Everything else is independent.

