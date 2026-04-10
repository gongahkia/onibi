# Phase 36: ISO 27001:2022 + NIST CSF 2.0 + CIS Benchmarks

**Estimated effort: 60-75 ideal hours**
**Blocked by: Phase 3 (rule engine), Phase 35 (SOC 2/PCI-DSS — shared evidence infrastructure)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.5.0**

---

## 1. Phase Overview

Phase 36 completes Piranesi's compliance coverage with three additional frameworks: ISO 27001:2022 (Annex A controls), NIST Cybersecurity Framework 2.0 (functions/categories/subcategories), and CIS Controls v8 for application security. Combined with existing frameworks (PDPA, MAS TRM, GDPR, CCPA, HIPAA, NIS2, EU AI Act) and Phase 35 (SOC 2, PCI-DSS), this brings total coverage to 10 regulatory/compliance frameworks.

ISO 27001 is the global standard for information security management systems (ISMS). NIST CSF 2.0 is the US government's cybersecurity framework, widely adopted by private sector. CIS Controls v8 provides prescriptive security benchmarks. Together, these three frameworks cover the remaining enterprise compliance requirements.

Implementation reuses all Phase 35 infrastructure: TOML rules, evidence generation, compliance gap reports, `BooleanFactTrue` scope guards.

---

## 2. ISO 27001:2022 Annex A Mapping

**Estimated effort: 20-25h**

### 2.1 Scope

ISO 27001:2022 restructured Annex A into 4 themes (Organizational, People, Physical, Technological) with 93 controls. Piranesi maps CWE findings to the Technological controls (A.8.x) most relevant to application security. The scan itself serves as evidence for SDL-related controls.

### 2.2 CWE-to-Annex A Mapping

| Annex A Control | Control Description | Mapped CWEs | Rationale |
|-----------------|-------------------|-------------|-----------|
| A.8.3 | Information Access Restriction | CWE-287 (Improper Auth), CWE-306 (Missing Auth), CWE-639 (IDOR), CWE-284 (Improper Access Control), CWE-862 (Missing AuthZ) | Access restriction failures at application layer |
| A.8.4 | Access to Source Code | CWE-798 (Hardcoded Credentials), CWE-540 (Source Code in Error), CWE-615 (Comments with Sensitive Info) | Secrets in code = source code access control failure |
| A.8.7 | Protection Against Malware | CWE-502 (Deserialization), CWE-94 (Code Injection), CWE-95 (Eval Injection), CWE-829 (Untrusted Functionality) | Arbitrary code execution vectors |
| A.8.8 | Management of Technical Vulnerabilities | All SCA findings (CVEs), all findings severity >= HIGH | Known vulnerabilities unpatched = vulnerability management gap |
| A.8.9 | Configuration Management | CWE-16 (Configuration), CWE-942 (Permissive CORS), CWE-693 (Protection Mechanism Failure), CWE-1004 (Missing HttpOnly) | Security misconfiguration findings |
| A.8.12 | Data Leakage Prevention | CWE-319 (Cleartext Transmission), CWE-22 (Path Traversal), CWE-200 (Information Exposure), CWE-532 (Sensitive Data in Logs) | Data exposed via insecure channels |
| A.8.24 | Use of Cryptography | CWE-327 (Broken Crypto), CWE-326 (Inadequate Encryption Strength), CWE-338 (Weak PRNG), CWE-328 (Reversible One-Way Hash) | Cryptographic implementation weaknesses |
| A.8.25 | Secure Development Lifecycle | (meta) Piranesi scan results | Scan execution = SDL evidence; findings = SDL maturity indicator |
| A.8.26 | Application Security Requirements | OWASP Top 10 mapping coverage | Coverage gaps indicate missing security requirements |
| A.8.28 | Secure Coding | CWE-89 (SQLi), CWE-79 (XSS), CWE-78 (CmdInj), CWE-20 (Improper Input Validation) | Injection + input validation = secure coding practice failures |

### 2.3 Rules

18 rules total:

| Rule ID | Annex A | Preconditions | Conclusion |
|---------|---------|---------------|------------|
| `iso27001_a8_3_auth_bypass` | A.8.3 | CWE ∈ {287, 306} | Information access restriction failure — authentication bypass |
| `iso27001_a8_3_idor` | A.8.3 | CWE ∈ {639, 862} | Information access restriction failure — authorization bypass |
| `iso27001_a8_3_access_control` | A.8.3 | CWE ∈ {284} | Information access restriction failure — broad access control gap |
| `iso27001_a8_4_hardcoded_secrets` | A.8.4 | CWE ∈ {798} | Source code access control failure — credentials embedded in source |
| `iso27001_a8_4_source_exposure` | A.8.4 | CWE ∈ {540, 615} | Source code access control failure — sensitive info in source artifacts |
| `iso27001_a8_7_deserialization` | A.8.7 | CWE ∈ {502} | Malware protection gap — unsafe deserialization allows arbitrary execution |
| `iso27001_a8_7_code_injection` | A.8.7 | CWE ∈ {94, 95, 829} | Malware protection gap — code injection vector |
| `iso27001_a8_8_known_cve` | A.8.8 | SCA finding with CVE | Technical vulnerability management failure — known CVE unpatched |
| `iso27001_a8_8_high_severity` | A.8.8 | Any finding, severity >= HIGH | Technical vulnerability management failure — high-severity finding unaddressed |
| `iso27001_a8_9_misconfig` | A.8.9 | CWE ∈ {16, 942, 693} | Configuration management failure — security misconfiguration |
| `iso27001_a8_9_cookie_flags` | A.8.9 | CWE ∈ {1004} | Configuration management failure — missing cookie security flags |
| `iso27001_a8_12_cleartext` | A.8.12 | CWE ∈ {319} | Data leakage risk — cleartext transmission |
| `iso27001_a8_12_path_traversal` | A.8.12 | CWE ∈ {22} | Data leakage risk — path traversal enables unauthorized data access |
| `iso27001_a8_12_info_exposure` | A.8.12 | CWE ∈ {200, 532} | Data leakage risk — sensitive information exposed |
| `iso27001_a8_24_weak_crypto` | A.8.24 | CWE ∈ {327, 326} | Cryptography control failure — weak or broken algorithm |
| `iso27001_a8_24_weak_prng` | A.8.24 | CWE ∈ {338, 328} | Cryptography control failure — weak randomness or reversible hash |
| `iso27001_a8_26_owasp_gap` | A.8.26 | OWASP Top 10 category with 0 detection rules | Application security requirements gap — uncovered attack category |
| `iso27001_a8_28_injection` | A.8.28 | CWE ∈ {89, 79, 78, 20} | Secure coding failure — injection or input validation deficiency |

### 2.4 Implementation

- `rules/iso27001.toml` — 18 rules
- `src/piranesi/legal/rules/iso27001.py` — loader
- Add `RegulatoryFramework.ISO_27001 = "ISO_27001"` enum value
- Wire into `rules/__init__.py` and `memo.py` auto-discovery
- Evidence generation reuses Phase 35 `evidence.py` infrastructure

### 2.5 TOML Rule Example

```toml
[rules.iso27001_a8_24_weak_crypto]
framework = "ISO_27001"
control_ref = "A.8.24"
control_name = "Use of Cryptography"
preconditions = [
    {type = "vuln_class", cwes = ["CWE-327", "CWE-326"]},
]
conclusion = "ISO 27001 A.8.24 control gap: weak or broken cryptographic algorithm in use. The organization shall define and implement rules for the effective use of cryptography, including cryptographic key management."
consequence = "remediate"
evidence_template = "iso27001_a8_finding"
severity_weight = 3
```

---

## 3. NIST CSF 2.0 Mapping

**Estimated effort: 18-22h**

### 3.1 Scope

NIST CSF 2.0 (released February 2024) organizes cybersecurity outcomes into 6 Functions: GOVERN, IDENTIFY, PROTECT, DETECT, RESPOND, RECOVER. Piranesi maps to 4 of 6 functions (GOVERN and RECOVER are organizational/process-level, outside SAST scope).

### 3.2 Function-Level Mapping

| Function | Piranesi Mapping | Evidence Type |
|----------|------------------|---------------|
| GOVERN (GV) | Not directly mapped — organizational governance | N/A |
| IDENTIFY (ID) | Asset inventory: scanned files, packages, dependencies, data flows | Scan metadata |
| PROTECT (PR) | Access control, data security, crypto, information protection findings | Finding-based |
| DETECT (DE) | Scan execution = continuous monitoring evidence | Meta-evidence |
| RESPOND (RS) | Patch generation = response capability evidence | Meta-evidence |
| RECOVER (RC) | Not directly mapped — recovery planning is operational | N/A |

### 3.3 Subcategory Mappings

| CSF Subcategory | Description | Mapped CWEs / Evidence |
|-----------------|-------------|----------------------|
| ID.AM-1 | Inventories of hardware/software | Scanned file inventory, dependency list |
| ID.AM-2 | Software platforms/applications inventoried | Detected frameworks (Express, Fastify, NestJS, etc.) |
| ID.RA-1 | Vulnerabilities identified | All findings = vulnerability identification evidence |
| ID.RA-2 | Cyber threat intelligence received | CVE database integration via SCA |
| PR.AA-1 | Identities and credentials managed | CWE-287, CWE-306, CWE-798, CWE-521 |
| PR.AA-2 | Identities proofed/bound to credentials | CWE-287, CWE-384 (Session Fixation) |
| PR.DS-1 | Data-at-rest protected | CWE-312 (Cleartext Storage), CWE-327, CWE-326 |
| PR.DS-2 | Data-in-transit protected | CWE-319, CWE-295, CWE-523 |
| PR.DS-10 | Data-in-use protected | CWE-200 (Information Exposure), CWE-532 |
| PR.PS-1 | Configuration management practices established | CWE-16, CWE-942, CWE-693 |
| DE.CM-1 | Networks monitored | (meta) Piranesi incremental scan + watch mode |
| DE.CM-6 | External service provider activity monitored | SCA dependency monitoring |
| DE.AE-2 | Potentially adverse events analyzed | Finding triage + severity assessment |
| RS.MA-1 | Incidents managed | Patch generation for confirmed findings |
| RS.MI-1 | Incidents contained | Auto-generated patches = containment capability |

### 3.4 Rules

15 rules total:

| Rule ID | CSF Subcategory | Preconditions | Conclusion |
|---------|-----------------|---------------|------------|
| `nist_csf_pr_aa_1_auth` | PR.AA-1 | CWE ∈ {287, 306} | Identity/credential management gap — authentication weakness |
| `nist_csf_pr_aa_1_secrets` | PR.AA-1 | CWE ∈ {798} | Identity/credential management gap — hardcoded credentials |
| `nist_csf_pr_aa_1_password` | PR.AA-1 | CWE ∈ {521} | Identity/credential management gap — weak password policy |
| `nist_csf_pr_aa_2_session` | PR.AA-2 | CWE ∈ {384} | Identity binding gap — session fixation |
| `nist_csf_pr_ds_1_storage` | PR.DS-1 | CWE ∈ {312, 327, 326} | Data-at-rest protection gap — cleartext or weak encryption |
| `nist_csf_pr_ds_2_transit` | PR.DS-2 | CWE ∈ {319, 295, 523} | Data-in-transit protection gap — cleartext or invalid cert |
| `nist_csf_pr_ds_10_in_use` | PR.DS-10 | CWE ∈ {200, 532} | Data-in-use protection gap — information exposure |
| `nist_csf_pr_ps_1_config` | PR.PS-1 | CWE ∈ {16, 942, 693} | Configuration management gap — security misconfiguration |
| `nist_csf_id_ra_1_vuln` | ID.RA-1 | Any finding severity >= HIGH | Vulnerability identification — high-severity finding detected |
| `nist_csf_id_ra_2_cve` | ID.RA-2 | SCA finding with CVE | Threat intelligence integration — known CVE in dependency |
| `nist_csf_de_cm_6_deps` | DE.CM-6 | SCA finding, outdated dependency | External service monitoring gap — outdated third-party component |
| `nist_csf_pr_ip_injection` | PR.IP (mapped to PR.DS) | CWE ∈ {89, 79, 78} | Information protection gap — injection vulnerability |
| `nist_csf_pr_ip_input_val` | PR.IP (mapped to PR.DS) | CWE ∈ {20} | Information protection gap — improper input validation |
| `nist_csf_pr_ac_idor` | PR.AA (mapped) | CWE ∈ {639, 862} | Access control gap — object-level authorization missing |
| `nist_csf_pr_ac_escalation` | PR.AA (mapped) | CWE ∈ {269} (Improper Privilege Mgmt) | Access control gap — privilege escalation vector |

### 3.5 Implementation

- `rules/nist_csf.toml` — 15 rules
- `src/piranesi/legal/rules/nist_csf.py` — loader
- Add `RegulatoryFramework.NIST_CSF = "NIST_CSF"` enum value
- Meta-evidence rules (DE.CM-1, RS.MA-1) produce evidence artifacts from scan execution metadata rather than individual findings

### 3.6 Meta-Evidence Generation

Some NIST CSF mappings are meta-level: Piranesi's existence and execution constitutes evidence. These produce "capability evidence" artifacts:

```json
{
    "framework": "NIST_CSF",
    "subcategory": "DE.CM-1",
    "subcategory_name": "Networks monitored",
    "evidence_type": "capability",
    "capability": "continuous_monitoring",
    "description": "Piranesi SAST tool executed in incremental scan mode, providing continuous code-level monitoring.",
    "scan_date": "2026-04-11T10:30:00Z",
    "scan_mode": "incremental",
    "files_monitored": 142,
    "scan_frequency": "per-commit"
}
```

---

## 4. CIS Controls v8 (Application Security)

**Estimated effort: 8-10h**

### 4.1 Scope

CIS Controls v8 Control Group 16 covers Application Software Security. This is a lightweight mapping — CIS Controls are prescriptive process controls, and Piranesi primarily provides evidence that the controls are being implemented. 8 rules total.

### 4.2 CIS Control 16 Mapping

| CIS Control | Sub-Control | Description | Piranesi Mapping |
|-------------|-------------|-------------|------------------|
| 16.1 | Establish and Maintain a Secure Application Development Process | Process-level | (meta) Piranesi integration in CI/CD = evidence |
| 16.2 | Establish and Maintain a Process to Accept and Address Software Vulnerabilities | Vulnerability intake | (meta) Scan results + triage workflow |
| 16.3 | Perform Root Cause Analysis on Security Vulnerabilities | Root cause | Finding CWE classification = root cause categorization |
| 16.4 | Establish and Manage an Inventory of Third-Party Software Components | SCA | SCA dependency inventory |
| 16.5 | Use Up-to-Date Third-Party Software Components | Patching | SCA findings for outdated deps |
| 16.6 | Establish and Maintain a Severity Rating System and Process | Severity | Finding severity scoring system |
| 16.9 | Train Developers in Application Security Concepts and Secure Coding | Training | (indirect) Gap report identifies training needs per CWE class |
| 16.12 | Implement Code-Level Security Checks | Static analysis | (meta) Piranesi execution = implementation of this control |

### 4.3 Rules

| Rule ID | CIS Control | Preconditions | Conclusion |
|---------|-------------|---------------|------------|
| `cis_16_1_sdl_evidence` | 16.1 | (meta) Scan executed | Secure development process evidence — SAST scan performed |
| `cis_16_3_root_cause` | 16.3 | Any finding with CWE classification | Root cause analysis performed — finding categorized by CWE |
| `cis_16_4_sca_inventory` | 16.4 | SCA module executed | Third-party inventory maintained — dependency scan performed |
| `cis_16_5_outdated_deps` | 16.5 | SCA finding, outdated dependency | Outdated third-party component detected — update required |
| `cis_16_5_known_cve` | 16.5 | SCA finding with CVE | Known-vulnerable third-party component — patch required |
| `cis_16_6_severity_applied` | 16.6 | (meta) Finding has severity rating | Severity rating system in use — finding classified |
| `cis_16_9_training_gap` | 16.9 | >= 3 findings of same CWE class | Training gap indicator — repeated vulnerability pattern suggests developer knowledge gap |
| `cis_16_12_sast_evidence` | 16.12 | (meta) Scan executed | Code-level security check implemented — Piranesi SAST executed |

### 4.4 Implementation

- `rules/cis_v8.toml` — 8 rules
- `src/piranesi/legal/rules/cis_v8.py` — loader
- Add `RegulatoryFramework.CIS_V8 = "CIS_V8"` enum value
- CIS rules are primarily meta-evidence producers; most produce "pass" artifacts when Piranesi runs

---

## 5. Maturity Scoring

**Estimated effort: 8-10h**

### 5.1 Per-Framework Maturity Score

Each framework receives a maturity score (0-5) based on scan results and historical trends:

| Level | Label | Criteria |
|-------|-------|----------|
| 0 | None | No scanning performed (framework not evaluated) |
| 1 | Initial | Scanning active but > 10 critical findings |
| 2 | Developing | Scanning active, <= 10 critical findings, no remediation tracking |
| 3 | Defined | Scanning active, <= 5 critical, baseline established, trend tracking active |
| 4 | Managed | Scanning active, 0 critical, <= 5 high, active remediation (fix rate > 0) |
| 5 | Optimizing | 0 critical, 0 high, all applicable framework controls covered, continuous monitoring (incremental scan) active |

### 5.2 Maturity Calculation

```python
def compute_maturity(framework: str, findings: list[Finding],
                     trends: TrendData | None, config: ScanConfig) -> int:
    critical = count_by_severity(findings, framework, "CRITICAL")
    high = count_by_severity(findings, framework, "HIGH")
    has_baseline = trends is not None and trends.has_baseline
    has_trend = trends is not None and trends.scan_count >= 2
    fix_rate = trends.fix_rate if trends else 0.0
    incremental = config.incremental_mode
    if critical > 10: return 1
    if critical > 0 and not has_baseline: return 2
    if critical <= 5 and has_baseline and has_trend: 
        if critical == 0 and high <= 5 and fix_rate > 0: return 4
        return 3
    if critical == 0 and high == 0 and incremental: return 5
    return 1  # fallback
```

### 5.3 CLI

```
piranesi compliance maturity
piranesi compliance maturity --framework iso27001
piranesi compliance maturity --format json
```

Terminal output:
```
Compliance Maturity Assessment
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Framework       Level  Label        Score
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PDPA            ████░  Managed      4/5
MAS TRM         ███░░  Defined      3/5
GDPR            ████░  Managed      4/5
CCPA            █████  Optimizing   5/5
HIPAA           ██░░░  Developing   2/5
NIS2            ███░░  Defined      3/5
EU AI Act       ███░░  Defined      3/5
SOC 2           ████░  Managed      4/5
PCI-DSS         ███░░  Defined      3/5
ISO 27001       ████░  Managed      4/5
NIST CSF        ████░  Managed      4/5
CIS v8          █████  Optimizing   5/5
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Overall:        ███▓░  3.5/5.0
```

### 5.4 Historical Maturity Tracking

Integration with `trends.py` from Phase 25:
- Store maturity scores per scan in trend data.
- `piranesi compliance maturity --history` shows maturity progression over time.
- Alert when maturity regresses (level drops between consecutive scans).

### 5.5 Implementation

- `src/piranesi/legal/maturity.py` — maturity scoring module
- Integration with `report/compliance.py` for dashboard rendering
- Integration with `trends.py` for historical tracking
- Pydantic model for maturity assessment results

---

## 6. Unified Compliance Dashboard

**Estimated effort: 6-8h**

### 6.1 Updated Framework Coverage

Update `report/compliance.py` to include all 12 frameworks:

| Category | Frameworks |
|----------|------------|
| Privacy/Data Protection | PDPA, GDPR, CCPA, HIPAA |
| Financial Regulation | MAS TRM |
| Cybersecurity Directives | NIS2, EU AI Act |
| Enterprise Security | SOC 2, PCI-DSS |
| Standards/Benchmarks | ISO 27001, NIST CSF, CIS v8 |

### 6.2 Cross-Framework Analysis

Identify findings that impact the most frameworks — these are the highest-priority remediations:

```
Cross-Framework Impact Analysis
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Finding                  Frameworks Affected  Priority
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
CWE-89 users.ts:42       8/12                 CRITICAL
CWE-798 config.ts:15     7/12                 HIGH
CWE-287 auth.ts:8        7/12                 HIGH
CWE-319 api.ts:22        6/12                 HIGH
CWE-327 crypto.ts:5      5/12                 MEDIUM
```

### 6.3 Executive Summary Command

```
piranesi compliance summary --all
```

Output:
```
Piranesi Compliance Summary — 2026-04-11
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Frameworks assessed:  12
Findings total:       23
Critical:             2
High:                 5
Medium:               12
Low:                  4

Framework Coverage:
  Privacy (4/4):      PDPA ✓  GDPR ✓  CCPA ✓  HIPAA ✓
  Financial (1/1):    MAS TRM ✓
  Cyber (2/2):        NIS2 ✓  EU AI Act ✓
  Enterprise (2/2):   SOC 2 ✓  PCI-DSS ✓
  Standards (3/3):    ISO 27001 ✓  NIST CSF ✓  CIS v8 ✓

Overall Maturity:     3.5/5.0 (Defined → Managed)

Top 3 Remediation Priorities:
  1. CWE-89 users.ts:42    — impacts 8 frameworks
  2. CWE-798 config.ts:15  — impacts 7 frameworks
  3. CWE-287 auth.ts:8     — impacts 7 frameworks
```

### 6.4 Implementation

- Update `src/piranesi/report/compliance.py` — add SOC 2, PCI-DSS, ISO 27001, NIST CSF, CIS v8 sections
- Cross-framework overlap computation: for each finding, count how many frameworks flag it
- Executive summary renderer using Rich tables
- `--all` flag aggregates across all frameworks

---

## 7. Testing

**Estimated effort: 10-12h**

### 7.1 ISO 27001 Rule Tests (8 cases)

| Test | Input | Expected Output |
|------|-------|-----------------|
| `test_iso27001_auth_maps_a8_3` | CWE-287 finding | A.8.3 control gap |
| `test_iso27001_secrets_maps_a8_4` | CWE-798 finding | A.8.4 control gap |
| `test_iso27001_deser_maps_a8_7` | CWE-502 finding | A.8.7 control gap |
| `test_iso27001_cve_maps_a8_8` | SCA CVE finding | A.8.8 control gap |
| `test_iso27001_misconfig_maps_a8_9` | CWE-942 finding | A.8.9 control gap |
| `test_iso27001_cleartext_maps_a8_12` | CWE-319 finding | A.8.12 control gap |
| `test_iso27001_weak_crypto_maps_a8_24` | CWE-327 finding | A.8.24 control gap |
| `test_iso27001_injection_maps_a8_28` | CWE-89 finding | A.8.28 control gap |

### 7.2 NIST CSF Rule Tests (7 cases)

| Test | Input | Expected Output |
|------|-------|-----------------|
| `test_nist_csf_auth_maps_pr_aa_1` | CWE-287 finding | PR.AA-1 gap |
| `test_nist_csf_cleartext_maps_pr_ds_2` | CWE-319 finding | PR.DS-2 gap |
| `test_nist_csf_crypto_maps_pr_ds_1` | CWE-327 finding | PR.DS-1 gap |
| `test_nist_csf_injection_maps_pr_ip` | CWE-89 finding | PR.IP gap |
| `test_nist_csf_cve_maps_id_ra_2` | SCA CVE finding | ID.RA-2 evidence |
| `test_nist_csf_meta_detect` | Scan executed | DE.CM-1 capability evidence |
| `test_nist_csf_meta_respond` | Patch generated | RS.MA-1 capability evidence |

### 7.3 CIS v8 Rule Tests (4 cases)

| Test | Input | Expected Output |
|------|-------|-----------------|
| `test_cis_16_5_outdated_dep` | Outdated dependency finding | CIS 16.5 gap |
| `test_cis_16_5_known_cve` | SCA CVE finding | CIS 16.5 gap |
| `test_cis_16_9_training_gap` | 3+ CWE-89 findings | CIS 16.9 training gap indicator |
| `test_cis_16_12_meta_evidence` | Scan executed | CIS 16.12 evidence |

### 7.4 Maturity Score Tests (6 cases)

| Test | Input | Expected Score |
|------|-------|---------------|
| `test_maturity_level_1_many_criticals` | 15 critical findings | Level 1 |
| `test_maturity_level_2_no_baseline` | 5 critical, no baseline | Level 2 |
| `test_maturity_level_3_with_trends` | 3 critical, baseline, trends | Level 3 |
| `test_maturity_level_4_no_critical` | 0 critical, 3 high, fix rate > 0 | Level 4 |
| `test_maturity_level_5_clean` | 0 critical, 0 high, incremental | Level 5 |
| `test_maturity_regression_alert` | Level drops from 4 to 3 | Alert generated |

### 7.5 Cross-Framework Tests (5 cases)

| Test | Validates |
|------|-----------|
| `test_cross_framework_overlap_count` | CWE-89 finding flagged by >= 5 frameworks |
| `test_cross_framework_priority_order` | Highest-overlap finding ranked first |
| `test_unified_dashboard_all_frameworks` | All 12 frameworks appear in summary |
| `test_evidence_schema_iso27001` | ISO 27001 evidence artifact validates |
| `test_evidence_schema_nist_csf` | NIST CSF evidence artifact validates |

---

## 8. Risks

- **ISO 27001 audit scope**: Annex A controls are selected per organization's risk assessment. Not all A.8.x controls apply to every organization. Mitigation: allow per-project framework config to include/exclude specific controls via `piranesi.toml`.
- **NIST CSF abstraction level**: CSF subcategories are outcome-oriented, not prescriptive. Mapping CWEs to outcomes requires interpretive judgment. Mitigation: document mapping rationale, allow TOML rule overrides.
- **CIS Controls meta-evidence**: most CIS rules produce "pass" artifacts when Piranesi runs. This could give a false sense of compliance. Mitigation: clearly label meta-evidence vs. finding-evidence in reports, add disclaimer.
- **Maturity score gaming**: teams could suppress findings to inflate maturity. Mitigation: maturity calculation includes suppressed_ratio; high suppression rate caps maturity at Level 3.
- **Framework version drift**: ISO 27001:2022, NIST CSF 2.0, CIS v8 will eventually be superseded. Mitigation: version-tag all rule files, support multiple versions concurrently.
