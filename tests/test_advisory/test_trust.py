from __future__ import annotations

from pathlib import Path

from piranesi.advisory.db import AdvisoryDB, AdvisorySnapshotProvenance, get_advisory_db_status
from piranesi.advisory.trust import verify_snapshot_manifest, write_snapshot_manifest


def test_snapshot_manifest_sign_and_verify_roundtrip(tmp_path: Path) -> None:
    snapshot = tmp_path / "advisory.db"
    snapshot.write_bytes(b"snapshot-bytes")
    manifest = tmp_path / "advisory.db.manifest.json"

    write_snapshot_manifest(
        snapshot,
        manifest,
        signing_key="test-secret",
        signer="security-team",
    )
    result = verify_snapshot_manifest(
        snapshot,
        manifest,
        verification_key="test-secret",
    )

    assert result.verified is True
    assert result.tampered is False
    assert result.has_signature is True
    assert result.signature_scheme == "hmac-sha256"
    assert result.signature_signer == "security-team"


def test_snapshot_manifest_verification_detects_tampering(tmp_path: Path) -> None:
    snapshot = tmp_path / "advisory.db"
    snapshot.write_bytes(b"snapshot-bytes")
    manifest = tmp_path / "advisory.db.manifest.json"

    write_snapshot_manifest(
        snapshot,
        manifest,
        signing_key="test-secret",
        signer="security-team",
    )
    snapshot.write_bytes(b"mutated-bytes")
    result = verify_snapshot_manifest(
        snapshot,
        manifest,
        verification_key="test-secret",
    )

    assert result.verified is False
    assert result.tampered is True
    assert result.reason == "snapshot digest mismatch"


def test_advisory_db_status_defaults_to_unsigned_without_provenance(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path):
        pass

    status = get_advisory_db_status(db_path)

    assert status.exists is True
    assert status.trust_state == "unsigned"
    assert status.provenance_verified is None
    assert any("no snapshot provenance metadata" in warning for warning in status.warnings)


def test_advisory_db_status_reports_verified_provenance(tmp_path: Path) -> None:
    db_path = tmp_path / "advisory.db"
    with AdvisoryDB(db_path) as db:
        db.upsert_snapshot_provenance(
            AdvisorySnapshotProvenance(
                source_path=str(tmp_path / "signed-advisory.db"),
                snapshot_sha256="abc123",
                manifest_path=str(tmp_path / "signed-advisory.db.manifest.json"),
                manifest_sha256="def456",
                signature_scheme="hmac-sha256",
                signature_signer="security-team",
                signature_value="feedbeef",
                verified=True,
                verification_reason="signature verified",
                imported_at="2026-04-19T00:00:00Z",
            )
        )

    status = get_advisory_db_status(db_path)

    assert status.trust_state == "verified"
    assert status.provenance_verified is True
    assert status.provenance_signature_scheme == "hmac-sha256"
    assert status.provenance_signature_signer == "security-team"
