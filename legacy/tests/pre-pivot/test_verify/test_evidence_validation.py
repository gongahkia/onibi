from __future__ import annotations

import json
from pathlib import Path

from piranesi.verify.validation import validate_evidence_bundle


def test_validate_evidence_bundle_accepts_confirmed_finding_with_artifact(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    evidence_dir = output_dir / "verification-evidence"
    evidence_dir.mkdir(parents=True)
    (evidence_dir / "f1.json").write_text('{"finding_id":"f1"}', encoding="utf-8")
    (output_dir / "verify.json").write_text(
        json.dumps(
            {
                "findings": [],
                "attempts": [
                    {
                        "finding_id": "f1",
                        "status": "confirmed",
                        "reason": "verified",
                        "proof_mode": "safe",
                        "evidence": ["payload reflected"],
                        "evidence_artifact_path": "verification-evidence/f1.json",
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    (output_dir / "report.json").write_text(
        json.dumps(
            {
                "findings": [
                    {
                        "finding_id": "f1",
                        "evidence_status": "confirmed",
                        "severity": "high",
                        "composite_risk_band": "high",
                        "source_location": {"file": "app.js", "line": 1},
                        "sink_location": {"file": "app.js", "line": 2},
                        "reproducer_script": "# Only run against systems you own\ncurl /",
                    }
                ]
            }
        ),
        encoding="utf-8",
    )

    report = validate_evidence_bundle(output_dir)

    assert report.valid is True
    assert report.checked_findings == 1
    assert report.findings[0].valid is True


def test_validate_evidence_bundle_rejects_confirmed_finding_without_attempt(
    tmp_path: Path,
) -> None:
    output_dir = tmp_path / "out"
    output_dir.mkdir()
    (output_dir / "verify.json").write_text('{"findings":[],"attempts":[]}', encoding="utf-8")
    (output_dir / "report.json").write_text(
        json.dumps(
            {
                "findings": [
                    {
                        "finding_id": "f1",
                        "evidence_status": "confirmed",
                        "severity": "high",
                        "source_location": {"file": "app.js", "line": 1},
                        "sink_location": {"file": "app.js", "line": 2},
                        "reproducer_script": "# Only run against systems you own\ncurl /",
                    }
                ]
            }
        ),
        encoding="utf-8",
    )

    report = validate_evidence_bundle(output_dir)

    assert report.valid is False
    assert report.invalid_findings == 1
    failed_checks = {check.name for check in report.findings[0].checks if not check.passed}
    assert "log_corroboration" in failed_checks
    assert "claims_vs_raw" in failed_checks
