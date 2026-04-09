# Phase 1: Joern-Backed Taint Analysis

> Piranesi Phase 1 planning document.
> This phase produces `ScanResult` and `CandidateFinding` artifacts via Joern's Code Property Graph (CPG) platform.

---

## 1. Phase Overview

### What this phase builds

A Joern-backed taint analysis pipeline that scans TypeScript/JavaScript source code and produces two Pydantic artifacts consumed by downstream phases:

- **`ScanResult`** -- project-wide attack surface: call graph, entry points, files scanned.
- **`CandidateFinding`** -- per-vulnerability record: CWE class, taint source, taint sink, full data flow path, path conditions, confidence, severity.

### Why Joern

Joern (Apache 2.0, [github.com/joernio/joern](https://github.com/joernio/joern)) is a JVM-based CPG platform with built-in inter-procedural data flow analysis. Adopting it replaces what would have been a custom 8-layer tree-sitter taint engine (estimated 380--530h). With Joern, Phase 1 drops to **128--186h**, saving 200+ hours.

Key properties:
- Battle-tested inter-procedural data flow via `reachableByFlows`.
- CPGQL query language for declarative source/sink specification.
- Ships with a JS/TS frontend (`jssrc2cpg`). TypeScript must be transpiled to JS first (Section 3).
- Runs as a subprocess or REST server. Piranesi wraps it in Python.

### What Piranesi still builds on top of Joern

Joern provides the taint engine. Piranesi owns everything else:

| Piranesi responsibility | Implementation |
|---|---|
| TypeScript transpilation + source map mapping | `transpile.py` |
| Joern server lifecycle management | `joern.py` |
| Source/sink specifications as CPGQL templates | `queries.py`, `specs.py` |
| Joern flow output -> Pydantic `CandidateFinding` | `flows.py` |
| Path condition extraction from CPG control flow | `conditions.py` |
| Data category classification for regulatory mapping | `categories.py` |
| Attack surface / `ScanResult` construction | `surface.py` |
| Full test suite with annotated fixtures | `tests/` |

### Incremental build strategy

1. **Spike first** (Milestone 1.0). Validate Joern's detection rate on real Express projects before committing to any implementation.
2. **Infrastructure** (1.1, 1.2). TS transpilation and Joern server management -- these are prerequisites for all query work. Can run in parallel.
3. **Query development** (1.3). Define all v1 source/sink CPGQL patterns.
4. **Core extraction** (1.4, 1.5). Data flow extraction and path condition extraction -- the hardest milestones.
5. **Classification and surface** (1.6, 1.7). Data categories and attack surface mapping -- parallelizable.
6. **Testing** (1.8). Full integration and unit test suite.

---

## 2. Joern Validation Spike (16--24h) -- FIRST TASK

This is the first task before any other Phase 1 work. The spike determines whether Joern is viable. All subsequent milestones are contingent on spike success.

### Setup

- Install Joern: `brew install joern` (macOS) or binary release from [GitHub](https://github.com/joernio/joern/releases).
- JVM 11+ required: `brew install openjdk@11`.

### Procedure

1. Select 5 real-world TypeScript Express projects (public repos, varying complexity).
2. Transpile each to JS:
   ```sh
   tsc --outDir /tmp/spike --declaration false --allowJs --target ES2020 --module commonjs
   ```
3. Import each project into Joern:
   ```sh
   joern --script import.sc --param inputPath=/tmp/spike
   ```
4. Write CPGQL queries to detect data flows for each category:

| Flow | Source | Sink |
|---|---|---|
| SQLi | `req.body` | `query()` / `$queryRaw()` |
| Command injection | `req.query` | `exec()` / `spawn()` |
| XSS | `req.params` | `res.send()` / `res.render()` |
| Path traversal | `req.body` | `readFile()` / `writeFile()` |

5. Measure:
   - **Detection rate** -- target >= 80% of known-vulnerable flows detected.
   - **Latency** -- target < 60s for a 500-file project.
   - **False positive rate** -- record but no hard threshold for the spike.

### Decision criteria

| Detection rate | Action |
|---|---|
| >= 80% | Proceed with Joern. Continue to Milestone 1.1. |
| 60--80% | Investigate CPGQL query improvements. If improved to >= 80%, proceed. |
| < 60% | Escalate. See Section 16. |

### Deliverable

Spike report containing:
- Per-project detection results (true positives, false negatives, false positives).
- Latency measurements per project.
- List of undetectable patterns with root cause (query gap vs. Joern limitation).
- Go/no-go recommendation.

---

## 3. TypeScript Transpilation Pipeline (10--15h)

### Why

Joern's JS frontend (`jssrc2cpg`) does not parse TypeScript natively. All TS must be transpiled to JS before Joern import. Source maps are required to map Joern's JS-line output back to original TS locations.

### Implementation: `src/piranesi/scan/transpile.py`

**Transpilation flow:**

1. **SECURITY INVARIANT: NEVER use the target repo's `tsconfig.json`.** A malicious tsconfig can specify compiler plugins (via `ts-patch`/`ttypescript`) that execute arbitrary code during compilation. Since `tsc` runs on the HOST machine (outside Docker), this is a pre-sandbox code execution vector.
2. Generate Piranesi's own minimal `tsconfig.json` in a temp directory:
   ```json
   {
     "compilerOptions": {
       "target": "ES2020",
       "module": "commonjs",
       "outDir": "<temp_dir>",
       "declaration": false,
       "sourceMap": true,
       "allowJs": true,
       "esModuleInterop": true,
       "resolveJsonModule": true,
       "strict": false,
       "skipLibCheck": true,
       "noEmit": false
     },
     "include": ["<target_dir>/**/*.ts", "<target_dir>/**/*.tsx", "<target_dir>/**/*.js", "<target_dir>/**/*.jsx"],
     "exclude": ["<target_dir>/node_modules/**"]
   }
   ```
3. Run `tsc` with the generated config:
   ```sh
   tsc --project /tmp/piranesi-tsconfig-XXXX/tsconfig.json
   ```
4. If `tsc` not installed: try `npx tsc` as fallback. If that also fails, error with installation instructions.
5. **Also ignore**: `.npmrc`, `.node-version`, `.nvmrc`, `.tool-versions` from the target repo. These can redirect npm registries or force vulnerable Node.js versions. Piranesi uses its own npm config and the system Node.js.

**Source map handling:**

- Parse `.map` files (JSON format) to build a bidirectional line mapping: `(transpiled_file, transpiled_line) <-> (original_file, original_line)`.
- Use the `sourcemap` Python package, or parse the JSON source map manually (the format is a simple VLQ-encoded mapping).
- Must handle:
  - 1:1 mappings (simple cases).
  - One TS line -> multiple JS lines (class transforms, enum transforms, decorator lowering).
  - Multiple TS files merged into one JS file (rare with `--module commonjs`, but possible).

**Error handling:**

- If `tsc` fails on some files (type errors): retry with `--noEmit false --skipLibCheck` to force emit.
- Log which files failed transpilation. These will be gaps in analysis.
- If > 20% of files fail transpilation: warn the user with a summary of failures.

### Testing

- Transpile 3 real Express TS projects.
- Verify source maps produce correct line mappings by spot-checking 20+ locations.
- Verify failed-file logging and the > 20% warning threshold.

---

## 4. Joern Server Management (10--15h)

### Implementation: `src/piranesi/scan/joern.py`

Joern runs in server mode during analysis. JVM startup takes 3--5s; server mode amortizes this cost across all queries in a scan.

**Server command:**
```sh
joern --server --server-host localhost --server-port 8080
```

### Lifecycle

1. Start Joern server as a subprocess at the beginning of `piranesi scan`.
2. Wait for server readiness: poll health endpoint with exponential backoff, max 30s.
3. Import the transpiled JS project: POST to Joern's REST API with the project path.
4. Execute CPGQL queries via HTTP requests.
5. Parse JSON responses.
6. Shut down server after `piranesi detect` completes (or on error/timeout).

### Subprocess management

- Use `subprocess.Popen` with stdout/stderr capture.
- Implement a context manager:
  ```python
  with JoernServer(port=8080) as server:
      server.import_project("/path/to/js")
      results = server.query("cpg.method.l")
  ```
- Handle:
  - **Server crash**: restart once, then fail with diagnostic output.
  - **Port conflict**: try next port (8081, 8082, ...).
  - **Timeout**: kill process, report error.

### Configuration in `piranesi.toml`

```toml
[joern]
binary_path = "joern"           # or absolute path
server_port = 8080
startup_timeout_seconds = 30
query_timeout_seconds = 60
jvm_memory = "2g"               # -Xmx for JVM heap
```

### Error messages

- `joern` binary not found: `"Joern is required. Install via: brew install joern (macOS) or see https://github.com/joernio/joern"`
- JVM not found: `"JVM 11+ is required. Install via: brew install openjdk@11"`

---

## 5. CPGQL Query Development -- Source/Sink Detection (15--20h)

### Implementation: `src/piranesi/scan/queries.py`

Each source and sink is defined as a CPGQL query template (Python string). The template is parameterized where needed and rendered into valid CPGQL before execution.

### Sources (v1)

| Source | CPGQL pattern | Source type |
|---|---|---|
| `req.body` | `cpg.method.parameter.name("req").dotAccess("body")` | `request_body` |
| `req.query` | `cpg.method.parameter.name("req").dotAccess("query")` | `request_param` |
| `req.params` | `cpg.method.parameter.name("req").dotAccess("params")` | `request_param` |
| `req.headers` | `cpg.method.parameter.name("req").dotAccess("headers")` | `header` |
| `req.cookies` | `cpg.method.parameter.name("req").dotAccess("cookies")` | `cookie` |
| `process.env` | `cpg.call.name(".*env.*")` | `env_var` |
| URL / URLSearchParams | `cpg.call.name("URL\|URLSearchParams")` | `url_param` |

### Sinks (v1)

| Sink | CPGQL pattern | Sink type | CWE |
|---|---|---|---|
| Raw SQL | `cpg.call.name("query\|\\$queryRaw\|\\$executeRaw\|raw")` | `sql_query` | CWE-89 |
| `exec` / `execSync` | `cpg.call.name("exec\|execSync")` | `shell_exec` | CWE-78 |
| `spawn` / `spawnSync` | `cpg.call.name("spawn\|spawnSync")` | `shell_exec` | CWE-78 |
| `eval` / `Function` | `cpg.call.name("eval\|Function")` | `eval` | CWE-94 |
| `dangerouslySetInnerHTML` | `cpg.call.name("dangerouslySetInnerHTML")` | `html_output` | CWE-79 |
| `res.send` / `render` / `write` | `cpg.call.name("send\|render\|write")` | `html_output` | CWE-79 |
| `readFile` / `readFileSync` | `cpg.call.name("readFile\|readFileSync")` | `file_read` | CWE-22 |
| `writeFile` / `writeFileSync` | `cpg.call.name("writeFile\|writeFileSync")` | `file_write` | CWE-22 |
| `fetch` / `axios` | `cpg.call.name("fetch\|get\|post\|request")` | `http_request` | CWE-918 |

### Method chaining

Queries must match the method name on the correct receiver. For example, `connection.query()` must match `query` called on a database connection object, not any call named `query`. Use receiver type constraints where Joern's type resolution permits.

### Extensibility

Users can add custom source/sink patterns via `piranesi.toml`:

```toml
[scan.custom_sources]
patterns = ["cpg.call.name(\"customInput\").argument"]

[scan.custom_sinks]
patterns = ["cpg.call.name(\"customDangerous\").argument"]
```

### Testing

Verify each query against hand-crafted test JS files containing known sources and sinks.

---

## 6. Data Flow Extraction via CPGQL (20--30h)

### Implementation: `src/piranesi/detect/flows.py`

### Core query

For each source-sink pair identified in Section 5:

```
sink.reachableByFlows(source).l
```

This returns all data flow paths from sources to sinks. Each path is a sequence of CPG nodes.

### Joern flow node properties

| Property | Description |
|---|---|
| `filename` | File path (in transpiled JS -- must map back to TS) |
| `lineNumber` | Line in transpiled JS |
| `columnNumber` | Column |
| `code` | Source snippet at this point in the flow |
| `methodFullName` | Enclosing function |
| `nodeType` | `CALL`, `IDENTIFIER`, `METHOD_PARAMETER_IN`, `RETURN`, etc. |

### Mapping Joern flows to `list[TaintStep]`

```python
def joern_flow_to_taint_steps(flow: dict, source_map: SourceMap) -> list[TaintStep]:
    steps = []
    for node in flow["elements"]:
        ts_location = source_map.resolve(node["filename"], node["lineNumber"])
        steps.append(TaintStep(
            location=SourceLocation(
                file=ts_location.file,
                line=ts_location.line,
                column=node.get("columnNumber", 0),
                snippet=node["code"],
            ),
            operation=classify_operation(node["nodeType"]),
            taint_state="tainted",
            through_function=node.get("methodFullName"),
        ))
    return steps
```

### `classify_operation` mapping

| CPG node type | Operation |
|---|---|
| `CALL` | `call_arg` |
| `IDENTIFIER` | `assignment` |
| `METHOD_PARAMETER_IN` | `call_arg` |
| `RETURN` | `return` |
| `FIELD_IDENTIFIER` | `property_access` |
| `LITERAL` | `assignment` |

### Sanitizer detection

Check if any node in the flow path calls a known sanitizer function (`escapeHtml`, `parameterize`, `sanitize`, `encode`, etc.). If found, mark subsequent steps as `sanitized` in `TaintStep.sanitizer_applied`. Each flow is a distinct path, so sanitizer effects are context-sensitive by construction.

### Finding generation

For each flow, create a `CandidateFinding`:

| Field | Value |
|---|---|
| `id` | SHA-256 of `(vuln_class + source_location + sink_location)` |
| `vuln_class` | Determined by sink type: `sql_query` -> CWE-89, `shell_exec` -> CWE-78, `html_output` -> CWE-79, etc. |
| `confidence` | `0.7` for all Joern-detected flows (adjusted by triage in later phases) |
| `severity` | Based on CWE class: CWE-78 -> `critical`, CWE-89 -> `high`, CWE-79 -> `medium`, CWE-22 -> `medium` |

---

## 7. Path Condition Extraction from CPG (20--30h)

### Implementation: `src/piranesi/detect/conditions.py`

Joern's CPG includes control flow graph (CFG) edges. Along a data flow path, branch points (if/else, switch, ternary, short-circuit) constrain which inputs reach the sink. Extracting these conditions enables the Z3 solver (Phase 2) to generate targeted payloads.

### Extraction procedure

For each branch point on the flow path:

1. Query the CPG for the branch condition:
   ```
   cpg.method.name("X").ast.isControlStructure.condition.code.l
   ```
2. Parse the condition text into a `PathCondition`.
3. Determine `required_value` (true/false): which branch does the flow take?

### Condition parsing examples

| JS condition | PathCondition |
|---|---|
| `typeof x === "string"` | TypeCheck: `var="x"`, `type="string"` |
| `x.length > 5` | StringLength: `var="x"`, `op="gt"`, `n=5` |
| `x.includes("admin")` | StringContains: `var="x"`, `substr="admin"` |
| `x === "expected"` | StringEq: `var="x"`, `val="expected"` |
| `x > 0` | IntBound: `var="x"`, `op="gt"`, `n=0` |

### Best-effort extraction

Many conditions will be too complex to parse into symbolic constraints. For those:

- Store the raw expression text in `PathCondition.expression`.
- Set `symbolic_constraint = None`.
- The Z3 solver (Phase 2) will skip these and use template-based payloads instead.

### Testing

Hand-crafted JS files with known branch conditions. Verify extracted `PathCondition` objects match expectations.

---

## 8. Data Category Classification (8--12h)

### Implementation: `src/piranesi/detect/categories.py`

For regulatory mapping (Phase 3), each taint source must be annotated with data categories describing what type of personal data flows through it.

### Classification approaches (priority order)

1. **Field name heuristics**: `nric` -> `["nric"]`, `email` -> `["contact_email"]`, `credit_card` -> `["financial_credit_card"]`, `password` -> `["credentials"]`.
2. **Route context heuristics**: endpoint `/api/users/:id` likely handles personal data.
3. **LLM classification** (via Phase 4 provider): "What type of personal data is likely stored in a field named `{field_name}` in the context of a `{route_pattern}` endpoint?"

### Data category taxonomy (aligned with PDPA / Phase 3)

| Tier | Categories |
|---|---|
| Tier 1 (most sensitive) | `nric`, `fin`, `biometric`, `genetic`, `health` |
| Tier 2 | `financial_credit_card`, `financial_bank`, `income`, `employment`, `criminal` |
| Tier 3 | `contact_email`, `contact_phone`, `contact_address`, `dob`, `nationality`, `race`, `religion` |
| Tier 4 (least sensitive) | `name`, `username`, `public_info` |

### Output

`TaintSource.data_categories` populated for each source in `CandidateFinding`.

### Testing

Verify heuristics correctly classify common field names across a representative set.

---

## 9. Attack Surface Mapping (8--10h)

### Implementation: `src/piranesi/scan/surface.py`

Uses Joern CPG to construct the `ScanResult` artifact.

### Fields

| Field | Source |
|---|---|
| `files_scanned` | All `.js` files imported into Joern, mapped back to original `.ts` files via source maps. |
| `call_graph` | `cpg.method.callOut.map(m => (m.fullName, m.callOut.fullName)).l` -> `dict[str, list[str]]` |
| `entry_points` | Express route handlers detected via CPGQL: `cpg.call.name("get\|post\|put\|delete\|patch\|use").argument.isMethodRef.l`. Extract HTTP method, route pattern, handler function. |
| `attack_surface` | Combine entry points with source detection from Section 5. |

### Testing

Verify `ScanResult` for a known Express app matches manual inspection of routes, call graph, and attack surface.

---

## 10. Source and Sink Specifications (10--15h)

### Implementation: `src/piranesi/scan/specs.py`

Full v1 specification of all sources, sinks, and sanitizers as structured CPGQL query patterns.

### Sources

| Source | CPGQL Pattern | Source Type |
|---|---|---|
| Express `req.body` | `cpg.method.parameter.name("req")...dotAccess("body")` | `request_body` |
| Express `req.query` | `cpg.method.parameter.name("req")...dotAccess("query")` | `request_param` |
| Express `req.params` | `cpg.method.parameter.name("req")...dotAccess("params")` | `request_param` |
| Express `req.headers` | `cpg.method.parameter.name("req")...dotAccess("headers")` | `header` |
| Express `req.cookies` | `cpg.method.parameter.name("req")...dotAccess("cookies")` | `cookie` |
| `process.env` | `cpg.call.name(".*env.*")` | `env_var` |
| URL / URLSearchParams | `cpg.call.name("URL\|URLSearchParams")` | `url_param` |

### Sinks

| Sink | CPGQL Pattern | Sink Type | CWE |
|---|---|---|---|
| Raw SQL query | `cpg.call.name("query\|\\$queryRaw\|\\$executeRaw\|raw")` | `sql_query` | CWE-89 |
| `child_process.exec` | `cpg.call.name("exec\|execSync")` | `shell_exec` | CWE-78 |
| `child_process.spawn` | `cpg.call.name("spawn\|spawnSync")` | `shell_exec` | CWE-78 |
| `eval` / `Function` | `cpg.call.name("eval\|Function")` | `eval` | CWE-94 |
| `dangerouslySetInnerHTML` | `cpg.call.name("dangerouslySetInnerHTML")` | `html_output` | CWE-79 |
| `res.send` (unsanitized) | `cpg.call.name("send\|render\|write")` | `html_output` | CWE-79 |
| `fs.readFile` | `cpg.call.name("readFile\|readFileSync")` | `file_read` | CWE-22 |
| `fs.writeFile` | `cpg.call.name("writeFile\|writeFileSync")` | `file_write` | CWE-22 |
| `fetch` / `axios` | `cpg.call.name("fetch\|get\|post\|request")` | `http_request` | CWE-918 |

### Known sanitizers (suppress flows through these)

| Sanitizer | CPGQL Pattern |
|---|---|
| `escapeHtml` / `escape` | `cpg.call.name("escape\|escapeHtml\|sanitize\|encode")` |
| Parameterized query | `cpg.call.name("prepare\|parameterize\|\\$query")` (without `Raw`) |
| `path.normalize` | `cpg.call.name("normalize\|resolve")` used on path input |

---

## 11. Testing Strategy (20--30h)

### Test fixtures

Directory: `tests/fixtures/`

Each test file is a TypeScript file with known vulnerabilities, annotated with comment markers:

```typescript
// @piranesi-expect: CWE-89, source=req.body.userId, sink=db.query
// @piranesi-expect-clean: this parameterized query is safe
```

### Test categories

All categories must be handled by Joern:

| Category | Description |
|---|---|
| Simple direct flow | `req.body` -> `query()` |
| Inter-procedural flow | `req.body` -> `helper()` -> `query()` |
| Sanitized flow | `req.body` -> `escape()` -> `query()` -- NOT flagged |
| Cross-module flow | Taint crosses module boundaries via import/export |
| False positive tests | Known-safe patterns that must not be flagged |

### Integration tests

- Start Joern server, import test project, run queries, verify findings match fixture annotations.
- Requires JVM + Joern installed.
- Pytest marker: `@pytest.mark.joern` -- skipped if Joern unavailable.

### Unit tests (no Joern required)

| Component | What is tested |
|---|---|
| Source map parsing | Line mapping accuracy |
| CPGQL query templates | Template generation produces valid CPGQL |
| Joern JSON -> Pydantic | Response deserialization into `CandidateFinding`, `TaintStep`, etc. |
| Path condition parsing | Condition text -> `PathCondition` |
| Data category heuristics | Field name -> data category classification |

---

## 12. Known Limitations (Joern-specific)

- **TS transpilation indirection.** Source location mapping may be imprecise for complex TS constructs: decorators, enums, namespace merging, const enums.
- **JS feature coverage.** Joern's JS frontend may not handle all modern JS features: optional chaining (`?.`), nullish coalescing (`??`), private class fields (`#field`). The spike will identify gaps.
- **Black box taint propagation.** If Joern misses a data flow path, Piranesi cannot fix it without modifying Joern's source (100K+ lines of Scala). Workaround: supplementary pattern-based checks for known gaps.
- **JVM dependency.** Users must install JVM 11+. Adds ~200MB to runtime footprint.
- **Context sensitivity depth.** Not configurable from CPGQL. Analysis precision is whatever Joern provides.
- **Soundness holes** (same as any JS static analyzer):
  - Prototype pollution
  - `eval()` with dynamic strings
  - Dynamic imports (`import()` expressions)
  - `Reflect` / `Proxy` metaprogramming
- **Event emitters.** `emitter.on()` / `emitter.emit()` patterns are not tracked as data flow by Joern.
- **Third-party libraries.** Joern can import `node_modules` but analysis becomes very slow on large dependency trees. Recommend excluding `node_modules` from import.

---

## 13. Reading List

| Resource | Why |
|---|---|
| [Joern documentation](https://docs.joern.io/) | Primary reference for CPGQL, server mode, frontends |
| [CPGQL reference](https://docs.joern.io/cpgql/reference) | Query language syntax and operators |
| [Joern JS frontend](https://docs.joern.io/frontends/jssrc) | JS-specific CPG construction details |
| Yamaguchi et al., "Modeling and Discovering Vulnerabilities with Code Property Graphs" (IEEE S&P 2014) | Foundational CPG paper |
| Arzt et al., "FlowDroid: Precise Context, Flow, Field, Object-sensitive and Lifecycle-aware Taint Analysis for Android Apps" (PLDI 2014) | Taint analysis concepts that Joern implements |
| Nielson, Nielson, Hankin, "Principles of Program Analysis" (chapters 1--3) | Theoretical grounding for data flow analysis |

---

## 14. Milestones

| # | Milestone | Effort (h) | Dependencies | Deliverable |
|---|---|---|---|---|
| 1.0 | Joern validation spike | 16--24 | Phase 0 complete | Spike report: detection rate, latency, gap analysis |
| 1.1 | TS transpilation pipeline | 10--15 | 1.0 (spike passes) | `transpile.py`: `tsc` invocation, source map parsing, line mapping |
| 1.2 | Joern server management | 10--15 | 1.0 | `joern.py`: context manager, health check, lifecycle |
| 1.3 | Source/sink CPGQL queries | 15--20 | 1.0, 1.2 | `queries.py` + `specs.py`: all v1 source/sink patterns |
| 1.4 | Data flow extraction | 20--30 | 1.1, 1.2, 1.3 | `flows.py`: Joern flows -> `CandidateFinding` with `TaintStep`s |
| 1.5 | Path condition extraction | 20--30 | 1.4 | `conditions.py`: CPG control flow -> `PathCondition` |
| 1.6 | Data category classification | 8--12 | 1.4 | `categories.py`: field name heuristics + LLM classification |
| 1.7 | Attack surface mapping | 8--10 | 1.2, 1.3 | `surface.py`: `ScanResult` construction from Joern CPG |
| 1.8 | Testing | 20--30 | 1.4, 1.5, 1.6, 1.7 | Full test suite with annotated fixtures |

**Total: 128--186 ideal hours**

**Critical path:** 1.0 -> 1.1 + 1.2 (parallel) -> 1.3 -> 1.4 -> 1.5 -> 1.8

**Parallel opportunities:**
- 1.1 and 1.2 can run in parallel (both depend only on 1.0).
- 1.6 and 1.7 can start after 1.4.

---

## 15. Phase Dependencies

| Relationship | Phase |
|---|---|
| **Blocked by** | Phase 0 (project scaffolding) |
| **Blocks** | Phase 2 (needs `CandidateFinding` with taint paths and path conditions) |
| **Blocks** | Phase 3 (needs findings with data categories) |
| **Blocks** | Phase 5 (needs working analyzer) |
| **Parallel with** | Phase 4 (LLM orchestration can start after Phase 0) |

Critical path: YES, but now shorter (128--186h vs 305--390h for custom engine).

---

## 16. If the Spike Fails

If the Joern validation spike (Milestone 1.0) shows < 60% detection rate or unacceptable latency:

1. **Check CPGQL queries.** Common failure modes: queries too broad (high FP) or too narrow (low detection). Iterate on query patterns.
2. **Supplementary analysis.** If detection gaps are in specific patterns (e.g., callback chains, event emitters), add supplementary pattern-based static analysis for those patterns only. Joern handles the rest.
3. **Full fallback.** If fundamental (Joern's JS frontend can't parse the code at all), the fallback is the custom tree-sitter engine. The original Phase 1 plan exists in git history. This would reset Phase 1 to the 305--390h estimate.

This document assumes the spike succeeds. All milestones from 1.1 onward are contingent on spike success.

---

## Appendix: Data Models (contracts from ARCHITECTURE.md)

Phase 1 must produce artifacts conforming to these Pydantic models.

```python
class ScanResult(BaseModel):
    project_root: str
    files_scanned: list[str]
    call_graph: dict[str, list[str]]  # function_id -> [callee_ids]
    entry_points: list[EntryPoint]
    attack_surface: list[AttackSurfaceNode]
    metadata: ScanMetadata

class CandidateFinding(BaseModel):
    id: str                          # deterministic SHA-256 hash
    vuln_class: str                  # CWE identifier
    source: TaintSource
    sink: TaintSink
    taint_path: list[TaintStep]
    path_conditions: list[PathCondition]
    confidence: float
    severity: str

class TaintSource(BaseModel):
    location: SourceLocation
    source_type: str       # "request_body" | "request_param" | "header" | etc.
    data_categories: list[str]
    parameter_name: str | None = None

class TaintSink(BaseModel):
    location: SourceLocation
    sink_type: str         # "sql_query" | "shell_exec" | "html_output" | etc.
    api_name: str          # e.g. "db.query", "child_process.exec"

class TaintStep(BaseModel):
    location: SourceLocation
    operation: str
    taint_state: str
    through_function: str | None = None
    sanitizer_applied: str | None = None

class PathCondition(BaseModel):
    location: SourceLocation
    condition_type: str
    expression: str
    required_value: bool
    symbolic_constraint: str | None = None
```
