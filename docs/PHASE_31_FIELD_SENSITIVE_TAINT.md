# Phase 31: Field-Sensitive Taint Propagation

**Estimated effort: 40-55 ideal hours**
**Blocked by: Phase 22 (advanced taint analysis, alias tracking)**
**Blocks: none (optional input to Phase 23 reachability pruning)**

---

## 1. Overview

### 1.1 Problem

Current taint analysis operates at object granularity. When `req.body` is marked as a taint source, every destructured binding inherits the taint label unconditionally:

```typescript
const { id, name } = req.body;
// current: id=tainted, name=tainted

const sanitizedName = escapeHtml(name);
db.query(`SELECT * FROM users WHERE id = '${id}'`);      // TP — id is unsanitized
res.send(`Hello ${sanitizedName}`);                        // FP — name was sanitized for XSS
```

The existing `_AliasState` in `detect/alias.py` tracks `properties: dict[tuple[str, str], _TaintOrigin]` and `objects: dict[str, _TaintOrigin]`, but the Joern-backed flow analysis in `detect/flows.py` does not refine per-field taint after `reachableByFlows` returns a candidate path. Every field extracted from a tainted object is treated identically.

### 1.2 Goal

Implement field-sensitive taint propagation: track which specific property paths carry tainted data through the flow, so that sanitization of one field does not suppress findings for a different field from the same source object.

### 1.3 Expected Impact

- **15-25% fewer false positives** on multi-field destructuring patterns (most Express/Fastify handlers destructure `req.body`).
- **Zero TP regression** — conservative fallback for ambiguous cases preserves all existing true positives.
- Directly improves precision metrics against the ground truth suite.

---

## 2. Taint Label Model

### 2.1 Data Structures

Replace the implicit binary tainted/untainted model with per-field taint labels.

```python
@dataclass(frozen=True, slots=True)
class TaintLabel:
    source_id: str                    # e.g. "req.body" — ties back to SourceSpec
    field_path: str                   # e.g. "body.user.email", "" for whole-object
    confidence: float                 # 0.0-1.0
    sanitized_for: frozenset[str]     # CWE IDs for which this field has been sanitized

@dataclass(slots=True)
class FieldTaintState:
    labels: dict[str, TaintLabel]     # variable_name -> TaintLabel
    object_labels: dict[str, dict[str, TaintLabel]]  # obj_name -> {field -> TaintLabel}
```

A variable with `field_path=""` means the entire object is tainted (no field narrowing has occurred). Any property read on such a variable produces a label with the specific field path.

### 2.2 Propagation Rules

| Operation | Input | Output Label |
|---|---|---|
| Property read: `obj.x` | `obj` has whole-object label `L` | `TaintLabel(L.source_id, L.field_path + ".x", L.confidence, L.sanitized_for)` |
| Property read: `obj.x` | `obj` has field labels `{x: Lx, y: Ly}` | Propagate `Lx` only |
| Destructuring: `const {x, y} = obj` | `obj` has whole-object label `L` | `x` gets `L` with field_path `+".x"`, `y` gets `L` with field_path `+".y"` |
| Destructuring with rename: `const {x: a} = obj` | `obj` has whole-object label `L` | `a` gets `L` with field_path `+".x"` |
| Spread: `{...obj, safe: 1}` | `obj` has labels `{a: La, b: Lb, safe: Ls}` | Propagate all labels except `safe` (overridden by literal) |
| Computed property: `obj[key]` | `obj` has any labels | Propagate ALL labels (conservative — key unknown at analysis time) |
| Array index: `arr[i]` | `arr` has element labels | Propagate ALL element labels (conservative) |
| Method return: `f(x)` | `FunctionSummary` for `f` | Consult `TaintTransfer` from `interprocedural.py`; attach originating field_path |
| Sanitizer: `escapeHtml(x)` | `x` has label `L` | `L` with `sanitized_for |= {"CWE-79"}` |
| Assignment: `y = x` | `x` has label `L` | `y` gets `L` (direct propagation) |

### 2.3 Propagation Algorithm (Pseudocode)

```python
def propagate_field_taint(
    flow_path: list[TaintStep],
    source: TaintSource,
    source_spec: SourceSpec,
) -> FieldTaintState:
    state = FieldTaintState(labels={}, object_labels={})

    # seed from source
    initial_label = TaintLabel(
        source_id=source_spec.name,
        field_path=source.parameter_name or "",  # "" = whole object
        confidence=1.0,
        sanitized_for=frozenset(),
    )
    state.labels[_extract_variable(flow_path[0])] = initial_label

    for step in flow_path[1:]:
        snippet = step.location.snippet
        op = classify_step_operation(snippet)

        match op:
            case PropertyRead(obj_name, field_name):
                parent_label = state.labels.get(obj_name)
                if parent_label is None:
                    parent_label = _lookup_object_field(state, obj_name, field_name)
                if parent_label is not None:
                    new_path = _append_field(parent_label.field_path, field_name)
                    state.labels[f"{obj_name}.{field_name}"] = TaintLabel(
                        source_id=parent_label.source_id,
                        field_path=new_path,
                        confidence=parent_label.confidence,
                        sanitized_for=parent_label.sanitized_for,
                    )

            case Destructuring(bindings, source_expr):
                parent_label = state.labels.get(source_expr)
                if parent_label is None:
                    parent_label = _resolve_from_objects(state, source_expr)
                if parent_label is not None:
                    for prop_name, var_name in bindings:
                        new_path = _append_field(parent_label.field_path, prop_name)
                        state.labels[var_name] = TaintLabel(
                            source_id=parent_label.source_id,
                            field_path=new_path,
                            confidence=parent_label.confidence,
                            sanitized_for=parent_label.sanitized_for,
                        )

            case SpreadInto(target_name, spread_sources, explicit_keys):
                for spread_src in spread_sources:
                    src_labels = state.object_labels.get(spread_src, {})
                    if not src_labels:
                        # whole-object label — propagate all except overridden keys
                        base = state.labels.get(spread_src)
                        if base is not None:
                            state.object_labels.setdefault(target_name, {})[
                                "__whole__"
                            ] = base
                    else:
                        for field, lbl in src_labels.items():
                            if field not in explicit_keys:
                                state.object_labels.setdefault(target_name, {})[
                                    field
                                ] = lbl

            case ComputedAccess(obj_name):
                # conservative: propagate all labels from obj
                for field, lbl in state.object_labels.get(obj_name, {}).items():
                    state.labels[f"{obj_name}[?]"] = lbl
                base = state.labels.get(obj_name)
                if base is not None:
                    state.labels[f"{obj_name}[?]"] = base

            case SanitizerCall(sanitizer_name, arg_var, cwes_sanitized):
                existing = state.labels.get(arg_var)
                if existing is not None:
                    state.labels[arg_var] = TaintLabel(
                        source_id=existing.source_id,
                        field_path=existing.field_path,
                        confidence=existing.confidence,
                        sanitized_for=existing.sanitized_for | cwes_sanitized,
                    )

            case Assignment(target, source_var):
                src = state.labels.get(source_var)
                if src is not None:
                    state.labels[target] = src

            case _:
                pass  # unknown operation — no propagation change

    return state
```

### 2.4 Sink Check

```python
def is_field_tainted_at_sink(
    state: FieldTaintState,
    sink_variable: str,
    sink_cwe: str,
) -> bool:
    label = state.labels.get(sink_variable)
    if label is None:
        return False
    if sink_cwe in label.sanitized_for:
        return False  # this specific field was sanitized for this CWE
    return True
```

---

## 3. Joern CPG Integration

### 3.1 Strategy

Joern's `reachableByFlows` does not natively distinguish field-level taint. The approach is a **post-processing refinement layer**:

1. **Existing**: extract Joern taint flows via `reachableByFlows` (unchanged).
2. **New**: for each flow path returned by Joern, query the CPG AST for property-access and destructuring nodes along the path.
3. **New**: overlay a field-level flow graph on top of the Joern path.
4. **New**: prune flows where the specific field reaching the sink has been sanitized for the sink's CWE, or where the field never actually reaches the sink.

### 3.2 CPGQL Queries for Field Tracking

**Property access nodes along a path:**

```scala
// get all fieldAccess operations within a method
cpg.call.name("<operator>.fieldAccess")
  .filter(_.method.fullName == "{method_full_name}")
  .map(c => (c.lineNumber, c.code, c.argument.code.l))
  .toJsonPretty
```

This returns tuples of `(line, code, [object, field])`. For example, `req.body.id` yields `["req.body", "id"]`.

**Destructuring bindings (AST-level local variable declarations):**

```scala
// find destructuring pattern — Joern models this as multiple LOCALs from same line
cpg.local
  .filter(_.method.fullName == "{method_full_name}")
  .filter(l => l.lineNumber.exists(_ == {line_number}))
  .map(l => (l.name, l.code))
  .toJsonPretty
```

Cross-reference with the source snippet regex `_DESTRUCTURING_PATTERN` from `alias.py` to resolve which binding maps to which property name.

**Assignment nodes:**

```scala
cpg.call.name("<operator>.assignment")
  .filter(_.method.fullName == "{method_full_name}")
  .map(c => (c.lineNumber, c.argument(0).code, c.argument(1).code))
  .toJsonPretty
```

Returns `(line, lhs, rhs)` — needed to track `obj.field = expr` property assignments.

**Index access (computed property):**

```scala
cpg.call.name("<operator>.indexAccess")
  .filter(_.method.fullName == "{method_full_name}")
  .map(c => (c.lineNumber, c.argument(0).code, c.argument(1).code))
  .toJsonPretty
```

Returns `(line, obj, key_expression)`. If `key_expression` is a string literal, we can resolve the field; otherwise conservative.

**Identifier references (post-destructuring):**

```scala
cpg.identifier.name("{variable_name}")
  .filter(_.method.fullName == "{method_full_name}")
  .map(i => (i.lineNumber, i.code))
  .toJsonPretty
```

Tracks where a destructured variable is subsequently used.

### 3.3 Flow Path Field Annotation

For each `TaintStep` in a `CandidateFinding.taint_path`, we annotate with field metadata by running the queries above against the method containing that step. The result is a parallel list of `FieldTaintStep` objects:

```python
@dataclass(frozen=True, slots=True)
class FieldTaintStep:
    original_step: TaintStep
    field_path: str | None       # resolved field, or None if ambiguous
    operation_type: FieldOp      # property_read | destructure | spread | computed | sanitizer | assignment | unknown
    narrowed: bool               # True if field was narrowed at this step
```

---

## 4. Implementation

### 4.1 New Module: `src/piranesi/detect/field_taint.py`

```
src/piranesi/detect/field_taint.py
├── TaintLabel              (dataclass)
├── FieldTaintState         (dataclass)
├── FieldTaintStep          (dataclass)
├── FieldOp                 (enum: property_read, destructure, spread, computed, sanitizer, assignment, unknown)
├── StepClassification      (union type for classified operations)
├── classify_step_operation(snippet: str) -> StepClassification
├── propagate_field_taint(flow_path, source, source_spec) -> FieldTaintState
├── annotate_flow_with_fields(finding, joern_server) -> list[FieldTaintStep]
├── prune_untainted_fields(finding, field_steps, state) -> CandidateFinding | None
└── apply_field_sensitive_pruning(findings, joern_server, source_specs) -> list[CandidateFinding]
```

**`classify_step_operation`**: regex-based classification of a `TaintStep.location.snippet` into one of the `StepClassification` variants. Reuses patterns from `alias.py` (`_DESTRUCTURING_PATTERN`, `_SPREAD_ASSIGNMENT_PATTERN`, `_PROPERTY_ASSIGNMENT_PATTERN`, `_VARIABLE_ASSIGNMENT_PATTERN`) and adds:

```python
_FIELD_ACCESS_PATTERN = re.compile(
    r"(?P<object>[A-Za-z_$][\w$]*)\.(?P<field>[A-Za-z_$][\w$]*)"
)
_COMPUTED_ACCESS_PATTERN = re.compile(
    r"(?P<object>[A-Za-z_$][\w$]*)\[(?P<key>[^\]]+)\]"
)
```

**`propagate_field_taint`**: see pseudocode in Section 2.3. Walks the `taint_path` step by step, maintaining `FieldTaintState`. Each step either narrows, widens, or preserves field labels.

**`prune_untainted_fields`**: given a `CandidateFinding` and the computed `FieldTaintState` at the sink, check `is_field_tainted_at_sink`. Returns `None` if the specific field at the sink is sanitized for the finding's CWE, otherwise returns the finding (possibly with adjusted confidence).

**`apply_field_sensitive_pruning`**: top-level entry point. For each `CandidateFinding`:
1. Check if the source is a multi-field object (`req.body`, `req.query`, etc. with `parameter_name is None` or source_type `request_body`/`request_param`). If source is single-field (e.g. `req.params.id`), skip — no field ambiguity.
2. Call `propagate_field_taint`.
3. Call `prune_untainted_fields`.
4. Collect surviving findings.

### 4.2 Integration Point: `detect/flows.py`

In `extract_candidate_findings`, after building the list of `CandidateFinding` objects and before returning:

```python
# existing: candidate_findings = [... built from Joern flows ...]
# new: field-sensitive pruning pass
if field_sensitive:
    candidate_findings = apply_field_sensitive_pruning(
        candidate_findings,
        joern_server=server,
        source_specs=source_specs,
    )
```

The `field_sensitive` flag is controlled by config:

```toml
[analysis]
field_sensitive_taint = true  # default true
```

### 4.3 Integration Point: `detect/alias.py`

The alias-based scanner already has a `_AliasState` with `properties` and `object_safe_properties`. Extend it to emit `TaintLabel` metadata in addition to `_TaintOrigin`:

```python
@dataclass(slots=True)
class _AliasState:
    variables: dict[str, _TaintOrigin]
    properties: dict[tuple[str, str], _TaintOrigin]
    objects: dict[str, _TaintOrigin]
    object_safe_properties: dict[str, set[str]]
    field_labels: dict[str, TaintLabel]  # NEW: per-variable field label
```

When `_process_destructuring_assignment` binds a variable, it also creates a `TaintLabel` with the narrowed `field_path`. When `_origin_for_expression` is called for a sink argument, the field label is consulted to check if that specific field was sanitized.

### 4.4 Integration Point: `detect/interprocedural.py`

Extend `TaintTransfer` to optionally carry field path:

```python
@dataclass(frozen=True, slots=True)
class TaintTransfer:
    from_param_index: int
    to_return: bool = False
    to_sink: str | None = None
    sink_api_name: str | None = None
    sink_file: str | None = None
    sink_line: int | None = None
    sink_column: int | None = None
    sink_snippet: str | None = None
    via_callback_param_index: int | None = None
    to_callback_argument_index: int | None = None
    confidence: float = 1.0
    field_path: str | None = None  # NEW: specific field transferred, None = whole object
```

When building function summaries, if the function body destructures a parameter and only forwards specific fields, the `TaintTransfer` records which field_path was transferred.

---

## 5. Handling Ambiguity

### 5.1 Conservative Defaults

Every ambiguous case defaults to **all-tainted** (preserves soundness):

| Situation | Handling | Rationale |
|---|---|---|
| Computed property: `obj[dynamicKey]` | All labels propagated | Key unknown statically |
| Spread into unknown target | All labels propagated | Cannot enumerate overridden keys |
| `JSON.parse(taintedStr)` | All fields of result tainted | Parse result can contain any structure |
| `JSON.stringify(obj)` | Output string tainted if any field tainted | Serialized form contains all field data |
| `Object.keys(obj)` | Output tainted if any field tainted | Key names may be attacker-controlled |
| `Object.values(obj)` | Output tainted if any field tainted | Values derive from tainted fields |
| `Object.entries(obj)` | Output tainted if any field tainted | Entries contain both keys and values |
| `Object.assign(target, src)` | Target inherits all labels from src, minus target overrides | Equivalent to spread |
| `Array.from(obj)` | All element labels propagated | Array conversion preserves taint |

### 5.2 JSON.parse Specifics

```python
def handle_json_parse(state: FieldTaintState, result_var: str, input_var: str) -> None:
    input_label = state.labels.get(input_var)
    if input_label is None:
        return
    # result is a fresh object — all fields inherit taint from the serialized source
    state.labels[result_var] = TaintLabel(
        source_id=input_label.source_id,
        field_path="",  # whole-object — any field access on result is tainted
        confidence=input_label.confidence * 0.95,  # slight confidence reduction
        sanitized_for=frozenset(),  # sanitizations do not survive serialize/deserialize
    )
```

### 5.3 Template Literal Interpolation

```typescript
const query = `SELECT * FROM users WHERE id = '${id}' AND name = '${sanitizedName}'`;
db.query(query);
```

Template literals with multiple interpolated variables: the resulting string is tainted if ANY interpolated variable is tainted. Field-sensitive analysis checks each interpolated expression independently — if `id` is tainted for CWE-89 and `sanitizedName` is sanitized for CWE-89, the finding still fires because `id` flows to the sink.

Implementation: parse template literal interpolation expressions with:

```python
_TEMPLATE_INTERPOLATION_PATTERN = re.compile(r"\$\{([^}]+)\}")
```

Check each captured group against `FieldTaintState.labels`. If any is tainted and unsanitized for the sink CWE, the finding survives.

---

## 6. Performance Considerations

### 6.1 Phase Ordering

Field analysis runs **after** Joern flow extraction (not during CPG construction):

```
Joern reachableByFlows  →  CandidateFinding[]  →  field_taint pruning  →  filtered CandidateFinding[]
```

This means zero overhead on the Joern query phase. The field analysis is pure Python post-processing on the already-extracted flow paths.

### 6.2 Skip Conditions

Not all findings need field analysis. Skip when:

1. **Single-field source**: `source.parameter_name is not None` and source is a leaf property (e.g. `req.params.id`). No field ambiguity exists.
2. **No destructuring/spread in path**: if the taint path contains no destructuring, spread, or property assignment steps, field analysis adds nothing.
3. **Short paths** (≤2 steps): source directly reaches sink — no intermediate field operations.

Detection of skip condition 2 is a fast regex scan over `TaintStep.location.snippet` values:

```python
_FIELD_OPERATION_INDICATORS = re.compile(
    r"(?:const|let|var)\s*\{|"       # destructuring
    r"\.\.\.|"                        # spread
    r"\.\w+\s*=|"                     # property assignment
    r"\[\s*['\"]"                     # bracket property access
)

def needs_field_analysis(finding: CandidateFinding) -> bool:
    if finding.source.parameter_name is not None:
        return False
    return any(
        _FIELD_OPERATION_INDICATORS.search(step.location.snippet)
        for step in finding.taint_path
    )
```

### 6.3 Caching

Field summaries per function are cached alongside interprocedural `FunctionSummary` objects. When the same function appears in multiple flow paths, its field-level AST queries are not re-executed:

```python
@dataclass(slots=True)
class FieldSummaryCache:
    _cache: dict[str, dict[int, list[FieldASTNode]]] = field(default_factory=dict)
    # method_full_name -> {line_number -> [FieldASTNode]}

    def get_or_query(
        self,
        method_full_name: str,
        server: JoernServer,
    ) -> dict[int, list[FieldASTNode]]:
        if method_full_name not in self._cache:
            self._cache[method_full_name] = _query_field_ast_nodes(server, method_full_name)
        return self._cache[method_full_name]
```

### 6.4 Complexity Bounds

Worst case: O(F × S × P) where F = number of findings, S = average path length, P = average fields per object. In practice S ≤ 20, P ≤ 10, and the skip conditions eliminate ~60% of findings from analysis.

---

## 7. Testing Strategy

### 7.1 Fixture Categories (20+ fixtures)

**FP reduction fixtures** — cases where current analysis flags but field-sensitive must NOT:

| Fixture | Pattern | Expected |
|---|---|---|
| `field-destructure-sanitized.ts` | `const {id, name} = req.body; name = escapeHtml(name); res.send(name)` | No finding (name sanitized for XSS) |
| `field-spread-override.ts` | `const obj = {...req.body, safe: "literal"}; db.query(obj.safe)` | No finding (safe overridden) |
| `field-independent-sanitize.ts` | `const {email, token} = req.body; email = sanitize(email); db.query(email)` | No finding (email sanitized for SQLi) |
| `field-reassign-safe.ts` | `let {url} = req.body; url = "https://safe.com"; fetch(url)` | No finding (url reassigned to literal) |
| `field-partial-destructure.ts` | `const {x} = req.body; res.send(escapeHtml(x))` | No finding (x sanitized) |
| `field-json-parse-safe-field.ts` | `const parsed = JSON.parse(input); const safeId = parseInt(parsed.id); db.query(safeId)` | No finding (parseInt sanitizes) |
| `field-object-assign-override.ts` | `const cfg = Object.assign({}, req.body, {admin: false}); if (cfg.admin)` | No finding (admin overridden) |

**TP preservation fixtures** — cases where field-sensitive must still detect:

| Fixture | Pattern | Expected |
|---|---|---|
| `field-destructure-unsanitized.ts` | `const {id, name} = req.body; db.query(id)` | Finding (id unsanitized) |
| `field-spread-tainted.ts` | `const obj = {...req.body}; db.query(obj.name)` | Finding (name not overridden) |
| `field-computed-access.ts` | `const key = getKey(); db.query(req.body[key])` | Finding (conservative: computed access) |
| `field-json-parse-sink.ts` | `const parsed = JSON.parse(req.body.data); db.query(parsed.sql)` | Finding (JSON.parse result tainted) |
| `field-template-mixed.ts` | ``db.query(`SELECT * WHERE id='${id}' AND name='${escapeHtml(name)}'`)`` | Finding (id unsanitized in template) |
| `field-through-function.ts` | `function passThrough(x) { return x; } db.query(passThrough(req.body.id))` | Finding (interprocedural passthrough) |
| `field-array-map.ts` | `req.body.items.map(i => db.query(i.sql))` | Finding (array element tainted) |
| `field-nested-property.ts` | `const email = req.body.user.email; db.query(email)` | Finding (nested property access) |
| `field-aliased-object.ts` | `const data = req.body; const {id} = data; db.query(id)` | Finding (alias chain) |
| `field-wrong-cwe-sanitizer.ts` | `const id = escapeHtml(req.body.id); db.query(id)` | Finding (HTML sanitizer does NOT protect against SQLi) |

**Edge case fixtures:**

| Fixture | Pattern | Expected |
|---|---|---|
| `field-rest-spread-destructure.ts` | `const {safe, ...rest} = req.body; db.query(rest.id)` | Finding (rest contains all other fields) |
| `field-default-value.ts` | `const {id = "default"} = req.body; db.query(id)` | Finding (default only applies when undefined) |
| `field-dynamic-key-write.ts` | `const obj = {}; obj[req.body.key] = req.body.value; db.query(obj.x)` | Finding (conservative: dynamic key) |

### 7.2 Regression Suite

Run `apply_field_sensitive_pruning` against the full ground truth (`eval/ground_truth/gt-*.yaml`):

```python
def test_field_sensitivity_no_tp_loss():
    """Every GT entry with label=true_positive must still be detected after field pruning."""
    for gt_entry in load_ground_truth():
        if gt_entry.label == "true_positive":
            assert finding_survives_field_pruning(gt_entry), (
                f"TP lost by field pruning: {gt_entry.id}"
            )

def test_field_sensitivity_fp_reduction():
    """At least 10% of FP GT entries should be pruned by field analysis."""
    fp_entries = [e for e in load_ground_truth() if e.label == "false_positive"]
    pruned = [e for e in fp_entries if not finding_survives_field_pruning(e)]
    assert len(pruned) / len(fp_entries) >= 0.10
```

### 7.3 Unit Tests for `field_taint.py`

```
tests/test_detect/test_field_taint.py
├── test_classify_step_property_read
├── test_classify_step_destructuring
├── test_classify_step_spread
├── test_classify_step_computed_access
├── test_classify_step_sanitizer
├── test_propagate_simple_destructuring
├── test_propagate_nested_property
├── test_propagate_spread_with_override
├── test_propagate_computed_access_conservative
├── test_propagate_json_parse
├── test_propagate_template_literal
├── test_prune_sanitized_field
├── test_preserve_unsanitized_field
├── test_skip_single_field_source
├── test_skip_short_path
└── test_wrong_cwe_sanitizer_not_pruned
```

---

## 8. Ground Truth Updates

### 8.1 Schema Extension

Add optional `taint_field_path` to GT entry YAML schema:

```yaml
# existing fields...
taint_source: req.body
taint_sink: db.query()
taint_field_path: body.id     # NEW: specific field that reaches the sink
field_sensitive_label: true_positive  # NEW: expected result under field-sensitive analysis
```

`taint_field_path` is `null`/omitted for findings where the entire object reaches the sink or where field sensitivity is not applicable.

`field_sensitive_label` can differ from `label` — e.g. a finding labeled `true_positive` under object-level analysis but `false_positive` under field-sensitive analysis (because the specific field was sanitized).

### 8.2 New GT Entries (15 entries)

| ID | CWE | Source | Sink | Field Path | Object-Level Label | Field-Sensitive Label |
|---|---|---|---|---|---|---|
| gt-150 | CWE-89 | req.body | db.query() | body.id | TP | TP |
| gt-151 | CWE-79 | req.body | res.send() | body.name | TP | FP (sanitized by escapeHtml) |
| gt-152 | CWE-89 | req.body | db.query() | body.email | TP | FP (sanitized by parameterize) |
| gt-153 | CWE-78 | req.query | exec() | query.cmd | TP | TP |
| gt-154 | CWE-89 | req.body | db.query() | body.sql | TP | TP (escapeHtml does not help SQLi) |
| gt-155 | CWE-79 | req.body | res.send() | body.html | TP | TP (no sanitizer) |
| gt-156 | CWE-918 | req.body | fetch() | body.url | TP | FP (URL allowlist applied) |
| gt-157 | CWE-22 | req.body | readFile() | body.path | TP | TP |
| gt-158 | CWE-89 | req.body | db.query() | body.safe | TP | FP (spread override: `{...req.body, safe: "literal"}`) |
| gt-159 | CWE-79 | req.body | res.render() | body.title | TP | TP (no sanitizer on title) |
| gt-160 | CWE-89 | req.body | db.query() | body.* (computed) | TP | TP (conservative: computed access) |
| gt-161 | CWE-79 | req.body | res.send() | body.bio | TP | FP (reassigned to literal) |
| gt-162 | CWE-89 | req.body | db.query() | body.rest.id | TP | TP (rest spread captures id) |
| gt-163 | CWE-89 | req.body | db.query() | body.user.email | TP | TP (nested access, no sanitizer) |
| gt-164 | CWE-79 | req.body | res.send() | body.data | TP | FP (JSON.parse then parseInt on data) |

### 8.3 GT Fixture Files

Each new GT entry references a corresponding fixture file under `eval/synthetic/field-*.ts`. These fixtures are minimal (10-20 lines) Express/Fastify handlers demonstrating the specific pattern.

---

## 9. Risks and Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| Over-pruning: field analysis incorrectly marks a TP as safe | High | Conservative defaults for all ambiguous cases; regression test against full GT |
| Regex-based snippet classification misparses complex expressions | Medium | Fall back to `unknown` op type (no field narrowing) on parse failure |
| Joern AST queries add latency | Low | Queries only run for findings that pass `needs_field_analysis`; results cached per function |
| Template literal parsing misses interpolation in nested contexts | Medium | Parse only top-level `${}` groups; nested template literals treated conservatively |
| Cross-function field tracking loses precision | Medium | Use `FunctionSummary.field_path` from interprocedural analysis; fall back to whole-object if summary unavailable |
