# Phase 14: Incremental Scanning + Performance

**Estimated effort: 30-40 ideal hours**
**Blocked by: Phase 6 (pipeline working)**
**Blocks: Nothing (independent optimization)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Every `piranesi run` re-transpiles, re-imports to Joern, and re-analyzes the entire project. For large codebases in CI, this means 2-5 minutes per scan even when only 3 files changed. This phase adds incremental scanning (only analyze changed files), CPG caching (reuse Joern's Code Property Graph between runs), and intra-stage parallelism.

---

## 2. Incremental Scanning

**Estimated effort: 12-15h**

### 2.1 Change Detection

Implement `src/piranesi/scan/incremental.py`:

```python
def detect_changed_files(
    target_dir: Path,
    baseline_dir: Path | None,
) -> IncrementalResult:
    """Compare current source against previous scan's file manifest."""
```

Strategy:
1. On each scan, write a file manifest (`{output_dir}/_manifest.json`) with: file path, SHA-256 hash, mtime, size.
2. On next scan with `--incremental` (or `--baseline {previous_output_dir}`), compare manifests.
3. Classify files as: `added`, `modified`, `deleted`, `unchanged`.
4. Only transpile and re-import modified/added files to Joern.

### 2.2 Selective Transpilation

Update `scan/transpile.py`:
- Accept a `changed_files: set[Path] | None` parameter.
- When set, only transpile changed files (copy unchanged `.js` from previous output).
- Regenerate source maps only for changed files.

### 2.3 Selective Joern Import

Update `scan/joern.py`:
- If a cached CPG exists and only some files changed, use Joern's incremental import.
- Fallback: full re-import if incremental isn't supported by the Joern version.

### 2.4 CLI Integration

Add `--incremental` flag to `piranesi run` and `piranesi scan`:
```bash
piranesi run ./target --incremental --output ./results
# Second run: only re-analyzes changed files
piranesi run ./target --incremental --output ./results
```

### 2.5 Finding Stability

When files are deleted, findings referencing those files are auto-removed. When files are unchanged, findings from the previous scan are carried forward without re-analysis.

---

## 3. CPG Caching

**Estimated effort: 8-10h**

### 3.1 Cache Key

```python
def cpg_cache_key(target_dir: Path, config: PiranesiConfig) -> str:
    """SHA-256 of: sorted file hashes + config hash."""
```

### 3.2 Cache Storage

Store cached CPG at `{output_dir}/_cpg_cache/{cache_key}.bin`. On scan:
1. Compute cache key from current file hashes + config hash.
2. If cache hit: skip transpile + Joern import, load CPG directly.
3. If cache miss: full transpile + import, save CPG to cache.

### 3.3 Cache Invalidation

Invalidate when:
- Any source file changes (hash mismatch)
- `piranesi.toml` config changes (config hash mismatch)
- Joern version changes
- `--no-cache` flag passed

---

## 4. Intra-Stage Parallelism

**Estimated effort: 6-8h**

### 4.1 Parallel Legal + Patch

ARCHITECTURE.md correctly notes legal and patch both consume verify output independently. Refactor pipeline.py to run them in parallel:

```python
with ThreadPoolExecutor(max_workers=2) as pool:
    legal_future = pool.submit(_run_legal_stage, context, config, prev)
    patch_future = pool.submit(_run_patch_stage, context, config, prev)
    legal_result = legal_future.result()
    patch_result = patch_future.result()
```

### 4.2 Parallel Finding Processing

Within detect, triage, and verify stages, process findings in parallel:
- Detect: batch Joern queries (submit multiple CPGQL queries, await all)
- Triage: ensemble voting already supports concurrent model calls; extend to process multiple findings concurrently
- Verify: run sandbox containers in parallel (up to `max_containers` config limit)

---

## 5. Performance Metrics

Add `--profile` flag that outputs per-stage timing breakdown:
```
Stage       Duration  Findings  Cache
scan        1.2s      -         HIT
detect      3.4s      12        -
triage      5.1s      12→8      -
verify      12.3s     8→6       -
legal       0.4s      6         -
patch       2.1s      6         -
report      0.1s      -         -
TOTAL       24.6s     6 confirmed
```

---

## 6. Acceptance Criteria

- [ ] `--incremental` flag skips unchanged files
- [ ] File manifest written and compared between runs
- [ ] CPG caching with config-hash key
- [ ] `--no-cache` flag to force full re-scan
- [ ] Legal + patch stages run in parallel
- [ ] `--profile` flag shows per-stage breakdown
- [ ] 50%+ speedup on second scan of unchanged codebase
- [ ] No finding regression between full and incremental scans
