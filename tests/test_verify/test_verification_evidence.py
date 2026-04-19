from __future__ import annotations

from pathlib import Path

from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.verify.evidence import (
    build_verification_evidence,
    write_verification_evidence_artifact,
)
from piranesi.verify.sandbox import SynthesizedPayload


def _candidate_finding(
    tmp_path: Path,
    *,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    location = SourceLocation(
        file=str(tmp_path / "app.ts"),
        line=12,
        column=1,
        snippet="res.send(req.query.q)",
    )
    return CandidateFinding(
        id="finding-active",
        vuln_class="CWE-79: Reflected XSS",
        source=TaintSource(
            location=location,
            source_type="req.query.q",
            data_categories=["unknown"],
            parameter_name="q",
        ),
        sink=TaintSink(
            location=location,
            sink_type="html_output",
            api_name="res.send",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.8,
        severity="high",
        metadata={} if metadata is None else metadata,
    )


def test_build_verification_evidence_redacts_inline_secrets(tmp_path: Path) -> None:
    finding = _candidate_finding(tmp_path)
    payload = SynthesizedPayload(
        method="GET",
        url="/search",
        headers={"Authorization": "Bearer abcdef"},
        body={"q": "<script>alert(1)</script>"},
        payload_values={"q": "<script>alert(1)</script>"},
        encoding="query",
    )

    rich, reason, evidence, _error_text, _artifact = build_verification_evidence(
        finding=finding,
        template_id="reflected-xss-probe",
        payload=payload,
        base_url="http://127.0.0.1:3000",
        baseline_response=None,
        exploit_response=None,
        baseline_capture=None,
        exploit_capture=None,
        reason="authorization: Bearer abcdef",
        evidence=["cookie: sid=abc123"],
        error_text=None,
    )

    assert "Bearer abcdef" not in reason
    assert "sid=abc123" not in evidence[0]
    assert "[REDACTED]" in reason
    assert "[REDACTED]" in evidence[0]
    assert rich.redaction_status.applied is True


def test_write_verification_evidence_artifact_sanitizes_finding_id(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"

    malicious_path = write_verification_evidence_artifact(
        output_dir=output_dir,
        finding_id="../../etc/passwd",
        payload={"status": "attempted"},
    )
    malicious_artifact = Path(malicious_path)
    evidence_root = (output_dir / "verification-evidence").resolve(strict=False)

    assert malicious_artifact.resolve(strict=False).is_relative_to(evidence_root)
    assert malicious_artifact.exists()
    assert malicious_artifact.suffix == ".json"
    assert ".." not in malicious_artifact.name
    assert "/" not in malicious_artifact.name

    safe_path = write_verification_evidence_artifact(
        output_dir=output_dir,
        finding_id="finding-active",
        payload={"status": "attempted"},
    )
    safe_artifact = Path(safe_path)
    assert safe_artifact.name == "finding-active.json"


def test_build_verification_evidence_redacts_json_and_assignment_secret_formats(
    tmp_path: Path,
) -> None:
    finding = _candidate_finding(
        tmp_path,
        metadata={
            "context": {
                "auth_token": "nested-metadata-token",
                "safe": "ok",
            }
        },
    )
    payload = SynthesizedPayload(
        method="POST",
        url="/checkout",
        headers={"X-Request-Id": "req-1"},
        body={
            "headers": {"authorization": "Bearer body-secret-token"},
            "api_key": "body-api-key",
            "note": "safe",
        },
        payload_values={},
        encoding="json",
    )

    _rich, reason, evidence, _error_text, artifact = build_verification_evidence(
        finding=finding,
        template_id="generic-probe",
        payload=payload,
        base_url="https://example.test",
        baseline_response=None,
        exploit_response=None,
        baseline_capture=None,
        exploit_capture=None,
        reason='{"authorization":"Bearer leaked-token","password":"hunter2"}',
        evidence=['OPENAI_API_KEY = "sk-live-test-key" and token: nested-metadata-token'],
        error_text=None,
    )

    assert "leaked-token" not in reason
    assert "hunter2" not in reason
    assert "sk-live-test-key" not in evidence[0]
    assert "nested-metadata-token" not in evidence[0]
    assert "[REDACTED]" in reason
    assert "[REDACTED]" in evidence[0]

    request_preview = artifact["request"]["body_excerpt"]["preview"]
    assert "body-secret-token" not in request_preview
    assert "body-api-key" not in request_preview
    assert "[REDACTED]" in request_preview
