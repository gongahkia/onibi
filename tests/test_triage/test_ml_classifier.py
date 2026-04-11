from __future__ import annotations

from pathlib import Path

import pytest
import yaml
from eval.train_classifier import train

import piranesi.pipeline as pipeline_module
from piranesi.config import PiranesiConfig, TriageConfig
from piranesi.llm.cost import CostTracker
from piranesi.models import (
    CandidateFinding,
    PathCondition,
    SourceLocation,
    TaintStep,
    TriagedFinding,
)
from piranesi.pipeline import DetectArtifact, PipelineContext, StageResult, _run_triage_stage
from piranesi.trace import TraceWriter
from piranesi.triage.ml_classifier import (
    MLClassifier,
    MLPrediction,
    extract_features,
    feature_names,
    load_model,
    predict,
)

from ._helpers import build_candidate_finding


def test_extract_features_from_candidate_finding(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "api" / "users.ts"
    source_file.parent.mkdir(parents=True)
    source_file.write_text("export const noop = true;\n", encoding="utf-8")
    source_file.with_name("users.test.ts").write_text("test('ok', () => {});\n", encoding="utf-8")

    base = build_candidate_finding()
    source_location = base.source.location.model_copy(update={"file": str(source_file)})
    sink_location = base.sink.location.model_copy(
        update={
            "file": str(source_file),
            "snippet": "if (flag && ready) { db.query(sql) ? run() : halt(); }",
        }
    )
    path_conditions = [
        PathCondition(
            location=source_location,
            condition_type="branch",
            expression="flag && ready",
            required_value=True,
        )
    ]
    taint_path = [
        base.taint_path[0].model_copy(
            update={
                "location": base.taint_path[0]
                .location.model_copy(update={"file": str(source_file)}),
                "sanitizer_applied": "parameterized_query",
                "through_function": "loadUser",
            }
        ),
        TaintStep(
            location=SourceLocation(
                file=str(source_file),
                line=16,
                column=5,
                snippet="executeQuery(sql);",
            ),
            operation="execute_query",
            taint_state="tainted",
            through_function="executeQuery",
        ),
    ]
    finding = base.model_copy(
        update={
            "source": base.source.model_copy(
                update={"location": source_location, "source_type": "req.query.userId"}
            ),
            "sink": base.sink.model_copy(
                update={"location": sink_location, "sink_type": "sql", "api_name": "db.query"}
            ),
            "taint_path": taint_path,
            "path_conditions": path_conditions,
            "metadata": {
                "framework": "express",
                "dep_reachable": False,
                "field_sensitive": True,
                "z3_result": "UNSAT",
                "commit_age_days": 14,
            },
            "severity": "critical",
        }
    )

    features = extract_features(finding)

    assert set(feature_names()) == set(features)
    assert features["cwe_CWE-89"] == 1.0
    assert features["has_sanitizer_on_path"] == 1.0
    assert features["sanitizer_cwe_match"] == 1.0
    assert features["src_req.query"] == 1.0
    assert features["sink_query"] == 1.0
    assert features["fw_express"] == 1.0
    assert features["ext_.ts"] == 1.0
    assert features["function_depth"] == 2.0
    assert features["is_dep_reachable"] == 0.0
    assert features["field_sensitive_taint"] == 1.0
    assert features["path_condition_count"] == 1.0
    assert features["z3_UNSAT"] == 1.0
    assert features["has_test_coverage"] == 1.0
    assert features["has_path_condition_unsat"] == 1.0
    assert features["severity_ordinal"] == 4.0
    assert features["code_complexity"] >= 3.0


def test_train_and_predict_on_ground_truth_subset(tmp_path: Path) -> None:
    pytest.importorskip("sklearn")

    gt_dir = tmp_path / "ground_truth"
    output_dir = tmp_path / "models"
    gt_dir.mkdir()
    output_dir.mkdir()

    entries = [
        _ground_truth_entry(
            "gt-001",
            label="true_positive",
            cwe_id="CWE-89",
            cwe_name="SQL Injection",
            taint_source="req.query.id",
            taint_sink="db.query(sql)",
            framework="express",
        ),
        _ground_truth_entry(
            "gt-002",
            label="true_positive",
            cwe_id="CWE-78",
            cwe_name="OS Command Injection",
            taint_source="req.body.cmd",
            taint_sink="child_process.exec(cmd)",
            framework="express",
        ),
        _ground_truth_entry(
            "gt-fp-003",
            label="false_positive",
            cwe_id="CWE-79",
            cwe_name="Cross-Site Scripting",
            taint_source="req.query.q",
            taint_sink="res.render('page')",
            framework="nextjs",
        ),
        _ground_truth_entry(
            "gt-fp-004",
            label="false_positive",
            cwe_id="CWE-601",
            cwe_name="Open Redirect",
            taint_source="req.query.next",
            taint_sink="res.redirect(next)",
            framework="express",
        ),
    ]
    for entry in entries:
        (gt_dir / f"{entry['id']}.yaml").write_text(
            yaml.safe_dump(entry, sort_keys=False),
            encoding="utf-8",
        )

    model_path = train(gt_dir=gt_dir, output_dir=output_dir, model_version=1, min_recall=0.5)

    assert model_path.exists()
    assert (output_dir / "fp_classifier.pkl").exists()
    classifier = load_model(output_dir / "fp_classifier.pkl")
    assert classifier is not None

    predictions = predict([build_candidate_finding()], classifier=classifier)

    assert len(predictions) == 1
    assert 0.0 <= predictions[0].true_positive_probability <= 1.0


def test_run_triage_stage_prefilters_ml_findings_before_llm(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    findings = [
        build_candidate_finding().model_copy(update={"id": "finding-ml-fp"}),
        build_candidate_finding().model_copy(update={"id": "finding-llm-1"}),
        build_candidate_finding().model_copy(update={"id": "finding-llm-2"}),
    ]
    llm_calls: list[str] = []

    class _FakeVoter:
        def __init__(self, *, provider: object, router: object | None = None) -> None:
            _ = (provider, router)

        def triage_finding(
            self,
            finding: object,
            *,
            skeptic: object | None = None,
        ) -> TriagedFinding:
            _ = skeptic
            assert isinstance(finding, type(findings[0]))
            llm_calls.append(finding.id)
            return TriagedFinding(
                finding=finding,
                triage_verdict="true_positive",
                skeptic_analysis="LLM triage",
                ensemble_score=0.91,
                escalated=False,
            )

    class _FakeSkeptic:
        def __init__(self, *, provider: object, router: object | None = None) -> None:
            _ = (provider, router)

    def _fake_predict(
        findings_for_prediction: list[CandidateFinding],
        *,
        classifier: MLClassifier | None = None,
        model_path: Path | str | None = None,
    ) -> list[MLPrediction]:
        _ = (classifier, model_path)
        return [
            MLPrediction(
                finding=finding,
                true_positive_probability=0.2 if finding.id == "finding-ml-fp" else 0.88,
            )
            for finding in findings_for_prediction
        ]

    monkeypatch.setattr(pipeline_module, "_llm_is_configured", lambda: True)
    monkeypatch.setattr(pipeline_module, "CalibratedEnsembleVoter", _FakeVoter)
    monkeypatch.setattr(pipeline_module, "SkepticAgent", _FakeSkeptic)
    monkeypatch.setattr(pipeline_module, "load_model", lambda model_path=None: object())
    monkeypatch.setattr(pipeline_module, "predict", _fake_predict)

    config = PiranesiConfig(triage=TriageConfig(ml_prefilter=True, ml_threshold=0.5))
    context = PipelineContext(
        target_dir=tmp_path,
        output_dir=tmp_path / "out",
        provider=object(),  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=CostTracker(),
        trace_writer=TraceWriter(config.trace, config.budget),
    )
    prev_result = StageResult(
        stage="detect",
        success=True,
        artifact=DetectArtifact(findings=findings),
        elapsed_s=0.0,
    )

    result = _run_triage_stage(context, config, prev_result)

    triaged = result.artifact.findings
    assert [item.finding.id for item in triaged] == [
        "finding-ml-fp",
        "finding-llm-1",
        "finding-llm-2",
    ]
    assert triaged[0].triage_verdict == "false_positive"
    assert "ML pre-filter" in triaged[0].skeptic_analysis
    assert llm_calls == ["finding-llm-1", "finding-llm-2"]


def _ground_truth_entry(
    entry_id: str,
    *,
    label: str,
    cwe_id: str,
    cwe_name: str,
    taint_source: str,
    taint_sink: str,
    framework: str,
) -> dict[str, object]:
    return {
        "id": entry_id,
        "source_project": "unit-test",
        "commit_hash": "unit-test-commit",
        "cwe_id": cwe_id,
        "cwe_name": cwe_name,
        "label": label,
        "affected_files": [f"fixtures/{entry_id}.ts"],
        "line_numbers": [10, 20],
        "taint_source": taint_source,
        "taint_sink": taint_sink,
        "taint_path": [taint_source, taint_sink],
        "complexity": "simple",
        "exploitable": label == "true_positive",
        "reference_exploit": None,
        "reference_fix_commit": None,
        "notes": "unit-test fixture",
        "framework": framework,
        "language": "typescript",
    }
