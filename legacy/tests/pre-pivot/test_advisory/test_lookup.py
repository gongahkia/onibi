from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.advisory.db import AdvisoryDB
from piranesi.advisory.lookup import lookup_dependencies, parse_lockfiles
from piranesi.advisory.models import Advisory, AffectedPackage


def test_lookup_npm_package(populated_db: AdvisoryDB, tmp_path: Path) -> None:
    (tmp_path / "package-lock.json").write_text(
        (
            "{\n"
            '  "name": "demo",\n'
            '  "lockfileVersion": 3,\n'
            '  "packages": {\n'
            '    "": {"name": "demo", "version": "1.0.0"},\n'
            '    "node_modules/lodash": {"version": "4.17.20"}\n'
            "  }\n"
            "}\n"
        ),
        encoding="utf-8",
    )

    findings = lookup_dependencies(populated_db, tmp_path)

    assert len(findings) == 1
    finding = findings[0]
    assert finding.metadata["package"] == "lodash"
    assert finding.metadata["fix_version"] == "4.17.21"
    assert finding.metadata["epss_label"] == "actively_exploited_risk"
    assert finding.metadata["advisory_priority_precedence"] == "epss_v4"
    assert finding.metadata["epss_model_version"] == "v4"


def test_lookup_no_match(populated_db: AdvisoryDB, tmp_path: Path) -> None:
    (tmp_path / "package-lock.json").write_text(
        (
            "{\n"
            '  "name": "demo",\n'
            '  "lockfileVersion": 3,\n'
            '  "packages": {\n'
            '    "": {"name": "demo", "version": "1.0.0"},\n'
            '    "node_modules/lodash": {"version": "4.17.21"}\n'
            "  }\n"
            "}\n"
        ),
        encoding="utf-8",
    )

    assert lookup_dependencies(populated_db, tmp_path) == []


def test_lockfile_parsing_yarn(tmp_path: Path) -> None:
    (tmp_path / "yarn.lock").write_text(
        (
            '"lodash@^4.17.0":\n'
            '  version "4.17.20"\n'
            '  resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.20.tgz"\n'
        ),
        encoding="utf-8",
    )

    dependencies = parse_lockfiles(tmp_path)

    assert any(dep.name == "lodash" and dep.version == "4.17.20" for dep in dependencies)


def test_lockfile_parsing_go_sum(tmp_path: Path) -> None:
    (tmp_path / "go.sum").write_text(
        (
            "github.com/example/lib v1.2.3 h1:checksum\n"
            "github.com/example/lib v1.2.3/go.mod h1:checksum\n"
        ),
        encoding="utf-8",
    )

    dependencies = parse_lockfiles(tmp_path)

    assert any(
        dep.name == "github.com/example/lib" and dep.version == "v1.2.3" for dep in dependencies
    )


def test_lockfile_parsing_gemfile_lock(tmp_path: Path) -> None:
    (tmp_path / "Gemfile.lock").write_text(
        (
            "GEM\n"
            "  remote: https://rubygems.org/\n"
            "  specs:\n"
            "    rack (2.2.4)\n"
            "    rails (7.0.4)\n"
            "      rack (= 2.2.4)\n"
            "\n"
            "PLATFORMS\n"
            "  ruby\n"
        ),
        encoding="utf-8",
    )

    dependencies = parse_lockfiles(tmp_path)

    assert any(
        dep.ecosystem == "rubygems" and dep.name == "rack" and dep.version == "2.2.4"
        for dep in dependencies
    )
    assert any(
        dep.ecosystem == "rubygems" and dep.name == "rails" and dep.version == "7.0.4"
        for dep in dependencies
    )


def test_lookup_rubygems_package(populated_db: AdvisoryDB, tmp_path: Path) -> None:
    (tmp_path / "Gemfile.lock").write_text(
        ("GEM\n  remote: https://rubygems.org/\n  specs:\n    rack (2.2.4)\n\nPLATFORMS\n  ruby\n"),
        encoding="utf-8",
    )

    findings = lookup_dependencies(populated_db, tmp_path)

    assert len(findings) == 1
    finding = findings[0]
    assert finding.metadata["ecosystem"] == "rubygems"
    assert finding.metadata["package"] == "rack"
    assert finding.metadata["package_version"] == "2.2.4"
    assert finding.metadata["advisory_id"] == "CVE-2026-3333"
    assert finding.metadata["fix_version"] == "2.2.6.2"


def test_export_import_roundtrip(tmp_path: Path) -> None:
    source_db_path = tmp_path / "source.db"
    export_path = tmp_path / "export.db"
    imported_db_path = tmp_path / "imported.db"
    with AdvisoryDB(source_db_path) as source_db:
        source_db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-1111",
                    cve_id="CVE-2026-1111",
                    ghsa_id=None,
                    cwe_ids=("CWE-79",),
                    title="advisory",
                    description="description",
                    affected_packages=(
                        AffectedPackage(
                            ecosystem="npm",
                            name="lodash",
                            vulnerable_ranges=("<4.17.21",),
                            fixed_versions=("4.17.21",),
                        ),
                    ),
                    severity="high",
                    sources=("osv",),
                ),
            )
        )
        source_db.export_to(export_path)

    with AdvisoryDB(imported_db_path) as imported_db:
        imported_db.import_from(export_path)
        advisory = imported_db.get_advisory("CVE-2026-1111")

    assert advisory is not None
    assert advisory.advisory.affected_packages[0].name == "lodash"


def test_merge_import(tmp_path: Path) -> None:
    destination_path = tmp_path / "destination.db"
    source_path = tmp_path / "source.db"
    with AdvisoryDB(destination_path) as destination_db:
        destination_db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-1111",
                    cve_id="CVE-2026-1111",
                    ghsa_id=None,
                    cwe_ids=(),
                    title="existing",
                    description="",
                    affected_packages=(),
                    severity="medium",
                    sources=("nvd",),
                ),
            )
        )
    with AdvisoryDB(source_path) as source_db:
        source_db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-2222",
                    cve_id="CVE-2026-2222",
                    ghsa_id=None,
                    cwe_ids=(),
                    title="new",
                    description="",
                    affected_packages=(),
                    severity="high",
                    sources=("osv",),
                ),
            )
        )
    with AdvisoryDB(destination_path) as destination_db:
        destination_db.import_from(source_path, merge=True)
        advisories = destination_db.iter_all_advisories()

    assert {row.advisory.advisory_id for row in advisories} == {"CVE-2026-1111", "CVE-2026-2222"}


def _build_populated_db(path: Path) -> AdvisoryDB:
    db = AdvisoryDB(path)
    db.upsert_advisories(
        (
            Advisory(
                advisory_id="CVE-2026-1111",
                cve_id="CVE-2026-1111",
                ghsa_id="GHSA-fvqr-27wr-82fm",
                cwe_ids=("CWE-1321",),
                title="Prototype Pollution in lodash",
                description="Description",
                affected_packages=(
                    AffectedPackage(
                        ecosystem="npm",
                        name="lodash",
                        vulnerable_ranges=("<4.17.21",),
                        fixed_versions=("4.17.21",),
                    ),
                ),
                severity="high",
                epss_score=0.62,
                epss_percentile=0.98,
                fix_available=True,
                fix_version="4.17.21",
                sources=("ghsa", "osv"),
                references=("https://github.com/advisories/GHSA-fvqr-27wr-82fm",),
            ),
            Advisory(
                advisory_id="CVE-2026-3333",
                cve_id="CVE-2026-3333",
                ghsa_id=None,
                cwe_ids=("CWE-400",),
                title="DoS in rack multipart parser",
                description="Description",
                affected_packages=(
                    AffectedPackage(
                        ecosystem="rubygems",
                        name="rack",
                        vulnerable_ranges=("<2.2.6.2",),
                        fixed_versions=("2.2.6.2",),
                    ),
                ),
                severity="high",
                fix_available=True,
                fix_version="2.2.6.2",
                sources=("rubysec",),
                references=("https://github.com/rubysec/ruby-advisory-db",),
            ),
        )
    )
    return db


def _close_db(db: AdvisoryDB) -> None:
    db.close()


@pytest.fixture
def populated_db(tmp_path: Path):  # type: ignore[no-untyped-def]
    db = _build_populated_db(tmp_path / "advisory.db")
    try:
        yield db
    finally:
        _close_db(db)
