from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from typer.testing import CliRunner

from piranesi.advisory.db import (
    AdvisoryDB,
    AdvisorySnapshotProvenance,
    get_advisory_db_status,
    utc_now,
)
from piranesi.advisory.policy import evaluate_trust_policy
from piranesi.cli import app

runner = CliRunner()


def test_evaluate_trust_policy_flags_verified_only_when_unsigned(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path):
        pass
    status = get_advisory_db_status(db_path)

    outcome = evaluate_trust_policy(
        status,
        mode="verified-only",
        on_missing="ignore",
        on_stale="ignore",
        on_unsigned="warn",
    )

    assert outcome.allowed is False
    assert outcome.violations
    assert any("verified-only policy requires" in item for item in outcome.violations)


def test_evaluate_trust_policy_can_fail_on_stale(tmp_path: Path) -> None:
    missing_path = tmp_path / "missing.db"
    status = get_advisory_db_status(missing_path)

    outcome = evaluate_trust_policy(
        status,
        mode="permissive",
        on_missing="fail",
        on_stale="warn",
        on_unsigned="ignore",
    )

    assert outcome.allowed is False
    assert "advisory database is missing" in outcome.violations


def test_evaluate_trust_policy_can_fail_on_stale_freshness(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path) as db:
        db.upsert_sync_metadata(
            source="osv",
            last_sync="2020-01-01T00:00:00Z",
            last_cursor=None,
            record_count=1,
        )
        db.upsert_snapshot_provenance(
            AdvisorySnapshotProvenance(
                source_path=str(tmp_path / "snapshot.db"),
                snapshot_sha256="abc123",
                manifest_path=str(tmp_path / "snapshot.db.manifest.json"),
                manifest_sha256="def456",
                signature_scheme="hmac-sha256",
                signature_signer="appsec",
                signature_value="deadbeef",
                verified=True,
                verification_reason="signature verified",
                imported_at=utc_now(),
            )
        )
    status = get_advisory_db_status(
        db_path,
        stale_after_days=14,
        now=datetime(2026, 4, 19, tzinfo=UTC),
    )

    outcome = evaluate_trust_policy(
        status,
        mode="permissive",
        on_missing="ignore",
        on_stale="fail",
        on_unsigned="ignore",
    )

    assert outcome.allowed is False
    assert "advisory database is stale" in outcome.violations


def test_cli_advisory_status_policy_failure_returns_non_zero(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path):
        pass

    result = runner.invoke(
        app,
        [
            "advisory",
            "status",
            "--db",
            str(db_path),
            "--trust-policy",
            "verified-only",
            "--json",
        ],
    )

    assert result.exit_code == 1
    payload = json.loads(result.stdout)
    assert payload["policy"]["allowed"] is False
    assert payload["policy"]["violations"]


def test_cli_advisory_status_policy_passes_for_verified_snapshot(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path) as db:
        db.upsert_snapshot_provenance(
            AdvisorySnapshotProvenance(
                source_path=str(tmp_path / "snapshot.db"),
                snapshot_sha256="abc123",
                manifest_path=str(tmp_path / "snapshot.db.manifest.json"),
                manifest_sha256="def456",
                signature_scheme="hmac-sha256",
                signature_signer="appsec",
                signature_value="deadbeef",
                verified=True,
                verification_reason="signature verified",
                imported_at="2026-04-19T00:00:00Z",
            )
        )

    result = runner.invoke(
        app,
        [
            "advisory",
            "status",
            "--db",
            str(db_path),
            "--trust-policy",
            "verified-only",
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["policy"]["allowed"] is True
    assert payload["trust_state"] == "verified"


def test_cli_advisory_status_fails_when_unsigned_policy_is_fail(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path):
        pass

    result = runner.invoke(
        app,
        [
            "advisory",
            "status",
            "--db",
            str(db_path),
            "--on-unsigned",
            "fail",
            "--json",
        ],
    )

    assert result.exit_code == 1
    payload = json.loads(result.stdout)
    assert payload["policy"]["allowed"] is False
    assert any("not cryptographically verified" in item for item in payload["policy"]["violations"])
