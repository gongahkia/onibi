# Phase 5: Evaluation Harness

## 1. Phase Overview

The evaluation harness is the mechanism that proves Piranesi's precision claim. Without it, the v1 release is not ready.

**Why precision is the headline metric:** False positives kill maintainer trust. The curl project terminated its bug bounty program because AI-generated reports were overwhelmingly false positives. Every major static analyzer suffers from alert fatigue. Piranesi's core value proposition is precision -- confirmed, exploitable findings only -- and the eval harness is how we prove and maintain that claim.

**What this phase builds:**
- A curated ground truth dataset of real and synthetic TS/JS vulnerabilities
- A scoring pipeline that computes precision, recall, and F1 per stage and per CWE class
- Baseline integrations (Semgrep, LLM-only) for direct comparison
- Pareto frontier plotting across model routing configurations to validate the AISLE thesis (mixed-model pipelines beat single-model approaches on cost-precision tradeoffs)
- Regression testing infrastructure to prevent precision regressions on every commit

The eval harness also provides the calibration data that Phase 4's ensemble and optimizer consume.

---

## 2. Ground Truth Dataset Curation

**Estimated effort: 25-35 ideal hours**

### 2.1 Sourcing Strategy

**a) OWASP NodeGoat** (deliberately vulnerable Node.js app)

Target vulnerabilities:
- SQLi via MongoDB injection (`$gt`, `$ne` operators in unparameterized queries)
- XSS in user profile fields (stored XSS)
- Command injection via `child_process.exec()` with user input
- SSRF via unchecked URL fetch
- Insecure direct object references (IDOR)

Extract: specific file paths, line numbers, taint paths from source to sink.

**b) OWASP Juice Shop** (TypeScript)

Target vulnerabilities:
- SQLi via TypeORM raw queries
- XSS in product search/review
- Path traversal in file serving endpoints
- Broken authentication (JWT issues)
- NoSQL injection

Advantage: TypeScript source, broad CWE coverage, actively maintained.

**c) Real CVEs from open source projects**

Search strategy: NVD + GitHub Security Advisories, filter for TypeScript/JavaScript, require available fix commits (so we have before/after snapshots).

Candidate CVEs:

| Project | CVE Pattern | CWE | Notes |
|---------|-------------|-----|-------|
| Express.js ecosystem | Input validation bypass | CWE-20 | Middleware parsing issues |
| Prisma ORM | Raw query injection | CWE-89 | Bypassing parameterization |
| Next.js | Server-side injection | CWE-94 | SSR code injection vectors |
| node-postgres | SQL injection | CWE-89 | Parameter handling edge cases |
| Fastify | Prototype pollution | CWE-1321 | Schema validation bypass |
| Sequelize | SQL injection | CWE-89 | Raw query misuse |

For each CVE: pin the vulnerable commit, extract the affected code, document the taint path, capture the fix commit as the reference patch.

**d) Hand-crafted synthetic test cases**

Create TypeScript files under `eval/synthetic/` with:
- **True positive patterns**: direct taint flows that are definitely exploitable
- **False positive patterns**: code that looks suspicious but is safe (parameterized queries that superficially resemble string concatenation, sanitized input that passes through a taint-tracked path, dead code with apparent vulnerabilities)

False positive tests are critical: they validate that Piranesi's triage stage correctly filters safe code.

### 2.2 Ground Truth Entry Schema

File: `eval/ground_truth/schema.py` (Pydantic model)

```python
from pydantic import BaseModel
from enum import Enum

class Label(str, Enum):
    TRUE_POSITIVE = "true_positive"
    FALSE_POSITIVE = "false_positive" # known safe, should NOT be flagged

class Complexity(str, Enum):
    SIMPLE = "simple"           # direct taint flow, single function
    INTERPROCEDURAL = "inter"   # crosses function boundaries
    CONTEXT_SENSITIVE = "ctx"   # requires context-sensitive analysis

class GroundTruthEntry(BaseModel):
    id: str                          # e.g., "gt-001"
    source_project: str              # e.g., "owasp-nodegoat"
    commit_hash: str                 # pinned commit
    cwe_id: str                      # e.g., "CWE-89"
    cwe_name: str                    # e.g., "SQL Injection"
    label: Label                     # true_positive or false_positive
    affected_files: list[str]        # relative paths
    line_numbers: list[int]          # primary vulnerable lines
    taint_source: str                # e.g., "req.query.id"
    taint_sink: str                  # e.g., "db.query()"
    taint_path: list[str]            # intermediate steps
    complexity: Complexity
    exploitable: bool                # is a working exploit possible?
    reference_exploit: str | None    # exploit description or script path
    reference_fix_commit: str | None # commit that fixed the vuln
    notes: str                       # additional context
```

### 2.3 Entry Format (on disk)

YAML files in `eval/ground_truth/`, one per entry:

```yaml
# eval/ground_truth/gt-001.yaml
id: gt-001
source_project: owasp-nodegoat
commit_hash: "a1b2c3d4e5f6789012345678901234567890abcd"
cwe_id: CWE-89
cwe_name: SQL Injection
label: true_positive
affected_files:
  - app/routes/contributions.js
line_numbers: [45, 47]
taint_source: req.body.userId
taint_sink: db.collection.find()
taint_path:
  - req.body.userId
  - contributions.handleContribution(userId)
  - db.collection.find({userId: userId})
complexity: inter
exploitable: true
reference_exploit: "POST /contributions with userId={$gt: ''} bypasses auth check"
reference_fix_commit: "b2c3d4e5f6789012345678901234567890abcdef"
notes: "MongoDB NoSQL injection via unvalidated $gt operator"
```

### 2.4 Distribution Targets

| Category | Minimum Count | Sources |
|----------|--------------|---------|
| SQLi (CWE-89) | 5 | NodeGoat, Juice Shop, real CVEs |
| XSS (CWE-79) | 5 | NodeGoat, Juice Shop, synthetic |
| Command injection (CWE-78) | 3 | NodeGoat, synthetic |
| Path traversal (CWE-22) | 3 | Juice Shop, synthetic |
| Known false positives | 4 | Hand-crafted (parameterized queries, sanitized inputs, dead code, type-safe patterns) |
| **Total minimum** | **20** | |

Complexity distribution: at least 30% inter-procedural or context-sensitive.

### 2.5 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.2a: Schema definition + YAML tooling | 3-4h | Pydantic model, YAML loader, validation script |
| M5.2b: OWASP NodeGoat entries (5-6 entries) | 6-8h | Analyzed taint paths, documented entries |
| M5.2c: OWASP Juice Shop entries (5-6 entries) | 6-8h | Analyzed taint paths, documented entries |
| M5.2d: Real CVE entries (4-5 entries) | 6-8h | CVE research, code analysis, entry docs |
| M5.2e: Synthetic test cases (4-5 entries) | 4-6h | TP + FP patterns with known labels |

---

## 3. Scoring Methodology

**Estimated effort: 12-15 ideal hours**

### 3.1 Primary Metrics

| Metric | Formula | Target (v1) |
|--------|---------|-------------|
| Precision | TP / (TP + FP) | >= 0.80 |
| Recall | TP / (TP + FN) | >= 0.50 |
| F1 | 2 * P * R / (P + R) | >= 0.60 |

Precision is the headline metric. A tool with 0.9 precision and 0.5 recall is more valuable than one with 0.5 precision and 0.9 recall, because users stop trusting tools that cry wolf.

### 3.2 Per-Stage Metrics

| Stage | Metric | What it Measures |
|-------|--------|------------------|
| Scan | Source/sink recall | % of ground-truth sources and sinks identified by the scanner |
| Detect | Candidate precision, recall | How many candidates are real? How many real vulns were found? |
| Triage | FP filter rate | % of FPs correctly filtered out by triage (ensemble + skeptic) |
| Triage | TP retention rate | % of TPs that survive triage (should be near 100%) |
| Verify | Confirmation rate | % of post-triage TPs that were successfully exploited |
| Legal | Correctness | Manual review against expected regulatory obligations |

### 3.3 Matching Logic

A pipeline finding matches a ground truth entry when:
1. Same file (at least one `affected_file` in common)
2. Same vulnerability class (CWE ID matches)
3. Taint path overlap: finding's source matches ground truth source AND finding's sink matches ground truth sink

Matching is done by normalized string comparison (strip whitespace, lowercase). Partial credit: if source matches but sink is different (but still in the same file), score as a partial match (0.5 weight) to distinguish "found the right area but wrong sink" from "completely missed."

### 3.4 Scoring Script

File: `eval/scoring.py`

```
Usage: python eval/scoring.py --pipeline-output results.json --ground-truth eval/ground_truth/

Output:
  - eval/scores/latest.json (machine-readable)
  - stdout: human-readable table
```

### 3.5 Concrete Scoring Output

```
Piranesi Evaluation Report
==========================
Date: 2025-01-20
Commit: a1b2c3d
Ground truth entries: 20 (16 TP, 4 FP)
Pipeline findings: 18

Overall Metrics:
  Precision:  0.846 (11/13 confirmed findings are real)
  Recall:     0.688 (11/16 real vulnerabilities found)
  F1:         0.759

Per-CWE Breakdown:
  CWE-89 (SQLi):        P=1.00  R=0.80  F1=0.89  (4/5 found, 0 FP)
  CWE-79 (XSS):         P=0.80  R=0.80  F1=0.80  (4/5 found, 1 FP)
  CWE-78 (Cmd Inj):     P=0.67  R=0.67  F1=0.67  (2/3 found, 1 FP)
  CWE-22 (Path Trav):   P=1.00  R=0.33  F1=0.50  (1/3 found, 0 FP)

False Positive Handling:
  Known FP entries: 4
  Correctly filtered: 3
  Leaked through: 1 (gt-fp-003: parameterized query false alarm)

Per-Stage Breakdown:
  Scan:    18/20 sources identified (90%), 16/20 sinks identified (80%)
  Detect:  15 candidate findings generated
  Triage:  2 FPs filtered, 0 TPs incorrectly filtered
  Verify:  11/13 post-triage findings confirmed exploitable

Cost: $3.47 total, $0.32/finding, $0.32/TP
```

### 3.6 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.3a: Matching logic implementation | 4-5h | Matcher with partial credit, CWE normalization |
| M5.3b: Scoring calculator | 4-5h | Precision/recall/F1, per-CWE, per-stage |
| M5.3c: Report generator (JSON + table) | 4-5h | `scoring.py` with CLI interface |

---

## 4. Baseline Integrations

**Estimated effort: 20-25 ideal hours**

### 4.1 OpenGrep Baseline (preferred) / Semgrep CE Fallback

**Estimated effort: 10-12 ideal hours**

**Purpose:** Measure Piranesi against the strongest available open source SAST baseline for TypeScript/JavaScript.

**Primary baseline: OpenGrep** ([github.com/opengrep/opengrep](https://github.com/opengrep/opengrep))
- License: LGPL-2.1 — safe to invoke as a subprocess (no linking into Piranesi's Apache-2.0 code)
- OpenGrep is a community fork of Semgrep CE that restores cross-function taint analysis, inter-procedural scanning, and fingerprinting that Semgrep moved to its commercial tier
- This makes OpenGrep a *stronger* baseline than Semgrep CE, which means Piranesi must beat a higher bar

**Fallback baseline: Semgrep CE** — use if OpenGrep is unavailable or unstable. Semgrep CE only provides intra-file pattern matching (no inter-file taint), so it's a weaker baseline.

**Setup (OpenGrep):**
```bash
# install via pip (or binary release)
pip install opengrep
# run against a ground truth project
opengrep --config=p/typescript --config=p/javascript --json -o opengrep-results.json <project_dir>
```

**Setup (Semgrep CE fallback):**
```bash
pip install semgrep
semgrep --config=p/typescript --config=p/javascript --json -o semgrep-results.json <project_dir>
```

**Integration steps:**

1. **Runner** (`eval/baselines/opengrep_runner.py`):
   - Accepts a ground truth project path
   - Runs OpenGrep with default security rulesets (`p/typescript`, `p/javascript`, `p/security-audit`)
   - Captures JSON output (SARIF or Semgrep-compatible JSON format)
   - Falls back to Semgrep CE if OpenGrep binary is not available (log warning)

2. **Normalizer** (`eval/baselines/opengrep_normalizer.py`):
   - Maps rule IDs to CWE IDs (OpenGrep inherits Semgrep's metadata format)
   - Extracts file, line number, rule description
   - For OpenGrep: also extracts inter-file taint paths if available in output
   - Outputs normalized findings in the same format as Piranesi's output

3. **Scorer**: Use the same `eval/scoring.py` against normalized output

**Expected outcomes (OpenGrep):**
- Good recall for pattern-based vulnerabilities AND some inter-procedural flows (OpenGrep restores cross-file taint)
- Higher recall than Semgrep CE for inter-file data flows
- Higher false positive rate than Piranesi (no triage/verification/SMT stage)
- Zero regulatory mapping capability
- This is the baseline Piranesi must beat on precision to validate the "real analysis, not pattern matching" claim

**Expected outcomes (Semgrep CE):**
- Good recall for pattern-based vulnerabilities (direct SQLi via string concatenation, obvious XSS)
- Lower recall for inter-procedural flows (no cross-file taint in CE)
- Higher false positive rate than Piranesi
- Zero regulatory mapping capability

### 4.2 PentestGPT-Style LLM-Only Baseline

**Estimated effort: 10-13 ideal hours**

**Purpose:** This baseline represents the "shallow LLM wrapper" approach -- feed source code to an LLM and ask it to find vulnerabilities. This is what most AI security tools actually do under the hood.

**Setup:**

1. **Runner** (`eval/baselines/llm_only_runner.py`):
   - For each source file in a ground truth project, send the full file to the LLM with a security audit prompt
   - Use the same model as Piranesi's detector (e.g., Claude Sonnet) for fair comparison
   - Prompt: "Identify all security vulnerabilities in the following TypeScript/JavaScript code. For each vulnerability, specify: file, line number(s), CWE ID, description, and severity."
   - Parse structured JSON output

2. **Normalizer**: extract findings into the standard format

3. **Scorer**: same `eval/scoring.py`

**Key differences from Piranesi:**
- No taint analysis (LLM "guesses" data flow)
- No verification (no exploit generation or sandbox execution)
- No regulatory mapping
- No ensemble/skeptic (single model, single pass)
- Much higher token usage (sends full files, not targeted code paths)

**Expected outcomes:**
- Moderate recall (LLMs are good at pattern recognition)
- Lower precision (no verification, LLMs are overconfident -- cf. Claude Sonnet 4.5 misidentifying safe code as vulnerable in the AISLE benchmarks)
- Significantly higher cost per finding (full-file context windows)
- No regulatory output

### 4.3 Baseline Comparison Report

The scoring script produces a side-by-side comparison:

```
Baseline Comparison
===================
                  Piranesi    Semgrep     LLM-Only
Precision         0.846       0.545       0.600
Recall            0.688       0.625       0.750
F1                0.759       0.583       0.667
Cost              $3.47       $0.00       $12.80
Cost/TP           $0.32       $0.00       $1.07
FP count          2           7           6
Regulatory map    Yes         No          No
Exploit gen       Yes         No          No
```

### 4.4 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.4a: Semgrep runner + normalizer | 5-6h | Run Semgrep, normalize output |
| M5.4b: Semgrep scoring + comparison | 3-4h | Score and tabulate vs Piranesi |
| M5.4c: LLM-only runner + normalizer | 5-6h | Single-model baseline implementation |
| M5.4d: LLM-only scoring + comparison | 3-4h | Score and tabulate vs Piranesi |
| M5.4e: Combined comparison report | 2-3h | Side-by-side output formatting |

---

## 5. Pareto Frontier Plotting

**Estimated effort: 8-12 ideal hours**

### 5.1 Configurations to Test

Each configuration uses a different model routing setup (Phase 4 router config):

| Config Name | Scanner | Detector | Triage | Patcher | Expected Cost | Expected Precision |
|-------------|---------|----------|--------|---------|---------------|-------------------|
| all-local | Llama 8B | Llama 8B | Llama 8B | Llama 8B | ~$0.01 | Low |
| all-cheap-api | Llama 70B | Llama 70B | Llama 70B | Llama 70B | ~$0.50 | Medium-low |
| mixed-default | Llama 70B | DeepSeek R1 | Claude Sonnet | Claude Sonnet | ~$3.50 | High |
| all-frontier | Claude Opus | Claude Opus | Claude Opus | Claude Opus | ~$20.00 | High |
| cheap-scan-frontier-triage | Llama 8B | Llama 70B | Claude Opus | Claude Sonnet | ~$5.00 | High |
| no-llm | N/A | N/A | N/A | N/A | $0.00 | Medium (static-only) |

The `no-llm` configuration runs Piranesi with all LLM augmentation disabled, isolating the taint engine's standalone performance.

### 5.2 Evaluation Loop

Script: `eval/pareto.py`

```python
for config in CONFIGS:
    # 1. set model routing config
    # 2. run piranesi against full ground truth suite
    # 3. score output
    # 4. record: config_name, precision, recall, f1, total_cost, cost_per_tp
    # 5. append to eval/results/pareto_data.json
```

### 5.3 Plots Generated

File: `eval/plots.py` (generates all plots via matplotlib/seaborn)

**Plot 1: Cost vs Precision (Pareto frontier)**
- X-axis: total cost (USD, log scale)
- Y-axis: precision
- Each point is a configuration
- Pareto-optimal configs connected by a line
- Baselines (Semgrep, LLM-only) plotted as reference points
- This is the headline plot: shows that `mixed-default` achieves near-frontier precision at 5-10x lower cost than `all-frontier`

**Plot 2: Cost vs Recall**
- Same axes structure as Plot 1 but Y-axis is recall
- Shows that recall scales more smoothly with cost than precision

**Plot 3: Precision-Recall Curve**
- X-axis: recall
- Y-axis: precision
- Each point is a configuration, labeled
- Baselines plotted as reference
- Shows the precision-recall tradeoff across configurations

**Plot 4: Cost Breakdown by Stage**
- Stacked bar chart
- X-axis: configuration name
- Y-axis: cost (USD)
- Stacked by stage (scanner, detector, triage, patcher, legal_memo)
- Shows where the money goes in each configuration

Output: PNG + SVG in `eval/results/plots/`

### 5.4 AISLE Thesis Validation

These plots directly test the AISLE thesis claims:

1. **"Coverage beats depth"**: Compare `mixed-default` (cheap scanner + frontier triage) vs `all-frontier`. If mixed achieves >= 90% of frontier precision at <= 30% of cost, the thesis holds.
2. **"Small models handle most detection"**: Compare `all-cheap-api` recall vs `all-frontier` recall. The gap should be smaller than the cost gap.
3. **"Model rankings reshuffle across tasks"**: Check per-stage performance -- a model that's best for detection may not be best for triage.

### 5.5 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.5a: Pareto evaluation loop | 3-4h | Config runner, result collection |
| M5.5b: Plot generation (4 plots) | 3-5h | matplotlib/seaborn plots, PNG + SVG |
| M5.5c: AISLE thesis validation analysis | 2-3h | Automated checks against thesis claims |

---

## 6. Regression Testing

**Estimated effort: 10-15 ideal hours**

### 6.1 CI Integration (per-commit)

On every push/PR:

1. Run Piranesi against a **fast subset** of the ground truth (~5 entries, selected for diversity: 2 SQLi, 1 XSS, 1 cmd inj, 1 FP)
2. Score against ground truth
3. **Fail CI if:**
   - Precision drops below threshold (configurable via `eval/config.toml`, default 0.80)
   - Recall drops below threshold (default 0.50)
   - Any previously-confirmed TP is now missed (regression)
   - Any previously-filtered FP now leaks through (regression)

Fast subset selection criteria:
- Covers at least 3 CWE classes
- Includes at least 1 inter-procedural flow
- Includes at least 1 known FP
- Total runtime < 5 minutes with mock LLM calls (for CI speed)

Configuration (`eval/config.toml`):
```toml
[regression]
fast_subset = ["gt-001", "gt-003", "gt-008", "gt-012", "gt-fp-002"]
precision_threshold = 0.80
recall_threshold = 0.50
fail_on_regression = true

[regression.ci]
use_mock_llm = true  # use mock provider in CI to avoid API costs
timeout_seconds = 300
```

### 6.2 Scheduled Full Evaluation (weekly)

Weekly CI job (GitHub Actions cron):

1. Run Piranesi against the **full** ground truth dataset
2. Run Semgrep baseline
3. Run LLM-only baseline (with real API calls, budget-capped)
4. Score all three
5. Regenerate Pareto frontier plots
6. Commit results to `eval/results/YYYY-MM-DD.json`

Results format:
```json
{
  "date": "2025-01-20",
  "commit": "a1b2c3d",
  "piranesi": {"precision": 0.846, "recall": 0.688, "f1": 0.759, "cost": 3.47},
  "semgrep": {"precision": 0.545, "recall": 0.625, "f1": 0.583, "cost": 0.00},
  "llm_only": {"precision": 0.600, "recall": 0.750, "f1": 0.667, "cost": 12.80},
  "pareto_configs": [...]
}
```

### 6.3 Regression Detection

Compare current run vs previous run (most recent `eval/results/*.json`):

| Change | Severity | Action |
|--------|----------|--------|
| TP -> FN (was found, now missed) | High | Fail CI, log affected entry |
| FP -> TP (was leaked, now filtered) | Positive | Log improvement |
| FN -> TP (was missed, now found) | Positive | Log improvement |
| TP -> FP (was confirmed, now can't exploit) | Medium | Warning, investigate verification |
| Precision drop > 5% | High | Fail CI |
| Recall drop > 10% | Medium | Warning |

### 6.4 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.6a: Fast subset selection + CI script | 4-5h | CI config, subset runner, threshold checks |
| M5.6b: Regression detection logic | 3-4h | Diff previous vs current, classify changes |
| M5.6c: Weekly full eval CI job | 3-4h | GitHub Actions cron workflow |
| M5.6d: Results persistence + history | 2-3h | JSON output, commit to repo |

---

## 7. Cost Tracking and Reporting

**Estimated effort: 5-8 ideal hours**

### 7.1 Per-Run Cost Metrics

Every evaluation run tracks:

| Metric | Formula | Purpose |
|--------|---------|---------|
| Total LLM cost | Sum of all LLM call costs | Budget validation |
| Cost per finding | Total cost / number of findings reported | Efficiency metric |
| Cost per TP | Total cost / TP count | Value metric |
| Cost per stage | Sum of costs for calls in each stage | Stage-level optimization |
| Cost per model | Sum of costs for calls to each model | Model-level optimization |

### 7.2 Cost in Pareto Analysis

Cost is the X-axis of the Pareto frontier plots. The cost-aware optimizer (Phase 4) uses these empirical cost measurements to improve its estimates over time.

### 7.3 Cost Report Format

Included as a section in the scoring report:

```
Cost Breakdown
==============
Total: $3.47

By stage:
  scanner:    $0.22 (6.3%)   - 50 calls, $0.004/call avg
  detector:   $0.35 (10.1%)  - 50 calls, $0.007/call avg
  triage:     $1.80 (51.9%)  - 15 calls, $0.120/call avg
  skeptic:    $0.15 (4.3%)   - 15 calls, $0.010/call avg
  patcher:    $0.60 (17.3%)  - 5 calls,  $0.120/call avg
  legal_memo: $0.35 (10.1%)  - 3 calls,  $0.117/call avg

By model:
  llama-3.1-70b:      $0.22 (6.3%)
  deepseek-r1:         $0.50 (14.4%)
  claude-sonnet-4-6:   $2.75 (79.3%)

Efficiency:
  Cost/finding: $0.27 (13 findings)
  Cost/TP:      $0.32 (11 true positives)
```

### 7.4 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.7a: Cost aggregation from trace logs | 2-3h | Read Phase 4 trace JSONL, aggregate |
| M5.7b: Cost report generation | 2-3h | Per-stage, per-model, efficiency metrics |
| M5.7c: Cost integration with Pareto plots | 1-2h | Feed empirical costs to Pareto analysis |

---

## 8. Testing Strategy

**Estimated effort: 5-8 ideal hours**

### 8.1 Unit Tests

- **Scoring logic** (`tests/eval/test_scoring.py`):
  - Precision/recall/F1 calculation with known inputs
  - Edge cases: zero TP (precision undefined), zero FP (precision = 1.0), empty ground truth
  - Per-CWE breakdown correctness
  - Partial match scoring (source matches, sink differs)

- **Matching logic** (`tests/eval/test_matching.py`):
  - Exact match (same file, same CWE, same source/sink)
  - Partial match (same file and CWE, different sink)
  - No match (different file or CWE)
  - Normalization (whitespace, casing)

- **Ground truth loader** (`tests/eval/test_ground_truth.py`):
  - YAML parsing and validation
  - Schema enforcement (required fields, enum values)
  - Duplicate ID detection

### 8.2 Integration Tests

- **Scoring integration**: create a mock pipeline output and a mock ground truth set with known expected scores. Run `scoring.py` and verify output matches expected values exactly.
- **Baseline integration**: use pre-recorded Semgrep JSON output (checked into test fixtures) to test the normalizer and scorer without requiring Semgrep installation.

### 8.3 Milestones

| Milestone | Effort | Deliverable |
|-----------|--------|-------------|
| M5.8a: Unit tests (scoring, matching, loader) | 3-4h | Full unit test coverage |
| M5.8b: Integration tests (scorer, baselines) | 2-4h | End-to-end scoring test with fixtures |

---

## 9. Milestones Summary

| # | Milestone | Effort (ideal hours) | Dependencies |
|---|-----------|---------------------|--------------|
| M5.2 | Ground truth dataset curation | 25-35h | None (can start immediately) |
| M5.3 | Scoring methodology | 12-15h | M5.2 (needs schema) |
| M5.4 | Baseline integrations | 20-25h | M5.2, M5.3 |
| M5.5 | Pareto frontier plotting | 8-12h | M5.3, M5.4, Phase 4 (model routing) |
| M5.6 | Regression testing | 10-15h | M5.2, M5.3 |
| M5.7 | Cost tracking and reporting | 5-8h | M5.3, Phase 4 (trace logs) |
| M5.8 | Testing | 5-8h | M5.2, M5.3 |

**Total: 85-118 ideal hours** (target: 80-120h)

### Critical Path

```
M5.2 (ground truth) -> M5.3 (scoring) -> M5.4 (baselines) -> M5.5 (Pareto)
                                      \-> M5.6 (regression)
                                      \-> M5.7 (cost tracking)
                                      \-> M5.8 (testing)
```

M5.2 (ground truth curation) is the longest single milestone and has no dependencies -- start it on day one. M5.3 depends only on M5.2's schema (not the full dataset), so it can start as soon as the schema is defined. M5.4, M5.6, M5.7, and M5.8 parallelize after M5.3.

---

## 10. Phase Dependencies

| Relationship | Phase | Notes |
|-------------|-------|-------|
| **Blocked by** | Phase 1 (Taint Analysis) | Needs a working taint analyzer to produce findings for scoring |
| **Blocked by** | Phase 2 (Verification) | Needs exploit verification to produce confirmed findings |
| **Partially blocked by** | Phase 4 (LLM Orchestration) | Needs model routing for multi-config Pareto evaluation. However, scoring methodology and ground truth curation are independent of Phase 4. |
| **Not blocked by** | Phase 3 (Regulatory Engine) | Regulatory mapping evaluation can be added as a separate scoring module later |
| **Blocks** | Phase 6 (Release) | v1 release requires eval results demonstrating the precision claim (>= 0.80 precision on the ground truth dataset) |
| **Feeds into** | Phase 4 (LLM Orchestration) | Calibration data for ensemble temperature scaling; performance estimates for cost-aware optimizer |

**Recommended start:** Begin M5.2 (ground truth curation) immediately, in parallel with Phase 1 development. The ground truth dataset is the longest lead-time item and has zero code dependencies. The schema can be defined and entries can be researched/documented before any Piranesi code exists.

**Release gate:** Phase 6 cannot proceed until the eval harness demonstrates:
1. Piranesi precision >= 0.80 on the full ground truth dataset
2. Piranesi precision > Semgrep precision on the same dataset
3. Piranesi precision > LLM-only baseline precision on the same dataset
4. Pareto frontier shows mixed-model config achieves >= 90% of all-frontier precision at <= 50% of cost
