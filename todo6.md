# TODO 6: Add Exploitability, Blast Radius, And Remediation Urgency Ranking

## Goal

Replace the current host ranking with a richer prioritization model that scores findings by exploitability, blast radius, remediation urgency, severity, confidence, and evidence quality.

The proposal calls for ranking findings by exploitability, blast radius, and remediation urgency. Current host mode mostly sorts by severity and confidence and subtracts fixed posture-score penalties.

## Current State

Relevant files:

- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `src/piranesi/host/report.py`
- `src/piranesi/cli.py`
- `tests/test_host_posture.py`
- `src/piranesi/advisory/*`
- `src/piranesi/advisory/epss.py`
- `src/piranesi/advisory/exploit.py`

Current host ranking:

- `_rank_findings()` sorts by severity rank, confidence, title.
- `_posture_score()` subtracts fixed penalties by severity.
- `_top_actions()` groups by broad category.

Legacy source-code reporting already has richer risk concepts. Reuse ideas where practical, but do not tightly couple host mode to legacy SAST models if that causes churn.

## Desired Model

Add a host risk model:

```python
class HostRiskScore(BaseModel):
    total: float = Field(ge=0.0, le=100.0)
    severity: float
    confidence: float
    exploitability: float
    blast_radius: float
    remediation_urgency: float
    evidence_quality: float
    rationale: list[str] = Field(default_factory=list)
```

Add to `HostFinding`:

```python
risk: HostRiskScore | None = None
```

If changing `HostFinding` is too broad, add a `risk_scores: dict[str, HostRiskScore]` to `HostPostureReport`, keyed by finding id. Prefer embedding on the finding for report portability.

## Scoring Dimensions

### Severity

Map existing severity:

- critical: 1.0
- high: 0.8
- medium: 0.55
- low: 0.25
- informational: 0.05

### Confidence

Use `finding.confidence`.

### Exploitability

Signals:

- Public listener: high.
- Private listener with `--treat-private-as-public`: medium-high.
- SSH public plus password auth: high.
- Trivy CVE with known exploit or CISA KEV: high.
- Trivy CVE with high EPSS: high.
- Misconfiguration with no exposure: lower.
- Coverage gaps: very low.

Use existing advisory modules if practical:

- CISA KEV support exists in `src/piranesi/advisory/exploit.py`.
- EPSS support exists in `src/piranesi/advisory/epss.py`.

Avoid live network calls during `assess` unless there is already an explicit sync/cache workflow. Prefer local advisory DB or optional imported intel.

### Blast Radius

Signals:

- Public interface or global address.
- Any-address bind (`0.0.0.0`, `::`).
- High-risk service port.
- Privileged user or root-impacting setting.
- Kernel setting.
- Host has multiple IPs/interfaces.
- Running process evidence confirms service is active.

### Remediation Urgency

Signals:

- Fixed version available.
- Security update pending.
- Critical severity.
- Known exploited vulnerability.
- Internet-exposed service.
- Easy config remediation such as SSH hardening.
- Reboot likely required, if kernel/core package.

### Evidence Quality

Signals:

- Direct tool evidence with source and value.
- Multiple corroborating evidence items.
- Collection manifest shows required capability healthy.
- LLM-only evidence should be lower than deterministic direct evidence.
- Coverage findings should not inflate risk.

## Ranking

Update `_rank_findings()` to sort by:

1. Risk total descending, when present.
2. Severity rank.
3. Confidence.
4. Title.

Update `posture_score` to derive from risk totals or keep severity penalties but document the choice. Prefer a risk-based penalty with caps so many low-risk findings do not reduce score to zero.

## Reporting

JSON:

- Include risk score object in each finding.

Markdown:

- Add "Risk score: X/100".
- Add concise risk rationale.

Dashboard/PDF:

- If todo1 has landed, render risk scores there too.

Top actions:

- Use risk score ordering, not severity-only ordering.
- Include the highest-risk finding IDs per category.

## Tests

Add tests for:

- Public Redis ranks above missing Trivy evidence.
- Public SSH + password auth ranks above SSH public alone.
- CVE with fixed version has remediation urgency.
- KEV/EPSS local intel, if available, raises exploitability.
- Coverage findings do not dominate top actions.
- Markdown includes risk score.

Use small fixture snapshots rather than relying on live advisory downloads.

## Acceptance Criteria

- Every host finding has a risk score.
- Ranking is risk-based and deterministic.
- Top actions reflect risk, not just category presence.
- Reports explain why a finding is high priority.
- Existing host tests are updated without weakening behavior.

## Out Of Scope

- Full asset inventory criticality.
- Live internet intel fetching during assessment.
- Organization-specific business impact scoring.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py
uv run piranesi assess tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-risk-out --format both
```

