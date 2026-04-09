# Phase 3: Regulatory Engine

## 1. Phase Overview

Phase 3 builds a datalog-style forward-chaining rule engine that maps confirmed security findings to specific regulatory obligations. This is the legal moat. No existing AI security tool maps findings to specific statutory obligations with formal inference.

**Input:** `ConfirmedFinding` objects from Phase 2 (exploit verification).

**Output:** `LegalAssessment` objects -- structured analysis identifying which regulatory obligations are implicated, with statutory references, penalty ranges, notification timelines, and enforcement precedents.

The engine is domain-specific but framework-agnostic: rules for PDPA, MAS TRM, and EU AI Act are encoded in the same formal representation and processed by the same inference engine. Adding a new regulatory framework means adding new rules, not new code.

The design is inspired by Yuho (a DSL for Singapore criminal law) -- applying formal legal reasoning to data protection and financial regulation. Rules are composable, referenceable, and produce auditable inference traces.

---

## 2. Rule Representation

**Estimated effort: 10-15 ideal hours**

### Rule Schema

```python
from pydantic import BaseModel
from enum import Enum

class RegulatoryFramework(str, Enum):
    PDPA = "PDPA"
    MAS_TRM = "MAS_TRM"
    EU_AI_ACT = "EU_AI_ACT"

class Consequence(str, Enum):
    NOTIFY_REGULATOR = "notify_regulator"
    NOTIFY_INDIVIDUALS = "notify_individuals"
    REMEDIATE = "remediate"
    DOCUMENT = "document"
    REVIEW = "review"

class Predicate(BaseModel):
    """base class for rule preconditions."""
    pass

class DataCategoryIn(Predicate):
    finding_field: str = "data_categories"
    categories: list[str]  # e.g., ["nric", "biometric"]

class VulnClassIn(Predicate):
    finding_field: str = "vuln_class"
    cwes: list[str]  # e.g., ["CWE-89", "CWE-79"]

class SeverityGte(Predicate):
    finding_field: str = "severity"
    threshold: str  # "CRITICAL", "HIGH", "MEDIUM", "LOW"

class DataFlowsCrossBorder(Predicate):
    finding_field: str = "cross_border"

class SystemIsHighRiskAI(Predicate):
    finding_field: str = "is_high_risk_ai"

class AffectedIndividualsGte(Predicate):
    finding_field: str = "affected_individuals_estimate"
    threshold: int

class ThirdPartyProcessorInvolved(Predicate):
    finding_field: str = "third_party_processor"

class RegulatoryRule(BaseModel):
    rule_id: str                              # e.g., "pdpa_s24_protection"
    framework: RegulatoryFramework
    section: str                              # e.g., "Section 24"
    preconditions: list[Predicate]            # ALL must be satisfied (conjunction)
    obligation_text: str                      # specific obligation from statute
    consequences: list[Consequence]           # required actions
    penalty_range: str | None                 # e.g., "$1,000,000 or 10% of annual turnover"
    notification_timeline: str | None         # e.g., "3 calendar days"
    enforcement_precedents: list[str]         # case references
    cross_references: list[str]              # other rule_ids this rule references
    severity_modifier: str | None            # "aggravated", "standard", None
```

### Rule Composition

Rules can reference each other via `cross_references`. When a rule fires, its `rule_id` is added to the derived fact set. Other rules can have preconditions that check for the presence of a derived rule:

```python
class RuleFired(Predicate):
    """true if a specific rule has already fired in this inference run."""
    rule_id: str
```

This enables chained reasoning: Rule A fires, which causes Rule B to fire (because Rule B has a precondition `RuleFired("rule_a")`).

### Rule Storage

Rules are defined in TOML files (one per framework):

```toml
# rules/pdpa.toml

[[rules]]
rule_id = "pdpa_s24_protection"
framework = "PDPA"
section = "Section 24"
preconditions = [
    { type = "VulnClassIn", cwes = ["CWE-89", "CWE-79", "CWE-78", "CWE-22"] },
    { type = "DataCategoryIn", categories = ["nric", "biometric", "financial", "health", "contact_email", "contact_phone", "name", "dob"] },
]
obligation_text = "An organisation shall protect personal data in its possession or under its control by making reasonable security arrangements to prevent unauthorised access, collection, use, disclosure, copying, modification, disposal or similar risks."
consequences = ["remediate", "document"]
penalty_range = "Up to $1,000,000"
enforcement_precedents = ["[2019] SGPDPC 7 (SingHealth)", "[2020] SGPDPC 1 (Grab)"]
```

---

## 3. Inference Engine Choice and Justification

**Estimated effort: 20-25 ideal hours**

### Options Considered

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **PyDatalog** | Prolog-like syntax, Python integration | Last meaningful PyPI release ~2021. Limited docs. Poor type support. Uncertain maintenance. | NOT recommended |
| **Souffle** | Excellent performance. Supports complex recursion. Well-designed Datalog dialect. | Requires C++ compilation step. Deployment complexity (must ship Souffle binary or compile on target). Overkill for ~50 rules. | Recommended for v2+ (>200 rules) |
| **Hand-rolled forward-chaining engine** | Simple Python. Debuggable. No external deps. Full type safety with Pydantic models. Embeddable. | No built-in recursion optimization. Manual fixed-point implementation. | **Recommended for v1** |

### Justification for Hand-Rolled Engine

The v1 rule set is small (~50 rules). The inference is simple -- no recursion is needed for v1 rules (no rule derives a fact that is a precondition of a rule that derives a fact that is a precondition of the first rule). Forward-chaining reaches a fixed point in 2-3 iterations for typical findings. The primary value is **debuggability** -- when a legal assessment is wrong, we need to trace exactly which rules fired and why. A hand-rolled engine provides full control over this trace.

Migration path: if v2+ rule sets exceed ~200 rules or require recursive inference (e.g., inferring data flows across organizational boundaries), migrate to Souffle. The rule representation (Section 2) is designed to be translatable to Souffle Datalog.

### Implementation

```python
from dataclasses import dataclass, field

@dataclass(frozen=True)
class Fact:
    predicate: str
    arguments: tuple  # hashable for set membership

class ForwardChainingEngine:
    def __init__(self):
        self._rules: list[RegulatoryRule] = []
        self._facts: set[Fact] = set()
        self._trace: list[tuple[str, Fact]] = []  # (rule_id, derived_fact)

    def add_rule(self, rule: RegulatoryRule) -> None:
        self._rules.append(rule)

    def add_fact(self, fact: Fact) -> None:
        self._facts.add(fact)

    def run(self, max_iterations: int = 10) -> None:
        """forward-chain until fixed point or max iterations."""
        for _ in range(max_iterations):
            new_facts: set[Fact] = set()
            for rule in self._rules:
                if self._preconditions_satisfied(rule):
                    derived = self._derive_facts(rule)
                    for f in derived:
                        if f not in self._facts:
                            new_facts.add(f)
                            self._trace.append((rule.rule_id, f))
            if not new_facts:
                break  # fixed point reached
            self._facts |= new_facts

    def _preconditions_satisfied(self, rule: RegulatoryRule) -> bool:
        """check if all preconditions of a rule are satisfied by current facts."""
        for precondition in rule.preconditions:
            if not self._evaluate_predicate(precondition):
                return False
        return True

    def _evaluate_predicate(self, predicate: Predicate) -> bool:
        """evaluate a single predicate against the fact database."""
        if isinstance(predicate, DataCategoryIn):
            return any(
                Fact("data_category", (cat,)) in self._facts
                for cat in predicate.categories
            )
        elif isinstance(predicate, VulnClassIn):
            return any(
                Fact("vuln_class", (cwe,)) in self._facts
                for cwe in predicate.cwes
            )
        elif isinstance(predicate, SeverityGte):
            severity_order = {"LOW": 0, "MEDIUM": 1, "HIGH": 2, "CRITICAL": 3}
            threshold = severity_order[predicate.threshold]
            return any(
                Fact("severity", (sev,)) in self._facts
                and severity_order.get(sev, -1) >= threshold
                for sev in ["LOW", "MEDIUM", "HIGH", "CRITICAL"]
            )
        elif isinstance(predicate, DataFlowsCrossBorder):
            return Fact("cross_border", (True,)) in self._facts
        elif isinstance(predicate, SystemIsHighRiskAI):
            return Fact("is_high_risk_ai", (True,)) in self._facts
        elif isinstance(predicate, AffectedIndividualsGte):
            return any(
                Fact("affected_individuals", (n,)) in self._facts
                and n >= predicate.threshold
                for n in range(predicate.threshold, 10_000_001)  # practical upper bound
            )
        elif isinstance(predicate, RuleFired):
            return Fact("rule_fired", (predicate.rule_id,)) in self._facts
        elif isinstance(predicate, ThirdPartyProcessorInvolved):
            return Fact("third_party_processor", (True,)) in self._facts
        return False

    def _derive_facts(self, rule: RegulatoryRule) -> list[Fact]:
        """derive new facts from a fired rule."""
        facts = [
            Fact("rule_fired", (rule.rule_id,)),
            Fact("obligation", (rule.rule_id, rule.framework.value, rule.section, rule.obligation_text)),
        ]
        for consequence in rule.consequences:
            facts.append(Fact("consequence", (rule.rule_id, consequence.value)))
        if rule.penalty_range:
            facts.append(Fact("penalty_range", (rule.rule_id, rule.penalty_range)))
        if rule.notification_timeline:
            facts.append(Fact("notification_timeline", (rule.rule_id, rule.notification_timeline)))
        return facts

    def query_obligations(self) -> list[Fact]:
        """return all derived obligation facts."""
        return [f for f in self._facts if f.predicate == "obligation"]

    def query_consequences(self) -> list[Fact]:
        """return all derived consequence facts."""
        return [f for f in self._facts if f.predicate == "consequence"]

    def get_trace(self) -> list[tuple[str, Fact]]:
        """return the inference trace for debugging."""
        return list(self._trace)
```

### Fact Assertion from Finding

```python
def assert_finding_facts(engine: ForwardChainingEngine, finding: ConfirmedFinding) -> None:
    """assert facts about a confirmed finding into the engine."""
    engine.add_fact(Fact("vuln_class", (finding.finding.vuln_class,)))
    engine.add_fact(Fact("severity", (finding.finding.severity,)))
    engine.add_fact(Fact("confirmation_level", (finding.confirmation_level,)))
    for cat in finding.finding.source_annotation.data_categories:
        engine.add_fact(Fact("data_category", (cat,)))
    if finding.finding.cross_border:
        engine.add_fact(Fact("cross_border", (True,)))
    if finding.finding.is_high_risk_ai:
        engine.add_fact(Fact("is_high_risk_ai", (True,)))
    if finding.finding.affected_individuals_estimate:
        engine.add_fact(Fact("affected_individuals", (finding.finding.affected_individuals_estimate,)))
    if finding.finding.third_party_processor:
        engine.add_fact(Fact("third_party_processor", (True,)))
```

---

## 4. Personal Data Taxonomy

**Estimated effort: 8-10 ideal hours**

### Sensitivity Tiers

| Tier | Sensitivity | Data Categories | Examples |
|---|---|---|---|
| **Tier 1** | Highest | `nric`, `fin`, `biometric`, `genetic`, `health` | NRIC/FIN numbers, fingerprints, DNA, medical records |
| **Tier 2** | High | `financial_bank`, `financial_credit_card`, `financial_income`, `employment`, `criminal` | Bank account numbers, credit card numbers, salary data, criminal records |
| **Tier 3** | Moderate | `contact_phone`, `contact_email`, `contact_address`, `dob`, `nationality`, `race`, `religion` | Phone numbers, email addresses, physical addresses, date of birth |
| **Tier 4** | Basic | `name`, `username`, `public_info` | Full name, username/handle, publicly available information |

### Tier-to-Penalty Mapping

Higher tiers trigger aggravated obligations with higher penalty ranges:
- Tier 1: Aggravated breach. Maximum penalty applies. Mandatory notification likely.
- Tier 2: Standard breach with elevated concern. Higher end of penalty range.
- Tier 3: Standard breach. Standard penalty range.
- Tier 4: Standard breach. Lower end of penalty range. Notification unlikely unless at scale.

### Integration with Taint Analysis

The taint source annotation from Phase 1 must include `data_categories`. Assignment strategy:

**Field name heuristics (deterministic, applied first):**

```python
FIELD_NAME_TO_CATEGORY: dict[str, str] = {
    "nric": "nric",
    "ic_number": "nric",
    "ic_no": "nric",
    "fin": "fin",
    "fin_number": "fin",
    "biometric": "biometric",
    "fingerprint": "biometric",
    "face_data": "biometric",
    "health": "health",
    "medical": "health",
    "diagnosis": "health",
    "credit_card": "financial_credit_card",
    "card_number": "financial_credit_card",
    "cvv": "financial_credit_card",
    "bank_account": "financial_bank",
    "account_number": "financial_bank",
    "salary": "financial_income",
    "income": "financial_income",
    "email": "contact_email",
    "phone": "contact_phone",
    "mobile": "contact_phone",
    "address": "contact_address",
    "dob": "dob",
    "date_of_birth": "dob",
    "birthday": "dob",
    "race": "race",
    "ethnicity": "race",
    "religion": "religion",
    "name": "name",
    "full_name": "name",
    "first_name": "name",
    "last_name": "name",
    "username": "username",
    "user_name": "username",
    "password": "credential",
    "passwd": "credential",
    "secret": "credential",
    "token": "credential",
}
```

**LLM classification (fallback for ambiguous fields):**

When the field name does not match any heuristic, query the LLM via LiteLLM:

Prompt: `"What type of personal data is likely stored in a field named '{field_name}' in the context of {endpoint_description}? Respond with one of: nric, fin, biometric, genetic, health, financial_bank, financial_credit_card, financial_income, employment, criminal, contact_phone, contact_email, contact_address, dob, nationality, race, religion, name, username, public_info, none."`

Cache LLM responses for identical field names to avoid redundant API calls.

### Data Category Resolution

```python
def resolve_data_categories(field_name: str, context: str) -> list[str]:
    """resolve field name to data categories. heuristic first, LLM fallback."""
    normalized = field_name.lower().strip()
    if normalized in FIELD_NAME_TO_CATEGORY:
        return [FIELD_NAME_TO_CATEGORY[normalized]]
    for pattern, category in FIELD_NAME_TO_CATEGORY.items():
        if pattern in normalized:
            return [category]
    return llm_classify_field(field_name, context)  # LLM fallback

def get_sensitivity_tier(categories: list[str]) -> int:
    """return the highest (most sensitive) tier among the given categories."""
    tier_map = {
        "nric": 1, "fin": 1, "biometric": 1, "genetic": 1, "health": 1,
        "financial_bank": 2, "financial_credit_card": 2, "financial_income": 2,
        "employment": 2, "criminal": 2,
        "contact_phone": 3, "contact_email": 3, "contact_address": 3,
        "dob": 3, "nationality": 3, "race": 3, "religion": 3,
        "name": 4, "username": 4, "public_info": 4,
    }
    tiers = [tier_map.get(cat, 4) for cat in categories]
    return min(tiers) if tiers else 4  # min = highest sensitivity
```

---

## 5. V1 Rule Sets -- Full Sample Encodings

### 5a. PDPA Section 24 -- Protection Obligation

**Estimated effort: 12-15 ideal hours**

**Statutory text (Section 24, PDPA 2012):**
> "An organisation shall protect personal data in its possession or under its control by making reasonable security arrangements to prevent unauthorised access, collection, use, disclosure, copying, modification, disposal or similar risks."

---

**Rule 1: Standard S24 Protection Obligation Breach**

Any confirmed injection/traversal vulnerability affecting personal data.

```python
RegulatoryRule(
    rule_id="pdpa_s24_standard",
    framework=RegulatoryFramework.PDPA,
    section="Section 24",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        DataCategoryIn(categories=[
            "nric", "fin", "biometric", "genetic", "health",
            "financial_bank", "financial_credit_card", "financial_income",
            "employment", "criminal",
            "contact_phone", "contact_email", "contact_address",
            "dob", "nationality", "race", "religion",
            "name", "username",
        ]),
    ],
    obligation_text=(
        "Section 24 requires organisations to protect personal data by making "
        "reasonable security arrangements to prevent unauthorised access, collection, "
        "use, disclosure, copying, modification, disposal or similar risks. "
        "A confirmed exploitable vulnerability in a system processing personal data "
        "constitutes a failure to make reasonable security arrangements."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT],
    penalty_range="Up to $1,000,000",
    notification_timeline=None,
    enforcement_precedents=[
        "[2019] SGPDPC 7 (SingHealth) -- failure to implement adequate security measures "
        "for a patient database system, resulting in unauthorised access to 1.5M patient records",
        "[2020] SGPDPC 1 (Grab) -- inadequate protection of personal data due to "
        "insufficient access controls in application",
    ],
    cross_references=[],
    severity_modifier="standard",
)
```

---

**Rule 2: Aggravated S24 Breach -- Tier 1 Personal Data**

Tier 1 data (NRIC, biometric, health) in a vulnerable path triggers aggravated breach with higher penalty.

```python
RegulatoryRule(
    rule_id="pdpa_s24_aggravated_tier1",
    framework=RegulatoryFramework.PDPA,
    section="Section 24",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        DataCategoryIn(categories=["nric", "fin", "biometric", "genetic", "health"]),
    ],
    obligation_text=(
        "Section 24 protection obligation with aggravated concern: the vulnerable system "
        "processes Tier 1 personal data (NRIC/FIN, biometric, genetic, or health data). "
        "The PDPC has consistently held that organisations handling high-sensitivity data "
        "are expected to implement commensurately stronger security measures. Failure to "
        "do so may attract penalties at the higher end of the statutory range."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT, Consequence.REVIEW],
    penalty_range="Up to $1,000,000 (higher end expected for Tier 1 data)",
    notification_timeline=None,
    enforcement_precedents=[
        "[2019] SGPDPC 7 (SingHealth) -- highest financial penalty ($250,000 to IHiS, "
        "$250,000 to SingHealth) for breach involving health/medical data",
    ],
    cross_references=["pdpa_s24_standard"],
    severity_modifier="aggravated",
)
```

---

**Rule 3: S24 Breach -- No Encryption on Personal Data at Rest**

If the taint analysis reveals personal data flowing through a vulnerable path without encryption at rest (detected by absence of encryption-related function calls in the taint path).

```python
RegulatoryRule(
    rule_id="pdpa_s24_no_encryption",
    framework=RegulatoryFramework.PDPA,
    section="Section 24",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        DataCategoryIn(categories=[
            "nric", "fin", "biometric", "genetic", "health",
            "financial_bank", "financial_credit_card", "financial_income",
        ]),
        # Note: absence of encryption is detected by Phase 1 taint analysis
        # and asserted as a fact: Fact("no_encryption_at_rest", (True,))
    ],
    obligation_text=(
        "Section 24 requires reasonable security arrangements. The PDPC Advisory Guidelines "
        "on the PDPA for Selected Topics (revised 2021) specifically identify encryption "
        "as a reasonable security measure for high-sensitivity personal data. Absence of "
        "encryption for Tier 1/2 personal data in a system with confirmed vulnerabilities "
        "represents an additional failure of reasonable security arrangements."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT],
    penalty_range="Up to $1,000,000",
    notification_timeline=None,
    enforcement_precedents=[
        "[2019] SGPDPC 7 (SingHealth) -- PDPC noted lack of encryption as an "
        "aggravating factor in assessing penalty",
    ],
    cross_references=["pdpa_s24_standard"],
    severity_modifier="aggravated",
)
```

---

**Rule 4: Mandatory Breach Notification -- S26D (>500 Individuals)**

Personal data of >500 individuals potentially affected triggers mandatory notification under S26D.

```python
RegulatoryRule(
    rule_id="pdpa_s26d_notification",
    framework=RegulatoryFramework.PDPA,
    section="Section 26D",
    preconditions=[
        RuleFired(rule_id="pdpa_s24_standard"),  # S24 breach must have fired first
        AffectedIndividualsGte(threshold=500),
    ],
    obligation_text=(
        "Section 26D(1) of the PDPA requires an organisation to notify the PDPC of a "
        "notifiable data breach (where the data breach affects 500 or more individuals, "
        "or is of a significant scale). Notification must be made within 3 calendar days "
        "of the organisation's assessment that the breach is notifiable."
    ),
    consequences=[Consequence.NOTIFY_REGULATOR, Consequence.NOTIFY_INDIVIDUALS],
    penalty_range="Up to $1,000,000 (separate from S24 penalty)",
    notification_timeline="3 calendar days from assessment of breach as notifiable",
    enforcement_precedents=[
        "[2021] SGPDPC 14 (Commeasure) -- failure to notify PDPC within stipulated "
        "timeframe resulted in additional directions issued",
    ],
    cross_references=["pdpa_s24_standard", "pdpa_s24_aggravated_tier1"],
    severity_modifier=None,
)
```

---

**Rule 5: S24 + S25 -- Third-Party Processor Involvement**

Data passed to an external API through a vulnerable path triggers both S24 (protection) and S25 (retention limitation / processor obligations).

```python
RegulatoryRule(
    rule_id="pdpa_s24_s25_third_party",
    framework=RegulatoryFramework.PDPA,
    section="Section 24 + Section 25",
    preconditions=[
        RuleFired(rule_id="pdpa_s24_standard"),
        ThirdPartyProcessorInvolved(),
    ],
    obligation_text=(
        "Where personal data is transferred to a third-party data intermediary or processor "
        "through a vulnerable path, the organisation bears dual obligations: (1) Section 24 "
        "requires the organisation to ensure the processor provides a comparable standard of "
        "protection (the vulnerability undermines this assurance); (2) Section 25 requires "
        "the organisation to ensure the processor ceases to retain the personal data when "
        "no longer necessary. A vulnerability exposing data to a third party may compromise "
        "both obligations."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT, Consequence.REVIEW],
    penalty_range="Up to $1,000,000 (per section; cumulative exposure possible)",
    notification_timeline=None,
    enforcement_precedents=[
        "[2020] SGPDPC 1 (Grab) -- PDPC considered data sharing with third-party "
        "service providers as a factor in assessing adequacy of protection measures",
    ],
    cross_references=["pdpa_s24_standard"],
    severity_modifier=None,
)
```

**Note on enforcement precedents:** The case citations above follow the PDPC decision citation format. Specific case details (parties, penalties, facts) should be verified against the PDPC's published decisions at https://www.pdpc.gov.sg/all-commissions-decisions before inclusion in production legal memos.

---

### 5b. MAS TRM Section 11 -- Systems Reliability, Availability and Recoverability

**Estimated effort: 8-10 ideal hours**

**Source:** MAS Technology Risk Management Guidelines (January 2021)

**Important distinction:** MAS TRM Guidelines are regulatory guidelines, not statute. Non-compliance is addressed through supervisory action (directions, requirements, conditions on license) rather than direct financial penalties. Rules must reflect this.

---

**Rule 1: TRM 11.1 -- System Reliability Concern**

Confirmed vulnerability in a system handling financial transactions.

```python
RegulatoryRule(
    rule_id="mas_trm_11_1_reliability",
    framework=RegulatoryFramework.MAS_TRM,
    section="Section 11.1 (System Reliability)",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        DataCategoryIn(categories=["financial_bank", "financial_credit_card", "financial_income"]),
    ],
    obligation_text=(
        "TRM Guidelines Section 11.1: 'The FI should ensure its IT systems are reliable and "
        "have the capacity to process current and projected business volumes under normal "
        "and peak operating conditions.' A confirmed exploitable vulnerability undermines "
        "system reliability by exposing the system to potential disruption via exploitation. "
        "The FI should remediate the vulnerability and review the security posture of "
        "affected systems."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.REVIEW, Consequence.DOCUMENT],
    penalty_range="Supervisory action (no direct financial penalty; may include directions, "
                  "conditions on license, or restrictions on business activities)",
    notification_timeline=None,
    enforcement_precedents=[
        "MAS has issued supervisory actions for technology risk management deficiencies. "
        "Specific enforcement actions are typically not published but communicated directly "
        "to the financial institution.",
    ],
    cross_references=[],
    severity_modifier=None,
)
```

---

**Rule 2: TRM 11.2 -- System Recoverability Compromised**

Command injection or path traversal in a financial system compromises system recoverability.

```python
RegulatoryRule(
    rule_id="mas_trm_11_2_recoverability",
    framework=RegulatoryFramework.MAS_TRM,
    section="Section 11.2 (System Recoverability)",
    preconditions=[
        VulnClassIn(cwes=["CWE-78", "CWE-22"]),  # command injection, path traversal
        DataCategoryIn(categories=["financial_bank", "financial_credit_card", "financial_income"]),
    ],
    obligation_text=(
        "TRM Guidelines Section 11.2: 'The FI should establish a recovery strategy and "
        "disaster recovery plan to ensure IT systems and services can be recovered in a "
        "timely manner.' Command injection and path traversal vulnerabilities can compromise "
        "system integrity and complicate recovery (attacker may modify system files or "
        "execute arbitrary commands). The FI should assess the impact on its recovery "
        "capabilities and update its disaster recovery plan accordingly."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.REVIEW, Consequence.DOCUMENT],
    penalty_range="Supervisory action",
    notification_timeline=None,
    enforcement_precedents=[],
    cross_references=["mas_trm_11_1_reliability"],
    severity_modifier=None,
)
```

---

**Rule 3: TRM 11.0.5 -- Inadequate Controls (Input Validation)**

Any confirmed injection vulnerability indicates inadequate input validation controls.

```python
RegulatoryRule(
    rule_id="mas_trm_11_0_5_controls",
    framework=RegulatoryFramework.MAS_TRM,
    section="Section 11.0.5 (Adequate Controls)",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78"]),  # any injection class
        DataCategoryIn(categories=["financial_bank", "financial_credit_card", "financial_income"]),
    ],
    obligation_text=(
        "TRM Guidelines Section 11.0.5: 'The FI should implement adequate controls to "
        "mitigate risks arising from the use of IT.' Injection vulnerabilities (SQL injection, "
        "cross-site scripting, command injection) indicate a failure to implement adequate "
        "input validation controls. The FI should review input validation practices across "
        "all systems processing financial data."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.REVIEW],
    penalty_range="Supervisory action",
    notification_timeline=None,
    enforcement_precedents=[],
    cross_references=["mas_trm_11_1_reliability"],
    severity_modifier=None,
)
```

---

### 5c. EU AI Act Article 15 -- Accuracy, Robustness, Cybersecurity

**Estimated effort: 8-10 ideal hours**

**Source:** Regulation (EU) 2024/1689 (EU AI Act)

**Critical precondition:** EU AI Act rules ONLY fire if the affected system is classified as an AI system. This is determined by the `SystemIsHighRiskAI` predicate, which is true if:
- (a) Auto-detected: code imports ML frameworks (TensorFlow, PyTorch, scikit-learn, transformers, langchain), OR
- (b) Manually annotated in `piranesi.toml` config: `is_ai_system = true`

---

**Rule 1: Article 15(1) -- Cybersecurity of High-Risk AI System**

Confirmed vulnerability in a high-risk AI system component.

```python
RegulatoryRule(
    rule_id="eu_ai_act_art15_1_cybersecurity",
    framework=RegulatoryFramework.EU_AI_ACT,
    section="Article 15(1)",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        SystemIsHighRiskAI(),
    ],
    obligation_text=(
        "Article 15(1): 'High-risk AI systems shall be designed and developed in such a way "
        "that they achieve an appropriate level of accuracy, robustness, and cybersecurity, "
        "and that they perform consistently in those respects throughout their lifecycle.' "
        "A confirmed exploitable vulnerability in a high-risk AI system component "
        "indicates a failure to achieve an appropriate level of cybersecurity."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT, Consequence.REVIEW],
    penalty_range="Up to EUR 15,000,000 or 3% of total worldwide annual turnover "
                  "(whichever is higher) per Article 99(3)",
    notification_timeline=None,
    enforcement_precedents=[
        "EU AI Act entered into application August 2024. Enforcement precedents are "
        "not yet established. National market surveillance authorities will be responsible "
        "for enforcement from August 2025.",
    ],
    cross_references=[],
    severity_modifier=None,
)
```

---

**Rule 2: Article 15(3) -- Data Poisoning / Accuracy Concern**

SQL injection affecting a training data pipeline in an AI system.

```python
RegulatoryRule(
    rule_id="eu_ai_act_art15_3_data_poisoning",
    framework=RegulatoryFramework.EU_AI_ACT,
    section="Article 15(3)",
    preconditions=[
        VulnClassIn(cwes=["CWE-89"]),  # SQL injection specifically
        SystemIsHighRiskAI(),
        # Additional precondition: the taint sink is in a data pipeline context
        # (detected by Phase 1 if the query feeds into model training data)
    ],
    obligation_text=(
        "Article 15(3): 'High-risk AI systems shall be resilient against attempts by "
        "unauthorised third parties to alter their use, outputs or performance by "
        "exploiting system vulnerabilities.' A SQL injection vulnerability in a data "
        "pipeline feeding training data to an AI model creates a data poisoning risk. "
        "An attacker could manipulate training data through the injection, potentially "
        "introducing bias or degrading model accuracy."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT, Consequence.REVIEW],
    penalty_range="Up to EUR 15,000,000 or 3% of total worldwide annual turnover",
    notification_timeline=None,
    enforcement_precedents=[
        "No precedents yet. Article 15(3) concerns are documented in ENISA's "
        "'Artificial Intelligence Cybersecurity Challenges' (2020) report.",
    ],
    cross_references=["eu_ai_act_art15_1_cybersecurity"],
    severity_modifier="aggravated",
)
```

---

**Rule 3: Article 15(4) -- Model Serving Endpoint Security**

Injection vulnerability in a model serving/inference endpoint.

```python
RegulatoryRule(
    rule_id="eu_ai_act_art15_4_serving",
    framework=RegulatoryFramework.EU_AI_ACT,
    section="Article 15(4)",
    preconditions=[
        VulnClassIn(cwes=["CWE-89", "CWE-79", "CWE-78", "CWE-22"]),
        SystemIsHighRiskAI(),
        SeverityGte(threshold="HIGH"),
    ],
    obligation_text=(
        "Article 15(4): 'The technical solutions aiming to ensure the cybersecurity of "
        "high-risk AI systems shall be appropriate to the relevant circumstances and the "
        "risks.' A high-severity injection vulnerability in a model serving endpoint "
        "is inappropriate to the risk context of a high-risk AI system. The provider "
        "must demonstrate that cybersecurity measures are proportionate to the risk "
        "classification of the AI system."
    ),
    consequences=[Consequence.REMEDIATE, Consequence.DOCUMENT, Consequence.REVIEW],
    penalty_range="Up to EUR 15,000,000 or 3% of total worldwide annual turnover",
    notification_timeline=None,
    enforcement_precedents=[],
    cross_references=["eu_ai_act_art15_1_cybersecurity"],
    severity_modifier=None,
)
```

---

## 6. Integration with Taint Analysis Output

**Estimated effort: 8-10 ideal hours**

### Input Pipeline

```python
def assess_finding(finding: ConfirmedFinding, rules: list[RegulatoryRule]) -> LegalAssessment:
    """run a confirmed finding through the regulatory engine."""
    engine = ForwardChainingEngine()
    for rule in rules:
        engine.add_rule(rule)
    assert_finding_facts(engine, finding)
    engine.run()
    obligations = engine.query_obligations()
    consequences = engine.query_consequences()
    trace = engine.get_trace()
    return LegalAssessment(
        finding_id=finding.finding.finding_id,
        obligations=obligations,
        consequences=consequences,
        inference_trace=trace,
        frameworks_triggered=list(set(
            o.arguments[1] for o in obligations  # framework is 2nd arg
        )),
    )
```

### Fact Extraction from ConfirmedFinding

The following properties are extracted and asserted as facts:

| Finding Property | Fact Asserted | Used By Rules |
|---|---|---|
| `vuln_class` (CWE identifier) | `Fact("vuln_class", ("CWE-89",))` | All vuln class preconditions |
| `data_categories` (from source annotation) | `Fact("data_category", ("nric",))` per category | All data category preconditions |
| `severity` | `Fact("severity", ("CRITICAL",))` | Severity threshold preconditions |
| `cross_border` flag | `Fact("cross_border", (True,))` | PDPA transfer obligation rules |
| `is_high_risk_ai` flag | `Fact("is_high_risk_ai", (True,))` | EU AI Act rules |
| `affected_individuals_estimate` | `Fact("affected_individuals", (1500,))` | PDPA S26D notification rule |
| `third_party_processor` flag | `Fact("third_party_processor", (True,))` | PDPA S24+S25 rule |
| `no_encryption_at_rest` flag | `Fact("no_encryption_at_rest", (True,))` | PDPA S24 encryption rule |

### Framework-Grouped Output

```python
@dataclass
class LegalAssessment:
    finding_id: str
    obligations: list[Fact]
    consequences: list[Fact]
    inference_trace: list[tuple[str, Fact]]
    frameworks_triggered: list[str]

    def group_by_framework(self) -> dict[str, list[Fact]]:
        """group obligations by regulatory framework for the legal memo."""
        grouped: dict[str, list[Fact]] = {}
        for o in self.obligations:
            framework = o.arguments[1]
            grouped.setdefault(framework, []).append(o)
        return grouped
```

---

## 7. Legal Memo Generation

**Estimated effort: 10-15 ideal hours**

### Memo Structure

For each confirmed finding with regulatory implications, generate a structured Markdown legal memo.

```python
def generate_legal_memo(
    finding: ConfirmedFinding,
    assessment: LegalAssessment,
    rules_db: dict[str, RegulatoryRule],
) -> str:
    """generate a structured legal memo for a confirmed finding."""
    lines: list[str] = []
    lines.append("# Regulatory Impact Assessment")
    lines.append("")
    lines.append("> **DISCLAIMER:** This analysis is generated by automated software and does "
                 "not constitute legal advice. Consult qualified legal counsel for definitive "
                 "regulatory guidance.")
    lines.append("")
    # ... (finding summary, framework sections, recommendations)
    return "\n".join(lines)
```

### Concrete Example: SQLi Affecting NRIC Data in a Singapore Fintech App

The following is an example legal memo for a SQL injection vulnerability (CWE-89) in a fintech application that processes NRIC numbers. This finding would trigger both PDPA S24 and MAS TRM 11 rules.

```markdown
# Regulatory Impact Assessment

> **DISCLAIMER:** This analysis is generated by automated software and does
> not constitute legal advice. Consult qualified legal counsel for definitive
> regulatory guidance.

## Finding Summary

| Field | Value |
|---|---|
| Finding ID | PIRANESI-2026-0042 |
| Vulnerability | SQL Injection (CWE-89) |
| File | src/routes/kyc.ts:87 |
| Taint Path | req.body.nric -> db.query() at line 87 |
| Confirmation | CONFIRMED (SQL error in response) |
| Data Categories | nric (Tier 1), name (Tier 4) |
| Affected Individuals | ~2,000 (estimated from database table row count) |
| Severity | CRITICAL |

## Regulatory Frameworks Implicated

### 1. Personal Data Protection Act 2012 (PDPA)

#### Section 24 -- Protection Obligation (STANDARD BREACH)

**Obligation:** An organisation shall protect personal data in its possession
or under its control by making reasonable security arrangements to prevent
unauthorised access, collection, use, disclosure, copying, modification,
disposal or similar risks.

**Analysis:** A confirmed SQL injection vulnerability in the KYC endpoint
allows an attacker to extract NRIC numbers and associated names from the
database. This constitutes a failure to make reasonable security arrangements
under Section 24.

**Penalty range:** Up to $1,000,000

**Comparable enforcement:**
- [2019] SGPDPC 7 (SingHealth) -- failure to implement adequate security
  measures; $250,000 penalty to IHiS, $250,000 to SingHealth

#### Section 24 -- Protection Obligation (AGGRAVATED -- Tier 1 Data)

**Obligation:** Enhanced protection expected for high-sensitivity personal
data (NRIC/FIN numbers).

**Analysis:** NRIC numbers are Tier 1 personal data under the PDPA. The PDPC
has consistently held that organisations handling high-sensitivity data are
expected to implement commensurately stronger security measures. The presence
of a basic SQL injection vulnerability in a system processing NRIC data
indicates a particularly significant gap in security arrangements.

**Penalty range:** Up to $1,000,000 (higher end expected)

#### Section 26D -- Mandatory Notification

**Obligation:** Notify PDPC within 3 calendar days of assessing the breach
as notifiable.

**Analysis:** The estimated number of affected individuals (~2,000) exceeds
the 500-individual threshold for mandatory notification under Section 26D(1).
The organisation must notify the PDPC and consider whether notification to
affected individuals is necessary.

**Notification timeline:** 3 calendar days from assessment

### 2. MAS Technology Risk Management Guidelines (MAS TRM)

#### Section 11.1 -- System Reliability

**Obligation:** The FI should ensure its IT systems are reliable and have
the capacity to process current and projected business volumes.

**Analysis:** A confirmed SQL injection vulnerability in a financial
institution's KYC system undermines system reliability. An attacker could
disrupt system availability through injection of malicious SQL queries.

**Penalty range:** Supervisory action (directions, conditions on license)

#### Section 11.0.5 -- Adequate Controls

**Obligation:** The FI should implement adequate controls to mitigate risks
arising from the use of IT.

**Analysis:** SQL injection indicates a failure to implement adequate input
validation controls. The FI should review input validation practices across
all systems processing financial data.

**Penalty range:** Supervisory action

## Risk Assessment

**Overall risk: CRITICAL**
- Tier 1 personal data (NRIC) exposed
- Confirmed exploitable vulnerability
- Mandatory breach notification triggered (>500 individuals)
- Dual regulatory exposure (PDPA + MAS TRM)

## Recommended Actions

1. **Immediate:** Remediate the SQL injection vulnerability in
   src/routes/kyc.ts:87 (parameterize the database query)
2. **Within 3 days:** Assess whether a notifiable data breach has occurred
   and, if so, notify the PDPC under Section 26D
3. **Within 1 week:** Review all database queries in the application for
   similar injection vulnerabilities
4. **Within 1 month:** Conduct a broader review of input validation practices
   across all systems processing personal data
5. **Document:** Record the vulnerability, remediation steps, and regulatory
   assessment for compliance records
```

---

## 8. Testing Strategy

**Estimated effort: 10-15 ideal hours**

### Unit Tests

**Forward-chaining engine tests:**
- Empty rule set, empty fact set -> no obligations derived.
- Single rule with satisfied preconditions -> obligation derived.
- Single rule with unsatisfied preconditions -> no obligation derived.
- Chained rules (Rule A fires, Rule B depends on Rule A) -> both fire.
- Fixed-point: engine terminates when no new facts are derived.
- Max iterations: engine terminates after `max_iterations` even if new facts are still being derived (safety bound).

**Rule evaluation tests (per rule):**

```python
def test_pdpa_s24_standard_fires():
    engine = ForwardChainingEngine()
    engine.add_rule(PDPA_S24_STANDARD)
    engine.add_fact(Fact("vuln_class", ("CWE-89",)))
    engine.add_fact(Fact("data_category", ("nric",)))
    engine.run()
    obligations = engine.query_obligations()
    assert len(obligations) == 1
    assert "Section 24" in obligations[0].arguments[2]

def test_pdpa_s24_does_not_fire_without_personal_data():
    engine = ForwardChainingEngine()
    engine.add_rule(PDPA_S24_STANDARD)
    engine.add_fact(Fact("vuln_class", ("CWE-89",)))
    # no data_category fact asserted
    engine.run()
    obligations = engine.query_obligations()
    assert len(obligations) == 0

def test_pdpa_s26d_requires_s24_and_threshold():
    engine = ForwardChainingEngine()
    engine.add_rule(PDPA_S24_STANDARD)
    engine.add_rule(PDPA_S26D_NOTIFICATION)
    engine.add_fact(Fact("vuln_class", ("CWE-89",)))
    engine.add_fact(Fact("data_category", ("nric",)))
    engine.add_fact(Fact("affected_individuals", (2000,)))
    engine.run()
    obligations = engine.query_obligations()
    rule_ids = [o.arguments[0] for o in obligations]
    assert "pdpa_s24_standard" in rule_ids
    assert "pdpa_s26d_notification" in rule_ids

def test_eu_ai_act_only_fires_for_ai_systems():
    engine = ForwardChainingEngine()
    engine.add_rule(EU_AI_ACT_ART15_1)
    engine.add_fact(Fact("vuln_class", ("CWE-89",)))
    # is_high_risk_ai NOT asserted
    engine.run()
    obligations = engine.query_obligations()
    assert len(obligations) == 0  # rule should not fire
```

**Data category resolution tests:**
- `resolve_data_categories("nric", ...)` -> `["nric"]`
- `resolve_data_categories("email", ...)` -> `["contact_email"]`
- `resolve_data_categories("ic_number", ...)` -> `["nric"]`
- `resolve_data_categories("obscure_field", ...)` -> LLM fallback (mock test)

**Sensitivity tier tests:**
- `get_sensitivity_tier(["nric"])` -> 1
- `get_sensitivity_tier(["financial_credit_card"])` -> 2
- `get_sensitivity_tier(["contact_email", "nric"])` -> 1 (highest sensitivity wins)
- `get_sensitivity_tier(["name"])` -> 4
- `get_sensitivity_tier([])` -> 4

### Integration Tests

**End-to-end memo generation:**

Given a mock `ConfirmedFinding` with:
- `vuln_class = "CWE-89"`
- `data_categories = ["nric", "name"]`
- `severity = "CRITICAL"`
- `affected_individuals_estimate = 2000`

Verify:
- PDPA S24 standard fires
- PDPA S24 aggravated (Tier 1) fires
- PDPA S26D notification fires
- MAS TRM does NOT fire (no financial data category)
- EU AI Act does NOT fire (not an AI system)
- Legal memo contains all expected sections
- Legal memo contains disclaimer

### Edge Cases

| Case | Expected Behavior |
|---|---|
| Finding with no personal data | No PDPA rules fire. Only framework-agnostic rules (if any) apply. |
| Finding in AI system (no personal data) | EU AI Act rules fire. PDPA rules do not fire. |
| Finding with multiple data categories spanning tiers | Highest sensitivity tier determines penalty range. All applicable rules fire. |
| Finding with `confirmation_level = UNVERIFIABLE` | Engine still runs (rules fire on vuln class + data categories). Memo notes unverifiable status. |
| Finding in a system with cross-border data flows | PDPA transfer rules fire (future rules, not in v1 scope). Fact is asserted for future use. |

---

## 9. Milestones with Effort Estimates

| # | Milestone | Deliverable | Effort (ideal hours) | Dependencies |
|---|---|---|---|---|
| M3.1 | Rule representation and storage | `models/regulatory.py` (Pydantic models), TOML rule files | 10-15h | Phase 1 data model (CandidateFinding) |
| M3.2 | Forward-chaining inference engine | `engine/regulatory_engine.py` | 20-25h | M3.1 |
| M3.3 | Personal data taxonomy and resolver | `taxonomy/personal_data.py` | 8-10h | None |
| M3.4 | PDPA Section 24 rule set (5 rules) | `rules/pdpa.toml` | 12-15h | M3.1, M3.2 |
| M3.5 | MAS TRM Section 11 rule set (3 rules) | `rules/mas_trm.toml` | 8-10h | M3.1, M3.2 |
| M3.6 | EU AI Act Article 15 rule set (3 rules) | `rules/eu_ai_act.toml` | 8-10h | M3.1, M3.2 |
| M3.7 | Integration layer (fact assertion from findings) | `engine/fact_asserter.py` | 8-10h | M3.2, Phase 2 ConfirmedFinding |
| M3.8 | Legal memo generator | `output/legal_memo.py` | 10-15h | M3.2, M3.7 |
| M3.9 | Unit test suite | Tests for engine, rules, taxonomy | 6-8h | M3.2-M3.6 |
| M3.10 | Integration test suite | End-to-end tests with mock findings | 4-7h | M3.7, M3.8 |

**Total: 94-125 ideal hours** (target: 80-120h, acceptable with buffer)

### Suggested Execution Order

```
M3.1 (rule representation) ──> M3.2 (engine) ──> M3.4 (PDPA rules)  ──┐
                                       │          M3.5 (MAS TRM rules)  ├──> M3.7 (integration) ──> M3.8 (memo gen)
                                       │          M3.6 (AI Act rules)  ──┘              │
M3.3 (taxonomy) ───────────────────────┘                                     M3.9 (unit) ──> M3.10 (integ)
```

M3.3 (taxonomy) and M3.1 (rule representation) can be developed in parallel. M3.4-M3.6 (rule sets) can be developed in parallel once M3.2 (engine) is complete.

---

## 10. Phase Dependencies

```
Phase 1 (Taint Analysis) ──── BLOCKS ────> Phase 3 (Regulatory Engine)
                                           (needs CandidateFinding data model
                                            with data_categories field)

Phase 2 (Exploit Verification) ── BLOCKS ──> Phase 3 integration layer
                                              (needs ConfirmedFinding as input,
                                               but can develop against mock data)

Phase 3 ──── BLOCKS ────> Phase 6 (Integration)
                           (integration needs working legal output)

Phase 3 can run IN PARALLEL with:
  - Phase 2 (Exploit Verification) -- develop against mock ConfirmedFindings
  - Phase 4 (LLM Orchestration) -- no dependency
```

### Early Start Strategy

Phase 3 can begin development before Phase 1 and Phase 2 are complete:

1. **After Phase 1 data model is finalized** (not after full taint engine is built): start M3.1 (rule representation) and M3.3 (taxonomy).
2. **In parallel with Phase 2**: develop M3.2 (engine), M3.4-M3.6 (rule sets), M3.8 (memo generator) using mock `ConfirmedFinding` objects.
3. **After Phase 2 delivers `ConfirmedFinding`**: wire up M3.7 (integration layer) and run M3.10 (integration tests) with real data.

### Interface Contract

Phase 3 requires from Phase 2:

```python
@dataclass
class ConfirmedFinding:
    finding: CandidateFinding          # includes vuln_class, data_categories, severity
    confirmation_level: Literal["CONFIRMED", "LIKELY", "UNVERIFIABLE"]
    evidence: str
    payload: SynthesizedPayload
    sandbox_capture: SandboxCapture
    reproducer_script: str
```

Phase 3 produces for Phase 6:

```python
@dataclass
class LegalAssessment:
    finding_id: str
    obligations: list[Fact]
    consequences: list[Fact]
    inference_trace: list[tuple[str, Fact]]
    frameworks_triggered: list[str]
    legal_memo: str                    # rendered Markdown memo
    risk_level: Literal["CRITICAL", "HIGH", "MEDIUM", "LOW"]
```
