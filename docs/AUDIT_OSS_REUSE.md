# Engineering Audit: Feasibility & OSS Reuse Analysis

---

## 1. Feasibility Audit

### Phase Estimates — Revised

| Phase | Planned (h) | Revised (h) | Verdict |
|-------|-------------|-------------|---------|
| 0: Foundations | 36-49 | 36-49 | REALISTIC |
| 1: Taint Analysis | 305-390 | 380-530 | UNDERESTIMATED |
| 2: Exploit Verification | 123-158 | 135-180 | SLIGHTLY UNDER |
| 3: Regulatory Engine | 94-125 | 94-125 | REALISTIC |
| 4: LLM Orchestration | 80-103 | 80-103 | REALISTIC |
| 5: Eval Harness | 85-118 | 85-118 | REALISTIC |
| 6: Integration | 60-80 | 60-80 | REALISTIC |
| **Total** | **783-1023** | **870-1185** | |

Note: RISKS doc states 660-950h; actual phase docs sum to 783-1023h. Corrected: **870-1185h**.

### Phase 1 Specific Underestimates

- **tree-sitter IR construction** (planned 25-35h → revised 35-50h): TS syntax coverage (decorators, namespaces, enums, conditional types, JSX/TSX) + module resolution (tsconfig paths, barrel exports, node_modules) is grunt work that always exceeds estimates.
- **Inter-procedural taint** (planned 50-60h → revised 60-80h): assumes correct call graph, but JS call graph construction is itself imprecise.
- **Path condition tracking** (planned 40-50h → revised 50-70h): long tail of JS idioms (short-circuit eval, chained ternaries, nullish coalescing, destructuring defaults inside conditionals).
- **Aliasing** (planned 30-40h → revised 35-50h): points-to analysis for JS is an open research problem.
- **Testing** (planned 25-30h → revised 35-45h): 130-195 tests with fixture creation and annotation parsing.

### Top 3 Schedule Risks

1. **Phase 1 scaling**: will k=2 context-sensitive analysis complete in <5min on a 500-file codebase? No profiling data exists. If it doesn't, redesign required (20-40h lost).
2. **Docker sandbox reliability**: auto-generated Dockerfiles for arbitrary Node.js apps will fail frequently. Unbounded debugging effort.
3. **Phase 4/5 circular dependency**: ensemble calibration requires eval data; eval requires working pipeline.

### Solo Dev Timeline

- Best case: **22-30 weeks (5.5-7.5 months)**
- Realistic (with investigation spikes, redesigns): **8-12 months**

---

## 2. OSS Reuse Candidates

### Recommended — Adopt After Spike

#### Joern (Apache 2.0) — HIGH IMPACT
- **Repo**: github.com/joernio/joern
- **License**: Apache 2.0 ✅
- **What**: Code Property Graph platform with inter-procedural data flow analysis. JS support (not native TS — needs transpilation or `.js` input).
- **Replaces**: Phase 1 Layers 1-6 (tree-sitter IR, call graph, intra/inter-procedural taint, context sensitivity, field sensitivity)
- **Hours saved**: 150-250h
- **Integration**: run as subprocess or via server mode REST API. Parse JSON/CPGQL output into Piranesi's data models.
- **Risks**: JVM dependency fractures Python-native architecture. TS support requires transpilation. Loss of fine-grained control over taint lattice and Express-specific patterns.
- **Spike required**: 16-24h to validate TS handling, data flow detection accuracy, and latency on 500-file project.

#### DSPy (MIT) — MEDIUM IMPACT
- **Repo**: github.com/stanfordnlp/dspy
- **License**: MIT ✅
- **What**: LLM programming framework with ensemble and optimizer features.
- **Replaces**: Phase 4 calibrated ensemble + cost-aware optimizer
- **Hours saved**: 25-40h
- **Integration**: Python-native. `dspy.Ensemble` wraps triage module.
- **Risks**: DSPy wants to own the LLM call layer — conflicts with LiteLLM. Opinionated abstractions. API churn.
- **Spike required**: 8-12h to test ensemble coexistence with LiteLLM.

### Recommended — Direct Adoption

#### OpenGrep (LGPL-2.1) — BASELINE ONLY
- **Repo**: github.com/opengrep/opengrep
- **License**: LGPL-2.1 ⚠️ (safe as subprocess, cannot link as library)
- **What**: Fork of Semgrep CE restoring cross-function taint analysis. TS/JS support.
- **Use**: Replace Semgrep as the baseline comparison in Phase 5 eval harness. Stronger baseline = more meaningful Piranesi vs. baseline comparison.
- **Hours saved**: 5-10h
- **Integration**: subprocess + JSON/SARIF output. No linking.

### Rejected

| Project | License | Reason |
|---------|---------|--------|
| CodeQL | Proprietary | Cannot redistribute or embed. Free for OSS only. |
| njsscan | LGPL-3.0 | No TypeScript support. Inactive maintenance. Wraps Semgrep. |
| ts-morph | MIT | Requires Node.js subprocess. Planning docs already rejected this. |
| ExpoSE | Unclear | License unverified. ES5.1 focused. Dynamic execution conflicts with static pipeline. |
| Jalangi2 | Apache 2.0 | ES5.1 focused. Requires runtime instrumentation. Poor fit. |
| Catala | Apache 2.0 | OCaml dependency. Designed for tax law. Overhead exceeds benefit. |
| OPA | Apache 2.0 | For ~50 rules, hand-rolled engine is simpler. Adds Go binary. Consider for v2 if >200 rules. |
| Guidance | MIT | Overlaps with LiteLLM `response_format`. Limited provider support. |
| BAML | Apache 2.0 | Adds DSL + build step. Marginal benefit over Pydantic. |
| Semgrep CE | LGPL-2.1 | No inter-file taint (commercial-only). Superseded by OpenGrep for baseline. |
| OWASP Benchmark | GPL-2.0 | Java only. No JS/TS version exists. |
| PyDatalog | LGPL | Poorly maintained. Planning docs already rejected. |

---

## 3. Revised Estimates With OSS Integration

### Scenario A: Joern spike succeeds + DSPy spike succeeds

| Phase | Original (h) | Revised (h) | Savings |
|-------|-------------|-------------|---------|
| 0 | 36-49 | 36-49 | — |
| 1 | 380-530 | 180-280 | -200-250h |
| 2 | 135-180 | 135-180 | — |
| 3 | 94-125 | 94-125 | — |
| 4 | 80-103 | 55-75 | -25-28h |
| 5 | 85-118 | 80-108 | -5-10h |
| 6 | 60-80 | 60-80 | — |
| Spikes | — | 24-36 | (investment) |
| **Total** | **870-1185** | **664-933** | **-206-252h** |

**Timeline**: 17-23 weeks (4-6 months)

### Scenario B: Joern fails, DSPy succeeds

| **Total** | **870-1185** | **840-1147** | **-30-38h** |

**Timeline**: 21-29 weeks (5-7 months)

### Scenario C: Both spikes fail

| **Total** | **870-1185** | **865-1175** | **-5-10h** |

**Timeline**: 22-30 weeks (5.5-7.5 months)

---

## 4. Minimum Viable v1 With OSS Reuse

Apply scope cuts 1-2-5 (EU AI Act, Pareto plots, cost optimizer) + Joern integration:

| Phase | Scope | Hours |
|-------|-------|-------|
| 0 | Full scaffolding | 36-49 |
| 1 | Joern-backed taint + custom source/sink specs. No path conditions. k=1 context. | 120-180 |
| 2 | Z3 template payloads + Docker sandbox. No path condition constraints. | 100-140 |
| 3 | PDPA + MAS TRM only. Hand-rolled engine. | 70-95 |
| 4 | LiteLLM + hardcoded routing + majority-vote ensemble. Skeptic agent. | 40-55 |
| 5 | 20-entry ground truth. Scoring. OpenGrep baseline. | 55-80 |
| 6 | Orchestrator + markdown report + 1 example run. | 50-70 |
| **Total** | | **471-669** |

**Timeline**: 12-17 weeks (3-4 months)

This v1 still delivers: real inter-procedural taint analysis, verified exploits (Z3 + Docker), PDPA/MAS TRM regulatory mapping, LLM-augmented FP discrimination, and measured precision on ground truth. Credible and differentiated.

---

## 5. Recommended Action Plan

### Week 1: Spikes (before committing to architecture)

1. **Joern spike (16-24h)**: Parse 5 real TS projects (transpile to JS if needed). Run Joern's data flow analysis. Verify it detects taint paths from `req.body` → `db.query()`. Measure latency. Assess output format.

2. **DSPy spike (8-12h)**: Build a triage module with 3-model ensemble. Test alongside LiteLLM. Assess abstraction compatibility.

### Week 2: Decide architecture based on spike results

- If Joern works: adopt as taint backend, build Piranesi-specific source/sink specs and Express patterns on top
- If Joern fails: proceed with custom tree-sitter engine per original Phase 1 plan
- If DSPy works: adopt for ensemble layer
- If DSPy fails: build custom ensemble per original Phase 4 plan

### Week 2+: Implement per revised phase plan

Critical path with Joern: Phase 0 → Phase 1 (reduced) → Phase 2 → Phase 5 → Phase 6
Parallel: Phase 3 + Phase 4 after Phase 0
