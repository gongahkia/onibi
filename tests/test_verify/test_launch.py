from __future__ import annotations

import json
from pathlib import Path

from piranesi.verify.launch import infer_launch_plan, render_target_profile_snippet


def test_infer_launch_plan_detects_package_json_start_script(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps(
            {
                "scripts": {"start": "node app.js"},
                "dependencies": {"express": "^4.18.0"},
            }
        ),
        encoding="utf-8",
    )

    plan = infer_launch_plan(tmp_path)

    assert len(plan.candidates) == 1
    candidate = plan.candidates[0]
    assert candidate.command == "npm run start"
    assert candidate.base_url == "http://127.0.0.1:3000"
    assert candidate.env["PORT"] == "3000"


def test_render_target_profile_snippet_escapes_command() -> None:
    snippet = render_target_profile_snippet(
        infer_launch_plan(Path("examples/vuln-express")).candidates[0],
        profile_name="local",
    )

    assert 'target_profile = "local"' in snippet
    assert "[verify.target_profiles.local]" in snippet
    assert 'command = "npm run start"' in snippet
