from __future__ import annotations

import json
from contextlib import contextmanager
from pathlib import Path
from subprocess import CompletedProcess
from types import SimpleNamespace

import pytest

from piranesi.config import OutputConfig, PiranesiConfig
from piranesi.detect.dependencies import DependencyScanResult, scan_dependency_findings
from piranesi.models import ScanMetadata, ScanResult, SourceLocation, TaintSink, TaintSource
from piranesi.models.finding import CandidateFinding
from piranesi.pipeline import PipelineContext, _run_detect_stage, _run_scan_stage

_DEPENDENCY_FIXTURES = Path(__file__).resolve().parents[1] / "fixtures" / "dependencies"


def test_scan_dependency_findings_parses_npm_audit_output(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (tmp_path / "package.json").write_text('{"name":"demo","version":"1.0.0"}\n', encoding="utf-8")
    (tmp_path / "package-lock.json").write_text(
        (
            "{\n"
            '  "name": "demo",\n'
            '  "lockfileVersion": 3,\n'
            '  "packages": {\n'
            '    "": {"name": "demo", "version": "1.0.0"},\n'
            '    "node_modules/lodash": {"version": "4.17.20"}\n'
            "  }\n"
            "}\n"
        ),
        encoding="utf-8",
    )

    audit_payload = {
        "auditReportVersion": 2,
        "vulnerabilities": {
            "lodash": {
                "name": "lodash",
                "severity": "high",
                "range": "<4.17.21",
                "fixAvailable": {"name": "lodash", "version": "4.17.21"},
                "via": [
                    {
                        "source": 123456,
                        "name": "lodash",
                        "dependency": "lodash",
                        "title": "Prototype Pollution in lodash",
                        "url": "https://github.com/advisories/GHSA-fvqr-27wr-82fm",
                        "severity": "high",
                        "range": "<4.17.21",
                        "cve": "CVE-2024-0001",
                    }
                ],
            }
        },
    }

    def _which(binary: str) -> str | None:
        return "/usr/bin/npm" if binary == "npm" else None

    def _run_subprocess(
        cmd: list[str],
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: object | None = None,
    ) -> CompletedProcess[str]:
        _ = (timeout, env, logger)
        assert cwd == tmp_path
        assert cmd == ["npm", "audit", "--json"]
        return CompletedProcess(cmd, 1, stdout=json.dumps(audit_payload), stderr="")

    monkeypatch.setattr("piranesi.detect.dependencies.shutil.which", _which)
    monkeypatch.setattr("piranesi.detect.dependencies.run_subprocess", _run_subprocess)

    result = scan_dependency_findings(tmp_path)

    assert result.sbom_artifacts == {}
    assert len(result.findings) == 1
    finding = result.findings[0]
    assert finding.vuln_class == "CWE-1395"
    assert finding.severity == "high"
    assert finding.source.location.file == str((tmp_path / "package-lock.json").resolve())
    assert finding.source.location.snippet == "lodash@4.17.20"
    assert finding.metadata["package"] == "lodash"
    assert finding.metadata["package_version"] == "4.17.20"
    assert finding.metadata["patched_version"] == "4.17.21"
    assert finding.metadata["cve_id"] == "CVE-2024-0001"
    assert finding.metadata["advisory_id"] == "CVE-2024-0001"


def test_scan_dependency_findings_gracefully_skips_missing_tools(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (tmp_path / "package.json").write_text('{"name":"demo","version":"1.0.0"}\n', encoding="utf-8")
    (tmp_path / "requirements.txt").write_text("flask==0.5\n", encoding="utf-8")

    monkeypatch.setattr("piranesi.detect.dependencies.shutil.which", lambda _binary: None)

    result = scan_dependency_findings(tmp_path)

    assert result.findings == ()
    assert result.sbom_artifacts == {}


def test_scan_dependency_findings_marks_unused_vulnerable_api_as_dep_unreachable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_root = _DEPENDENCY_FIXTURES / "npm_lodash_unused"
    _mock_npm_audit(
        monkeypatch,
        project_root=project_root,
        audit_payload=_lodash_defaultsdeep_audit_payload(),
    )

    result = scan_dependency_findings(project_root)

    assert len(result.findings) == 1
    assert result.findings[0].reachability == "dep_unreachable"


def test_scan_dependency_findings_keeps_used_vulnerable_api_reachable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_root = _DEPENDENCY_FIXTURES / "npm_lodash_used"
    _mock_npm_audit(
        monkeypatch,
        project_root=project_root,
        audit_payload=_lodash_defaultsdeep_audit_payload(),
    )

    result = scan_dependency_findings(project_root)

    assert len(result.findings) == 1
    assert result.findings[0].reachability == "reachable"


def test_scan_stage_persists_dependency_findings_for_detect(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "target"
    output_dir = tmp_path / "out"
    target_dir.mkdir()
    config = PiranesiConfig(output=OutputConfig(output_dir=str(output_dir)))

    dependency_finding = _dependency_candidate(target_dir / "package-lock.json")

    class _FakeScanSession:
        joern_project_root = target_dir
        source_map = None
        cache_status = "MISS"
        failed_files: tuple[Path, ...] = ()

    @contextmanager
    def _fake_scan_session(*args: object, **kwargs: object):
        _ = (args, kwargs)
        yield object(), _FakeScanSession()

    def _fake_build_scan_result(
        server: object,
        *,
        project_root: Path,
        metadata: ScanMetadata,
        joern_project_root: Path,
        source_map: object | None = None,
        **_: object,
    ) -> ScanResult:
        _ = (server, joern_project_root, source_map)
        return ScanResult(
            project_root=str(project_root.resolve(strict=False)),
            files_scanned=[],
            call_graph={},
            entry_points=[],
            attack_surface=[],
            metadata=metadata,
        )

    monkeypatch.setattr("piranesi.pipeline._scan_session", _fake_scan_session)
    monkeypatch.setattr("piranesi.pipeline.resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.get_sanitizer_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.build_scan_result", _fake_build_scan_result)
    monkeypatch.setattr(
        "piranesi.pipeline.scan_dependency_findings",
        lambda *args, **kwargs: DependencyScanResult(
            findings=(dependency_finding,),
            sbom_artifacts={},
        ),
    )
    monkeypatch.setattr("piranesi.pipeline.extract_candidate_findings", lambda *args, **kwargs: [])
    monkeypatch.setattr("piranesi.pipeline.extract_secret_findings", lambda *args, **kwargs: [])
    monkeypatch.setattr(
        "piranesi.pipeline.extract_misconfiguration_findings",
        lambda *args, **kwargs: [],
    )

    context = PipelineContext(
        target_dir=target_dir,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=SimpleNamespace(resolve=lambda _stage: None),
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
        use_cache=False,
    )

    scan_result = _run_scan_stage(context, config, None)
    assert scan_result.artifact.dependency_findings == [dependency_finding]

    context.stage_outputs["scan"] = scan_result.artifact
    detect_result = _run_detect_stage(context, config, None)

    assert [finding.id for finding in detect_result.artifact.findings] == [dependency_finding.id]


def _dependency_candidate(manifest_path: Path) -> CandidateFinding:
    location = SourceLocation(
        file=str(manifest_path.resolve(strict=False)),
        line=1,
        column=1,
        snippet="lodash@4.17.20",
    )
    return CandidateFinding(
        id="dep-test-1",
        vuln_class="CWE-1395",
        source=TaintSource(
            location=location,
            source_type="dependency_manifest",
            data_categories=[],
            parameter_name="lodash",
        ),
        sink=TaintSink(
            location=location,
            sink_type="dependency_vulnerability",
            api_name="CVE-2024-0001",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=1.0,
        severity="high",
        metadata={
            "package": "lodash",
            "package_version": "4.17.20",
            "patched_version": "4.17.21",
            "cve_id": "CVE-2024-0001",
        },
    )


def _mock_npm_audit(
    monkeypatch: pytest.MonkeyPatch,
    *,
    project_root: Path,
    audit_payload: dict[str, object],
) -> None:
    def _which(binary: str) -> str | None:
        return "/usr/bin/npm" if binary == "npm" else None

    def _run_subprocess(
        cmd: list[str],
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: object | None = None,
    ) -> CompletedProcess[str]:
        _ = (timeout, env, logger)
        assert cwd == project_root
        assert cmd == ["npm", "audit", "--json"]
        return CompletedProcess(cmd, 1, stdout=json.dumps(audit_payload), stderr="")

    monkeypatch.setattr("piranesi.detect.dependencies.shutil.which", _which)
    monkeypatch.setattr("piranesi.detect.dependencies.run_subprocess", _run_subprocess)


def _lodash_defaultsdeep_audit_payload() -> dict[str, object]:
    return {
        "auditReportVersion": 2,
        "vulnerabilities": {
            "lodash": {
                "name": "lodash",
                "severity": "high",
                "range": "<4.17.21",
                "fixAvailable": {"name": "lodash", "version": "4.17.21"},
                "via": [
                    {
                        "source": 123456,
                        "name": "lodash",
                        "dependency": "lodash",
                        "title": "Prototype Pollution in lodash.defaultsDeep()",
                        "url": "https://github.com/advisories/GHSA-fvqr-27wr-82fm",
                        "severity": "high",
                        "range": "<4.17.21",
                        "cve": "CVE-2024-0001",
                    }
                ],
            }
        },
    }
