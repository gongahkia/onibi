# Phase 23: Reachability Analysis + Dead Code Pruning

**Estimated effort: 25-35 ideal hours**
**Blocked by: Phase 22 (advanced taint analysis — uses call graph)**
**Blocks: Nothing (independent optimization)**

## 1. Motivation

Static analysis tools are noisy because they report vulnerabilities in code that can never execute: dead functions, unreachable branches, unused dependencies. Reachability analysis prunes findings in unreachable code, dramatically reducing false positive noise without sacrificing recall.

Two levels:
1. **Function reachability**: is the vulnerable function callable from any entry point?
2. **Dependency reachability**: is the vulnerable dependency function actually imported and used?

## 2. Function Reachability

### 2.1 Entry Point Detection

Identify all entry points in the scanned project:

**Express/Fastify/Koa (Node.js):**
```javascript
app.get('/path', handler)    // route handler
app.use(middleware)           // middleware
router.post('/path', handler) // router handler
```
CPGQL: `cpg.call.name("get|post|put|delete|patch|use|all").argument.order(2)`

**Flask/Django/FastAPI (Python):**
```python
@app.route('/path')          # Flask
@router.get('/path')         # FastAPI
urlpatterns = [path('/', view)] # Django
```

**Gin/Echo/Chi (Go):**
```go
r.GET("/path", handler)      // Gin
e.POST("/path", handler)     // Echo
r.Get("/path", handler)      // Chi
```

**Spring Boot (Java):**
```java
@GetMapping("/path")         // Spring
@PostMapping("/path")
```

**Other entry points:**
- Exported functions from `index.ts` / `__init__.py` / `main.go`
- CLI entry points from `package.json` `bin` or `pyproject.toml` `[project.scripts]`
- Test functions (only when `--include-tests`)
- `main()` functions

### 2.2 Call Graph Construction

Query Joern for the call graph:
```
cpg.method.callOut.callee.fullName.l
```

Build an adjacency list: `dict[str, set[str]]` mapping each function to its callees.

### 2.3 Reachability Computation

BFS from all entry points over the call graph. A function is reachable if there exists a path from any entry point to it.

```python
def compute_reachable(entry_points: set[str], call_graph: dict[str, set[str]]) -> set[str]:
    visited: set[str] = set()
    queue = deque(entry_points)
    while queue:
        fn = queue.popleft()
        if fn in visited:
            continue
        visited.add(fn)
        for callee in call_graph.get(fn, ()):
            if callee not in visited:
                queue.append(callee)
    return visited
```

### 2.4 Finding Annotation

For each `CandidateFinding`:
1. Check if the source function is in the reachable set.
2. If unreachable:
   - Set `reachability = "unreachable"`
   - Override severity to `informational`
   - Move to separate "Unreachable Findings" section in report

### 2.5 CLI Flags

```
piranesi run <dir>                    # default: prune unreachable
piranesi run <dir> --include-unreachable  # include unreachable in main report
piranesi run <dir> --dead-code-report     # output dead code list
```

### 2.6 Dead Code Report

When `--dead-code-report`:
```
Dead Code Report
================
23 functions unreachable from any entry point:

  src/utils/legacy.ts:
    - legacyHash() (line 12)
    - oldEncrypt() (line 45)

  src/middleware/deprecated.ts:
    - rateLimitV1() (line 8)
    - corsHandlerOld() (line 34)
```

## 3. Dependency Reachability

### 3.1 Problem

`npm audit` and `pip-audit` flag ALL vulnerabilities in installed dependencies, even if the vulnerable function is never called. This creates massive noise for large `node_modules`.

Example: `lodash` has a prototype pollution vulnerability in `_.defaultsDeep()`. If the project only uses `_.map()` and `_.filter()`, the vulnerability is unreachable.

### 3.2 Import Analysis

Parse import/require statements:

**JavaScript/TypeScript:**
```javascript
const { defaultsDeep } = require('lodash');  // imports defaultsDeep
import { map, filter } from 'lodash';        // does NOT import defaultsDeep
const _ = require('lodash');                  // imports everything (conservative)
```

**Python:**
```python
from pickle import loads     # imports loads (vulnerable)
import pickle               # imports everything (conservative)
from django.utils import html  # safe module
```

### 3.3 Matching

For each dependency vulnerability advisory:
1. Extract the vulnerable function/module name from the advisory.
2. Check if any import statement in the project imports that function.
3. If yes: check if the imported function is called (via call graph).
4. If no import or no call: mark as `reachability: "dep_unreachable"`.

### 3.4 Conservative Fallback

When a dependency is imported as a whole (`const _ = require('lodash')`), assume all exports are reachable. Only mark unreachable when specific named imports exclude the vulnerable function.

## 4. Data Model

```python
@dataclass
class ReachabilityResult:
    reachable_functions: set[str]
    unreachable_functions: set[str]
    entry_points: set[str]
    call_graph_edges: int

@dataclass  
class DepReachabilityResult:
    reachable_deps: set[str]     # package:function pairs
    unreachable_deps: set[str]
    import_graph_edges: int
```

Add to `CandidateFinding`:
```python
reachability: str = "reachable"  # "reachable" | "unreachable" | "dep_unreachable"
```

## 5. Integration with Report

### 5.1 Executive Summary

```
Findings: 12 total
  - 8 reachable (4 high, 3 medium, 1 low)
  - 2 unreachable (moved to appendix)
  - 2 dependency-unreachable (moved to appendix)
```

### 5.2 Report Sections

1. **Active Findings** — reachable findings only (default view)
2. **Unreachable Findings** — code-level unreachable (appendix)
3. **Dependency Noise** — SCA findings for unused vulnerable functions (appendix)

## 6. Tests

1. Fixture with 3 functions: one reachable from route handler, one called only from dead code, one never called.
2. Verify reachable function's finding stays in main report.
3. Verify unreachable function's finding moves to appendix with `informational` severity.
4. Fixture with `lodash` import — only `_.map` used, verify `defaultsDeep` vulnerability marked unreachable.
5. Fixture with wildcard import (`const _ = require('lodash')`), verify conservative: all vulns stay reachable.
6. Verify `--include-unreachable` includes everything.
7. Verify `--dead-code-report` outputs function list.

## 7. Risks

- **Dynamic dispatch**: `obj[method]()` or `eval()` makes static call graphs incomplete. Mitigation: conservative — if we can't prove unreachable, assume reachable.
- **Reflection**: Java's `Class.forName()`, Python's `getattr()`. Same mitigation.
- **Plugin loading**: frameworks that auto-discover handlers (e.g., Next.js file-based routing). Mitigation: framework plugins can declare additional entry point patterns.
