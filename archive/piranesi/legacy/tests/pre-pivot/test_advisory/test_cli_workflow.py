from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.advisory.db import AdvisoryDB, utc_now
from piranesi.advisory.models import Advisory, AffectedPackage
from piranesi.advisory.sync import SyncResult
from piranesi.cli import app

runner = CliRunner()


def _seed_advisory_db(path: Path) -> None:
    with AdvisoryDB(path) as db:
        db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-1111",
                    cve_id="CVE-2026-1111",
                    ghsa_id="GHSA-fvqr-27wr-82fm",
                    cwe_ids=("CWE-1321",),
                    title="Prototype Pollution in lodash",
                    description="Prototype pollution vulnerability",
                    affected_packages=(
                        AffectedPackage(
                            ecosystem="npm",
                            name="lodash",
                            vulnerable_ranges=("<4.17.21",),
                            fixed_versions=("4.17.21",),
                        ),
                    ),
                    severity="high",
                    fix_available=True,
                    fix_version="4.17.21",
                    sources=("osv",),
                    references=("https://osv.dev",),
                ),
            )
        )
        db.upsert_sync_metadata(
            source="osv",
            last_sync=utc_now(),
            last_cursor=None,
            record_count=1,
        )


def test_advisory_status_reports_missing_db(tmp_path: Path) -> None:
    db_path = tmp_path / "missing.db"

    result = runner.invoke(app, ["advisory", "status", "--db", str(db_path), "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["exists"] is False
    assert payload["freshness"] == "missing"
    assert payload["warnings"]


def test_advisory_import_and_search_workflow(tmp_path: Path) -> None:
    source_db = tmp_path / "source.db"
    destination_db = tmp_path / "destination.db"
    _seed_advisory_db(source_db)

    import_result = runner.invoke(
        app,
        ["advisory", "import", str(source_db), "--db", str(destination_db), "--json"],
    )

    assert import_result.exit_code == 0
    import_payload = json.loads(import_result.stdout)
    assert import_payload["exists"] is True
    assert import_payload["advisory_count"] == 1

    search_result = runner.invoke(
        app,
        [
            "advisory",
            "search",
            "--db",
            str(destination_db),
            "--ecosystem",
            "npm",
            "--package",
            "lodash",
            "--json",
        ],
    )

    assert search_result.exit_code == 0
    search_payload = json.loads(search_result.stdout)
    assert search_payload["count"] == 1
    assert search_payload["results"][0]["advisory_id"] == "CVE-2026-1111"


def test_advisory_update_uses_explicit_sync_command(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    db_path = tmp_path / "advisory.db"

    def fake_sync_advisories(
        db: AdvisoryDB,
        *,
        sources: tuple[str, ...] = ("osv",),
        full: bool = False,
        ecosystems: tuple[str, ...] | None = None,
        session=None,
        github_token: str | None = None,
        nvd_api_key: str | None = None,
    ) -> SyncResult:
        _ = (full, ecosystems, session, github_token, nvd_api_key)
        db.upsert_advisories(
            (
                Advisory(
                    advisory_id="CVE-2026-9999",
                    cve_id="CVE-2026-9999",
                    ghsa_id=None,
                    cwe_ids=("CWE-79",),
                    title="Example advisory",
                    description="Example",
                    affected_packages=(
                        AffectedPackage(
                            ecosystem="npm",
                            name="example",
                            vulnerable_ranges=("<1.0.1",),
                            fixed_versions=("1.0.1",),
                        ),
                    ),
                    severity="medium",
                    fix_available=True,
                    fix_version="1.0.1",
                    sources=sources,
                    references=("https://example.test/advisory",),
                ),
            )
        )
        db.upsert_sync_metadata(
            source=sources[0],
            last_sync=utc_now(),
            last_cursor=None,
            record_count=1,
        )
        return SyncResult(
            source_counts={sources[0]: 1},
            total_upserted=1,
            epss_updated=0,
            exploit_updated=0,
        )

    monkeypatch.setattr("piranesi.advisory.sync_advisories", fake_sync_advisories)

    result = runner.invoke(
        app,
        [
            "advisory",
            "update",
            "--db",
            str(db_path),
            "--source",
            "osv",
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["sync"]["total_upserted"] == 1
    assert payload["db"]["exists"] is True
    assert payload["db"]["advisory_count"] == 1


def test_advisory_sign_snapshot_and_verified_import(tmp_path: Path) -> None:
    source_db = tmp_path / "source.db"
    destination_db = tmp_path / "destination.db"
    key_file = tmp_path / "trust.key"
    manifest_path = tmp_path / "source.db.manifest.json"
    _seed_advisory_db(source_db)
    key_file.write_text("shared-secret", encoding="utf-8")

    sign_result = runner.invoke(
        app,
        [
            "advisory",
            "sign-snapshot",
            str(source_db),
            "--manifest",
            str(manifest_path),
            "--key-file",
            str(key_file),
            "--signer",
            "security-team",
            "--json",
        ],
    )
    assert sign_result.exit_code == 0
    sign_payload = json.loads(sign_result.stdout)
    assert sign_payload["signature"]["scheme"] == "hmac-sha256"

    import_result = runner.invoke(
        app,
        [
            "advisory",
            "import",
            str(source_db),
            "--db",
            str(destination_db),
            "--manifest",
            str(manifest_path),
            "--trust-key",
            str(key_file),
            "--require-verified-snapshot",
            "--json",
        ],
    )
    assert import_result.exit_code == 0
    import_payload = json.loads(import_result.stdout)
    assert import_payload["verification"]["verified"] is True
    assert import_payload["trust_state"] == "verified"


def test_advisory_import_rejects_tampered_snapshot_when_verification_required(
    tmp_path: Path,
) -> None:
    source_db = tmp_path / "source.db"
    destination_db = tmp_path / "destination.db"
    key_file = tmp_path / "trust.key"
    manifest_path = tmp_path / "source.db.manifest.json"
    _seed_advisory_db(source_db)
    key_file.write_text("shared-secret", encoding="utf-8")

    sign_result = runner.invoke(
        app,
        [
            "advisory",
            "sign-snapshot",
            str(source_db),
            "--manifest",
            str(manifest_path),
            "--key-file",
            str(key_file),
            "--json",
        ],
    )
    assert sign_result.exit_code == 0

    source_db.write_bytes(source_db.read_bytes() + b"tamper")
    import_result = runner.invoke(
        app,
        [
            "advisory",
            "import",
            str(source_db),
            "--db",
            str(destination_db),
            "--manifest",
            str(manifest_path),
            "--trust-key",
            str(key_file),
            "--require-verified-snapshot",
            "--json",
        ],
    )
    assert import_result.exit_code == 1
    assert "snapshot verification policy failed" in import_result.stdout


def test_advisory_import_require_manifest_fails_without_manifest(tmp_path: Path) -> None:
    source_db = tmp_path / "source.db"
    destination_db = tmp_path / "destination.db"
    _seed_advisory_db(source_db)

    result = runner.invoke(
        app,
        [
            "advisory",
            "import",
            str(source_db),
            "--db",
            str(destination_db),
            "--require-manifest",
            "--json",
        ],
    )

    assert result.exit_code == 1
    assert "--require-manifest was set" in result.stdout
