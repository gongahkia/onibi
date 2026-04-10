# Phase 22: Advanced Taint Analysis

**Estimated effort: 50-65 ideal hours**
**Blocked by: Phase 18 (multi-language depth)**
**Blocks: Phase 23 (reachability analysis)**

## 1. Motivation

Current taint analysis uses Joern's `reachableByFlows` which handles basic inter-procedural flows. However, modern JavaScript/TypeScript patterns — callbacks, Promises, event emitters, higher-order functions, destructuring, spread operators — create taint paths that basic flow analysis misses. This phase closes those gaps.

Additionally, current sanitizer detection is binary (present = suppress finding). Context-sensitive sanitizer validation recognizes that an HTML escape function does NOT sanitize SQL injection, dramatically reducing false negatives from incorrect suppression.

## 2. Deep Inter-Procedural Analysis

### 2.1 Callback Chains

**Pattern:**
```javascript
db.query(sql, (err, result) => {
    res.send(result.rows); // taint from sql → result via callback
});
```

**Detection:** Track taint through callback parameters. When a function call takes a callback argument, and the called function passes data to the callback, create a taint transfer edge from call arguments to callback parameters.

**CPGQL strategy:**
```
cpg.call.name("query").argument.isMethodRef
  .referencedMethod.parameter
```

### 2.2 Promise Chains

**Pattern:**
```javascript
fetch(userUrl)
    .then(response => response.json())
    .then(data => db.query(data.sql)); // taint from userUrl → data
```

**Detection:** `.then()` and `await` create taint transfer from the resolved value to the next handler's parameter. Model `Promise.resolve(x).then(f)` as `f(x)`.

### 2.3 Event Emitter Patterns

**Pattern:**
```javascript
emitter.on('user-input', (data) => {
    db.query(data.sql); // taint from emit → handler
});
// elsewhere:
emitter.emit('user-input', req.body);
```

**Detection:** Match `emitter.on(event, handler)` with `emitter.emit(event, data)` by event name. Create taint transfer from emit arguments to handler parameters.

### 2.4 Higher-Order Functions

**Pattern:**
```javascript
const results = userInputs.map(input => db.query(input));
```

**Detection:** For `Array.prototype.map/filter/forEach/reduce`, taint flows from the array elements to the callback's first parameter.

### 2.5 Implementation

`src/piranesi/detect/interprocedural.py`:

```python
@dataclass
class TaintTransfer:
    from_param_index: int
    to_return: bool
    to_sink: str | None # sink name if this function contains a sink
    confidence: float

@dataclass
class FunctionSummary:
    function_name: str
    module_path: str
    transfers: list[TaintTransfer]
```

For each function, compute a `FunctionSummary` by analyzing its body:
1. If parameter flows to return → `TaintTransfer(from_param=0, to_return=True)`.
2. If parameter flows to a sink → `TaintTransfer(from_param=0, to_sink="db.query")`.
3. Use summaries at call sites to propagate taint without re-analyzing the callee.

## 3. Alias + Prototype Pollution Analysis

### 3.1 Alias Tracking

Track taint through JavaScript object operations:

**Property assignment:**
```javascript
const obj = {};
obj.name = req.body.name; // taint obj.name
db.query(obj.name);       // should detect
```

**Destructuring:**
```javascript
const { name, email } = req.body; // taint name, email
db.query(name);                    // should detect
```

**Spread:**
```javascript
const merged = { ...req.body, safe: true };
db.query(merged.name); // should detect (spread preserves taint)
```

Implementation: extend Joern queries to track `FIELD_IDENTIFIER` nodes and property access chains.

### 3.2 Prototype Pollution (CWE-1321)

**Source:** User-controlled key in bracket notation: `obj[userKey] = userValue`.

**Sink:** Recursive merge functions that don't guard `__proto__`:
- `lodash.merge`, `lodash.defaultsDeep`
- `Object.assign` (shallow, but still dangerous with `__proto__`)
- Custom recursive merge functions

**Detection CPGQL:**
```
cpg.call.name("<operator>.indexAccess")
  .argument(1).reachableBy(sources)
```

**Sanitizer:** Libraries that freeze prototypes or validate keys (`Object.freeze(Object.prototype)`, key validation against `__proto__`/`constructor`).

### 3.3 Specs

Add to `scan/specs.py`:

```python
SinkSpec(
    name="prototype_pollution_merge",
    pattern='cpg.call.methodFullName(".*lodash[.](merge|defaultsDeep).*")',
    sink_type=SinkType.PROTOTYPE_POLLUTION,
    cwe_id="CWE-1321",
    severity="high",
)
```

## 4. Context-Sensitive Sanitizer Validation

### 4.1 Problem

Current behavior: if a flow passes through ANY sanitizer function, the finding is suppressed. This is incorrect:

- `escapeHtml(input)` sanitizes XSS but NOT SQLi.
- `db.query($1, [input])` (parameterized query) sanitizes SQLi but NOT XSS.
- `encodeURIComponent(input)` sanitizes some path traversal but NOT SSRF.

### 4.2 Sanitizer Effectiveness Matrix

| Sanitizer | CWE-89 (SQLi) | CWE-79 (XSS) | CWE-78 (CmdI) | CWE-22 (PathTrav) | CWE-918 (SSRF) |
|-----------|:-:|:-:|:-:|:-:|:-:|
| HTML escape | - | Y | - | - | - |
| SQL parameterize | Y | - | - | - | - |
| URL encode | - | P | - | P | - |
| Shell escape | - | - | Y | - | - |
| Path normalize | - | - | - | Y | - |
| URL whitelist | - | - | - | - | Y |
| Input validation (regex) | P | P | P | P | P |

Y = effective, P = partial (reduce confidence 0.3), - = ineffective.

### 4.3 Implementation

`src/piranesi/detect/sanitizer_validation.py`:

```python
SANITIZER_EFFECTIVENESS: dict[str, dict[str, str]] = {
    "html_escape": {"CWE-79": "effective", "CWE-89": "ineffective", ...},
    "sql_parameterize": {"CWE-89": "effective", "CWE-79": "ineffective", ...},
    ...
}

def validate_sanitizer(sanitizer_name: str, cwe_id: str) -> str:
    """Returns 'effective', 'partial', or 'ineffective'."""
```

### 4.4 Bypass Detection

Detect common sanitizer bypass patterns:
- Double encoding: `%253C` (double-URL-encoded `<`)
- Nested contexts: JSON string containing HTML (`{"html": "<script>"}`)
- Charset tricks: `%00` null byte truncation
- Case variation: `<ScRiPt>` bypassing case-sensitive filters

Flag these as `sanitizer_bypassed: true` with increased confidence.

## 5. Ground Truth Additions

- 10+ entries for callback/promise/event taint patterns
- 5+ entries for alias tracking (property, destructuring, spread)
- 5+ entries for prototype pollution
- 5+ entries for context-sensitive sanitizer scenarios (correct suppression + incorrect suppression that should be caught)

## 6. Tests

1. **Callback taint**: fixture with `db.query(sql, callback)` pattern, verify finding.
2. **Promise taint**: fixture with `.then()` chain, verify finding.
3. **Event emitter**: fixture with `emitter.on/emit`, verify finding.
4. **Alias**: fixture with destructuring + spread + property assignment to sink.
5. **Prototype pollution**: fixture with `lodash.merge` + user-controlled key.
6. **Sanitizer validation**: fixture where `escapeHtml` is used before SQL query — should NOT suppress SQLi.
7. **Bypass detection**: fixture with double-encoded XSS payload.

## 7. Risks

- **False positives from alias tracking**: over-approximation of object property taint. Mitigation: only track properties explicitly assigned from tainted sources.
- **Event emitter matching**: string event names may be dynamic (`emitter.emit(varName, data)`). Mitigation: only match literal string event names.
- **Performance**: deeper analysis increases Joern query count. Mitigation: compute function summaries once, cache, reuse at call sites.
