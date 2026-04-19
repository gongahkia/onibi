from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Literal

Tier = Literal["dev", "staging", "prod"]

_REQUIRED_ARTIFACTS: dict[Tier, tuple[str, ...]] = {
    "dev": ("scan.json", "detect.json", "report.json"),
    "staging": ("scan.json", "detect.json", "verify.json", "legal.json", "report.json"),
    "prod": ("scan.json", "detect.json", "verify.json", "legal.json", "report.json"),
}

_MIN_DRIFT_BY_TIER: dict[Tier, tuple[float, float]] = {
    "dev": (-0.10, -0.10),
    "staging": (-0.03, -0.03),
    "prod": (0.0, 0.0),
}


def _read_json(path: Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"expected JSON object: {path}")
    return payload


def _collect_missing_artifacts(*, artifacts_dir: Path, tier: Tier) -> list[str]:
    missing: list[str] = []
    for name in _REQUIRED_ARTIFACTS[tier]:
        if not (artifacts_dir / name).exists():
            missing.append(name)
    return missing


def _suppression_counts(report_payload: dict[str, Any]) -> dict[str, int]:
    summary = report_payload.get("executive_summary")
    if not isinstance(summary, dict):
        return {
            "invalid": 0,
            "expired": 0,
            "stale": 0,
        }
    return {
        "invalid": int(summary.get("suppression_rules_invalid", 0) or 0),
        "expired": int(summary.get("suppression_rules_expired", 0) or 0),
        "stale": int(summary.get("suppression_rules_stale", 0) or 0),
    }


def _active_critical_findings(report_payload: dict[str, Any]) -> int:
    active = report_payload.get("active_findings")
    if not isinstance(active, list):
        return 0
    count = 0
    for finding in active:
        if isinstance(finding, dict) and str(finding.get("severity", "")).lower() == "critical":
            count += 1
    return count


def _evaluate_verify_artifact(*, verify_payload: dict[str, Any], tier: Tier) -> list[str]:
    failures: list[str] = []
    attempts = verify_payload.get("attempts")
    if not isinstance(attempts, list):
        attempts = []

    if tier in {"staging", "prod"} and not attempts:
        failures.append("verify.json contains no attempts")
        return failures

    error_attempts = [
        attempt for attempt in attempts if isinstance(attempt, dict) and attempt.get("status") == "error"
    ]
    if tier in {"staging", "prod"} and error_attempts:
        failures.append(f"verify.json contains {len(error_attempts)} error attempt(s)")

    redaction_issues = 0
    for attempt in attempts:
        if not isinstance(attempt, dict):
            continue
        rich = attempt.get("rich_evidence")
        if not isinstance(rich, dict):
            continue
        status = rich.get("redaction_status")
        if not isinstance(status, dict):
            redaction_issues += 1
            continue
        fields = status.get("redacted_fields")
        applied = status.get("applied")
        if isinstance(fields, list) and fields and applied is not True:
            redaction_issues += 1

    if tier == "prod" and redaction_issues > 0:
        failures.append(
            "verify.json redaction metadata failed for "
            f"{redaction_issues} attempt(s) with rich evidence"
        )

    return failures


def _evaluate_drift(*, comparison_payload: dict[str, Any], tier: Tier) -> list[str]:
    failures: list[str] = []
    comparison = comparison_payload.get("comparison")
    if not isinstance(comparison, dict):
        failures.append("comparison payload missing top-level 'comparison' object")
        return failures

    overall = comparison.get("overall")
    if not isinstance(overall, dict):
        failures.append("comparison payload missing comparison.overall")
        return failures

    detection_bucket = overall.get("detection_rate")
    fp_bucket = overall.get("fp_suppression_rate")

    detection_delta = (
        detection_bucket.get("delta") if isinstance(detection_bucket, dict) else None
    )
    fp_delta = fp_bucket.get("delta") if isinstance(fp_bucket, dict) else None

    min_detection_delta, min_fp_delta = _MIN_DRIFT_BY_TIER[tier]

    if not isinstance(detection_delta, float):
        failures.append("comparison overall detection_rate delta is missing")
    elif detection_delta < min_detection_delta:
        failures.append(
            "detection-rate drift exceeded threshold: "
            f"delta={detection_delta:.3f} threshold={min_detection_delta:.3f}"
        )

    if not isinstance(fp_delta, float):
        failures.append("comparison overall fp_suppression_rate delta is missing")
    elif fp_delta < min_fp_delta:
        failures.append(
            "fp-suppression drift exceeded threshold: "
            f"delta={fp_delta:.3f} threshold={min_fp_delta:.3f}"
        )

    return failures


def _load_audit_events(path: Path) -> list[dict[str, Any]]:
    events: list[dict[str, Any]] = []
    if not path.exists():
        return events
    for line in path.read_text(encoding="utf-8").splitlines():
        text = line.strip()
        if not text:
            continue
        payload = json.loads(text)
        if isinstance(payload, dict):
            events.append(payload)
    return events


def _evaluate_audit_log(*, path: Path, tier: Tier) -> list[str]:
    failures: list[str] = []
    events = _load_audit_events(path)

    if tier == "prod" and not path.exists():
        failures.append(f"audit trail missing at {path}")
        return failures

    if not events:
        return failures

    unapproved_overrides = [
        event
        for event in events
        if event.get("event_type") == "policy_override_applied"
        and not bool(event.get("approved", False))
    ]
    if tier == "prod" and unapproved_overrides:
        failures.append(
            f"audit trail contains {len(unapproved_overrides)} unapproved policy override event(s)"
        )

    unredacted_exports = [
        event
        for event in events
        if event.get("event_type") in {"compliance_bundle_exported", "compliance_evidence_exported"}
        and event.get("redact") is False
    ]
    if tier == "prod" and unredacted_exports:
        failures.append(
            f"audit trail contains {len(unredacted_exports)} unredacted evidence export event(s)"
        )

    return failures


def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Validate rollout gates for dev/staging/prod tiers.")
    parser.add_argument(
        "--tier",
        required=True,
        choices=("dev", "staging", "prod"),
        help="Rollout tier to evaluate.",
    )
    parser.add_argument(
        "--artifacts-dir",
        type=Path,
        required=True,
        help="Directory containing Piranesi stage artifacts.",
    )
    parser.add_argument(
        "--comparison-json",
        type=Path,
        help="Optional eval compare-reports --json output for drift checks.",
    )
    parser.add_argument(
        "--audit-log",
        type=Path,
        help="Optional audit log JSONL path (defaults to <artifacts-dir>/audit-log.jsonl).",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Emit machine-readable result payload.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(argv)
    tier: Tier = args.tier
    artifacts_dir = args.artifacts_dir
    failures: list[str] = []

    missing_artifacts = _collect_missing_artifacts(artifacts_dir=artifacts_dir, tier=tier)
    if missing_artifacts:
        failures.append(f"missing required artifacts: {', '.join(missing_artifacts)}")

    report_payload: dict[str, Any] | None = None
    report_path = artifacts_dir / "report.json"
    if report_path.exists():
        try:
            report_payload = _read_json(report_path)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            failures.append(f"failed to parse report.json: {exc}")

    if report_payload is not None:
        suppression = _suppression_counts(report_payload)
        if suppression["invalid"] > 0:
            failures.append(f"suppression_rules_invalid must be 0 (got {suppression['invalid']})")
        if tier in {"staging", "prod"} and suppression["expired"] > 0:
            failures.append(f"suppression_rules_expired must be 0 (got {suppression['expired']})")
        if tier == "prod" and suppression["stale"] > 0:
            failures.append(f"suppression_rules_stale must be 0 (got {suppression['stale']})")

        if tier == "prod":
            critical = _active_critical_findings(report_payload)
            if critical > 0:
                failures.append(f"active critical findings must be 0 for prod rollout (got {critical})")

    verify_path = artifacts_dir / "verify.json"
    if verify_path.exists():
        try:
            verify_payload = _read_json(verify_path)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            failures.append(f"failed to parse verify.json: {exc}")
        else:
            failures.extend(_evaluate_verify_artifact(verify_payload=verify_payload, tier=tier))

    if args.comparison_json is not None:
        try:
            comparison_payload = _read_json(args.comparison_json)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            failures.append(f"failed to parse comparison payload: {exc}")
        else:
            failures.extend(_evaluate_drift(comparison_payload=comparison_payload, tier=tier))

    audit_path = args.audit_log or (artifacts_dir / "audit-log.jsonl")
    failures.extend(_evaluate_audit_log(path=audit_path, tier=tier))

    payload = {
        "tier": tier,
        "artifacts_dir": str(artifacts_dir),
        "comparison_json": None if args.comparison_json is None else str(args.comparison_json),
        "audit_log": str(audit_path),
        "status": "pass" if not failures else "fail",
        "failures": failures,
    }

    if args.json:
        print(json.dumps(payload, indent=2))
    elif not failures:
        print(f"rollout gate checks passed for tier={tier}")
    else:
        print("rollout gate checks failed:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)

    return 0 if not failures else 1


if __name__ == "__main__":
    raise SystemExit(main())
