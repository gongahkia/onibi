from __future__ import annotations

import json
import shutil
from collections.abc import Sequence
from hashlib import sha256
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.rescan.executor import execute_rescan_from_baseline
from piranesi.rescan.image_policy import AcceptedImage
from piranesi.rescan.runtime import ContainerRuntimeStatus
from piranesi.signing import audit_chain, verify_workspace

FIXTURE = Path(__file__).parent / "fixtures" / "pentest" / "nmap" / "localhost-http.xml"
DIGEST = "sha256:" + "a" * 64
runner = CliRunner()


def test_sign_workspace_is_stable_and_verifiable(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)

    first = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert first.exit_code == 0, first.output
    first_payload = json.loads(first.stdout)

    second = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert second.exit_code == 0, second.output
    second_payload = json.loads(second.stdout)

    assert second_payload["manifest_id"] == first_payload["manifest_id"]
    assert second_payload["sha256"] == first_payload["sha256"]
    manifest_path = Path(first_payload["path"])
    assert manifest_path.is_file()
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert manifest["schema_version"] == "piranesi.chain-of-custody.v1"
    assert {artifact["role"] for artifact in manifest["artifacts"]} >= {
        "workspace",
        "findings",
        "audit-log",
        "raw-input",
        "report",
    }
    assert manifest["tool_inputs"][0]["tool_version"] == "7.99"
    assert "nmap -sV" in manifest["tool_inputs"][0]["command_args"]

    verify = runner.invoke(app, ["sign", "--workspace", str(workspace), "--verify", "--json"])
    assert verify.exit_code == 0, verify.output
    assert json.loads(verify.stdout)["ok"] is True


def test_verify_accepts_legacy_manifest_without_replay_provenance(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output
    manifest_path = Path(json.loads(sign.stdout)["path"])
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest.pop("replay_provenance", None)
    manifest["manifest_id"] = _manifest_id(manifest)
    legacy_path = workspace / "signatures" / f"manifest-{manifest['manifest_id']}.json"
    legacy_path.write_text(_canonical_json(manifest), encoding="utf-8")

    result = verify_workspace(workspace, manifest_path=legacy_path)

    assert result.ok is True


def test_replay_manifest_includes_signed_provenance_envelope(
    monkeypatch,
    tmp_path: Path,
) -> None:
    baseline = tmp_path / "baseline"
    output = tmp_path / "current"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(FIXTURE), "--workspace", str(baseline)],
    )
    assert ingest.exit_code == 0, ingest.output

    monkeypatch.setattr(
        "piranesi.rescan.executor.ensure_container_runtime",
        lambda: ContainerRuntimeStatus(docker_python_available=True, docker_cli_path="/bin/docker"),
    )

    def fake_runner(
        _image: AcceptedImage,
        _command: Sequence[str],
        _host_output_dir: Path,
        host_output_path: Path,
        _timeout_seconds: int,
    ) -> None:
        shutil.copyfile(FIXTURE, host_output_path)

    result = execute_rescan_from_baseline(
        baseline,
        output_workspace=output,
        image_overrides=[f"nmap=ghcr.io/acme/nmap:v1@{DIGEST}"],
        allow_unenforced_network=True,
        container_runner=fake_runner,
    )
    sign = runner.invoke(app, ["sign", "--workspace", str(output), "--json"])
    assert sign.exit_code == 0, sign.output
    manifest_path = Path(json.loads(sign.stdout)["path"])
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    assert len(manifest["replay_provenance"]) == 1
    envelope = manifest["replay_provenance"][0]
    assert envelope["schema_version"] == "piranesi.replay-provenance.v1"
    assert envelope["tool"] == "nmap"
    assert envelope["replay_spec_sha256"] == result.outputs[0].spec_sha256
    assert envelope["command"][0] == "nmap"
    assert envelope["environment"] == {"allowlist": {}}
    assert envelope["image"]["image_reference"] == f"ghcr.io/acme/nmap:v1@{DIGEST}"
    assert envelope["image"]["image_digest"] == DIGEST
    assert envelope["input_evidence"][0]["sha256"]
    assert envelope["output_evidence"] == [
        {"path": result.outputs[0].raw_path, "sha256": result.outputs[0].sha256}
    ]

    raw_path = output / result.outputs[0].raw_path
    raw_path.write_text("<tampered />\n", encoding="utf-8")
    tampered = verify_workspace(output, manifest_path=manifest_path)
    assert tampered.ok is False
    assert any(failure.path == result.outputs[0].raw_path for failure in tampered.failures)


def test_sign_verify_reports_precise_tamper_failure(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output

    findings_path = workspace / "normalized" / "findings.json"
    findings_payload = json.loads(findings_path.read_text(encoding="utf-8"))
    findings_payload["findings"][0]["title"] = "tampered title"
    findings_path.write_text(json.dumps(findings_payload), encoding="utf-8")

    verify = runner.invoke(app, ["sign", "--workspace", str(workspace), "--verify", "--json"])
    assert verify.exit_code == 1, verify.output
    payload = json.loads(verify.stdout)
    assert payload["ok"] is False
    failure = next(
        item for item in payload["failures"] if item["path"] == "normalized/findings.json"
    )
    assert failure["message"] == "covered file digest mismatch"
    assert failure["expected_sha256"] != failure["actual_sha256"]


def test_sign_verify_covers_evidence_inventory(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    evidence = tmp_path / "operator-note.txt"
    evidence.write_text("operator note\n", encoding="utf-8")

    add = runner.invoke(
        app,
        [
            "evidence",
            "add",
            "--file",
            str(evidence),
            "--kind",
            "note",
            "--workspace",
            str(workspace),
        ],
    )
    assert add.exit_code == 0, add.output
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output
    manifest_path = Path(json.loads(sign.stdout)["path"])
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    roles = {artifact["role"] for artifact in manifest["artifacts"]}
    assert "evidence" in roles
    assert "raw-input" in roles

    evidence_index = workspace / "evidence" / "index.json"
    payload = json.loads(evidence_index.read_text(encoding="utf-8"))
    payload["evidence"][0]["title"] = "tampered evidence title"
    evidence_index.write_text(json.dumps(payload), encoding="utf-8")

    verify = runner.invoke(app, ["sign", "--workspace", str(workspace), "--verify", "--json"])
    assert verify.exit_code == 1, verify.output
    failures = json.loads(verify.stdout)["failures"]
    assert any(failure["path"] == "evidence/index.json" for failure in failures)


def test_audit_chain_detects_removed_reordered_or_edited_events(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output

    audit_path = workspace / "audit-log.jsonl"
    original_chain = audit_chain(audit_path)
    lines = audit_path.read_text(encoding="utf-8").splitlines()
    assert len(lines) >= 1
    event = json.loads(lines[0])
    event["summary"]["created"] = 99
    lines[0] = json.dumps(event, sort_keys=True)
    audit_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

    edited_chain = audit_chain(audit_path)
    assert edited_chain.head != original_chain.head
    result = verify_workspace(workspace)
    assert result.ok is False
    assert any(failure.path == "audit-log.jsonl" for failure in result.failures)


def test_report_references_latest_manifest(tmp_path: Path) -> None:
    workspace = _workspace_with_report(tmp_path)
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace), "--json"])
    assert sign.exit_code == 0, sign.output
    manifest_id = json.loads(sign.stdout)["manifest_id"]

    report = runner.invoke(
        app,
        ["report", "--workspace", str(workspace), "--format", "json", "--json"],
    )
    assert report.exit_code == 0, report.output
    report_path = Path(json.loads(report.stdout)["path"])
    report_payload = json.loads(report_path.read_text(encoding="utf-8"))

    latest = report_payload["chain_of_custody"]["latest_manifest"]
    assert latest["manifest_id"] == manifest_id
    assert latest["path"] == f"signatures/manifest-{manifest_id}.json"
    assert report_payload["chain_of_custody"]["manifest_status"] == "available"


def _workspace_with_report(tmp_path: Path) -> Path:
    workspace = tmp_path / "workspace"
    ingest = runner.invoke(
        app,
        ["ingest", "nmap", "--input", str(FIXTURE), "--workspace", str(workspace)],
    )
    assert ingest.exit_code == 0, ingest.output
    report = runner.invoke(app, ["report", "--workspace", str(workspace), "--format", "json"])
    assert report.exit_code == 0, report.output
    return workspace


def _manifest_id(payload: dict[str, object]) -> str:
    canonical_payload = dict(payload)
    canonical_payload["manifest_id"] = ""
    return sha256(_canonical_json(canonical_payload).encode("utf-8")).hexdigest()


def _canonical_json(payload: object) -> str:
    return json.dumps(payload, sort_keys=True, separators=(",", ":")) + "\n"
