from __future__ import annotations

from tests.test_threat._helpers import make_entry_point, make_finding

from piranesi.threat.dread import score_dread


def test_high_severity_public_route_scores_high() -> None:
    finding = make_finding(vuln_class="CWE-89", severity="critical", confidence=0.95)
    entry = make_entry_point(http_method="POST", route_pattern="/api/public")
    score = score_dread(finding, entry_points=[entry])
    assert score.normalized >= 8.0
    assert score.risk_level == "critical"


def test_low_severity_internal_scores_low() -> None:
    finding = make_finding(vuln_class="CWE-200", severity="low", confidence=0.5)
    score = score_dread(finding, entry_points=[])
    assert score.normalized < 4.0
    assert score.risk_level == "low"


def test_epss_boosts_exploitability() -> None:
    finding = make_finding(
        vuln_class="CWE-89",
        severity="high",
        metadata={"epss_score": 0.6},
    )
    score = score_dread(finding)
    assert score.exploitability >= 8


def test_dread_score_range() -> None:
    finding = make_finding(vuln_class="CWE-79", severity="medium")
    score = score_dread(finding)
    for dimension in (
        score.damage,
        score.reproducibility,
        score.exploitability,
        score.affected_users,
        score.discoverability,
    ):
        assert 1 <= dimension <= 10
    assert 1.0 <= score.normalized <= 10.0
