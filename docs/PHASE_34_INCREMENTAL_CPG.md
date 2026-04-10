# Phase 34: Incremental CPG Updates

**Estimated effort: 55-70 ideal hours**
**Blocked by: Phase 14 (incremental scanning), Phase 22 (advanced taint/interprocedural summaries)**
**Blocks: Nothing (independent optimization)**
**Target milestone: v0.5.0**

---

## 1. Overview

### 1.1 Problem

CPG generation via Joern is the single slowest step in the piranesi pipeline. On medium projects (50-200 files), `joern-parse` + `importCode` takes 10-60 seconds. On large projects (500+ files), it routinely exceeds 2 minutes. The current incremental scanning infrastructure (`scan/incremental.py`) detects changed files at the manifest level via SHA-256 + mtime comparison, but the pipeline still rebuilds the full CPG on every scan — `transpile_project` re-transpiles everything, `JoernServer.import_project` re-imports everything, and all `reachableByFlows` queries re-execute against the entire graph.

This means a single-file edit in a 200-file project pays the same CPG cost as a full initial scan.

### 1.2 Goal

Patch the CPG with only the changed ASTs, reducing re-scan time by 5-50x depending on the number of changed files. A single-file change in a medium project should complete in under 3 seconds, not 30.

### 1.3 Constraint

**Correctness over speed.** Stale CPG data producing missed findings is strictly worse than a slow but correct scan. The incremental path must be provably equivalent to a full scan, or it must fall back to full scan. No silent precision loss.

---

## 2. CPG Diff Strategy

### 2.1 Module: `src/piranesi/scan/cpg_diff.py`

This module orchestrates the incremental CPG update pipeline.

**Algorithm:**

1. Load the cached `PiranesiCPG` from `.piranesi-cache/cpg/`.
2. Receive the `IncrementalResult` from `scan/incremental.py` (sets of added, modified, deleted, unchanged files).
3. For each **changed file** (added or modified):
   - Re-parse AST via Joern (only that file).
   - Compute a function-level diff: which functions were added, removed, or modified.
   - For modified functions: compare function body hash to detect actual semantic changes vs whitespace-only edits.
4. For each **changed function**:
   - Remove the old function node and all its internal nodes from the `PiranesiCPG`.
   - Insert the new function node extracted from the Joern re-parse.
5. For **unchanged functions in changed files**: keep existing CPG nodes (the function body hash matches).
6. For **unchanged files**: keep all existing CPG nodes, zero work.
7. **Rebuild edges**: recompute call graph edges from/to any changed function.
8. **Rebuild taint flows**: re-run `reachableByFlows` only for source-sink paths that touch a changed function.

### 2.2 Function Identity

Functions are identified by the tuple `(file_path, function_name, parameter_signature)`. This handles:
- Renamed functions: treated as delete + add.
- Overloaded functions (TypeScript): distinguished by parameter signature.
- Anonymous functions: identified by `(file_path, line_number, enclosing_scope)` — these always re-analyze on file change since line numbers shift.

### 2.3 Function Body Hashing

```python
def function_body_hash(source: str) -> str:
    """SHA-256 of whitespace-normalized, comment-stripped function body."""
    normalized = _strip_comments(source)
    normalized = re.sub(r"\s+", " ", normalized).strip()
    return hashlib.sha256(normalized.encode()).hexdigest()
```

This avoids re-analyzing functions where only formatting or comments changed.

---

## 3. Joern CPG Manipulation

### 3.1 Options Evaluated

| Option | Approach | Pros | Cons |
|--------|----------|------|------|
| A | Joern `--overlay` mode | Native incremental support | Requires Joern 2.x overlay API; version-coupled |
| B | Joern Scala programmatic API | Full graph mutation control | Requires Scala/JVM interop from Python; heavyweight |
| C | Lightweight Python CPG + selective Joern queries | Portable, no Joern version lock | Must maintain our own graph; Joern used as oracle |

### 3.2 Recommended: Option C (PiranesiCPG)

Maintain a lightweight Python-native graph representation extracted from Joern. On full scan, populate it from Joern query results. On incremental scan, mutate it locally and only invoke Joern for new/changed function taint analysis.

**Rationale:**
- No Scala dependency. Python-only.
- Joern version changes don't break the incremental path (worst case: cache invalidation → full rebuild).
- The `PiranesiCPG` is a projection — it stores only what piranesi needs (functions, calls, dataflows, taint summaries), not the full OverflowDB graph.
- Deep analysis (sanitizer validation, path conditions, interprocedural taint) still delegates to Joern for the specific functions that changed.

### 3.3 Workflow

```
Full scan:
  joern-parse → full CPG → extract PiranesiCPG → cache PiranesiCPG → run all queries

Incremental scan:
  load PiranesiCPG from cache
  → identify changed functions (from cpg_diff)
  → joern-parse ONLY changed files → mini CPG
  → extract new function data from mini CPG
  → patch PiranesiCPG (remove old, insert new)
  → recompute affected edges + taint paths
  → re-run Joern taint queries ONLY for affected source-sink pairs
```

---

## 4. Data Structures

### 4.1 PiranesiCPG

```python
@dataclass(slots=True)
class CPGFunction:
    function_id: str                    # unique: f"{file_path}::{name}({param_sig})"
    name: str
    file_path: str                      # relative to project root
    line_start: int
    line_end: int
    parameters: list[str]
    body_hash: str                      # SHA-256 of normalized body
    is_entry_point: bool
    source_type: str | None             # "http_param", "query_string", etc.
    contains_sinks: list[str]           # sink spec names found in this function
    taint_summary: TaintSummary | None  # interprocedural summary

@dataclass(slots=True)
class TaintSummary:
    """Summarizes how taint flows through a function, for interprocedural analysis."""
    param_to_return: dict[int, float]   # param_index → confidence that param taints return
    param_to_sink: dict[int, list[SinkReference]]  # param_index → sinks reachable from that param
    return_tainted_by: list[str]        # source types that taint the return value

@dataclass(slots=True)
class SinkReference:
    sink_name: str
    sink_type: str
    cwe_id: str
    confidence: float

@dataclass(slots=True)
class CallEdge:
    caller_id: str      # CPGFunction.function_id
    callee_id: str      # CPGFunction.function_id
    call_site_line: int
    argument_mapping: dict[int, int] | None  # caller_arg_index → callee_param_index

@dataclass(slots=True)
class TaintFlowRecord:
    flow_id: str                 # deterministic hash of source+sink+path
    source_function_id: str
    sink_function_id: str
    source_spec: str             # source spec name
    sink_spec: str               # sink spec name
    intermediate_functions: list[str]  # function_ids on the path
    confidence: float
    finding_id: str | None       # CandidateFinding.id if finding was emitted

@dataclass(slots=True)
class PiranesiCPG:
    version: str                               # piranesi version that created this
    joern_version: str                         # joern version used
    config_hash: str                           # hash of scan config
    project_root: str
    functions: dict[str, CPGFunction]          # function_id → CPGFunction
    call_edges: list[CallEdge]
    taint_flows: list[TaintFlowRecord]
    file_hashes: dict[str, str]                # relative_path → SHA-256
    created_at: str                            # ISO 8601
    updated_at: str                            # ISO 8601

    # --- derived indexes (rebuilt on load) ---
    _callers_of: dict[str, set[str]]           # function_id → set of caller function_ids
    _callees_of: dict[str, set[str]]           # function_id → set of callee function_ids
    _functions_by_file: dict[str, set[str]]    # file_path → set of function_ids
    _flows_through: dict[str, list[int]]       # function_id → indexes into taint_flows
```

### 4.2 Serialization Format

The `PiranesiCPG` is serialized as MessagePack (compact binary, faster than JSON for large graphs). Fallback to JSON for debugging (`piranesi cache dump --format json`).

```
.piranesi-cache/cpg/{project_hash}/
    cpg.msgpack          # serialized PiranesiCPG
    cpg.msgpack.sha256   # integrity check
    metadata.json        # human-readable: version, joern_version, file count, created_at
```

### 4.3 Cache Key Computation

```python
def compute_cache_key(
    project_root: Path,
    config: PiranesiConfig,
    piranesi_version: str,
    joern_version: str,
) -> str:
    """Deterministic project hash for cache directory naming."""
    hasher = hashlib.sha256()
    hasher.update(str(project_root.resolve()).encode())
    hasher.update(piranesi_version.encode())
    hasher.update(joern_version.encode())
    hasher.update(config_hash(config).encode())
    return hasher.hexdigest()[:16]
```

The `project_hash` is a short prefix (16 hex chars) of the composite hash. It identifies the cache directory. The cache is valid only if the stored `PiranesiCPG.version`, `joern_version`, and `config_hash` match the current run.

---

## 5. Call Graph Invalidation

### 5.1 Direct Invalidation

When function `F` changes (body_hash differs or function is new/deleted):

1. **Outgoing edges**: remove all `CallEdge` where `caller_id == F.function_id`.
2. **Incoming edges**: remove all `CallEdge` where `callee_id == F.function_id`.
3. **Taint flows**: remove all `TaintFlowRecord` where `F.function_id` appears in `source_function_id`, `sink_function_id`, or `intermediate_functions`.
4. **Taint summary**: invalidate `F.taint_summary`.

### 5.2 Transitive Invalidation

If `F`'s taint summary changes (the new summary differs from the old one), callers of `F` may now have different taint propagation. This requires re-analysis of callers.

**Algorithm (BFS with depth limit):**

```
procedure INVALIDATE_TRANSITIVELY(changed_functions, cpg, max_depth=3):
    invalidation_queue = deque()
    for f in changed_functions:
        old_summary = cpg.functions[f].taint_summary
        new_summary = recompute_taint_summary(f)  # via Joern query on new function body
        if old_summary != new_summary:
            cpg.functions[f].taint_summary = new_summary
            for caller_id in cpg._callers_of.get(f, []):
                invalidation_queue.append((caller_id, 1))

    visited = set(changed_functions)
    additionally_invalidated = set()

    while invalidation_queue:
        func_id, depth = invalidation_queue.popleft()
        if func_id in visited:
            continue
        visited.add(func_id)
        additionally_invalidated.add(func_id)

        old_summary = cpg.functions[func_id].taint_summary
        new_summary = recompute_taint_summary(func_id)
        if old_summary != new_summary:
            cpg.functions[func_id].taint_summary = new_summary
            if depth < max_depth:
                for caller_id in cpg._callers_of.get(func_id, []):
                    invalidation_queue.append((caller_id, depth + 1))

    return additionally_invalidated
```

### 5.3 Depth Limit Rationale

- **depth=1**: handles direct callers. Catches most real-world cases (function signature change affects its caller).
- **depth=3** (default): handles wrapper patterns (`controller → service → repository`). Covers 95%+ of transitively affected paths in typical web apps.
- **depth > 3**: diminishing returns. At depth 4+, the set of invalidated functions approaches the full project. If transitive invalidation exceeds `incremental_threshold` functions, abort incremental and fall back to full scan.

### 5.4 Configuration

```toml
[scan]
incremental_invalidation_depth = 3  # max transitive invalidation depth
```

---

## 6. Correctness Guarantees

### 6.1 Invariant

**INVARIANT**: An incremental scan must NEVER miss a finding that a full scan would produce. Formally: `findings(incremental) ⊇ findings(full_scan)`. False positives introduced by incremental (stale edges producing spurious findings) are acceptable and self-correcting on next full scan. False negatives are not.

### 6.2 Conservative Strategy

When in doubt, over-invalidate. Specifically:
- If a function cannot be cleanly identified (anonymous, dynamically generated), re-analyze the entire file.
- If transitive invalidation exceeds depth limit, re-analyze all transitively reachable functions.
- If the cache integrity check fails (SHA-256 mismatch on `cpg.msgpack`), fall back to full scan.
- If `PiranesiCPG.version` or `joern_version` doesn't match, fall back to full scan.

### 6.3 Verification Mode

```bash
piranesi scan --verify-incremental ./target
```

This flag runs BOTH the incremental scan and a full scan, then compares the finding sets:

```
procedure VERIFY_INCREMENTAL(target_dir, config):
    incremental_findings = run_incremental_scan(target_dir, config)
    full_findings = run_full_scan(target_dir, config)

    incremental_ids = {f.id for f in incremental_findings}
    full_ids = {f.id for f in full_findings}

    missed = full_ids - incremental_ids   # false negatives — CRITICAL
    extra = incremental_ids - full_ids     # false positives — acceptable

    if missed:
        log.warning(
            "incremental scan missed %d findings that full scan found — "
            "invalidating cache and using full scan results",
            len(missed),
        )
        invalidate_cache(target_dir)
        return full_findings

    if extra:
        log.info(
            "incremental scan produced %d extra findings (stale edges) — "
            "these will self-correct on next full scan",
            len(extra),
        )

    return incremental_findings
```

### 6.4 Automated Verification in CI

The ground truth evaluation suite (`eval/`) must be run with both `--incremental` and without. The eval harness asserts that finding sets are identical (modulo ordering and metadata timestamps). Any divergence is a release blocker.

---

## 7. Cache Management

### 7.1 Cache Location

```
.piranesi-cache/
    cpg/
        {project_hash}/
            cpg.msgpack
            cpg.msgpack.sha256
            metadata.json
        {project_hash_2}/
            ...
    manifests/
        ...  (existing file manifests from scan/incremental.py)
```

The `.piranesi-cache/` directory is project-local (relative to `target_dir`). It should be added to `.gitignore`.

### 7.2 Cache Key

The cache is valid when ALL of the following match:
- `piranesi_version` — any piranesi upgrade invalidates (analysis logic may have changed)
- `joern_version` — any Joern upgrade invalidates (CPG structure may differ)
- `config_hash` — any scan config change invalidates (include/exclude patterns, frameworks, custom sources/sinks)
- `cpg.msgpack.sha256` — integrity check against corruption

### 7.3 Cache Size Management

```toml
[scan]
cpg_cache_max_mb = 500  # default 500 MB
```

When total cache size exceeds the limit, evict entries by LRU (based on `metadata.json → last_accessed`).

```python
def enforce_cache_limit(cache_root: Path, max_bytes: int) -> None:
    entries = []
    for project_dir in cache_root.iterdir():
        if not project_dir.is_dir():
            continue
        meta_path = project_dir / "metadata.json"
        if not meta_path.exists():
            shutil.rmtree(project_dir)  # orphaned — remove
            continue
        meta = json.loads(meta_path.read_text())
        total_size = sum(f.stat().st_size for f in project_dir.rglob("*") if f.is_file())
        entries.append((meta.get("last_accessed", ""), total_size, project_dir))

    entries.sort()  # oldest first (ISO 8601 sorts lexicographically)
    total = sum(size for _, size, _ in entries)
    while total > max_bytes and entries:
        _, size, path = entries.pop(0)
        shutil.rmtree(path)
        total -= size
```

### 7.4 CLI Commands

```bash
piranesi cache info          # show cache location, entry count, total size, per-project breakdown
piranesi cache clear         # delete all cached CPGs
piranesi cache clear --stale # delete only entries older than 30 days
piranesi cache dump --project-hash <hash> --format json  # dump PiranesiCPG as JSON (debugging)
```

---

## 8. Performance Targets

| Scenario | Current (full scan) | Target (incremental) | Speedup |
|----------|-------------------:|--------------------:|--------:|
| 1 file changed, 50-file project | 10-15s | < 3s | 3-5x |
| 1 file changed, 200-file project | 30-60s | < 3s | 10-20x |
| 5 files changed, 200-file project | 30-60s | < 10s | 3-6x |
| 20 files changed, 200-file project | 30-60s | < 25s | 1.2-2.5x |
| 50+ files changed | 30-60s | full scan (no incremental) | 1x |

### 8.1 Incremental Threshold

When the number of changed files exceeds a threshold, incremental overhead (loading cache, diffing, selective queries) approaches or exceeds full scan cost. Fall back to full scan.

```toml
[scan]
incremental_threshold = 20  # max changed files for incremental mode
```

The threshold is configurable. The heuristic: if `len(changed_files) > incremental_threshold`, skip incremental and run full scan.

### 8.2 Performance Breakdown Target (1 file changed)

| Step | Time budget |
|------|----------:|
| Load cached PiranesiCPG from disk | 200ms |
| Compute file manifest diff | 100ms |
| Joern parse single file | 1.5s |
| Extract function-level diff | 100ms |
| Update PiranesiCPG graph | 50ms |
| Re-run taint queries for affected functions | 800ms |
| Serialize updated PiranesiCPG to disk | 200ms |
| **Total** | **~3s** |

The bottleneck remains Joern startup + single-file parse. If a persistent Joern server is running (LSP mode from Phase 24), parse drops to ~500ms, bringing total under 1.5s.

---

## 9. Implementation Phases

### Sub-phase A: PiranesiCPG Extraction and Serialization (15-20h)

1. Define `CPGFunction`, `TaintSummary`, `CallEdge`, `TaintFlowRecord`, `PiranesiCPG` dataclasses in `src/piranesi/scan/cpg_graph.py`.
2. Implement `extract_piranesi_cpg(server: JoernServer, scan_result: ScanResult) -> PiranesiCPG`:
   - Query Joern for all functions, call edges, and taint summaries.
   - Build the `PiranesiCPG` from query results.
   - Populate derived indexes (`_callers_of`, `_callees_of`, `_functions_by_file`, `_flows_through`).
3. Implement serialization: `serialize_cpg(cpg: PiranesiCPG, path: Path)` and `deserialize_cpg(path: Path) -> PiranesiCPG` using MessagePack.
4. Implement `rebuild_indexes(cpg: PiranesiCPG)` to reconstruct derived indexes after deserialization.
5. Wire into pipeline: after full scan, extract and cache PiranesiCPG.
6. Tests: round-trip serialization, index consistency, cache write/read.

### Sub-phase B: Function-Level Diff and Selective Joern Re-query (15-20h)

1. Implement `src/piranesi/scan/cpg_diff.py`:
   - `compute_function_diff(old_cpg: PiranesiCPG, incremental: IncrementalResult, project_root: Path) -> FunctionDiff`
   - `FunctionDiff` contains: `added_functions`, `removed_functions`, `modified_functions`, `unchanged_functions`.
2. Implement single-file Joern parse: `parse_single_file(server: JoernServer, file_path: Path) -> list[CPGFunction]`.
3. Implement selective taint query: given a set of changed function IDs, query only the source-sink pairs that involve those functions.
4. Update `pipeline.py` incremental path: if cached CPG exists and incremental result has changes below threshold, use `cpg_diff` instead of full re-scan.
5. Tests: fixture project, modify one function, verify only that function's CPG nodes change.

### Sub-phase C: Call Graph Invalidation and Taint Flow Recomputation (12-15h)

1. Implement `invalidate_direct(cpg: PiranesiCPG, changed_ids: set[str])` — remove stale edges and flows.
2. Implement `invalidate_transitively(cpg: PiranesiCPG, changed_ids: set[str], max_depth: int) -> set[str]` — BFS transitive invalidation.
3. Implement `recompute_edges(cpg: PiranesiCPG, server: JoernServer, affected_ids: set[str])` — query Joern for new call edges from/to affected functions.
4. Implement `recompute_taint_flows(cpg: PiranesiCPG, server: JoernServer, affected_ids: set[str])` — re-run `reachableByFlows` for affected source-sink pairs.
5. Implement the fallback: if `len(affected_ids) > incremental_threshold`, abort incremental and trigger full scan.
6. Tests: modify a function that is called by 3 others, verify transitive invalidation up to depth 3, verify taint flows are recomputed.

### Sub-phase D: Cache Management and CLI Commands (8-10h)

1. Implement cache size tracking and LRU eviction in `src/piranesi/scan/cpg_cache.py`.
2. Add `piranesi cache info`, `piranesi cache clear`, `piranesi cache clear --stale`, `piranesi cache dump` CLI commands.
3. Add `cpg_cache_max_mb` and `incremental_threshold` and `incremental_invalidation_depth` to `ScanConfig`.
4. Implement `--verify-incremental` flag: dual-mode scan + comparison.
5. Update `.gitignore` template in `piranesi scaffold` to include `.piranesi-cache/`.
6. Tests: cache eviction, stale cache detection, verify-incremental comparison, CLI output.

### Sub-phase E: Integration Testing and Benchmarking (5-10h)

1. End-to-end test: 50-file fixture, modify 1/5/20 files, verify incremental correctness.
2. Performance benchmark: time full vs incremental for varying change sizes, assert targets from Section 8.
3. Ground truth suite: run with `--incremental`, assert identical findings.
4. Edge cases: file deletion, file rename, new file addition, function rename, anonymous function change.
5. Cache corruption: truncate `cpg.msgpack`, verify graceful fallback.

---

## 10. Testing Strategy

### 10.1 Fixtures

Create `tests/fixtures/incremental_cpg/` with a 50-file TypeScript project:
- 10 route handler files (entry points with HTTP sources).
- 10 service files (business logic, call DB sinks).
- 10 utility files (helper functions, some with sanitizers).
- 10 model/type files (no taint relevance).
- 10 test files (excluded by default).

### 10.2 Test Matrix

| Test | Changed files | Expected behavior |
|------|-------------|-------------------|
| No change | 0 | Cache hit, zero Joern invocations, identical findings |
| Single service edit | 1 | Re-parse 1 file, re-query taint for affected functions |
| New route handler | 1 (added) | Parse new file, add to CPG, discover new entry point |
| Delete a utility | 1 (deleted) | Remove from CPG, invalidate callers, re-query taint |
| Rename function | 1 (modified) | Treated as delete + add, transitive invalidation |
| Modify 5 services | 5 | Batch re-parse, batch taint re-query |
| Modify 25 files | 25 | Exceeds threshold, falls back to full scan |
| Config change | 0 (config) | Cache invalidated, full scan |
| Piranesi version bump | 0 (version) | Cache invalidated, full scan |
| Corrupt cache file | 0 | SHA-256 mismatch, full scan, cache rebuilt |
| `--verify-incremental` | 1 | Both modes run, findings compared, no divergence |

### 10.3 Performance Benchmarks

Benchmark tests are optional (marked `@pytest.mark.benchmark`) and run in CI nightly, not on every PR. They assert:
- 1-file incremental < 5s (relaxed for CI, tighter locally).
- 5-file incremental < 15s.
- Cache load + serialize round-trip < 500ms for a 500-function CPG.

### 10.4 Edge Cases

- **Anonymous functions**: file change forces re-analysis of all anonymous functions in that file.
- **Re-exported functions**: `export { foo } from './bar'` — changing `bar.ts` invalidates both `bar::foo` and any module that re-exports it.
- **Circular call graphs**: `A → B → C → A`. Changing `A` triggers transitive invalidation. BFS with `visited` set prevents infinite loops. Depth limit caps propagation.
- **Dynamic imports**: `import('./module').then(m => m.handler(req.body))` — treated conservatively as invalidating the entire dynamic import target.
- **Monorepo**: each workspace package gets its own `PiranesiCPG` cache. Cross-package call edges are tracked with qualified function IDs (`@scope/pkg::file::func`).

---

## 11. Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Joern parse of single file produces different AST structure than full-project parse | Incorrect CPG patch, missed findings | Medium | Verify-incremental mode catches divergence; fall back to full scan |
| MessagePack schema evolution breaks deserialization on piranesi upgrade | Cache load failure | Low | Version field in PiranesiCPG triggers invalidation; graceful fallback |
| Transitive invalidation cascades to most of the project | No speedup, wasted overhead | Medium | Threshold check: if invalidated functions > `incremental_threshold`, abort early |
| Anonymous/dynamic functions cause under-invalidation | Missed findings | Low | Conservative: any anonymous function in a changed file is re-analyzed |
| Large CPG cache consumes excessive disk | User complaint | Low | LRU eviction with configurable max size |
| Joern server startup overhead dominates for tiny changes | Minimal speedup for 1-file case | High | Reuse persistent Joern server from LSP mode (Phase 24); without it, startup is ~2s floor |

---

## 12. Future Extensions

- **Persistent Joern server**: if the LSP mode (Phase 24) keeps a Joern server running, incremental queries skip the 2s startup penalty. Single-file change drops to sub-1s.
- **Watch mode**: `piranesi watch ./target` — filesystem watcher triggers incremental scan on save. Requires persistent Joern server.
- **Distributed CPG cache**: share cached PiranesiCPG across CI runners via artifact storage. Cache key includes `project_hash`, so any runner scanning the same commit reuses the cache.
- **Partial Joern overlay**: when Joern natively supports incremental CPG overlays (tracked in joernio/joern), switch from Option C to Option A for tighter integration and reduced duplication.
