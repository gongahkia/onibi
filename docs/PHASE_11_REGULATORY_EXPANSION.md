# Phase 11: Regulatory Framework Expansion

**Estimated effort: 50-70 ideal hours**
**Blocked by: Phase 3 (rule engine working)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Piranesi's regulatory engine covers PDPA (Singapore), MAS TRM (Singapore financial sector), and EU AI Act. For global adoption, coverage must extend to US (CCPA/CPRA, HIPAA), EU (NIS2, GDPR), and provide a community contribution pathway for additional frameworks.

Each regulatory framework follows the same implementation pattern: TOML rule file + Python loader + integration tests. The rule engine infrastructure from Phase 3 is reusable — only the rules themselves are new.

---

## 2. CCPA/CPRA (California)

**Estimated effort: 12-15h**

### 2.1 Scope

The California Consumer Privacy Act (CCPA) as amended by the California Privacy Rights Act (CPRA) is the most significant US state privacy law. It applies to businesses that collect personal information of California residents.

### 2.2 Rules to Encode

| Rule ID | Section | Trigger | Obligation |
|---------|---------|---------|-----------|
| `ccpa_1798_100_disclosure` | 1798.100 | Any PI exposure | Right to know — consumers can request disclosure of PI collected |
| `ccpa_1798_105_categories` | 1798.105 | Sensitive PI exposure (SSN, biometric, geolocation, health) | Right to delete — consumers can request deletion |
| `ccpa_1798_150_damages` | 1798.150 | Non-encrypted/non-redacted PI breach | Private right of action — $100-$750 per consumer per incident |
| `ccpa_1798_155_aggravated` | 1798.155 | Intentional/willful violation | AG enforcement — $2,500 per violation (unintentional), $7,500 per violation (intentional) |
| `ccpa_1798_185_sensitive` | 1798.185 | Sensitive PI (race, ethnicity, health, biometric, sexual orientation) | Heightened protections — limit use/disclosure to what is necessary |

### 2.3 Data Category Mapping

Map existing Piranesi data categories to CCPA definitions:

| Piranesi Category | CCPA Category |
|-------------------|---------------|
| `nric`, `fin` | Government-issued ID |
| `biometric` | Biometric information |
| `health` | Health information |
| `financial`, `financial_*` | Financial information |
| `contact_email`, `contact_phone` | Personal information |
| `race`, `religion` | Sensitive personal information |
| `dob` | Personal information |

### 2.4 Implementation

- `rules/ccpa.toml` — 5+ rules following PDPA pattern
- `src/piranesi/legal/rules/ccpa.py` — loader (same pattern as pdpa.py)
- Wire into `rules/__init__.py` and `memo.py`
- Tests in `tests/test_legal/test_regulatory_rules.py`

---

## 3. HIPAA (US Healthcare)

**Estimated effort: 10-12h**

### 3.1 Scope

The Health Insurance Portability and Accountability Act (HIPAA) Security Rule applies to covered entities (healthcare providers, health plans, clearinghouses) and their business associates. Piranesi flags HIPAA when health data (`health` category) is involved AND the `is_healthcare_entity` boolean fact is true.

### 3.2 Rules to Encode

| Rule ID | Section | Trigger | Obligation |
|---------|---------|---------|-----------|
| `hipaa_164_312_access_control` | 164.312(a) | PHI exposure via injection | Technical safeguard — access control mechanisms required |
| `hipaa_164_312_audit` | 164.312(b) | PHI exposure | Audit controls — record and examine access activity |
| `hipaa_164_312_integrity` | 164.312(c) | PHI modification (CMDi, path traversal) | Integrity controls — protect PHI from improper alteration |
| `hipaa_164_312_transmission` | 164.312(e) | PHI in transit (SSRF, XSS) | Transmission security — encrypt PHI in transit |
| `hipaa_164_408_breach_notification` | 164.408 | PHI breach >= 500 individuals | Breach notification — notify HHS within 60 days |

### 3.3 Penalty Structure

| Tier | Culpability | Penalty per Violation | Annual Max |
|------|-------------|----------------------|------------|
| 1 | Did not know | $100 - $50,000 | $25,000 |
| 2 | Reasonable cause | $1,000 - $50,000 | $100,000 |
| 3 | Willful neglect (corrected) | $10,000 - $50,000 | $250,000 |
| 4 | Willful neglect (not corrected) | $50,000 | $1,500,000 |

### 3.4 Implementation

- `rules/hipaa.toml` — 5+ rules
- `src/piranesi/legal/rules/hipaa.py` — loader
- Add `is_healthcare_entity` boolean fact to finding model
- Tests

---

## 4. NIS2 (EU Cybersecurity)

**Estimated effort: 10-12h**

### 4.1 Scope

The Network and Information Systems Directive 2 (NIS2) is the EU's cross-sector cybersecurity regulation. It applies to essential and important entities in critical sectors (energy, transport, health, digital infrastructure, ICT service management, etc.).

### 4.2 Rules to Encode

| Rule ID | Section | Trigger | Obligation |
|---------|---------|---------|-----------|
| `nis2_art21_risk_management` | Article 21 | Any confirmed vulnerability | Cybersecurity risk management measures required |
| `nis2_art21_supply_chain` | Article 21(2)(d) | Third-party processor involved | Supply chain security assessment |
| `nis2_art23_incident_reporting` | Article 23 | Significant incident (>= 500 affected) | 24h early warning, 72h incident notification, 1 month final report |
| `nis2_art23_aggravated` | Article 23 | Cross-border impact | Notify CSIRT and competent authorities in all affected Member States |
| `nis2_art34_penalties` | Article 34 | Essential entity violation | Up to EUR 10,000,000 or 2% of worldwide turnover |
| `nis2_art34_important` | Article 34 | Important entity violation | Up to EUR 7,000,000 or 1.4% of worldwide turnover |

### 4.3 Implementation

- `rules/nis2.toml` — 6+ rules
- `src/piranesi/legal/rules/nis2.py` — loader
- Add `is_essential_entity` and `is_important_entity` boolean facts
- Tests

---

## 5. GDPR (EU General Data Protection)

**Estimated effort: 12-15h**

### 5.1 Scope

The General Data Protection Regulation is the EU's primary data protection law. It overlaps with NIS2 but focuses on personal data protection rather than cybersecurity.

### 5.2 Rules to Encode

| Rule ID | Section | Trigger | Obligation |
|---------|---------|---------|-----------|
| `gdpr_art32_security` | Article 32 | Any PI exposure | Appropriate technical and organizational measures |
| `gdpr_art32_encryption` | Article 32(1)(a) | PI exposure + no encryption | Pseudonymisation and encryption of personal data |
| `gdpr_art33_notification` | Article 33 | PI breach likely to result in risk | Notify supervisory authority within 72 hours |
| `gdpr_art34_communication` | Article 34 | High risk to individuals | Communicate breach to data subjects without undue delay |
| `gdpr_art83_standard` | Article 83(4) | Controller/processor obligation violation | Up to EUR 10,000,000 or 2% of worldwide turnover |
| `gdpr_art83_aggravated` | Article 83(5) | Basic processing principle violation | Up to EUR 20,000,000 or 4% of worldwide turnover |
| `gdpr_art83_special` | Article 83(5) | Special category data (Art 9) breach | Up to EUR 20,000,000 or 4% of worldwide turnover |

### 5.3 Special Category Data Mapping

| GDPR Special Category | Piranesi Category |
|----------------------|-------------------|
| Racial/ethnic origin | `race`, `nationality` |
| Political opinions | (new: `political`) |
| Religious/philosophical beliefs | `religion` |
| Trade union membership | (new: `trade_union`) |
| Genetic data | `genetic` |
| Biometric data | `biometric` |
| Health data | `health` |
| Sex life/sexual orientation | (new: `sexual_orientation`) |

### 5.4 Implementation

- `rules/gdpr.toml` — 7+ rules
- `src/piranesi/legal/rules/gdpr.py` — loader
- Extend `legal/taxonomy.py` with new data categories for GDPR special categories
- Tests

---

## 6. Community Contribution System

**Estimated effort: 8-10h**

### 6.1 Rule Contribution Workflow

1. Contributors create a TOML file under `rules/community/{framework_name}.toml`
2. TOML follows the `RegulatoryRuleSpec` schema (already defined)
3. PR review checklist:
   - Legal accuracy reviewed by domain expert
   - Penalty ranges cite specific statutory provisions
   - Cross-references are valid
   - Tests added to `tests/test_legal/test_regulatory_rules.py`

### 6.2 Auto-Discovery

Update `legal/memo.py` to auto-discover rule files:

```python
def discover_rule_files(rules_dir: Path | None = None) -> list[Path]:
    """Find all *.toml files in rules/ and rules/community/."""
    base = rules_dir or default_rules_dir()
    return sorted(base.glob("**/*.toml"))
```

### 6.3 Documentation

Create `docs/contributing-rules.md`:
- TOML schema reference
- Step-by-step guide for adding a new regulatory framework
- Template TOML file
- Testing requirements
- Legal review process

---

## 7. Acceptance Criteria

- [ ] CCPA/CPRA: 5+ rules, tested, wired into default engine
- [ ] HIPAA: 5+ rules with boolean fact gating
- [ ] NIS2: 6+ rules with entity type gating
- [ ] GDPR: 7+ rules including special category data
- [ ] Community contribution system with auto-discovery
- [ ] `docs/contributing-rules.md` with template and guide
- [ ] All new frameworks have integration tests
- [ ] Legal memo output correctly identifies applicable frameworks
