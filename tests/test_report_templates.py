from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.workspace import EngagementMetadata, create_workspace

runner = CliRunner()


def test_report_includes_selected_local_templates(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(workspace, engagement=EngagementMetadata(client="Example"))
    template_library = tmp_path / "report-text.json"
    template_library.write_text(
        json.dumps(
            {
                "schema_version": "piranesi.template-library.v1",
                "templates": [
                    {
                        "id": "methodology:web",
                        "kind": "methodology",
                        "title": "Web Methodology",
                        "version": "v1",
                        "body": "Reviewed imported web evidence with local methodology text.",
                    },
                    {
                        "id": "remediation:headers",
                        "kind": "remediation",
                        "title": "Security Header Hardening",
                        "version": "v2",
                        "body": "Set security headers and retest affected routes.",
                    },
                ],
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--format",
            "md",
            "--template-library",
            str(template_library),
            "--template",
            "methodology:web",
            "--template",
            "remediation:headers",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    report_path = Path(json.loads(result.stdout)["path"])
    markdown = report_path.read_text(encoding="utf-8")
    assert "Reviewed imported web evidence with local methodology text." in markdown
    assert "Security Header Hardening" in markdown
    assert "Set security headers and retest affected routes." in markdown

    json_result = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--format",
            "json",
            "--template-library",
            str(template_library),
            "--template",
            "remediation:headers",
            "--json",
        ],
    )
    assert json_result.exit_code == 0, json_result.output
    payload = json.loads(Path(json.loads(json_result.stdout)["path"]).read_text(encoding="utf-8"))
    assert payload["appendices"]["templates"] == [
        {
            "id": "remediation:headers",
            "kind": "remediation",
            "title": "Security Header Hardening",
            "version": "v2",
        }
    ]


def test_report_rejects_unknown_template_id(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(workspace)
    template_library = tmp_path / "report-text.json"
    template_library.write_text(
        json.dumps({"schema_version": "piranesi.template-library.v1", "templates": []}),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "report",
            "--workspace",
            str(workspace),
            "--template-library",
            str(template_library),
            "--template",
            "missing",
            "--json-errors",
        ],
    )

    assert result.exit_code == 2
    assert "unknown template id" in result.output
