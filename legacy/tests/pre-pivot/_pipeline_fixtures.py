from __future__ import annotations

from pathlib import Path

from pydantic import BaseModel

from piranesi.models import (
    ConfirmedFinding,
    LegalAssessment,
    PatchResult,
    RegulatoryObligation,
    SandboxResult,
    ScanMetadata,
    ScanResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
    TriagedFinding,
)
from piranesi.models.finding import CandidateFinding
from piranesi.pipeline import (
    DetectArtifact,
    LegalArtifact,
    PatchArtifact,
    TriageArtifact,
    VerifyArtifact,
)


def fixture_artifacts(target_dir: Path, *, severity: str = "high") -> dict[str, BaseModel]:
    source_location = SourceLocation(
        file=str(target_dir / "src" / "routes" / "login.ts"),
        line=10,
        column=11,
        snippet="const username = req.body.username;",
    )
    step_location = SourceLocation(
        file=str(target_dir / "src" / "routes" / "login.ts"),
        line=14,
        column=9,
        snippet="const sql = `SELECT * FROM users WHERE username = '${username}'`;",
    )
    sink_location = SourceLocation(
        file=str(target_dir / "src" / "routes" / "login.ts"),
        line=15,
        column=5,
        snippet="return db.query(sql);",
    )

    candidate = CandidateFinding(
        id="finding-001",
        vuln_class="CWE-89: SQL Injection",
        source=TaintSource(
            location=source_location,
            source_type="req.body.username",
            data_categories=["name"],
            parameter_name="username",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql_query",
            api_name="db.query",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="assignment",
                taint_state="tainted",
            )
        ],
        path_conditions=[],
        confidence=0.97,
        severity=severity,
        is_healthcare_entity=True,
    )
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="true_positive",
        skeptic_analysis="{}",
        ensemble_score=0.91,
        escalated=False,
    )
    confirmed = ConfirmedFinding(
        finding=triaged,
        exploit_payload="' OR 1=1--",
        exploit_constraints=["username == payload"],
        verification_template_id="sqli-read-probe",
        verification_template_reason=(
            "matched finding CWE CWE-89 [carriers=body; route=POST /login]"
        ),
        verification_template_risk_level="medium",
        verification_expected_evidence=[
            "SQL error markers appear only for exploit payload",
            "row count differs between baseline and exploit",
        ],
        sandbox_result=SandboxResult(
            container_id="sandbox-1",
            request={"method": "POST", "url": "http://127.0.0.1:3000/login"},
            response={"status": 200, "body": "admin"},
            timing_ms=23,
            side_effects=[],
            container_diff=[],
            stdout="",
            stderr="",
            exit_code=0,
            network_isolated=True,
            confirmed=True,
        ),
        reproducer_script=(
            "curl -X POST http://127.0.0.1:3000/login -d 'username=%27%20OR%201%3D1--'"
        ),
        related_cves=[],
    )
    legal = LegalAssessment(
        finding=confirmed,
        obligations=[
            RegulatoryObligation(
                framework="PDPA",
                section="Section 24",
                obligation_text="Notify the regulator of a notifiable breach.",
                data_categories_affected=["name"],
                penalty_range="Up to SGD 1M",
                notification_timeline="3 calendar days",
                enforcement_precedents=[],
            )
        ],
        risk_tier="high",
        memo_markdown="## Legal memo",
    )
    patch = PatchResult(
        finding=confirmed,
        patch_diff=(
            "--- a/src/routes/login.ts\n"
            "+++ b/src/routes/login.ts\n"
            "@@ -14,2 +14,2 @@\n"
            "-const sql = `SELECT * FROM users WHERE username = '${username}'`;\n"
            "-return db.query(sql);\n"
            "+const sql = 'SELECT * FROM users WHERE username = $1';\n"
            "+return db.query(sql, [username]);\n"
        ),
        patch_verified=False,
        patch_explanation="Switch to parameterized queries.",
    )
    scan = ScanResult(
        project_root=str(target_dir),
        files_scanned=[str(target_dir / "src" / "routes" / "login.ts")],
        call_graph={},
        entry_points=[],
        attack_surface=[],
        metadata=ScanMetadata(
            timestamp="2026-04-09T00:00:00Z",
            duration_ms=100,
            tree_sitter_version="unknown",
            piranesi_version="0.1.0",
            files_parsed=1,
            parse_errors=0,
            config_hash="abc123",
        ),
    )
    return {
        "scan": scan,
        "detect": DetectArtifact(findings=[candidate]),
        "triage": TriageArtifact(findings=[triaged]),
        "verify": VerifyArtifact(findings=[confirmed]),
        "legal": LegalArtifact(assessments=[legal]),
        "patch": PatchArtifact(patches=[patch]),
    }
