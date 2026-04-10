# Phase 8: False Positive Reduction

**Estimated effort: 20-30 ideal hours**
**Blocked by: Phase 5 (ground truth for measurement)**
**Blocks: Phase 10 (framework support needs clean baseline)**
**Target milestone: v0.2.0**

---

## 1. Phase Overview

Piranesi's NodeGoat benchmark produced 17 SSRF false positives and missed NoSQL injection. The vuln-express benchmark missed a SQLi in a local `query()` helper. These are systematic precision problems, not random noise.

This phase attacks false positives through three vectors: refined CPGQL sink queries, framework-aware sanitizer recognition, and taint path pruning heuristics. The goal is to bring precision from ~60% to ~85% without sacrificing recall.

---

## 2. SSRF Sink Query Refinement

**Estimated effort: 8-10h**

### 2.1 Problem

The current SSRF sink pattern matches any call to `fetch()`, `axios.get()`, `http.get()`, etc. In Express apps, route handlers that make HTTP requests to hardcoded internal services are flagged as SSRF even when the URL is not user-controlled.

### 2.2 Solution

Refine CPGQL queries in `src/piranesi/scan/specs.py` and `scan/queries.py`:

1. **URL argument taint check**: Only flag HTTP calls where the URL argument (first arg) is tainted. Currently we flag if ANY argument is tainted (e.g., body of a POST request being forwarded).
2. **Allowlist pattern detection**: If the code constructs a URL from a hardcoded base + user input path segment, check if the base URL is a constant string. `fetch(\`http://internal-service/api/${userId}\`)` is lower risk than `fetch(req.body.url)`.
3. **Protocol validation detection**: If the code checks `url.startsWith('https://')` or uses `new URL()` + `url.protocol` check before the sink, reduce confidence.

### 2.3 Implementation

Update `src/piranesi/scan/specs.py`:
- Split SSRF sinks into `ssrf_full_url` (user controls entire URL) and `ssrf_path_segment` (user controls path/query only).
- `ssrf_full_url` → severity HIGH, `ssrf_path_segment` → severity MEDIUM.

Update `src/piranesi/scan/queries.py`:
- Add CPGQL query to check if first argument to `fetch`/`axios`/`http.get` is tainted (not just any argument).
- Add pattern: if URL is a template literal with hardcoded scheme+host, downgrade to `ssrf_path_segment`.

### 2.4 Metrics

Measure before/after on NodeGoat:
- Before: 17 SSRF FPs
- Target: <= 3 SSRF FPs (reduce by 80%+)

---

## 3. Framework Sanitizer Recognition

**Estimated effort: 6-8h**

### 3.1 Problem

Express middleware like `helmet()`, `express-validator`, `xss-clean`, and `hpp` are invisible to Joern. Taint flows that pass through these middleware are not pruned.

### 3.2 Solution

Extend sanitizer specs in `src/piranesi/scan/specs.py`:

| Sanitizer | CWE Mitigated | Pattern |
|-----------|---------------|---------|
| `validator.escape()` | CWE-79 | `express-validator` |
| `sanitizeHtml()` | CWE-79 | `sanitize-html` |
| `DOMPurify.sanitize()` | CWE-79 | `dompurify` |
| `sqlstring.escape()` | CWE-89 | `sqlstring` |
| `pg.Pool` parameterized | CWE-89 | `$1` placeholder pattern |
| `path.resolve() + startsWith()` | CWE-22 | Path containment check |
| `parseInt()` / `Number()` | CWE-89 | Type coercion to number |
| `encodeURIComponent()` | CWE-79, CWE-918 | URL encoding |

### 3.3 Implementation

Add new `SanitizerSpec` entries to `scan/specs.py`. Each spec has:
- `name`: human-readable identifier
- `pattern`: CPGQL pattern matching the sanitizer call
- `mitigates`: list of CWE IDs this sanitizer addresses
- `confidence`: how confident we are this fully mitigates (0.0-1.0)

Update flow extraction in `detect/flows.py` to check if a taint path passes through any sanitizer and adjust confidence accordingly.

---

## 4. Taint Path Pruning Heuristics

**Estimated effort: 6-8h**

### 4.1 Dead Code Path Pruning

If a taint path passes through a branch that is unreachable (e.g., `if (false) { sink(tainted) }`), prune it. Use Joern's `isCallTo` and `controlledBy` to detect statically-false conditions.

### 4.2 Type Narrowing Pruning

If a taint path passes through `typeof x === 'number'` or `Number.isInteger(x)`, and the sink expects a string (e.g., SQL concatenation), prune the path. The type check prevents string-based injection.

### 4.3 Allowlist Pruning

If a taint path passes through a pattern like:
```javascript
const allowed = ['admin', 'user', 'guest'];
if (!allowed.includes(input)) return;
```
The allowlist bounds the input space, preventing injection. Detect `includes()` + early return patterns.

---

## 5. Ground Truth Validation

Run the improved pipeline against all 50 ground truth entries. Compare precision/recall before and after.

| Metric | Before | Target |
|--------|--------|--------|
| Precision | ~60% | >= 85% |
| Recall | ~75% | >= 70% (no regression) |
| SSRF FPs (NodeGoat) | 17 | <= 3 |
| Overall F1 | ~67% | >= 77% |

---

## 6. Acceptance Criteria

- [ ] SSRF false positives reduced by >= 80% on NodeGoat
- [ ] Framework sanitizers recognized for top 8 patterns
- [ ] Type narrowing pruning implemented
- [ ] Precision >= 85% on ground truth
- [ ] Recall >= 70% on ground truth (no regression)
- [ ] All changes covered by new test cases
