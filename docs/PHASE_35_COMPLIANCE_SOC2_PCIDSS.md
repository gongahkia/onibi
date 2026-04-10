# Phase 35: SOC 2 Type II + PCI-DSS v4.0 Compliance

**Estimated effort: 55-70 ideal hours**
**Blocked by: Phase 3 (rule engine), Phase 11 (regulatory expansion pattern)**
**Blocks: Phase 36 (ISO/NIST — shared compliance infrastructure)**
**Target milestone: v0.5.0**

---

## 1. Phase Overview

Piranesi's regulatory engine covers privacy and data-protection frameworks (PDPA, MAS TRM, GDPR, CCPA, HIPAA, NIS2, EU AI Act). Enterprise customers require compliance evidence for operational security frameworks — specifically SOC 2 Type II and PCI-DSS v4.0. These frameworks focus on security controls rather than data-protection obligations, requiring a different mapping strategy: CWE findings map to control gaps rather than statutory violations.

SOC 2 applies broadly to any SaaS or service organization. PCI-DSS applies to entities that store, process, or transmit cardholder data. Both are audit-driven: evidence artifacts matter as much as finding counts.

Implementation follows the established pattern: TOML rule file + Python loader + evidence generation + integration tests. The forward-chaining inference engine from Phase 3 is reused — only the rules and evidence templates are new.

---

## 2. SOC 2 Trust Services Criteria Mapping

**Estimated effort: 18-22h**

### 2.1 Scope

SOC 2 Type II evaluates an organization's controls against the AICPA Trust Services Criteria (TSC). Piranesi maps CWE findings to TSC control gaps, providing continuous evidence of control effectiveness (or lack thereof). The focus is on Common Criteria (CC) controls relevant to application security.

### 2.2 CWE-to-TSC Mapping

| TSC Control | Control Description | Mapped CWEs | Rationale |
|-------------|-------------------|-------------|-----------|
| CC6.1 | Logical Access Security | CWE-287 (Improper Auth), CWE-306 (Missing Auth), CWE-639 (IDOR), CWE-284 (Improper Access Control) | Auth bypass = logical access control failure |
| CC6.6 | Restricting External Threats | CWE-89 (SQLi), CWE-79 (XSS), CWE-78 (OS CmdInj), CWE-77 (Command Injection) | Injection = failure to restrict external threat vectors |
| CC6.7 | Restricting Physical/Logical Assets | CWE-22 (Path Traversal), CWE-918 (SSRF), CWE-611 (XXE) | Unauthorized access to internal resources or filesystem |
| CC6.8 | Preventing/Detecting Malware | CWE-502 (Deserialization), CWE-94 (Code Injection), CWE-95 (Eval Injection) | Arbitrary code execution = malware-equivalent risk |
| CC7.1 | Monitoring Activities | CWE-798 (Hardcoded Credentials), CWE-532 (Log Injection), CWE-117 (Log Forging) | Secrets exposure + logging gaps undermine monitoring |
| CC7.2 | Monitoring System Components | CWE-209 (Error Info Leak), CWE-200 (Information Exposure) | Information leakage indicates insufficient monitoring of what systems expose |
| CC8.1 | Change Management | Dependency CVEs (SCA findings) | Known-vulnerable dependencies indicate inadequate change management |

### 2.3 Rules

15 rules total, encoded in TOML:

| Rule ID | TSC Control | Preconditions | Conclusion |
|---------|-------------|---------------|------------|
| `soc2_cc6_1_auth_bypass` | CC6.1 | CWE ∈ {287, 306} | Logical access control gap — authentication mechanism missing or bypassable |
| `soc2_cc6_1_idor` | CC6.1 | CWE ∈ {639} | Authorization gap — object-level access control absent |
| `soc2_cc6_1_access_control` | CC6.1 | CWE ∈ {284} | Broad access control deficiency |
| `soc2_cc6_6_sqli` | CC6.6 | CWE ∈ {89} | External threat not restricted — SQL injection |
| `soc2_cc6_6_xss` | CC6.6 | CWE ∈ {79} | External threat not restricted — cross-site scripting |
| `soc2_cc6_6_cmdi` | CC6.6 | CWE ∈ {78, 77} | External threat not restricted — command injection |
| `soc2_cc6_7_path_traversal` | CC6.7 | CWE ∈ {22} | Logical asset boundary violated — path traversal |
| `soc2_cc6_7_ssrf` | CC6.7 | CWE ∈ {918} | Logical asset boundary violated — SSRF |
| `soc2_cc6_8_deserialization` | CC6.8 | CWE ∈ {502} | Malware prevention gap — unsafe deserialization |
| `soc2_cc6_8_code_injection` | CC6.8 | CWE ∈ {94, 95} | Malware prevention gap — code injection |
| `soc2_cc7_1_hardcoded_secrets` | CC7.1 | CWE ∈ {798} | Monitoring gap — hardcoded credentials bypass audit trail |
| `soc2_cc7_1_log_integrity` | CC7.1 | CWE ∈ {532, 117} | Monitoring gap — log injection undermines audit log integrity |
| `soc2_cc7_2_info_leak` | CC7.2 | CWE ∈ {209, 200} | Component monitoring gap — sensitive information exposed |
| `soc2_cc8_1_vulnerable_deps` | CC8.1 | SCA finding with known CVE | Change management gap — known-vulnerable dependency in production |
| `soc2_cc8_1_outdated_deps` | CC8.1 | SCA finding, dependency > 2 major versions behind | Change management gap — significantly outdated dependency |

### 2.4 Implementation

- `rules/soc2.toml` — 15 rules following the `RegulatoryRuleSpec` schema
- `src/piranesi/legal/rules/soc2.py` — loader (same pattern as `pdpa.py`, `ccpa.py`)
- Wire into `rules/__init__.py` and `memo.py` auto-discovery
- Add `RegulatoryFramework.SOC2 = "SOC2"` enum value

### 2.5 TOML Rule Example

```toml
[rules.soc2_cc6_6_sqli]
framework = "SOC2"
control_ref = "CC6.6"
control_name = "Restricting External Threats"
preconditions = [
    {type = "vuln_class", cwes = ["CWE-89"]},
    {type = "severity_gte", threshold = "MEDIUM"},
]
conclusion = "SOC 2 CC6.6 control gap: SQL injection vulnerability allows external threat actors to execute arbitrary SQL. Indicates insufficient input validation controls."
consequence = "remediate"
evidence_template = "soc2_cc6_finding"
severity_weight = 3
```

---

## 3. PCI-DSS v4.0 Mapping

**Estimated effort: 22-28h**

### 3.1 Scope

PCI-DSS v4.0 (effective March 2025) applies to entities that store, process, or transmit cardholder data (CHD) or sensitive authentication data (SAD). Piranesi detects PCI-DSS applicability via heuristic scope detection before applying rules.

### 3.2 Scope Detection Heuristic

PCI-DSS rules only fire when the scanned codebase handles payment data. Detection:

```python
PCI_SCOPE_KEYWORDS = {
    "imports": ["stripe", "braintree", "adyen", "square", "paypal",
                "authorize.net", "@stripe/stripe-js", "razorpay"],
    "identifiers": ["cardNumber", "card_number", "cvv", "cvc",
                     "pan", "expiry_date", "cardholder",
                     "payment_intent", "checkout_session"],
    "api_patterns": [r"/v\d+/charges", r"/v\d+/payments",
                     r"/checkout", r"/billing"],
}
```

If >= 2 keyword hits across the codebase, set `is_payment_processing = True` fact. This gates all PCI-DSS rule evaluation — non-payment code never triggers PCI-DSS findings.

### 3.3 CWE-to-PCI-DSS Mapping

| PCI-DSS Requirement | Description | Mapped CWEs | Rationale |
|---------------------|-------------|-------------|-----------|
| Req 3.4 | Protect Stored Account Data | CWE-327 (Broken Crypto), CWE-326 (Inadequate Encryption Strength), CWE-312 (Cleartext Storage) | Weak/missing encryption of stored CHD |
| Req 4.1 | Protect CHD in Transit | CWE-319 (Cleartext Transmission), CWE-295 (Improper Cert Validation), CWE-523 (Unprotected Transport Creds) | CHD transmitted without encryption |
| Req 6.2.1 | Bespoke/Custom Software Security | CWE-89, CWE-79, CWE-78, CWE-94, CWE-502 | All injection classes in custom code |
| Req 6.2.2 | Software Security Review | All findings with severity >= HIGH | High-severity findings = review gaps |
| Req 6.3.1 | Publicly Known Vulnerabilities | SCA findings with CVE identifiers | Known CVEs in dependencies |
| Req 6.3.2 | Software Inventory Maintenance | SCA findings, outdated dependencies | Dependency inventory gaps |
| Req 6.4.1 | Public-Facing Web App Protection | CWE-79 (XSS), CWE-352 (CSRF), CWE-918 (SSRF) | Web app attack surface unprotected |
| Req 6.4.2 | Automated Technical Solution for Web Apps | CWE-79 (XSS) in public-facing code | Automated XSS prevention missing |
| Req 6.5.1 | Secure Coding Practices — Injection | CWE-89, CWE-79, CWE-78, CWE-77 | OWASP Top 10 injection coverage |
| Req 6.5.2 | Secure Coding Practices — Buffer | CWE-120 (Buffer Overflow), CWE-119 (Memory Corruption) | Memory safety (relevant for native modules) |
| Req 6.5.3 | Secure Coding Practices — Crypto | CWE-327, CWE-326, CWE-338 (Weak PRNG) | Cryptographic implementation flaws |
| Req 6.5.4 | Secure Coding Practices — Comms | CWE-319, CWE-295 | Insecure communication channels |
| Req 6.5.5 | Secure Coding Practices — Error Handling | CWE-209 (Error Info Leak), CWE-200 | Improper error handling exposes data |
| Req 6.5.6 | Secure Coding Practices — High-Risk Vulns | All critical findings | Any identified high-risk vulnerability |
| Req 8.3.1 | Strong Authentication — MFA | CWE-287 (Improper Auth), CWE-384 (Session Fixation) | Authentication mechanism weakness |
| Req 8.3.6 | Authentication Factor Complexity | CWE-521 (Weak Password Requirements) | Password policy enforcement missing |
| Req 10.2.1 | Audit Log Coverage | CWE-778 (Insufficient Logging), logging detection gaps | Audit logging requirements unmet |
| Req 10.2.2 | Audit Log — Actions by Individuals | CWE-117 (Log Forging) | Audit log integrity compromised |
| Req 11.3.1 | Internal Vulnerability Scans | (meta) Piranesi execution = evidence | Piranesi scan satisfies this requirement |
| Req 11.3.2 | External Vulnerability Scans | (meta) Piranesi + SCA = partial evidence | SCA component of scan provides partial coverage |

### 3.4 Rules

20 rules total. Each rule follows the same `RegulatoryRuleSpec` schema with an additional `scope_guard`:

```toml
[rules.pci_dss_req_3_4_weak_crypto]
framework = "PCI_DSS"
control_ref = "Req 3.4"
control_name = "Protect Stored Account Data"
scope_guard = "is_payment_processing"
preconditions = [
    {type = "vuln_class", cwes = ["CWE-327", "CWE-326", "CWE-312"]},
]
conclusion = "PCI-DSS Req 3.4 violation: weak or missing cryptography for stored account data. Primary Account Numbers (PAN) must be rendered unreadable wherever stored."
consequence = "remediate"
evidence_template = "pci_dss_req3_finding"
severity_weight = 4
penalty_note = "Non-compliance may result in fines from $5,000 to $100,000/month from card brands, potential loss of card processing privileges."
```

### 3.5 Implementation

- `rules/pci_dss.toml` — 20 rules with `scope_guard` field
- `src/piranesi/legal/rules/pci_dss.py` — loader + scope detection logic
- `scope_guard` mechanism: new `Predicate` subclass `BooleanFactTrue` that checks a named fact
- Add `RegulatoryFramework.PCI_DSS = "PCI_DSS"` enum value
- Add `is_payment_processing` boolean fact to finding context (set during surface scan)

### 3.6 Scope Guard Predicate

```python
class BooleanFactTrue(Predicate):
    """fires only when a named boolean fact is true in the finding context."""
    finding_field: str = "context_facts"
    fact_name: str  # e.g., "is_payment_processing"
```

This reuses the existing `Predicate` infrastructure. The scope guard prevents false compliance findings for non-payment codebases.

---

## 4. Evidence Generation

**Estimated effort: 8-10h**

### 4.1 Evidence Artifacts

Per SOC 2 control / PCI-DSS requirement, Piranesi generates a structured evidence artifact suitable for audit tool ingestion (e.g., Vanta, Drata, Secureframe, AuditBoard).

### 4.2 Evidence Schema

```json
{
    "framework": "SOC2",
    "control_ref": "CC6.6",
    "control_name": "Restricting External Threats",
    "scan_date": "2026-04-11T10:30:00Z",
    "scan_tool": "piranesi",
    "scan_version": "0.5.0",
    "scope": {
        "files_scanned": 142,
        "languages": ["typescript", "python"],
        "project": "my-saas-app"
    },
    "findings": [
        {
            "id": "finding-abc123",
            "cwe": "CWE-89",
            "severity": "HIGH",
            "file": "src/api/users.ts",
            "line": 42,
            "status": "open",
            "first_detected": "2026-03-15T08:00:00Z"
        }
    ],
    "finding_count": 1,
    "finding_count_by_severity": {"critical": 0, "high": 1, "medium": 0, "low": 0},
    "remediation_status": {
        "open": 1,
        "in_progress": 0,
        "resolved": 0,
        "suppressed": 0
    },
    "control_assessment": "gap_identified",
    "evidence_narrative": "1 HIGH severity SQL injection finding detected in src/api/users.ts:42. This indicates a gap in CC6.6 (Restricting External Threats). Remediation recommended: parameterized queries."
}
```

### 4.3 CLI

```
piranesi compliance evidence --framework soc2 --output evidence/
piranesi compliance evidence --framework pci_dss --output evidence/
piranesi compliance evidence --framework all --output evidence/
```

Produces one JSON file per control with findings: `evidence/soc2_cc6_1.json`, `evidence/soc2_cc6_6.json`, etc. Controls with zero findings produce a "pass" artifact (evidence of control effectiveness).

### 4.4 Implementation

- `src/piranesi/legal/evidence.py` — evidence generation module
- Evidence templates per framework in `src/piranesi/legal/templates/`
- JSON schema validation for evidence artifacts (Pydantic model)
- Integration with `report/compliance.py` for narrative generation

---

## 5. Compliance Gap Report

**Estimated effort: 5-8h**

### 5.1 Enhanced `--format compliance`

Update existing compliance report format to include SOC 2 and PCI-DSS sections alongside existing frameworks.

### 5.2 Per-Control Report

```
SOC 2 Type II — Control Gap Analysis
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
CC6.1  Logical Access Security       2 findings (1 HIGH, 1 MED)
CC6.6  Restricting External Threats  4 findings (2 HIGH, 2 MED)
CC6.7  Restricting Assets            1 finding  (1 MED)
CC6.8  Preventing Malware            0 findings ✓
CC7.1  Monitoring Activities         1 finding  (1 HIGH)
CC7.2  Monitoring Components         0 findings ✓
CC8.1  Change Management             3 findings (3 MED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: 11 findings across 5/7 controls
```

### 5.3 Remediation Priority

Priority score = `finding_severity_weight × compliance_impact_weight`:

| Finding Severity | Weight |
|-----------------|--------|
| Critical | 4 |
| High | 3 |
| Medium | 2 |
| Low | 1 |

| Compliance Impact | Weight |
|-------------------|--------|
| Controls multiple frameworks | 3 |
| Controls single enterprise framework | 2 |
| Informational mapping | 1 |

Prioritized remediation list:
```
Remediation Priority (SOC 2 + PCI-DSS)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 #  Priority  Finding           Controls Affected
 1  12        CWE-89 users.ts   CC6.6, Req 6.2.1, Req 6.5.1
 2   9        CWE-287 auth.ts   CC6.1, Req 8.3.1
 3   6        CWE-798 config.ts CC7.1
```

### 5.4 Timeline Estimation

Rough remediation time estimates based on finding complexity:

| Complexity | Indicators | Estimated Hours |
|-----------|------------|-----------------|
| Low | Single file, single sink, parameterization fix | 1-2h |
| Medium | Cross-file flow, requires refactoring | 4-8h |
| High | Architectural issue, multiple entry points | 16-40h |

Complexity derived from taint flow length (number of edges) and number of affected files.

---

## 6. Testing

**Estimated effort: 8-12h**

### 6.1 SOC 2 Rule Tests (10 cases)

| Test | Input | Expected Output |
|------|-------|-----------------|
| `test_soc2_sqli_maps_cc6_6` | CWE-89 finding | CC6.6 control gap |
| `test_soc2_auth_maps_cc6_1` | CWE-287 finding | CC6.1 control gap |
| `test_soc2_path_traversal_maps_cc6_7` | CWE-22 finding | CC6.7 control gap |
| `test_soc2_deser_maps_cc6_8` | CWE-502 finding | CC6.8 control gap |
| `test_soc2_hardcoded_creds_maps_cc7_1` | CWE-798 finding | CC7.1 control gap |
| `test_soc2_info_leak_maps_cc7_2` | CWE-209 finding | CC7.2 control gap |
| `test_soc2_dep_vuln_maps_cc8_1` | SCA CVE finding | CC8.1 control gap |
| `test_soc2_low_severity_still_maps` | CWE-79 LOW severity | CC6.6 (severity doesn't gate mapping) |
| `test_soc2_multiple_controls_one_finding` | CWE-89 + CWE-79 findings | Both CC6.6 findings |
| `test_soc2_no_findings_pass_artifact` | No findings | All controls produce "pass" evidence |

### 6.2 PCI-DSS Rule Tests (12 cases)

| Test | Input | Expected Output |
|------|-------|-----------------|
| `test_pci_scope_detection_stripe` | Code with `import Stripe` | `is_payment_processing = True` |
| `test_pci_scope_detection_keywords` | Code with `cardNumber`, `checkout` | `is_payment_processing = True` |
| `test_pci_no_scope_no_rules` | Code without payment keywords | No PCI-DSS findings (scope guard blocks) |
| `test_pci_req_3_4_weak_crypto` | CWE-327 + payment scope | Req 3.4 violation |
| `test_pci_req_4_1_cleartext` | CWE-319 + payment scope | Req 4.1 violation |
| `test_pci_req_6_2_injection` | CWE-89 + payment scope | Req 6.2.1 violation |
| `test_pci_req_6_3_cve` | SCA CVE + payment scope | Req 6.3.1 violation |
| `test_pci_req_6_4_xss_public` | CWE-79 + payment scope | Req 6.4.1 violation |
| `test_pci_req_8_3_auth` | CWE-287 + payment scope | Req 8.3.1 violation |
| `test_pci_req_10_2_logging` | CWE-778 + payment scope | Req 10.2.1 violation |
| `test_pci_meta_req_11_3` | Any scan with payment scope | Req 11.3.1 satisfied (meta-evidence) |
| `test_pci_cross_framework_overlap` | CWE-89 + payment scope + GDPR context | Both PCI-DSS Req 6.2.1 + GDPR Art 32 flagged |

### 6.3 Evidence Artifact Tests (5 cases)

| Test | Validates |
|------|-----------|
| `test_evidence_json_schema` | Evidence artifact matches Pydantic schema |
| `test_evidence_per_control_files` | One file per control generated |
| `test_evidence_pass_artifact` | Controls with 0 findings produce pass evidence |
| `test_evidence_finding_count_accuracy` | `finding_count` matches actual findings list length |
| `test_evidence_scan_metadata` | `scan_date`, `scan_version`, `scope` fields populated |

### 6.4 Edge Cases

- Non-payment codebase scanned: zero PCI-DSS findings, SOC 2 findings still apply.
- Payment scope with zero vulnerabilities: PCI-DSS Req 11.3.1 meta-evidence generated.
- Finding maps to multiple PCI-DSS requirements: all applicable requirements listed.
- SOC 2 + PCI-DSS overlap: both frameworks flag the same finding independently.

---

## 7. Risks

- **SOC 2 interpretation variance**: TSC controls are principles-based, not prescriptive. Auditors may interpret control mappings differently. Mitigation: document mapping rationale per rule, allow override via TOML config.
- **PCI-DSS scope creep**: heuristic scope detection may false-positive on codebases that mention payment keywords in comments/docs. Mitigation: require >= 2 keyword hits in actual code (not comments), allow `--pci-scope=force|auto|off` CLI flag.
- **Evidence format compatibility**: audit platforms (Vanta, Drata) have different ingestion formats. Mitigation: start with generic JSON, add platform-specific adapters in future phase.
- **Regulatory updates**: SOC 2 TSC and PCI-DSS requirements evolve. Mitigation: TOML rules are user-editable, version-tagged per framework version (e.g., `pci_dss_v4_0`).
