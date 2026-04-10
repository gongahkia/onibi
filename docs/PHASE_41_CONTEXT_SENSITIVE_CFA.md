# Phase 41 — Context-Sensitive Call Graph (k-CFA)

Deepens inter-procedural analysis precision via k-limiting Control Flow Analysis. Reduces false positives caused by merging taint states from unrelated call sites.

---

## Section 1: Overview

### Problem
Current inter-procedural analysis (`detect/interprocedural.py`) uses a context-insensitive (0-CFA) call graph. One summary per function merges all calling contexts:

```typescript
function process(input: string) { db.query(input); }

// context A: safe — input is a constant
process("SELECT 1");

// context B: vulnerable — input is user-controlled
process(req.body.query);
```

0-CFA merges both call sites into a single summary: `input` is tainted. This produces a false positive for context A.

### Goal
Implement 1-CFA: separate function summaries per call site. When `process` is called from the safe context, it gets a clean taint state. When called from the vulnerable context, taint propagates correctly.

### Impact
- Estimated 10-20% FP reduction on real-world codebases
- Primarily benefits utility/helper functions called from multiple contexts
- No effect on direct taint paths (already precise)

---

## Section 2: k-CFA Background

### Context Sensitivity Levels

| Level | Description | Context String | Summaries per Function |
|-------|-------------|----------------|----------------------|
| 0-CFA | Context-insensitive (current) | `()` | 1 |
| 1-CFA | Call-site sensitive | `(caller:line)` | N (one per call site) |
| 2-CFA | Call-chain sensitive | `(caller1:line1, caller2:line2)` | N^2 |
| k-CFA | k-deep call chain | k-tuple of (caller:line) | N^k |

1-CFA is the sweet spot: significant precision gain with manageable overhead. 2-CFA provides diminishing returns for web apps where call chains are typically shallow.

### Formal Definition
For 1-CFA, the context of a function call `f()` at call site `s` in function `g` is:
```
context(f, s, g) = (g.name, s.line_number)
```

The taint summary for `f` under this context is independent of summaries under other contexts:
```
summary(f, ctx_a) != summary(f, ctx_b)  // independent analysis
```

---

## Section 3: Call Graph Construction

### Implementation: `src/piranesi/detect/cfa.py`

```python
@dataclass(frozen=True)
class CallContext:
    """Immutable context string for k-CFA."""
    chain: tuple[tuple[str, int], ...]  # ((method_name, call_line), ...)

    @staticmethod
    def empty() -> "CallContext":
        return CallContext(chain=())

    def extend(self, caller: str, line: int, k: int) -> "CallContext":
        new = self.chain + ((caller, line),)
        return CallContext(chain=new[-k:])  # truncate to k

    def __str__(self) -> str:
        return "→".join(f"{m}:{l}" for m, l in self.chain) or "∅"

@dataclass
class ContextualSummary:
    """Taint summary for a function under a specific calling context."""
    context: CallContext
    function_name: str
    param_taint: dict[int, set[str]]  # param_index → taint labels
    return_taint: set[str]            # taint labels on return value
    sink_reaches: list[SinkReach]     # sinks reachable with tainted params
```

### Algorithm
1. Start from entry points (route handlers, exports) with `CallContext.empty()`
2. At each call site `f()` in function `g` at line `L`:
   - Compute context: `ctx = current_ctx.extend(g.name, L, k=1)`
   - Look up or create `ContextualSummary(ctx, f.name)`
   - Analyze `f` under this context with only the taint state from this call site
3. Propagate taint through the contextual summary back to the caller
4. At k-limit: if context depth reaches k, merge into a single "overflow" context

### Context Merging at k-Limit
When context chain length reaches k:
```python
def merge_contexts(summaries: list[ContextualSummary]) -> ContextualSummary:
    """Conservative merge: union of all taint states."""
    merged_param_taint = {}
    merged_return_taint = set()
    for s in summaries:
        for idx, labels in s.param_taint.items():
            merged_param_taint.setdefault(idx, set()).update(labels)
        merged_return_taint.update(s.return_taint)
    return ContextualSummary(
        context=CallContext.empty(),
        function_name=summaries[0].function_name,
        param_taint=merged_param_taint,
        return_taint=merged_return_taint,
        sink_reaches=list(chain.from_iterable(s.sink_reaches for s in summaries)),
    )
```

---

## Section 4: Taint Propagation with Context

### Per-Context Taint State
Replace the global `TaintSummary` in `interprocedural.py` with context-keyed lookup:

```python
class ContextSensitiveStore:
    """Maps (function_name, context) → ContextualSummary."""
    _store: dict[tuple[str, CallContext], ContextualSummary]

    def get_or_create(self, func: str, ctx: CallContext) -> ContextualSummary:
        key = (func, ctx)
        if key not in self._store:
            self._store[key] = ContextualSummary(ctx, func, {}, set(), [])
        return self._store[key]

    def all_contexts(self, func: str) -> list[ContextualSummary]:
        return [v for (f, _), v in self._store.items() if f == func]
```

### Propagation Rules
When analyzing function `f` called from context `ctx`:
1. **Parameter taint**: only propagate taint labels present at the call site arguments
2. **Return taint**: return labels are propagated back to the caller's context only
3. **Sink reaches**: if a tainted parameter reaches a sink, record it under this context
4. **Transitive calls**: if `f` calls `g`, extend context and analyze `g` with `f`'s context-specific taint

### Example
```typescript
function escape(s: string) { return s.replace(/</g, "&lt;"); } // sanitizer
function query(s: string) { db.execute(s); }                    // sink

// route A: XSS path — escape is relevant
app.get("/a", (req, res) => {
    const safe = escape(req.query.name);
    res.send(safe);  // sanitized — no finding
});

// route B: SQLi path — escape is irrelevant
app.get("/b", (req, res) => {
    const escaped = escape(req.query.name);
    query(escaped);  // escape does NOT sanitize SQLi — finding!
});
```

With 1-CFA:
- `escape@(routeA:3)`: return is XSS-sanitized
- `escape@(routeB:9)`: return is XSS-sanitized but NOT SQLi-sanitized
- `query@(routeB:10)`: taint reaches SQLi sink → finding reported

Without 1-CFA (0-CFA): both paths merged, escape might incorrectly suppress the SQLi finding.

---

## Section 5: Dynamic Dispatch Resolution

### JavaScript
```javascript
class UserService { getUser(id) { return db.query(`SELECT * FROM users WHERE id = ${id}`); } }
class MockService { getUser(id) { return { id, name: "test" }; } }

function handler(service, req) {
    return service.getUser(req.params.id);  // which getUser?
}
```

Resolution strategy:
1. **Constructor analysis**: track `new UserService()` → type assignment
2. **Type narrowing**: `if (service instanceof UserService)` branches
3. **Conservative fallback**: if target unknown, analyze ALL possible implementations (union of results)

### TypeScript
Leverage type annotations for dispatch resolution:
```typescript
function handler(service: UserService, req: Request) {
    return service.getUser(req.params.id);  // resolved via type annotation
}
```

### Python
Approximate via static type inference and MRO:
```python
def handle(service: UserService, request):
    return service.get_user(request.args["id"])  # resolved via type hint
```
Without type hints: fallback to class hierarchy analysis (`__mro__`).

### Resolution Priority
1. Explicit type annotation → resolve directly
2. Constructor at call site → resolve to constructed type
3. isinstance/typeof narrowing → resolve within branch
4. Multiple candidates → analyze all (conservative union)

---

## Section 6: Performance Mitigation

### Context Budget
```python
MAX_CONTEXTS_PER_FUNCTION = 1000  # configurable via [detect] max_contexts
```
If a function exceeds the budget, collapse all contexts into a single 0-CFA summary:
```python
def check_budget(self, func: str) -> bool:
    contexts = self.all_contexts(func)
    if len(contexts) > MAX_CONTEXTS_PER_FUNCTION:
        merged = merge_contexts(contexts)
        self._store = {k: v for k, v in self._store.items() if k[0] != func}
        self._store[(func, CallContext.empty())] = merged
        return False  # degraded to 0-CFA
    return True
```

### Hot Function Detection
Functions called from > 50 distinct call sites are "hot" — analyze context-insensitively from the start:
```python
HOT_THRESHOLD = 50  # configurable

def is_hot(self, func: str, call_sites: list) -> bool:
    return len(call_sites) > HOT_THRESHOLD
```

Common hot functions in web apps: `console.log`, `JSON.stringify`, `toString`, utility formatters. These rarely carry security-relevant taint state, so 0-CFA is sufficient.

### Memoization
If two contexts produce identical taint states at function entry, share the summary:
```python
def taint_signature(params: dict[int, set[str]]) -> str:
    return hashlib.md5(str(sorted(params.items())).encode()).hexdigest()
```
Before computing a new context's summary, check if an existing context has the same taint signature → reuse.

### Lazy Context Splitting
Start with 0-CFA (single summary). Split into per-context summaries only when:
1. A function is called with different taint states from different call sites
2. The merged summary produces a finding that would be pruned in at least one context

This avoids the overhead of context splitting for functions that are always called with the same taint state.

---

## Section 7: Integration

### Replace Call Graph in `detect/interprocedural.py`
```python
# before (0-CFA)
summary = compute_taint_summary(function)

# after (configurable k-CFA)
k = config.detect.context_sensitivity  # default: 1
if k == 0:
    summary = compute_taint_summary(function)  # existing path
else:
    ctx = current_context.extend(caller, call_line, k)
    summary = compute_contextual_summary(function, ctx)
```

### Configuration
```toml
[detect]
context_sensitivity = 1      # 0 = current (0-CFA), 1 = 1-CFA, 2 = 2-CFA
max_contexts = 1000           # per-function context budget
hot_threshold = 50            # call sites before falling back to 0-CFA
context_timeout = 300         # seconds — degrade to 0-CFA if exceeded
```

### Fallback
If total analysis time exceeds `context_timeout`:
1. Log warning with function names that were still pending
2. Merge all pending contexts to 0-CFA
3. Continue with merged summaries
4. Report in output: `"context_sensitivity_degraded": true`

---

## Section 8: Testing Strategy

### Precision Tests (15+ fixtures)
Each fixture demonstrates a case where 1-CFA produces fewer FPs than 0-CFA:

1. **Utility function with mixed callers**: `sanitize()` called from safe and unsafe contexts
2. **Shared DB accessor**: `findById(id)` called with constant and user-controlled IDs
3. **Logging helper**: `log(data)` called with tainted and clean data — should not taint log callers
4. **Error handler**: `handleError(err)` called in various contexts
5. **Validation helper**: `validate(input)` called for different input types
6. **Callback propagation**: `process(data, callback)` with different callbacks
7. **Promise chain**: `fetch().then(transform).then(sink)` with different transforms per context
8. **HOF context**: `arr.map(fn)` where `fn` varies by context
9. **Module re-export**: same function re-exported and called in different modules
10. **Dynamic dispatch**: interface method called on different implementations
11. **Recursive function**: `walk(tree)` called on tainted vs clean subtrees
12. **Builder pattern**: `builder.set(key, val).build()` with mixed inputs
13. **Middleware chain**: same middleware applied to different routes
14. **Config reader**: `getConfig(key)` called for sensitive and non-sensitive keys
15. **Type narrowing**: same variable, different types in different branches

### Benchmark Tests
- **FP comparison**: run full GT suite with 0-CFA and 1-CFA, measure FP reduction
- **TP regression**: verify zero TP loss when upgrading to 1-CFA
- **Performance**: time overhead of 1-CFA vs 0-CFA on projects of varying size
  - Small (< 50 files): < 2x overhead acceptable
  - Medium (50-500 files): < 3x overhead acceptable
  - Large (500+ files): < 5x overhead acceptable, with degradation fallback

### Edge Case Tests
- Function with 0 call sites (dead code) → skip context analysis
- Function with 1 call site → 1-CFA = 0-CFA (no overhead)
- Recursive function → detect recursion, use fixed-point with bounded iterations
- Mutual recursion (A calls B calls A) → cycle detection, merge at cycle boundary
- Variadic functions → conservative: all args potentially tainted

---

## Section 9: Deliverables

| Artifact | Description |
|----------|-------------|
| `src/piranesi/detect/cfa.py` | Core k-CFA engine: `CallContext`, `ContextualSummary`, `ContextSensitiveStore` |
| `detect/interprocedural.py` updates | Replace 0-CFA call graph with configurable k-CFA |
| `piranesi.toml` schema | `[detect] context_sensitivity`, `max_contexts`, `hot_threshold`, `context_timeout` |
| `tests/test_detect/test_cfa.py` | 15+ precision fixtures, benchmark suite, edge cases |
| `eval/` updates | k-CFA vs 0-CFA comparison report |

### Dependencies
- Wave 31 (advanced taint / interprocedural.py) — existing code to integrate with
- Wave 38 (field-sensitive taint) — benefits from combined precision (field + context)

### Not In Scope
- Object sensitivity (tracking allocation site instead of call site) — future work
- Flow sensitivity within functions (already handled by Joern's CPG)
- Thread sensitivity (concurrent contexts) — out of scope for single-threaded JS/TS
