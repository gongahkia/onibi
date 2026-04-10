# Phase 25: Advanced Reporting + Trend Analysis

**Estimated effort: 35-45 ideal hours**
**Blocked by: Phase 15 (baselines), Phase 17 (output formats)**
**Blocks: Nothing (independent feature)**

## 1. Motivation

Current reporting covers single-scan output (JSON, Markdown, SARIF, JUnit, CSV). Production security programs need:
- **Trend analysis**: are we getting more or less secure over time?
- **Interactive reports**: HTML dashboards for security team review.
- **Compliance reports**: regulatory-focused views for auditors.

All outputs remain CLI-generated static files — no server, no SaaS, no database.

## 2. Historical Trend Analysis

### 2.1 Data Source

Piranesi already saves scan artifacts to `--output <dir>`. Each scan produces `detect.json`, `verify.json`, `report.json`. The trend engine reads these over time.

### 2.2 CLI Command

```
piranesi trends <output_dir>
piranesi trends <output_dir> --since 2026-01-01
piranesi trends <output_dir> --until 2026-04-01
piranesi trends <output_dir> --format json     # machine-readable
piranesi trends <output_dir> --format terminal  # sparkline charts (default)
```

### 2.3 Metrics Computed

| Metric | Description |
|--------|-------------|
| `total_findings` | Total findings per scan |
| `by_severity` | Breakdown: critical / high / medium / low / informational |
| `by_cwe` | Top CWE classes over time |
| `fix_rate` | Findings resolved between consecutive scans |
| `mean_time_to_fix` | Average days from first detection to resolution |
| `new_finding_velocity` | New findings introduced per scan |
| `suppressed_ratio` | Suppressed / total ratio |
| `false_positive_rate` | FP / total (when triage data available) |
| `confirmed_rate` | Docker-confirmed / total |
| `llm_cost_per_scan` | LLM spend per scan |

### 2.4 Terminal Output

```
Piranesi Trend Report (12 scans, 2026-01-15 → 2026-04-10)

Findings:  42 ▁▂▃▄▅▆▅▄▃▃▂▂  18  (-57%)
Critical:   3 ▃▃▃▂▂▁▁▁▁▁▁▁   0  (-100%)
High:      12 ▃▄▅▅▄▃▃▂▂▂▁▁   5  (-58%)
Medium:    20 ▃▃▄▅▅▅▅▄▃▃▃▃  10  (-50%)
Low:        7 ▂▂▂▃▃▃▃▃▃▃▃▃   3  (-57%)

Fix rate:      4.2 findings/scan
MTTF:          8.3 days
New velocity:  1.8 findings/scan
FP rate:       12% → 6% (improved)
LLM cost:      $2.40/scan avg
```

### 2.5 Trend Alerts

Automatic warnings when:
- Finding count increases >20% between consecutive scans.
- A new critical finding appears.
- Fix rate drops below 1.0 (accumulating debt).
- LLM cost exceeds budget warning threshold.

### 2.6 JSON Output

```json
{
    "period": {"start": "2026-01-15", "end": "2026-04-10"},
    "scans": 12,
    "series": {
        "total_findings": [42, 44, 48, 50, 45, 40, 35, 30, 25, 22, 20, 18],
        "by_severity": {
            "critical": [3, 3, 3, 2, 2, 1, 1, 1, 1, 1, 1, 0],
            ...
        },
        ...
    },
    "summary": {
        "total_reduction": -57,
        "fix_rate": 4.2,
        "mttf_days": 8.3,
        "alerts": ["critical findings eliminated"]
    }
}
```

## 3. Interactive TUI Report Viewer

### 3.1 Design Principles

- **CLI-native**: Rich TUI via `textual` or plain Rich Live display. No browser, no HTML.
- **Keyboard-driven**: vim-style navigation (j/k scroll, / search, q quit, Enter expand).
- **Pipe-friendly**: `--format tui` for interactive, all other formats remain pipe-safe stdout.

### 3.2 TUI Layout

```
┌─ Piranesi Report ─────────────────────────────────────────┐
│ Summary: 11 findings (3 HIGH, 6 MED, 2 LOW) | $2.40 LLM  │
├───────────────────────────────────────────────────────────┤
│ [Sev▼] [CWE▼] [File▼]  /search                          │
├───────────────────────────────────────────────────────────┤
│ ▸ HIGH  CWE-89  app.ts:12→db.ts:25   req.body → query() │
│   MED   CWE-79  api.ts:8→api.ts:15   req.query → send() │
│   MED   CWE-78  run.ts:3→run.ts:9    req.body → exec()  │
│   ...                                                     │
├───────────────────────────────────────────────────────────┤
│ ┌─ Finding #1 ──────────────────────────────────────────┐ │
│ │ CWE-89: SQL Injection                                 │ │
│ │ Source: req.body.userId (app.ts:12)                   │ │
│ │   → assignment: const userId = req.body.userId        │ │
│ │   → call_arg: db.query(`SELECT ... ${userId}`)        │ │
│ │ Sink: db.query (db.ts:25)                             │ │
│ │ Confidence: 0.92 | Confirmed: yes                     │ │
│ │ [p]atch  [l]egal  [r]eproducer  [s]uppress            │ │
│ └───────────────────────────────────────────────────────┘ │
└─ j/k:nav  Enter:expand  /:search  f:filter  q:quit ──────┘
```

### 3.3 Keybindings

| Key | Action |
|-----|--------|
| `j/k` or `↓/↑` | Navigate findings |
| `Enter` | Expand/collapse finding detail |
| `/` | Search findings (CWE, file, sink) |
| `f` | Cycle filter (severity, CWE, file) |
| `p` | Show patch diff for selected finding |
| `l` | Show legal memo for selected finding |
| `r` | Show reproducer script |
| `s` | Suppress finding (prompts for reason) |
| `e` | Export current view to markdown |
| `q` | Quit |

### 3.4 Non-Interactive Fallback

When stdout is not a TTY (piped), `--format tui` falls back to `--format markdown`. This keeps scripts and CI pipelines working.

### 3.5 CLI

```
piranesi report <findings.json> --format tui    # interactive TUI
piranesi run <dir> --format tui
```

### 3.6 Dependencies

Add to `[project.optional-dependencies]`:
```toml
tui = ["textual>=0.50.0"]
```

Install: `uv pip install piranesi[tui]`. Falls back to Rich tables if textual not installed.

## 4. Compliance Dashboard (CLI/TUI)

### 4.1 Terminal Compliance Table

Rendered via Rich tables to stdout (pipe-safe, no TUI required):

```
piranesi report <findings.json> --format compliance
```

Output:
```
Regulatory Coverage Matrix
                 GDPR  CCPA  HIPAA  NIS2  PDPA  EU AI  MAS TRM
Finding #1        *     *     -      *     *     -      -
Finding #2        *     -     *      -     -     -      -
Finding #3        -     -     -      -     *     -      *
─────────────────────────────────────────────────────────────
Affected:         2     1     1      1     2     0      1
```

### 4.2 Per-Framework Section

For each regulatory framework with findings:
- **Total affected findings**: count + severity breakdown.
- **Key obligations triggered**: section references (e.g., "GDPR Art. 32(1)(b)").
- **Required actions**: specific remediation steps.
- **Notification timeline**: if applicable (e.g., "72 hours" for GDPR breach notification).
- **Penalty exposure**: maximum penalty range.
- **Enforcement precedents**: relevant enforcement actions.

All rendered as Rich panels/tables to terminal.

### 4.3 Gap Analysis

OWASP Top 10 coverage assessment (Rich table):
```
OWASP Top 10 2021 Coverage:
  A01 Broken Access Control     - 0 findings (! no detection rules)
  A02 Cryptographic Failures    - 0 findings (! no detection rules)
  A03 Injection                 - 4 findings (+ CWE-89, CWE-79, CWE-78)
  A04 Insecure Design           - 0 findings (! architectural, not detectable)
  A05 Security Misconfiguration - 2 findings (+ CWE-942, CWE-693)
  A06 Vulnerable Components     - 3 findings (+ SCA)
  A07 Auth Failures             - 0 findings (! no detection rules)
  A08 Data Integrity            - 1 finding  (+ CWE-502)
  A09 Logging Failures          - 0 findings (! no detection rules)
  A10 SSRF                      - 1 finding  (+ CWE-918)
```

### 4.4 Attestation Template

`piranesi report <findings.json> --format compliance --attestation` outputs a pre-filled Markdown attestation to stdout:

```markdown
# Security Scan Attestation

**Project:** {project_name}
**Scan Date:** {timestamp}
**Tool:** Piranesi v{version}
**Scope:** {file_count} files across {languages}

## Summary
- {total_findings} findings detected
- {confirmed} confirmed via exploit verification
- {suppressed} suppressed (with documented rationale)
- {fixed} with auto-generated patches

## Regulatory Coverage
{frameworks_assessed}

## Limitations
This scan covers static analysis of source code only. It does not assess:
- Runtime configuration
- Infrastructure security
- Business logic flaws
- Authentication/authorization design

DISCLAIMER: This analysis is informational only. It is not legal advice.
Consult qualified legal counsel for regulatory compliance decisions.
```

Redirect to file: `piranesi report ... --format compliance --attestation > attestation.md`

### 4.5 TUI Compliance Mode

When running `piranesi report --format compliance --tui` (requires `piranesi[tui]`):
- Interactive compliance dashboard in terminal.
- Navigate by framework (tab between GDPR/CCPA/HIPAA/etc.).
- Expand per-framework obligations, drill into individual findings.
- Same keybindings as Section 3.3.

### 4.6 CLI

```
piranesi report <findings.json> --format compliance              # Rich tables to stdout
piranesi report <findings.json> --format compliance --tui        # interactive TUI
piranesi report <findings.json> --format compliance --attestation # Markdown attestation
piranesi run <dir> --format compliance
```

## 5. Tests

### Trend Analysis
1. Create 5 scan artifacts with varying finding counts and CWEs.
2. Verify trend computation: total, by-severity, fix rate, MTTF.
3. Verify `--since`/`--until` filtering.
4. Verify trend alerts trigger on >20% increase.
5. Verify JSON output schema.

### TUI Report
1. Verify non-TTY fallback renders markdown (no TUI crash).
2. Verify finding count in output matches input.
3. Verify keybindings dispatch correct actions (mock textual app).
4. Verify `--format tui` without textual installed falls back to Rich tables.

### Compliance Report
1. Generate compliance report from fixture findings with regulatory data.
2. Verify coverage matrix includes all active regulatory frameworks.
3. Verify gap analysis lists all OWASP categories.
4. Verify attestation template filled with correct metadata.
5. Verify disclaimer present.

## 6. Risks

- **Trend accuracy**: scan-to-scan comparison depends on stable fingerprints. Mitigation: use stable fingerprinting from Phase 15.
- **TUI compatibility**: terminal emulators vary. Mitigation: textual handles most terminals; fallback to Rich tables.
- **Compliance completeness**: attestation template is not legally binding. Mitigation: prominent disclaimer, encourage legal review.
