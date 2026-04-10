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

## 3. HTML Report with Interactive Taint Visualization

### 3.1 Design Principles

- **Single file**: one self-contained `.html` file, no external dependencies.
- **No JS framework**: vanilla JS + inline CSS. Keep it small (<200KB).
- **Print-friendly**: CSS media queries for print layout.
- **Accessible**: semantic HTML, ARIA labels, keyboard navigation.

### 3.2 Report Structure

```
┌─────────────────────────────────────────────┐
│ Executive Summary Dashboard                  │
│ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │
│ │ Severity  │ │ Top CWEs │ │ Regulatory   │ │
│ │ Donut     │ │ Bar Chart│ │ Coverage     │ │
│ └──────────┘ └──────────┘ └──────────────┘ │
├─────────────────────────────────────────────┤
│ Filters: [Severity ▼] [CWE ▼] [File ▼]    │
├─────────────────────────────────────────────┤
│ Finding Card #1                              │
│ ┌─ Source ─────────────────────────────────┐│
│ │ req.body.userId  (line 12, app.ts)       ││
│ └──────────┬──────────────────────────────┘│
│            │ TaintStep: assignment           │
│            ▼                                 │
│ ┌─ Sink ───────────────────────────────────┐│
│ │ db.query(sql)    (line 25, db.ts)        ││
│ └──────────────────────────────────────────┘│
│ Severity: HIGH | CWE-89 | Confidence: 0.92  │
│ [Show Patch] [Show Legal] [Copy Markdown]    │
├─────────────────────────────────────────────┤
│ Finding Card #2 ...                          │
└─────────────────────────────────────────────┘
```

### 3.3 Taint Path Visualization

Each taint step rendered as a flow node:

```html
<div class="taint-flow">
    <div class="taint-node taint-source">
        <code>req.body.userId</code>
        <span class="location">app.ts:12</span>
    </div>
    <div class="taint-edge">→ assignment</div>
    <div class="taint-node">
        <code>const userId = req.body.userId</code>
        <span class="location">app.ts:12</span>
    </div>
    <div class="taint-edge">→ call_arg</div>
    <div class="taint-node taint-sink">
        <code>db.query(`SELECT * FROM users WHERE id = ${userId}`)</code>
        <span class="location">db.ts:25</span>
    </div>
</div>
```

### 3.4 Client-Side Filtering

Vanilla JS filtering without any framework:
```javascript
document.querySelectorAll('[data-filter]').forEach(el => {
    el.addEventListener('change', () => {
        const severity = getFilter('severity');
        const cwe = getFilter('cwe');
        document.querySelectorAll('.finding-card').forEach(card => {
            card.hidden = !matchesFilters(card, severity, cwe);
        });
        updateCounts();
    });
});
```

### 3.5 Charts

SVG-based charts (no D3, no Chart.js):
- Severity donut chart: 4 arcs (critical/high/medium/low).
- CWE bar chart: horizontal bars for top 5 CWEs.
- Both generated server-side in Python, embedded as inline SVG.

### 3.6 CLI

```
piranesi report <findings.json> --format html --output report.html
piranesi run <dir> --format html
```

## 4. Compliance Dashboard Report

### 4.1 Regulatory Coverage Matrix

```
                GDPR  CCPA  HIPAA  NIS2  PDPA  EU AI  MAS TRM
Finding #1       ●     ●     -      ●     ●     -      -
Finding #2       ●     -     ●      -     -     -      -
Finding #3       -     -     -      -     ●     -      ●
────────────────────────────────────────────────────────────
Affected:        2     1     1      1     2     0      1
```

### 4.2 Per-Framework Section

For each regulatory framework with findings:
- **Total affected findings**: count + severity breakdown.
- **Key obligations triggered**: section references (e.g., "GDPR Art. 32(1)(b)").
- **Required actions**: specific remediation steps.
- **Notification timeline**: if applicable (e.g., "72 hours" for GDPR breach notification).
- **Penalty exposure**: maximum penalty range.
- **Enforcement precedents**: relevant enforcement actions.

### 4.3 Gap Analysis

OWASP Top 10 coverage assessment:
```
OWASP Top 10 2021 Coverage:
  A01 Broken Access Control     - 0 findings (⚠ no detection rules)
  A02 Cryptographic Failures    - 0 findings (⚠ no detection rules)
  A03 Injection                 - 4 findings (✓ CWE-89, CWE-79, CWE-78)
  A04 Insecure Design           - 0 findings (⚠ architectural, not detectable)
  A05 Security Misconfiguration - 2 findings (✓ CWE-942, CWE-693)
  A06 Vulnerable Components     - 3 findings (✓ SCA)
  A07 Auth Failures             - 0 findings (⚠ no detection rules)
  A08 Data Integrity            - 1 finding  (✓ CWE-502)
  A09 Logging Failures          - 0 findings (⚠ no detection rules)
  A10 SSRF                      - 1 finding  (✓ CWE-918)
```

### 4.4 Attestation Template

Pre-filled compliance attestation:
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

### 4.5 CLI

```
piranesi report <findings.json> --format compliance
piranesi run <dir> --format compliance
```

## 5. Tests

### Trend Analysis
1. Create 5 scan artifacts with varying finding counts and CWEs.
2. Verify trend computation: total, by-severity, fix rate, MTTF.
3. Verify `--since`/`--until` filtering.
4. Verify trend alerts trigger on >20% increase.
5. Verify JSON output schema.

### HTML Report
1. Generate HTML from fixture findings.
2. Parse with `html.parser` — verify well-formed HTML.
3. Verify finding count matches input.
4. Verify SVG charts present.
5. Verify client-side filter JS included.
6. Verify file size <200KB.

### Compliance Report
1. Generate compliance report from fixture findings with regulatory data.
2. Verify coverage matrix includes all active frameworks.
3. Verify gap analysis lists all OWASP categories.
4. Verify attestation template filled with correct metadata.
5. Verify disclaimer present.

## 6. Risks

- **HTML report size**: large codebases with many findings may produce large HTML. Mitigation: paginate findings (show 20, "Load more" button).
- **Trend accuracy**: scan-to-scan comparison depends on stable fingerprints. Mitigation: use stable fingerprinting from Phase 15.
- **Compliance completeness**: attestation template is not legally binding. Mitigation: prominent disclaimer, encourage legal review.
