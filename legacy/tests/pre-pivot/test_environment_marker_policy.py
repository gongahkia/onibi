from __future__ import annotations

import subprocess
import sys
from pathlib import Path


def _policy_script() -> Path:
    return Path(__file__).resolve().parents[1] / "scripts" / "check_environment_markers.py"


def test_environment_marker_policy_passes_for_repo_tests() -> None:
    result = subprocess.run(
        [sys.executable, str(_policy_script()), "--root", "tests"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_environment_marker_policy_reports_missing_markers(tmp_path: Path) -> None:
    test_file = tmp_path / "test_missing_markers.py"
    test_file.write_text(
        "from piranesi.scan.joern import JoernServer, is_joern_installed\n"
        "def test_missing_marker():\n"
        "    if not is_joern_installed():\n"
        "        return\n"
        "    JoernServer(port=8123)\n",
        encoding="utf-8",
    )

    result = subprocess.run(
        [sys.executable, str(_policy_script()), "--root", str(tmp_path)],
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 1
    assert "@pytest.mark.joern" in result.stderr
    assert "@pytest.mark.integration" in result.stderr
