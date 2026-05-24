from __future__ import annotations

from piranesi.advisory.models import Advisory, AffectedPackage, ExploitStatus
from piranesi.advisory.risk import advisory_priority_signal, infer_cvss_version


def _advisory(
    *,
    exploit_status: ExploitStatus = ExploitStatus.NONE,
    exploit_sources: tuple[str, ...] = (),
    epss_score: float | None = None,
    cvss_score: float | None = None,
    cvss_vector: str | None = None,
) -> Advisory:
    return Advisory(
        advisory_id="CVE-2026-0001",
        cve_id="CVE-2026-0001",
        ghsa_id=None,
        cwe_ids=("CWE-79",),
        title="Example",
        description="Example advisory",
        affected_packages=(
            AffectedPackage(
                ecosystem="npm",
                name="example",
                vulnerable_ranges=("<1.0.1",),
                fixed_versions=("1.0.1",),
            ),
        ),
        severity="high",
        cvss_score=cvss_score,
        cvss_vector=cvss_vector,
        epss_score=epss_score,
        epss_percentile=None,
        exploit_status=exploit_status,
        exploit_sources=exploit_sources,
        fix_available=True,
        fix_version="1.0.1",
        sources=("nvd",),
    )


def test_advisory_priority_prefers_kev_signal() -> None:
    signal = advisory_priority_signal(
        _advisory(
            exploit_status=ExploitStatus.IN_THE_WILD,
            exploit_sources=("cisa_kev",),
            epss_score=0.95,
            cvss_score=9.8,
            cvss_vector="CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
        )
    )
    assert signal.precedence == "kev"
    assert signal.score == 10.0
    assert signal.tier == "critical"
    assert signal.kev_listed is True


def test_advisory_priority_uses_epss_before_cvss() -> None:
    signal = advisory_priority_signal(
        _advisory(
            epss_score=0.72,
            cvss_score=9.8,
            cvss_vector="CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
        )
    )
    assert signal.precedence == "epss_v4"
    assert signal.cvss_version == "4.0"
    assert signal.epss_model_version == "v4"
    assert signal.score == 7.5


def test_infer_cvss_version_supports_v4_and_v31() -> None:
    assert (
        infer_cvss_version("CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H")
        == "4.0"
    )
    assert infer_cvss_version("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H") == "3.1"
