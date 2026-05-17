from __future__ import annotations

from pathlib import Path

from piranesi.advisory.db import AdvisoryDB
from piranesi.advisory.epss import enrich_epss, epss_label
from piranesi.advisory.models import Advisory, AffectedPackage


def test_epss_enrichment_updates_db(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path) as db:
        db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-1111",
                    cve_id="CVE-2026-1111",
                    ghsa_id=None,
                    cwe_ids=("CWE-79",),
                    title="Test advisory",
                    description="Description",
                    affected_packages=(
                        AffectedPackage(
                            ecosystem="npm",
                            name="demo",
                            vulnerable_ranges=("<1.2.3",),
                            fixed_versions=("1.2.3",),
                        ),
                    ),
                    severity="high",
                    sources=("osv",),
                ),
            )
        )

        class _FakeResponse:
            def raise_for_status(self) -> None:
                return None

            def json(self) -> object:
                return {
                    "data": [
                        {"cve": "CVE-2026-1111", "epss": "0.62", "percentile": "0.98"},
                    ]
                }

        class _FakeSession:
            def get(self, url: str, params: dict[str, str], timeout: int = 30):  # type: ignore[no-untyped-def]
                _ = timeout
                assert "CVE-2026-1111" in params["cve"]
                assert url.endswith("/epss")
                return _FakeResponse()

        updated = enrich_epss(db, session=_FakeSession(), sleep_s=0)  # type: ignore[arg-type]
        advisory = db.get_advisory("CVE-2026-1111")

    assert updated == 1
    assert advisory is not None
    assert advisory.advisory.epss_score == 0.62
    assert advisory.advisory.epss_percentile == 0.98


def test_epss_batch_splitting(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path) as db:
        advisories = []
        for index in range(205):
            cve_id = f"CVE-2026-{1000 + index}"
            advisories.append(
                Advisory(
                    advisory_id=cve_id,
                    cve_id=cve_id,
                    ghsa_id=None,
                    cwe_ids=(),
                    title=cve_id,
                    description="",
                    affected_packages=(),
                    severity="medium",
                    sources=("nvd",),
                )
            )
        db.upsert_advisories(tuple(advisories))
        calls: list[str] = []

        class _FakeResponse:
            def __init__(self, cves: str) -> None:
                self._cves = cves

            def raise_for_status(self) -> None:
                return None

            def json(self) -> object:
                return {
                    "data": [
                        {"cve": cve, "epss": "0.01", "percentile": "0.50"}
                        for cve in self._cves.split(",")
                    ]
                }

        class _FakeSession:
            def get(self, url: str, params: dict[str, str], timeout: int = 30):  # type: ignore[no-untyped-def]
                _ = (url, timeout)
                calls.append(params["cve"])
                return _FakeResponse(params["cve"])

        updated = enrich_epss(db, session=_FakeSession(), batch_size=100, sleep_s=0)  # type: ignore[arg-type]

    assert updated == 205
    assert [len(batch.split(",")) for batch in calls] == [100, 100, 5]


def test_epss_label_thresholds() -> None:
    assert epss_label(0.5, 0.97) == "actively_exploited_risk"
    assert epss_label(0.1, 0.9) == "high_exploit_probability"
    assert epss_label(0.01, 0.5) == "moderate_exploit_probability"
    assert epss_label(0.001, 0.1) == "low_exploit_probability"
