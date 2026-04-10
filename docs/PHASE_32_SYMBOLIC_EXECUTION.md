# Phase 32: Symbolic Execution Engine

**Estimated effort: 80-100 ideal hours**
**Blocked by: Phase 22 (advanced taint analysis), Phase 2 (exploit verification)**
**Blocks: none (deepens existing verification pipeline)**

## 1. Overview

### Current State

The existing verification pipeline (`src/piranesi/verify/solver.py`) translates `PathCondition` objects extracted from Joern branch points into Z3 assertions and solves for concrete exploit payloads. The `ExploitTemplate` (built by `extract_exploit_template` in `verify/constraints.py`) captures:

- Payload slots (which HTTP fields carry tainted data)
- Path conditions (branch predicates along the taint path)
- Constraint sets (disjunctive normal form of path conditions, capped at 10 disjuncts)

The solver iterates known-safe payload candidates per vulnerability class, asserts them as forced values, and checks SAT. If SAT, it synthesizes a `SynthesizedPayload` with concrete HTTP request parameters.

### Limitation

The current solver operates on **extracted constraints only** -- flat predicates pulled from Joern branch points along a single taint path. It does not:

- Execute the function body symbolically (misses constraints implied by assignments, loops, string transforms)
- Handle multi-function call chains (constraints are per-node, not inter-procedural)
- Model JS/TS runtime semantics (type coercion, string operations, object property access)
- Detect infeasible paths (if intermediate code transforms the tainted value in a way that makes exploitation impossible, the solver does not know)
- Handle loops (repeated operations on tainted data are invisible)

### Goal

Build a **concolic execution engine** that symbolically executes the AST along taint paths, producing richer constraint sets that capture the full transformation of tainted data from source to sink. This is about **depth of analysis** -- extracting more precise constraints from the same taint paths, not discovering new taint paths.

The engine slots into the existing pipeline as a second-tier verifier: `solver.py` (fast, shallow) handles simple cases; `concolic.py` (slow, thorough) handles findings that `solver.py` returns `UNVERIFIABLE` for, or where higher confidence is needed.

---

## 2. Concolic Execution Engine

### Architecture

**New module:** `src/piranesi/verify/concolic.py`

**Supporting modules:**
- `src/piranesi/verify/sym_state.py` -- symbolic execution state (store, constraints, call stack)
- `src/piranesi/verify/js_semantics.py` -- Z3 encodings of JS/TS operations
- `src/piranesi/verify/sym_memory.py` -- heap and stack memory model

### Input

```python
@dataclass(slots=True)
class ConcolicInput:
    finding: CandidateFinding      # from piranesi.models.finding
    taint_path: list[TaintStep]    # ordered source -> ... -> sink
    function_asts: dict[str, Any]  # function_id -> tree-sitter AST node
    call_graph: dict[str, list[str]]  # caller -> callees
    entry_point: EntryPoint        # route handler metadata
```

The `function_asts` are obtained by re-parsing the relevant source files with tree-sitter and extracting the function nodes that appear in the taint path (identified by `TaintStep.through_function` and source locations).

### Process

```
1. Initialize symbolic state at entry point (route handler)
   - Mark tainted parameters as symbolic (Z3 variables)
   - Mark known parameters as concrete values
2. Walk the AST of each function in taint-path order
   - For each statement: update symbolic store
   - For each branch (if/else, ternary, switch):
     Fork state, add path constraint to each fork
   - For each function call on the taint path:
     Push call frame, enter callee AST
   - For each loop: bounded unrolling (Section 4)
3. At sink node: collect full path constraint conjunction
4. Solve with Z3
   - SAT: extract concrete payload from model
   - UNSAT: taint path is infeasible, prune finding
   - TIMEOUT/UNKNOWN: fall back to solver.py result
```

### Core Engine Pseudocode

```python
import z3
from dataclasses import dataclass, field

@dataclass(slots=True)
class SymbolicState:
    store: dict[str, z3.ExprRef] = field(default_factory=dict)        # var -> Z3 expr
    constraints: list[z3.BoolRef] = field(default_factory=list)       # path constraints
    call_stack: list[CallFrame] = field(default_factory=list)
    heap: SymbolicHeap = field(default_factory=SymbolicHeap)
    path_depth: int = 0
    feasible: bool = True

@dataclass(slots=True)
class CallFrame:
    function_id: str
    locals: dict[str, z3.ExprRef]
    return_var: str | None = None  # where to store return value

@dataclass(slots=True)
class ConcolicResult:
    status: Literal["SAT", "UNSAT", "TIMEOUT", "UNKNOWN"]
    payload: SynthesizedPayload | None = None
    model_values: dict[str, str] | None = None
    execution_trace: list[TraceStep] = field(default_factory=list)
    path_constraints: list[str] = field(default_factory=list)  # human-readable
    infeasible_reason: str | None = None

def concolic_verify(
    inp: ConcolicInput,
    *,
    max_paths: int = 100,
    timeout_ms: int = 120_000,
    loop_bound: int = 3,
) -> ConcolicResult:
    initial_state = _init_state(inp)
    worklist: list[SymbolicState] = [initial_state]
    best_result: ConcolicResult | None = None
    paths_explored = 0
    deadline = time.monotonic() + timeout_ms / 1000

    while worklist and paths_explored < max_paths:
        if time.monotonic() > deadline:
            return best_result or ConcolicResult(status="TIMEOUT")
        state = _pick_state(worklist, inp.taint_path)  # guided by taint path
        paths_explored += 1
        result = _execute_path(state, inp, loop_bound=loop_bound, deadline=deadline)
        if result.status == "SAT":
            return result  # found concrete exploit
        if result.status == "UNSAT":
            continue  # path infeasible, try next
        if best_result is None:
            best_result = result

    if paths_explored > 0 and best_result is None:
        return ConcolicResult(status="UNSAT", infeasible_reason="all explored paths infeasible")
    return best_result or ConcolicResult(status="UNKNOWN")


def _init_state(inp: ConcolicInput) -> SymbolicState:
    state = SymbolicState()
    # tainted params become symbolic
    for param in inp.entry_point.parameters:
        if _is_tainted(param, inp.taint_path):
            state.store[param] = z3.String(f"input_{param}")
        else:
            state.store[param] = z3.StringVal("")  # concrete default
    state.call_stack.append(CallFrame(
        function_id=inp.entry_point.function_id,
        locals=dict(state.store),
    ))
    return state


def _pick_state(
    worklist: list[SymbolicState],
    taint_path: list[TaintStep],
) -> SymbolicState:
    # prioritize states closest to reaching the sink
    # uses taint_path step index as heuristic
    return worklist.pop()  # LIFO (DFS), with taint-guided sorting


def _execute_path(
    state: SymbolicState,
    inp: ConcolicInput,
    *,
    loop_bound: int,
    deadline: float,
) -> ConcolicResult:
    ast = inp.function_asts[state.call_stack[-1].function_id]
    for node in _walk_statements(ast):
        if time.monotonic() > deadline:
            return ConcolicResult(status="TIMEOUT")
        match node.type:
            case "variable_declaration" | "assignment_expression":
                _exec_assignment(state, node)
            case "if_statement":
                branches = _fork_on_branch(state, node)
                # return forked states to worklist (handled by caller)
                ...
            case "call_expression":
                _exec_call(state, node, inp)
            case "return_statement":
                _exec_return(state, node)
            case "for_statement" | "while_statement":
                _exec_loop(state, node, bound=loop_bound)
            case _:
                pass  # skip non-modeled statements
    return _solve_at_sink(state, inp)


def _solve_at_sink(state: SymbolicState, inp: ConcolicInput) -> ConcolicResult:
    solver = z3.Solver()
    solver.set("timeout", 30_000)
    for constraint in state.constraints:
        solver.add(constraint)
    # add vulnerability-class constraints (payload must trigger the vuln)
    payload_var = state.store.get(_sink_input_var(inp))
    if payload_var is not None:
        for vc in vulnerability_constraints(inp.finding.vuln_class, payload_var):
            solver.add(vc)

    outcome = solver.check()
    if outcome == z3.sat:
        model = solver.model()
        values = _extract_values(model, state)
        payload = _build_payload(values, inp)
        return ConcolicResult(status="SAT", payload=payload, model_values=values)
    if outcome == z3.unsat:
        return ConcolicResult(status="UNSAT", infeasible_reason="path constraints unsatisfiable")
    return ConcolicResult(status="UNKNOWN")
```

### Branch Forking

When the engine hits an `if` statement:

```python
def _fork_on_branch(
    state: SymbolicState,
    if_node: ASTNode,
) -> tuple[SymbolicState, SymbolicState]:
    condition_expr = _translate_expression(state, if_node.condition)
    # true branch
    true_state = _clone_state(state)
    true_state.constraints.append(condition_expr)
    true_state.path_depth += 1
    # false branch
    false_state = _clone_state(state)
    false_state.constraints.append(z3.Not(condition_expr))
    false_state.path_depth += 1
    # quick feasibility check (optional, avoids dead branches early)
    for s in (true_state, false_state):
        quick = z3.Solver()
        quick.set("timeout", 1000)
        for c in s.constraints:
            quick.add(c)
        if quick.check() == z3.unsat:
            s.feasible = False
    return true_state, false_state
```

---

## 3. JavaScript/TypeScript Symbolic Semantics

**Module:** `src/piranesi/verify/js_semantics.py`

This module translates JS/TS AST expressions into Z3 expressions. Every operation on a symbolic value produces a new Z3 expression rather than a concrete value.

### 3.1 String Operations

| JS/TS Expression | Z3 Encoding |
|---|---|
| `a + b` (string concat) | `z3.Concat(a, b)` |
| `s.slice(i, j)` | `z3.SubString(s, i, j - i)` |
| `s.substring(i, j)` | `z3.SubString(s, i, j - i)` |
| `s.indexOf(sub)` | `z3.IndexOf(s, sub, z3.IntVal(0))` |
| `s.includes(sub)` | `z3.Contains(s, sub)` |
| `s.startsWith(pre)` | `z3.PrefixOf(pre, s)` |
| `s.endsWith(suf)` | `z3.SuffixOf(suf, s)` |
| `s.replace(old, new)` | `z3.Replace(s, old, new)` |
| `s.length` | `z3.Length(s)` |
| `s.trim()` | approximate: assert no leading/trailing whitespace |
| `s.toLowerCase()` | approximate: fresh symbolic + constraint (see below) |
| `s.toUpperCase()` | approximate: fresh symbolic + constraint (see below) |
| `s.split(delim)` | model as Z3 sequence of strings (bounded) |

**Case conversion approximation:**

```python
def _model_to_lower(state: SymbolicState, s: z3.ExprRef) -> z3.ExprRef:
    # exact toLowerCase is undecidable in Z3 string theory
    # approximation: create fresh variable, constrain length equality,
    # and for known-constant prefixes/suffixes, assert concrete lowercase
    result = z3.String(f"_lower_{state.path_depth}")
    state.constraints.append(z3.Length(result) == z3.Length(s))
    # if s is partially concrete (e.g., Concat("ABC", sym)), lower concrete parts
    # for fully symbolic: result is unconstrained beyond length (conservative)
    return result
```

**Template literal / tagged template handling:**

```python
def _translate_template_literal(state: SymbolicState, node: ASTNode) -> z3.ExprRef:
    parts: list[z3.ExprRef] = []
    for child in node.children:
        if child.type == "string_fragment":
            parts.append(z3.StringVal(child.text))
        elif child.type == "template_substitution":
            parts.append(_translate_expression(state, child.expression))
    if not parts:
        return z3.StringVal("")
    result = parts[0]
    for part in parts[1:]:
        result = z3.Concat(result, _to_string_z3(part))
    return result
```

### 3.2 Numeric Operations

Standard Z3 arithmetic maps directly:

| JS/TS | Z3 |
|---|---|
| `a + b` (numeric) | `a + b` (Z3 `ArithRef`) |
| `a - b` | `a - b` |
| `a * b` | `a * b` |
| `a / b` | `z3.ToReal(a) / z3.ToReal(b)` (JS division is float) |
| `a % b` | `a % b` (Z3 `Mod`) |
| `a ** b` | not directly supported; concretize or skip |
| `Math.floor(x)` | `z3.ToInt(x)` |
| `Math.ceil(x)` | `z3.ToInt(x) + z3.If(x != z3.ToInt(x), 1, 0)` |
| `Math.max(a, b)` | `z3.If(a >= b, a, b)` |
| `Math.min(a, b)` | `z3.If(a <= b, a, b)` |
| `parseInt(s)` | `z3.StrToInt(s)` |
| `Number(s)` | `z3.StrToInt(s)` (integer approximation) |
| `String(n)` | `z3.IntToStr(n)` |

### 3.3 Boolean Operations

| JS/TS | Z3 |
|---|---|
| `a && b` | `z3.And(a, b)` |
| `a \|\| b` | `z3.Or(a, b)` |
| `!a` | `z3.Not(a)` |
| `a === b` | `a == b` (sort-matched) |
| `a !== b` | `a != b` |
| `a == b` (loose) | see type coercion below |
| `a ? b : c` | `z3.If(a, b, c)` |

### 3.4 Type Coercion (Loose Equality)

JS loose equality (`==`) is notoriously complex. We model a conservative approximation:

```python
def _loose_eq(a: z3.ExprRef, b: z3.ExprRef) -> z3.BoolRef:
    a_sort = a.sort()
    b_sort = b.sort()
    if a_sort == b_sort:
        return a == b  # same type -> strict
    # string == int: coerce string to int
    if a_sort == z3.StringSort() and b_sort == z3.IntSort():
        return z3.StrToInt(a) == b
    if a_sort == z3.IntSort() and b_sort == z3.StringSort():
        return a == z3.StrToInt(b)
    # bool == anything: coerce bool to int (true=1, false=0)
    if a_sort == z3.BoolSort():
        return _loose_eq(z3.If(a, z3.IntVal(1), z3.IntVal(0)), b)
    if b_sort == z3.BoolSort():
        return _loose_eq(a, z3.If(b, z3.IntVal(1), z3.IntVal(0)))
    # null/undefined: modeled as distinct constant, only == each other
    return z3.BoolVal(False)  # conservative: unknown coercion -> not equal
```

### 3.5 Object Operations

Objects are modeled as Z3 arrays (`Array(String, Value)`) where the key is the property name:

```python
def _model_object(state: SymbolicState, name: str) -> z3.ExprRef:
    # Z3 array: String -> String (property name -> property value as string)
    return z3.Array(name, z3.StringSort(), z3.StringSort())

def _property_read(obj: z3.ExprRef, prop: str) -> z3.ExprRef:
    return z3.Select(obj, z3.StringVal(prop))

def _property_write(obj: z3.ExprRef, prop: str, val: z3.ExprRef) -> z3.ExprRef:
    return z3.Store(obj, z3.StringVal(prop), val)
```

**Destructuring:**

```python
# const { a, b } = obj;
def _exec_destructuring(state: SymbolicState, pattern: ASTNode, obj_expr: z3.ExprRef):
    for prop in pattern.properties:
        key = prop.key.text
        alias = prop.value.text if prop.value else key
        state.store[alias] = z3.Select(obj_expr, z3.StringVal(key))
```

**Spread operator:**

```python
# const merged = { ...base, ...override };
def _exec_spread(state: SymbolicState, spreads: list[z3.ExprRef]) -> z3.ExprRef:
    # model as: later spreads overwrite earlier properties
    # use uninterpreted function for property resolution
    result_name = f"_spread_{state.path_depth}"
    result = z3.Array(result_name, z3.StringSort(), z3.StringSort())
    # for each spread source, store known properties
    # unknown properties remain symbolic (array default)
    return result
```

### 3.6 Array Operations

Arrays are modeled as Z3 sequences (`SeqSort(StringSort())`):

```python
def _model_array(name: str) -> z3.ExprRef:
    return z3.Const(name, z3.SeqSort(z3.StringSort()))

def _array_index(arr: z3.ExprRef, idx: z3.ExprRef) -> z3.ExprRef:
    return z3.Unit(arr)[idx]  # approximate: Nth element

def _array_length(arr: z3.ExprRef) -> z3.ExprRef:
    return z3.Length(arr)

def _array_push(arr: z3.ExprRef, val: z3.ExprRef) -> z3.ExprRef:
    return z3.Concat(arr, z3.Unit(val))
```

### 3.7 Unsupported Constructs

| Construct | Strategy |
|---|---|
| Closures | Conservative: treat captured variables as unconstrained symbolic values |
| Generators (`function*`) | Skip: treat yield points as returning unconstrained symbolic |
| `async`/`await` | Flatten: `await expr` treated as the resolved value of `expr` |
| `Proxy` | Skip: not modeled |
| `eval()` | Skip: flag as potentially dangerous but do not model |
| `with` statement | Skip: deprecated, rare in modern TS |
| Regular expressions | Approximate: Z3 regex support for simple patterns, skip complex |

---

## 4. Loop Handling

### Strategy: Bounded Unrolling

Exact loop analysis is undecidable. We use bounded unrolling with configurable depth (default `k=3`).

### 4.1 Loop Detection

Back edges in the AST control flow:

```python
def _is_loop_node(node: ASTNode) -> bool:
    return node.type in ("for_statement", "while_statement", "do_statement",
                         "for_in_statement", "for_of_statement")
```

### 4.2 For Loops

```javascript
for (let i = 0; i < n; i++) { body }
```

Unroll `k` times:

```python
def _exec_for_loop(
    state: SymbolicState,
    node: ASTNode,
    *,
    bound: int = 3,
) -> None:
    _exec_statement(state, node.initializer)  # let i = 0
    for iteration in range(bound):
        cond = _translate_expression(state, node.condition)
        # add loop entry constraint
        state.constraints.append(cond)
        _exec_statement(state, node.body)
        _exec_statement(state, node.update)  # i++
    # after k iterations: havoc modified variables
    _havoc_loop_targets(state, node)
```

**Havoc:** after `k` unrollings, variables modified inside the loop body are replaced with fresh unconstrained symbolic values. This is sound (overapproximates) but imprecise.

```python
def _havoc_loop_targets(state: SymbolicState, loop_node: ASTNode) -> None:
    modified_vars = _collect_assignments(loop_node.body)
    for var in modified_vars:
        fresh = z3.String(f"_havoc_{var}_{state.path_depth}")
        state.store[var] = fresh
    # add post-loop condition (negation of loop guard)
    post_cond = z3.Not(_translate_expression(state, loop_node.condition))
    state.constraints.append(post_cond)
```

### 4.3 While Loops

Same as for-loop without initializer/update:

```python
def _exec_while_loop(state: SymbolicState, node: ASTNode, *, bound: int = 3) -> None:
    for iteration in range(bound):
        cond = _translate_expression(state, node.condition)
        state.constraints.append(cond)
        _exec_statement(state, node.body)
    _havoc_loop_targets(state, node)
```

### 4.4 Array Higher-Order Methods

```javascript
arr.forEach(item => sink(item));
arr.map(item => transform(item));
arr.filter(item => check(item));
```

Unroll for first `k` elements:

```python
def _exec_array_method(
    state: SymbolicState,
    node: ASTNode,
    method: str,
    *,
    bound: int = 3,
) -> z3.ExprRef | None:
    arr_expr = _translate_expression(state, node.object)
    callback = node.arguments[0]  # the lambda/arrow function
    param_name = callback.parameters[0].text

    results: list[z3.ExprRef] = []
    for i in range(bound):
        elem = z3.String(f"_elem_{param_name}_{i}")
        state.store[param_name] = elem
        # constrain: elem is the i-th element of arr (if arr is a Z3 sequence)
        _exec_statement(state, callback.body)
        if method == "map":
            results.append(_translate_expression(state, callback.body.return_value))
        elif method == "filter":
            filter_cond = _translate_expression(state, callback.body.return_value)
            # element included if filter returns truthy -- branch
            state.constraints.append(filter_cond)

    if method == "map":
        return _build_sequence(results)
    return None
```

### 4.5 Symbolic Loop Bounds

If the loop bound is a symbolic value (e.g., `for(let i=0; i<userInput.length; i++)`):

1. Unroll `k=3` times unconditionally
2. After unrolling, add constraint: `loop_counter >= k` (remaining iterations happened)
3. Havoc all loop-modified state
4. The solver determines feasibility of the bound being > `k`

---

## 5. Path Merging and Explosion Mitigation

### Problem

A function with `n` independent `if/else` branches produces `2^n` paths. 10 branches = 1024 paths. Real functions can have 20+ branches. Without mitigation, the engine stalls.

### Strategy 1: Taint-Guided Path Prioritization

The Joern taint path tells us which branches the tainted data actually flows through. Use this as a priority signal:

```python
def _pick_state(
    worklist: list[SymbolicState],
    taint_path: list[TaintStep],
) -> SymbolicState:
    taint_locations = {(s.location.file, s.location.line) for s in taint_path}
    # score each state by how many taint-path locations it has covered
    def score(state: SymbolicState) -> int:
        return sum(1 for loc in state.covered_locations if loc in taint_locations)
    worklist.sort(key=score)
    return worklist.pop()  # highest score first
```

Branches that do not affect tainted data are not forked -- the engine picks the branch that keeps tainted data flowing toward the sink.

### Strategy 2: State Merging at Join Points

At if/else join points, instead of maintaining two separate states, merge into one state using Z3 `If-Then-Else`:

```python
def _merge_states(
    true_state: SymbolicState,
    false_state: SymbolicState,
    branch_cond: z3.BoolRef,
) -> SymbolicState:
    merged = SymbolicState()
    all_vars = set(true_state.store) | set(false_state.store)
    for var in all_vars:
        true_val = true_state.store.get(var)
        false_val = false_state.store.get(var)
        if true_val is None or false_val is None:
            # variable only exists in one branch -- keep as-is
            merged.store[var] = true_val or false_val
        elif true_val.eq(false_val):
            merged.store[var] = true_val  # same in both branches
        else:
            merged.store[var] = z3.If(branch_cond, true_val, false_val)
    # constraints: take intersection (constraints from before the branch)
    merged.constraints = list(true_state.constraints[:-1])  # pre-branch constraints
    return merged
```

**When to merge vs. fork:**
- If neither branch modifies tainted variables: always merge (branch is irrelevant)
- If both branches modify tainted variables differently: merge with ITE
- If one branch is infeasible (quick UNSAT check): prune it, continue with other

### Strategy 3: Per-Finding Timeout

```python
TIMEOUT_PER_FINDING_MS = 120_000  # 2 minutes default

def concolic_verify(inp: ConcolicInput, *, timeout_ms: int = TIMEOUT_PER_FINDING_MS) -> ConcolicResult:
    deadline = time.monotonic() + timeout_ms / 1000
    # ... check deadline at every branch point, loop iteration, function call
```

If exceeded, return the best result found so far (partial trace) or `TIMEOUT`.

### Strategy 4: Function Summaries

For non-tainted helper functions called multiple times, compute a symbolic summary once and reuse:

```python
@dataclass(slots=True)
class FunctionSummary:
    function_id: str
    param_names: list[str]
    return_expr: z3.ExprRef  # symbolic return value in terms of params
    side_effects: list[tuple[str, z3.ExprRef]]  # (heap_path, new_value)
    constraints: list[z3.BoolRef]  # path constraints imposed by the function

_summary_cache: dict[str, FunctionSummary] = {}

def _get_or_compute_summary(
    function_id: str,
    function_ast: Any,
    state: SymbolicState,
) -> FunctionSummary:
    if function_id in _summary_cache:
        return _summary_cache[function_id]
    summary = _compute_summary(function_id, function_ast)
    _summary_cache[function_id] = summary
    return summary

def _apply_summary(
    state: SymbolicState,
    summary: FunctionSummary,
    actual_args: list[z3.ExprRef],
) -> z3.ExprRef:
    # substitute formal params with actual args in return expr
    substitutions = list(zip(
        [z3.String(p) for p in summary.param_names],
        actual_args,
    ))
    result = z3.substitute(summary.return_expr, *substitutions)
    for constraint in summary.constraints:
        state.constraints.append(z3.substitute(constraint, *substitutions))
    return result
```

### Strategy 5: Max Paths Cap

```python
MAX_PATHS_PER_FINDING = 100  # configurable via [verify] max_symbolic_paths
```

Hard limit. After exhausting the budget, return the best result (SAT if any path found one, UNSAT if all explored paths were infeasible, UNKNOWN otherwise).

---

## 6. Memory Model

### Module: `src/piranesi/verify/sym_memory.py`

### 6.1 Heap

The heap maps addresses (string identifiers) to Z3 arrays representing objects:

```python
@dataclass(slots=True)
class SymbolicHeap:
    objects: dict[str, z3.ArrayRef] = field(default_factory=dict)
    alias_constraints: list[z3.BoolRef] = field(default_factory=list)
    _next_addr: int = 0

    def alloc(self, name: str | None = None) -> str:
        addr = name or f"_obj_{self._next_addr}"
        self._next_addr += 1
        # Z3 array: property name (String) -> property value (String)
        self.objects[addr] = z3.Array(addr, z3.StringSort(), z3.StringSort())
        return addr

    def read_prop(self, addr: str, prop: str) -> z3.ExprRef:
        obj = self.objects[addr]
        return z3.Select(obj, z3.StringVal(prop))

    def write_prop(self, addr: str, prop: str, val: z3.ExprRef) -> None:
        obj = self.objects[addr]
        self.objects[addr] = z3.Store(obj, z3.StringVal(prop), val)

    def read_dynamic(self, addr: str, prop_expr: z3.ExprRef) -> z3.ExprRef:
        """Dynamic property access: obj[expr]"""
        obj = self.objects[addr]
        return z3.Select(obj, prop_expr)

    def write_dynamic(self, addr: str, prop_expr: z3.ExprRef, val: z3.ExprRef) -> None:
        obj = self.objects[addr]
        self.objects[addr] = z3.Store(obj, prop_expr, val)
```

### 6.2 Stack

Ordered list of frames. Each frame is a local variable scope:

```python
@dataclass(slots=True)
class SymbolicStack:
    frames: list[CallFrame] = field(default_factory=list)

    def push(self, function_id: str, params: dict[str, z3.ExprRef]) -> None:
        self.frames.append(CallFrame(function_id=function_id, locals=dict(params)))

    def pop(self) -> CallFrame:
        return self.frames.pop()

    def current(self) -> CallFrame:
        return self.frames[-1]

    def lookup(self, name: str) -> z3.ExprRef | None:
        # walk stack frames from top to bottom (lexical scoping approximation)
        for frame in reversed(self.frames):
            if name in frame.locals:
                return frame.locals[name]
        return None

    def assign(self, name: str, val: z3.ExprRef) -> None:
        self.current().locals[name] = val
```

### 6.3 Property Access Translation

```python
def _translate_member_expression(
    state: SymbolicState,
    node: ASTNode,
) -> z3.ExprRef:
    obj_expr = _translate_expression(state, node.object)
    prop = node.property.text

    # if obj_expr is a heap address reference
    if isinstance(obj_expr, str) and obj_expr in state.heap.objects:
        return state.heap.read_prop(obj_expr, prop)

    # if obj_expr is a Z3 array (inline object)
    if isinstance(obj_expr, z3.ArrayRef):
        return z3.Select(obj_expr, z3.StringVal(prop))

    # if obj_expr is a Z3 string and prop is a known method
    if obj_expr.sort() == z3.StringSort():
        return _translate_string_method(state, obj_expr, prop, node)

    # fallback: uninterpreted function
    f = z3.Function(f"prop_{prop}", obj_expr.sort(), z3.StringSort())
    return f(obj_expr)
```

### 6.4 Aliasing

Track when two variables may refer to the same heap object:

```python
def _exec_assignment_alias(
    state: SymbolicState,
    target: str,
    source: str,
) -> None:
    # both names now point to the same heap address
    if source in state.heap.objects:
        state.heap.objects[target] = state.heap.objects[source]
        # record alias constraint for precision
        state.heap.alias_constraints.append(
            z3.Bool(f"alias_{target}_{source}")
        )
```

For cases where aliasing is uncertain (conditional assignment), the engine creates an ITE over the heap arrays.

---

## 7. Integration with Existing Solver

### Pipeline Architecture

```
CandidateFinding
       |
       v
  extract_exploit_template()   [verify/constraints.py -- existing]
       |
       v
  solve_exploit_template()     [verify/solver.py -- existing, fast]
       |
       +---> SAT: done, have payload
       |
       +---> UNVERIFIABLE:
                |
                v
          concolic_verify()    [verify/concolic.py -- NEW, thorough]
                |
                +---> SAT: richer payload + execution trace
                +---> UNSAT: prune finding (infeasible path)
                +---> TIMEOUT/UNKNOWN: keep original UNVERIFIABLE status
```

### Integration Point in Pipeline

In `pipeline.py`, the verification step currently calls `solve_exploit_template`. Add a fallback:

```python
def _verify_finding(finding: CandidateFinding, config: PiranesiConfig) -> VerifyResult:
    template = extract_exploit_template(finding)
    result = solve_exploit_template(template)
    if result.status == "SAT":
        return VerifyResult(status="SAT", payload=result.solutions[0].payload)
    # fallback to concolic execution for deeper analysis
    if config.verify.symbolic_enabled:
        concolic_input = _build_concolic_input(finding)
        concolic_result = concolic_verify(
            concolic_input,
            timeout_ms=config.verify.symbolic_timeout_per_finding,
            max_paths=config.verify.max_symbolic_paths,
            loop_bound=config.verify.symbolic_loop_bound,
        )
        if concolic_result.status == "SAT":
            return VerifyResult(
                status="SAT",
                payload=concolic_result.payload,
                execution_trace=concolic_result.execution_trace,
            )
        if concolic_result.status == "UNSAT":
            return VerifyResult(status="INFEASIBLE", reason=concolic_result.infeasible_reason)
    return VerifyResult(status="UNVERIFIABLE", reason=result.reason)
```

### Result Enrichment

`ConcolicResult` provides richer evidence than the basic solver:

```python
@dataclass(slots=True)
class TraceStep:
    location: SourceLocation       # file, line, column
    statement_text: str            # human-readable source
    symbolic_state_snapshot: dict[str, str]  # var -> Z3 expr as string
    constraint_added: str | None   # if a branch was taken

@dataclass(slots=True)
class ConcolicResult:
    status: Literal["SAT", "UNSAT", "TIMEOUT", "UNKNOWN"]
    payload: SynthesizedPayload | None = None
    model_values: dict[str, str] | None = None
    execution_trace: list[TraceStep] = field(default_factory=list)
    path_constraints: list[str] = field(default_factory=list)
    infeasible_reason: str | None = None
    paths_explored: int = 0
    z3_solve_time_ms: int = 0
```

The execution trace allows downstream reporting to show **why** a payload works: "at line 15, the `if (role === 'admin')` branch was taken because `role` was constrained to `'admin'` by the payload."

### Configuration

New config section in `piranesi.toml`:

```toml
[verify]
symbolic_enabled = true
symbolic_timeout_per_finding = 120000  # ms, per finding
symbolic_timeout_total = 600000        # ms, total budget for all findings
max_symbolic_paths = 100               # max forked paths per finding
symbolic_loop_bound = 3                # loop unrolling depth
symbolic_merge_enabled = true          # use state merging at join points
symbolic_summaries_enabled = true      # cache function summaries
```

---

## 8. Testing Strategy

### 8.1 Unit Tests: JS Semantics (`tests/test_verify/test_js_semantics.py`)

15+ test cases for Z3 encoding correctness:

| Test | Input | Expected |
|---|---|---|
| `test_string_concat` | `a + b` where `a="x"` | `z3.Concat(StringVal("x"), b)` |
| `test_slice` | `s.slice(1, 3)` | `z3.SubString(s, 1, 2)` |
| `test_includes` | `s.includes("foo")` | `z3.Contains(s, StringVal("foo"))` |
| `test_indexof` | `s.indexOf("x") >= 0` | `z3.IndexOf(s, StringVal("x"), 0) >= 0` |
| `test_replace` | `s.replace("'", "")` | `z3.Replace(s, StringVal("'"), StringVal(""))` |
| `test_parseint` | `parseInt(s) > 0` | `z3.StrToInt(s) > 0` |
| `test_loose_eq_string_int` | `x == 0` where x is string | `z3.StrToInt(x) == 0` |
| `test_ternary` | `x ? a : b` | `z3.If(x, a, b)` |
| `test_template_literal` | `` `hello ${name}` `` | `z3.Concat(StringVal("hello "), name)` |
| `test_destructuring` | `const {a} = obj` | `z3.Select(obj, StringVal("a"))` |

### 8.2 Integration Tests: Complex Path Conditions (`tests/test_verify/test_concolic.py`)

**Fixture 1: Nested if/else with sanitizer bypass**
```typescript
app.get("/search", (req, res) => {
    let q = req.query.q;
    if (typeof q !== "string") return res.status(400).end();
    if (q.length > 100) q = q.slice(0, 100);
    if (q.includes("'")) q = q.replace("'", "");  // broken sanitizer
    db.query(`SELECT * FROM items WHERE name = '${q}'`);
});
```
Expected: SAT. Payload `q = "x' OR 1=1--"` contains `'` but `replace("'", "")` only removes the first occurrence. The engine should find `"x'' OR 1=1--"`.

**Fixture 2: Infeasible path**
```typescript
app.post("/admin", (req, res) => {
    const input = req.body.cmd;
    if (!input || typeof input !== "string") return;
    const sanitized = input.replace(/[;&|`$]/g, "");
    exec(sanitized);  // regex removes all dangerous chars
});
```
Expected: UNSAT. After regex replace, no command injection metacharacters survive. (Note: Z3 regex support needed, or conservative approximation flags as UNKNOWN.)

**Fixture 3: Loop-dependent transformation**
```typescript
app.post("/encode", (req, res) => {
    let data = req.body.data;
    for (let i = 0; i < 3; i++) {
        data = data.replace("<", "&lt;");
    }
    res.send(data);  // XSS sink
});
```
Expected: UNSAT with `k>=3`. Three replacements remove first 3 `<` chars. With `k=2`, may report SAT (false positive from underapproximation). Test verifies `k=3` produces UNSAT.

**Fixture 4: Cross-function taint**
```typescript
function validate(input: string): string {
    if (input.length > 50) throw new Error("too long");
    return input;
}
app.get("/page", (req, res) => {
    const name = validate(req.query.name);
    res.send(`<h1>Hello ${name}</h1>`);
});
```
Expected: SAT. `validate` only checks length, does not sanitize XSS. Payload: `<script>alert(1)</script>` (29 chars, under 50 limit).

**Fixture 5: Object property taint propagation**
```typescript
app.post("/user", (req, res) => {
    const user = { name: req.body.name, role: "viewer" };
    const merged = { ...user, ...req.body };  // prototype pollution-adjacent
    db.query(`SELECT * FROM users WHERE role = '${merged.role}'`);
});
```
Expected: SAT. The spread allows `req.body.role` to override the hardcoded `"viewer"`. Payload: `role = "' OR 1=1--"`.

### 8.3 Benchmark Tests (`tests/test_verify/test_concolic_bench.py`)

Compare basic solver vs. concolic engine:

```python
def test_concolic_improves_over_basic_solver():
    findings = load_fixtures("complex_path_conditions/")
    basic_results = [solve_exploit_template(extract_exploit_template(f)) for f in findings]
    concolic_results = [concolic_verify(build_concolic_input(f)) for f in findings]
    basic_sat = sum(1 for r in basic_results if r.status == "SAT")
    concolic_sat = sum(1 for r in concolic_results if r.status == "SAT")
    assert concolic_sat >= basic_sat, "concolic should find at least as many exploits"
    # expect at least 30% improvement on complex cases
    improvement = (concolic_sat - basic_sat) / max(len(findings), 1)
    assert improvement >= 0.3 or concolic_sat == len(findings)
```

### 8.4 Infeasible Path Pruning Tests

```python
def test_infeasible_paths_pruned():
    """Concolic engine marks truly-sanitized paths as UNSAT."""
    sanitized_fixtures = load_fixtures("properly_sanitized/")
    for finding in sanitized_fixtures:
        result = concolic_verify(build_concolic_input(finding))
        assert result.status == "UNSAT", f"expected UNSAT for {finding.id}"
```

### 8.5 Timeout / Graceful Degradation Tests

```python
def test_timeout_returns_partial_result():
    """Engine returns TIMEOUT with partial info, not crash."""
    complex_finding = load_fixture("deeply_nested_branches.ts")
    result = concolic_verify(
        build_concolic_input(complex_finding),
        timeout_ms=1000,  # very tight
    )
    assert result.status in ("TIMEOUT", "UNKNOWN")
    assert result.paths_explored > 0

def test_max_paths_respected():
    result = concolic_verify(
        build_concolic_input(branchy_finding),
        max_paths=5,
    )
    assert result.paths_explored <= 5
```

---

## 9. Performance Budget

### Per-Finding Targets

| Complexity | Branches | Loops | Functions | Target Time |
|---|---|---|---|---|
| Simple | 1-2 | 0 | 1 | < 5s |
| Medium | 3-5 | 0-1 | 1-2 | < 30s |
| Complex | 6+ | 1+ | 2+ | < 120s |
| Pathological | 10+ | nested | 3+ | timeout at 120s |

### Total Scan Budget

```toml
[verify]
symbolic_timeout_total = 600  # seconds, total across all findings
```

If the total budget is exhausted, remaining findings skip concolic verification and keep their `solver.py` result.

### Resource Constraints

- **Memory:** Z3 solver instances are short-lived (one per path solve). Peak memory is bounded by `max_symbolic_paths * state_size`. State size is dominated by the Z3 expression DAG, typically < 10MB per state for functions under 200 LOC.
- **CPU:** Z3 solving is single-threaded per instance. The engine itself is single-threaded per finding. Parallelism across findings is handled by the pipeline's existing `ThreadPoolExecutor`.
- **Z3 incremental mode:** Use `solver.push()`/`solver.pop()` for branch exploration to avoid rebuilding the solver from scratch:

```python
def _explore_branch(solver: z3.Solver, branch_constraint: z3.BoolRef) -> z3.CheckSatResult:
    solver.push()
    solver.add(branch_constraint)
    result = solver.check()
    solver.pop()
    return result
```

### Profiling Integration

Add timing instrumentation to `ConcolicResult`:

```python
@dataclass(slots=True)
class ConcolicTimings:
    ast_parse_ms: int = 0
    symbolic_exec_ms: int = 0
    z3_solve_ms: int = 0
    total_ms: int = 0
    paths_explored: int = 0
    paths_merged: int = 0
    paths_pruned: int = 0
    summaries_computed: int = 0
    summaries_reused: int = 0
```

These metrics feed into the existing scan metadata for performance monitoring.

---

## Appendix A: Z3 String Theory Pitfalls

Z3's string theory (`QF_S`) has known limitations relevant to this engine:

1. **`Replace` is single-occurrence.** `z3.Replace(s, old, new)` replaces only the first match. JS `String.replace(string, ...)` also replaces only the first match, so this is actually correct. For `replaceAll` or regex-based replace, model as iterated `Replace` (bounded).

2. **`StrToInt` returns -1 for non-numeric strings.** This matches `parseInt` returning `NaN` only loosely. Add guard: `z3.If(z3.StrToInt(s) >= 0, z3.StrToInt(s), z3.IntVal(-1))`.

3. **String length is unbounded.** Without explicit bounds, Z3 may generate very long strings. Add default length bound: `z3.Length(payload) <= 10000`.

4. **Regex support is limited.** Z3 supports `z3.InRe(s, regex)` but regex construction is via Z3's API (`z3.Re(...)`, `z3.Star(...)`, etc.), not PCRE. Complex JS regexes (lookahead, backreferences) cannot be translated.

5. **Solver may return UNKNOWN.** The string theory is decidable for the quantifier-free fragment, but complex combinations with arrays/sequences can push Z3 into undecidable territory. Always handle `UNKNOWN` gracefully.

## Appendix B: File Manifest

New files:
- `src/piranesi/verify/concolic.py` -- main concolic execution engine
- `src/piranesi/verify/sym_state.py` -- SymbolicState, SymbolicStack, CallFrame
- `src/piranesi/verify/sym_memory.py` -- SymbolicHeap, aliasing
- `src/piranesi/verify/js_semantics.py` -- JS/TS operation -> Z3 translation
- `tests/test_verify/test_concolic.py` -- integration tests
- `tests/test_verify/test_js_semantics.py` -- unit tests for Z3 encodings
- `tests/test_verify/test_concolic_bench.py` -- benchmark tests
- `tests/fixtures/typescript/concolic/` -- test fixture directory

Modified files:
- `src/piranesi/verify/__init__.py` -- re-export concolic API
- `src/piranesi/pipeline.py` -- add concolic fallback to verification step
- `src/piranesi/config.py` -- add `VerifyConfig` section with symbolic execution settings
