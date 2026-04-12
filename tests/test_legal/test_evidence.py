from __future__ import annotations

from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.legal import assess_finding, build_default_engine
from piranesi.legal.evidence import (
    generate_evidence_bundles,
    load_evidence_artifacts,
    write_evidence_bundles,
)
from piranesi.models import (
    ConfirmedFinding,
    SandboxResult,
    ScanMetadata,
    ScanResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TriagedFinding,
)
from piranesi.models.finding import CandidateFinding
from piranesi.pipeline import LegalArtifact

runner = CliRunner()


def test_generate_evidence_bundles_for_soc2_include_counts_and_affected_files(
    tmp_path: Path,
) -> None:
    source_file = tmp_path / "src" / "routes" / "orders.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const orders = true;\n", encoding="utf-8")
    assessment = _assessment_for(
        "CWE-89",
        file_name=str(source_file),
    )
    scan = _scan_artifact(tmp_path, files=[source_file])

    bundles = generate_evidence_bundles(
        scan=scan,
        assessments=[assessment],
        framework="soc2",
    )

    assert len(bundles) == 7
    by_control = {bundle.control_ref: bundle for bundle in bundles}
    assert by_control["CC6.6"].finding_count == 1
    assert by_control["CC6.6"].affected_files == [str(source_file)]
    assert by_control["CC6.6"].scan_date == scan.metadata.timestamp
    assert by_control["CC6.6"].finding_count_by_severity["high"] == 1
    assert by_control["CC6.1"].control_assessment == "pass"


def test_generate_evidence_bundles_mark_pci_controls_not_in_scope(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "routes" / "orders.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const orders = true;\n", encoding="utf-8")
    scan = _scan_artifact(tmp_path, files=[source_file])

    bundles = generate_evidence_bundles(
        scan=scan,
        assessments=[],
        framework="pci_dss",
    )

    assert len(bundles) == 20
    assert all(bundle.control_assessment == "not_in_scope" for bundle in bundles)


def test_generate_evidence_bundles_emit_meta_pci_controls_for_in_scope_zero_findings(
    tmp_path: Path,
) -> None:
    source_file = tmp_path / "src" / "checkout.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text(
        "import Stripe from 'stripe';\nconst route = '/checkout';\n",
        encoding="utf-8",
    )
    scan = _scan_artifact(tmp_path, files=[source_file])

    bundles = generate_evidence_bundles(
        scan=scan,
        assessments=[],
        framework="pci_dss",
    )

    by_control = {bundle.control_ref: bundle for bundle in bundles}
    assert by_control["Req 11.3.1"].control_assessment == "pass"
    assert by_control["Req 11.3.2"].control_assessment == "partial_evidence"
    assert by_control["Req 3.4"].control_assessment == "pass"


def test_write_evidence_bundles_writes_one_file_per_soc2_control(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "routes" / "orders.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const orders = true;\n", encoding="utf-8")
    assessment = _assessment_for("CWE-89", file_name=str(source_file))
    scan = _scan_artifact(tmp_path, files=[source_file])
    output_dir = tmp_path / "evidence"

    written = write_evidence_bundles(
        scan=scan,
        assessments=[assessment],
        framework="soc2",
        output_dir=output_dir,
    )

    assert len(written) == 7
    assert (output_dir / "soc2_cc6_6.json").exists()


def test_load_evidence_artifacts_reads_legal_json(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "routes" / "orders.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const orders = true;\n", encoding="utf-8")
    assessment = _assessment_for("CWE-89", file_name=str(source_file))
    artifacts_dir = _write_artifacts(
        tmp_path,
        assessments=[assessment],
        files=[source_file],
    )

    scan, assessments = load_evidence_artifacts(artifacts_dir)

    assert scan.project_root == str(tmp_path)
    assert any(obligation.framework == "SOC2" for obligation in assessments[0].obligations)


def test_cli_compliance_evidence_writes_output_directory(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "routes" / "orders.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const orders = true;\n", encoding="utf-8")
    assessment = _assessment_for("CWE-89", file_name=str(source_file))
    artifacts_dir = _write_artifacts(
        tmp_path,
        assessments=[assessment],
        files=[source_file],
    )
    output_dir = tmp_path / "evidence"

    result = runner.invoke(
        app,
        [
            "compliance",
            "evidence",
            "--framework",
            "soc2",
            "--artifacts-dir",
            str(artifacts_dir),
            "--output",
            str(output_dir),
        ],
    )

    assert result.exit_code == 0
    assert "wrote 7 evidence bundle(s)" in result.stdout
    assert (output_dir / "soc2_cc6_6.json").exists()


def _write_artifacts(
    project_root: Path,
    *,
    assessments: list[object],
    files: list[Path],
) -> Path:
    artifacts_dir = project_root / "piranesi-output"
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    scan = _scan_artifact(project_root, files=files)
    (artifacts_dir / "scan.json").write_text(scan.model_dump_json(indent=2), encoding="utf-8")
    (artifacts_dir / "legal.json").write_text(
        LegalArtifact(assessments=list(assessments)).model_dump_json(indent=2),
        encoding="utf-8",
    )
    return artifacts_dir


def _scan_artifact(project_root: Path, *, files: list[Path]) -> ScanResult:
    return ScanResult(
        project_root=str(project_root),
        files_scanned=[str(path) for path in files],
        call_graph={},
        entry_points=[],
        attack_surface=[],
        metadata=ScanMetadata(
            timestamp="2026-04-11T10:30:00Z",
            duration_ms=10,
            tree_sitter_version="test",
            piranesi_version="0.5.0",
            files_parsed=len(files),
            parse_errors=0,
            config_hash="test",
        ),
    )


def _assessment_for(
    cwe: str,
    *,
    file_name: str,
) -> object:
    location = SourceLocation(file=file_name, line=12, column=1, snippet="placeholder")
    candidate = CandidateFinding(
        id=f"finding-{cwe.lower()}",
        vuln_class=f"{cwe}: Example finding",
        source=TaintSource(
            location=location,
            source_type="req.body.payment",
            data_categories=[],
            parameter_name="payment",
        ),
        sink=TaintSink(
            location=location,
            sink_type="sink",
            api_name="execute",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.98,
        severity="high",
        metadata={},
    )
    confirmed = ConfirmedFinding(
        finding=TriagedFinding(
            finding=candidate,
            triage_verdict="true_positive",
            skeptic_analysis="test fixture",
            ensemble_score=0.99,
            escalated=False,
        ),
        exploit_payload="payload",
        exploit_constraints=[],
        sandbox_result=SandboxResult(
            container_id="sandbox",
            request={},
            response={},
            timing_ms=1,
            side_effects=[],
            container_diff=[],
            stdout="",
            stderr="",
            exit_code=0,
            network_isolated=True,
            confirmed=True,
        ),
        reproducer_script="echo test",
        related_cves=[],
    )
    return assess_finding(confirmed, build_default_engine())
