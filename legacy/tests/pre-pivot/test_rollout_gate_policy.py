from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


def _script_path() -> Path:
    return Path(__file__).resolve().parents[1] / "scripts" / "check_rollout_gates.py"


def _write_artifacts(base: Path, *, include_verify: bool, critical_active: int = 0) -> None:
    base.mkdir(parents=True, exist_ok=True)
    (base / "scan.json").write_text("{}", encoding="utf-8")
    (base / "detect.json").write_text("{}", encoding="utf-8")
    (base / "legal.json").write_text("{}", encoding="utf-8")

    if include_verify:
        verify_payload = {
            "attempts": [
                {
                    "finding_id": "f-1",
                    "status": "confirmed",
                    "reason": "confirmed",
                    "proof_mode": "safe",
                    "rich_evidence": {
                        "redaction_status": {
                            "applied": True,
                            "redacted_value_count": 1,
                            "redacted_fields": ["authorization"],
                        }
                    },
                }
            ]
        }
        (base / "verify.json").write_text(json.dumps(verify_payload, indent=2), encoding="utf-8")

    active_findings = [{"severity": "critical"} for _ in range(critical_active)]
    report_payload = {
        "executive_summary": {
            "suppression_rules_invalid": 0,
            "suppression_rules_expired": 0,
            "suppression_rules_stale": 0,
        },
        "active_findings": active_findings,
    }
    (base / "report.json").write_text(json.dumps(report_payload, indent=2), encoding="utf-8")


def test_rollout_gate_dev_passes_with_minimum_artifacts(tmp_path: Path) -> None:
    artifacts = tmp_path / "artifacts"
    _write_artifacts(artifacts, include_verify=False)

    result = subprocess.run(
        [
            sys.executable,
            str(_script_path()),
            "--tier",
            "dev",
            "--artifacts-dir",
            str(artifacts),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr


def test_rollout_gate_prod_requires_audit_log(tmp_path: Path) -> None:
    artifacts = tmp_path / "artifacts"
    _write_artifacts(artifacts, include_verify=True)

    result = subprocess.run(
        [
            sys.executable,
            str(_script_path()),
            "--tier",
            "prod",
            "--artifacts-dir",
            str(artifacts),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert result.returncode == 1
    assert "audit trail missing" in result.stderr


def test_rollout_gate_prod_rejects_unapproved_overrides_and_critical_findings(
    tmp_path: Path,
) -> None:
    artifacts = tmp_path / "artifacts"
    _write_artifacts(artifacts, include_verify=True, critical_active=1)
    (artifacts / "audit-log.jsonl").write_text(
        json.dumps(
            {
                "event_type": "policy_override_applied",
                "approved": False,
                "overrides": {"verify.proof_mode": "unsafe"},
            }
        )
        + "\n",
        encoding="utf-8",
    )

    result = subprocess.run(
        [
            sys.executable,
            str(_script_path()),
            "--tier",
            "prod",
            "--artifacts-dir",
            str(artifacts),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert result.returncode == 1
    assert "active critical findings" in result.stderr
    assert "unapproved policy override" in result.stderr


def test_rollout_gate_drift_thresholds_apply_by_tier(tmp_path: Path) -> None:
    artifacts = tmp_path / "artifacts"
    _write_artifacts(artifacts, include_verify=True)
    (artifacts / "audit-log.jsonl").write_text(
        json.dumps(
            {
                "event_type": "policy_override_applied",
                "approved": True,
            }
        )
        + "\n",
        encoding="utf-8",
    )

    comparison = tmp_path / "comparison.json"
    comparison.write_text(
        json.dumps(
            {
                "comparison": {
                    "overall": {
                        "detection_rate": {"delta": -0.02},
                        "fp_suppression_rate": {"delta": -0.02},
                    }
                }
            },
            indent=2,
        ),
        encoding="utf-8",
    )

    staging = subprocess.run(
        [
            sys.executable,
            str(_script_path()),
            "--tier",
            "staging",
            "--artifacts-dir",
            str(artifacts),
            "--comparison-json",
            str(comparison),
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    prod = subprocess.run(
        [
            sys.executable,
            str(_script_path()),
            "--tier",
            "prod",
            "--artifacts-dir",
            str(artifacts),
            "--comparison-json",
            str(comparison),
        ],
        check=False,
        capture_output=True,
        text=True,
    )

    assert staging.returncode == 0, staging.stderr
    assert prod.returncode == 1
    assert "drift exceeded threshold" in prod.stderr
