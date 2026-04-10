# Phase 9: Ground Truth Research Expansion

**Estimated effort: 40-60 ideal hours**
**Blocked by: Phase 5 (scoring harness)**
**Blocks: Phase 8 (needs ground truth for measurement), calibration work**
**Target milestone: v0.2.0**

---

## 1. Phase Overview

Piranesi's evaluation harness has 50 ground truth entries (36 TPs, 14 FPs). This is enough for directional results but not statistically meaningful per-CWE metrics (n=3-9 per CWE). Confidence intervals are enormous.

This phase systematically expands ground truth to 150+ entries across 7 CWE categories, drawing from 10+ real-world vulnerable Node.js/TypeScript applications. The goal is n>=15 per CWE category, enabling meaningful precision/recall claims.

---

## 2. Target CWE Distribution

| CWE | Current Count | Target Count | Sources |
|-----|--------------|-------------|---------|
| CWE-89 (SQLi) | 9 | 20 | NodeGoat, DVNA, Goof, Juice Shop, real CVEs |
| CWE-79 (XSS) | 9 | 20 | NodeGoat, Juice Shop, DVNA, Damn Vulnerable Web App |
| CWE-78 (CMDi) | 5 | 15 | DVNA, Goof, synthetic, real CVEs |
| CWE-22 (Path Traversal) | 6 | 15 | DVNA, Juice Shop, synthetic, real CVEs |
| CWE-918 (SSRF) | 3 | 15 | NodeGoat, Juice Shop, synthetic |
| CWE-94 (Code Injection) | 0 | 10 | eval(), new Function(), vm module patterns |
| CWE-611 (XXE) | 0 | 5 | XML parser configurations |
| **False Positives** | 14 | 30 | Parameterized queries, sanitizers, type guards, allowlists |
| **Total** | 50 | 150+ | |

---

## 3. Source Applications

### 3.1 Primary Sources (Real Vulnerabilities)

| Application | Description | Expected Findings |
|------------|-------------|-------------------|
| OWASP NodeGoat | Express + MongoDB | NoSQL injection, XSS, SSRF, auth bypass |
| OWASP Juice Shop | Express + SQLite/Sequelize | SQLi, XSS, path traversal, SSRF |
| DVNA (Damn Vulnerable NodeJS App) | Express + MySQL | SQLi, CMDi, XSS, path traversal |
| Goof (Snyk) | Express + MongoDB | NoSQL injection, CMDi, XSS |
| Nodegoat-sequelize | Express + Sequelize | SQLi via ORM |
| vulnerable-node | Express + various | Multiple CWEs |

### 3.2 CVE-Based Sources

Mine real CVEs from:
- **npm advisory database** — search for Express/Koa/Fastify advisories with CWE tags
- **Snyk vulnerability DB** — public disclosures with affected code
- **GitHub Advisory Database** — `GHSA-*` entries for Node.js packages

For each CVE:
1. Clone the affected package at the vulnerable commit
2. Identify the taint flow (source → sink)
3. Document as ground truth YAML entry
4. Pin commit hash

### 3.3 Synthetic Sources

Expand `eval/synthetic/` with hand-crafted patterns for:
- **Edge cases**: nested template literals, dynamic property access, spread operators
- **Framework patterns**: Express router chains, Koa context, Fastify decorators
- **Async patterns**: Promise chains, async/await, callback-based flows
- **FP patterns**: every sanitizer from Phase 8, type coercion guards, allowlists

---

## 4. Research Methodology

### 4.1 Per-Application Workflow

For each source application:

1. **Clone at pinned commit** — reproducibility requires exact commit
2. **Manual audit** — identify all taint flows (source → sink) by reading the code
3. **Run Piranesi** — compare Piranesi's findings against manual audit
4. **Document discrepancies** — missed findings become gap analysis, false findings become FP ground truth
5. **Write YAML entries** — one file per finding, following `eval/ground_truth/schema.py`

### 4.2 Quality Standards

Each ground truth entry must have:
- Exact file paths and line numbers (verified against pinned commit)
- Complete taint path (source → intermediate steps → sink)
- Exploitability assessment (exploitable: true/false with reasoning)
- For TPs: a reference exploit or exploit description
- For FPs: explanation of why the finding is false (sanitizer, type guard, dead code, etc.)

### 4.3 Agent Delegation

This phase is highly parallelizable. Each source application can be researched independently:

- **Agent A**: OWASP Juice Shop ground truth extraction
- **Agent B**: DVNA ground truth extraction
- **Agent C**: Goof + vulnerable-node extraction
- **Agent D**: CVE mining from npm/Snyk/GitHub advisories
- **Agent E**: Synthetic edge case generation
- **Agent F**: False positive pattern expansion

---

## 5. Deliverables

1. `eval/ground_truth/gt-051.yaml` through `gt-150+.yaml` — 100+ new entries
2. Updated `eval/synthetic/` — 30+ new synthetic TS files
3. `docs/GROUND_TRUTH_CATALOG.md` — index of all entries with per-CWE counts
4. Updated `tests/eval/test_baselines.py` — adjust expected counts
5. `eval/benchmark_results/` — Piranesi run results against each source application

---

## 6. Acceptance Criteria

- [ ] 150+ total ground truth entries
- [ ] n >= 15 per CWE category (CWE-89, 79, 78, 22, 918)
- [ ] n >= 10 for CWE-94
- [ ] n >= 5 for CWE-611
- [ ] 30+ false positive entries
- [ ] All entries have pinned commits and verified file paths
- [ ] Per-CWE precision/recall confidence intervals < 15%
- [ ] All eval tests pass with expanded dataset
