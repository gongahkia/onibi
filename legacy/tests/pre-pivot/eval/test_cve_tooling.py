from __future__ import annotations

import subprocess
import sys
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "eval") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "eval"))
if str(REPO_ROOT / "src") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "src"))

from eval.extract_fixture import extract_fixture  # noqa: E402
from eval.fixture_validation import validation_result_from_findings  # noqa: E402
from eval.ground_truth.schema import (  # noqa: E402
    Complexity,
    DiscoveryMethod,
    GroundTruthEntry,
    Label,
)
from eval.mine_cves import query_nvd  # noqa: E402
from eval.scoring import NormalizedFinding  # noqa: E402


class _FakeResponse:
    def __init__(self, payload: dict[str, Any]) -> None:
        self._payload = payload

    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, Any]:
        return self._payload


class _FakeSession:
    def __init__(self, payload: dict[str, Any]) -> None:
        self.payload = payload

    def get(self, *_args: Any, **_kwargs: Any) -> _FakeResponse:
        return _FakeResponse(self.payload)


def _ground_truth_entry(*, label: Label = Label.TRUE_POSITIVE) -> GroundTruthEntry:
    return GroundTruthEntry(
        id="gt-999",
        source_project="fixture",
        commit_hash="deadbeef",
        cwe_id="CWE-89",
        cwe_name="SQL Injection",
        label=label,
        affected_files=["eval/synthetic/phase29/express/gt-277/repo.ts"],
        line_numbers=[9],
        taint_source="req.body.name",
        taint_sink="db.query()",
        taint_path=["req.body.name", "db.query(sql)"],
        complexity=Complexity.CROSS_MODULE,
        exploitable=label == Label.TRUE_POSITIVE,
        reference_exploit=None,
        reference_fix_commit=None,
        notes="fixture",
        discovery_method=DiscoveryMethod.SYNTHETIC,
        language="typescript",
        framework="express",
        taint_step_count=4,
    )


def test_query_nvd_filters_candidates(monkeypatch: pytest.MonkeyPatch) -> None:
    payload = {
        "totalResults": 2,
        "vulnerabilities": [
            {
                "cve": {
                    "id": "CVE-2024-1111",
                    "descriptions": [
                        {
                            "lang": "en",
                            "value": "Express package vulnerable to SQL injection in Node.js apps.",
                        }
                    ],
                    "weaknesses": [{"description": [{"value": "CWE-89"}]}],
                    "references": [
                        {
                            "url": "https://github.com/example/express-app/pull/12",
                            "tags": ["Patch"],
                        }
                    ],
                    "metrics": {
                        "cvssMetricV31": [{"cvssData": {"baseScore": 8.8}}],
                    },
                    "configurations": [
                        {
                            "nodes": [
                                {
                                    "cpeMatch": [
                                        {
                                            "criteria": (
                                                "cpe:2.3:a:example:express-app:1.0:"
                                                "*:*:*:*:node.js:*:*"
                                            )
                                        }
                                    ]
                                }
                            ]
                        }
                    ],
                },
                "published": "2024-01-02T00:00:00.000",
            },
            {
                "cve": {
                    "id": "CVE-2024-2222",
                    "descriptions": [
                        {"lang": "en", "value": "Python issue with no GitHub reference."}
                    ],
                    "weaknesses": [{"description": [{"value": "CWE-89"}]}],
                    "references": [{"url": "https://nvd.nist.gov/vuln/detail/CVE-2024-2222"}],
                    "metrics": {
                        "cvssMetricV31": [{"cvssData": {"baseScore": 9.1}}],
                    },
                },
                "published": "2024-01-02T00:00:00.000",
            },
        ],
    }

    monkeypatch.setattr("eval.mine_cves.requests.Session", lambda: _FakeSession(payload))

    result = query_nvd(
        cwe_id="CWE-89",
        keywords="express,sequelize",
        language="typescript",
        since="2024-01-01",
        min_cvss=5.0,
        api_key=None,
        results_per_page=100,
        max_results=None,
    )

    assert result["total_results"] == 2
    assert len(result["candidates"]) == 1
    candidate = result["candidates"][0]
    assert candidate["cve_id"] == "CVE-2024-1111"
    assert candidate["repo_url"] == "https://github.com/example/express-app"
    assert candidate["cwe_ids"] == ["CWE-89"]
    assert candidate["ecosystem"] == "npm"


def test_extract_fixture_creates_stub_from_local_repo(tmp_path: Path) -> None:
    repo_dir = tmp_path / "repo"
    repo_dir.mkdir()
    subprocess.run(["git", "init"], cwd=repo_dir, check=True, capture_output=True, text=True)
    subprocess.run(
        ["git", "config", "user.email", "fixture@example.com"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    subprocess.run(
        ["git", "config", "user.name", "Fixture Bot"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )

    source_path = repo_dir / "app.ts"
    source_path.write_text(
        "const db = { query(sql: string) { return sql; } };\n"
        "export function run(input: string) {\n"
        "  return db.query(`SELECT * FROM users WHERE name = '${input}'`);\n"
        "}\n",
        encoding="utf-8",
    )
    subprocess.run(
        ["git", "add", "app.ts"], cwd=repo_dir, check=True, capture_output=True, text=True
    )
    subprocess.run(
        ["git", "commit", "-m", "vulnerable"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    vulnerable_commit = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()

    source_path.write_text(
        "const db = { query(sql: string, params: string[]) { return { sql, params }; } };\n"
        "export function run(input: string) {\n"
        '  return db.query("SELECT * FROM users WHERE name = ?", [input]);\n'
        "}\n",
        encoding="utf-8",
    )
    subprocess.run(
        ["git", "add", "app.ts"], cwd=repo_dir, check=True, capture_output=True, text=True
    )
    subprocess.run(
        ["git", "commit", "-m", "fix"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    fix_commit = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()

    output_path = tmp_path / "fixture.ts"
    extract_fixture(
        repo=str(repo_dir),
        vulnerable_commit=vulnerable_commit,
        fix_commit=fix_commit,
        affected_file="app.ts",
        cwe_id="CWE-89",
        output_path=output_path,
        cve_id="CVE-2024-1111",
        package="fixture-app",
        context_lines=4,
    )

    text = output_path.read_text(encoding="utf-8")
    assert "AUTO-GENERATED FIXTURE STUB" in text
    assert "CVE: CVE-2024-1111 | CWE: CWE-89 | Package: fixture-app" in text
    assert "SELECT * FROM users WHERE name" in text
    assert "--- FIX DIFF ---" in text
    assert '+  return db.query("SELECT * FROM users WHERE name = ?", [input]);' in text


def test_validation_result_from_findings_detects_true_positive(tmp_path: Path) -> None:
    entry = _ground_truth_entry()
    finding = NormalizedFinding(
        id="finding-1",
        cwe_id="CWE-89",
        affected_files=("eval/synthetic/phase29/express/gt-277/repo.ts",),
        taint_source="req.body.name",
        taint_sink="db.query()",
        line_numbers=(9,),
    )

    result = validation_result_from_findings(
        entry,
        findings=[finding],
        fixture_root=tmp_path,
        output_dir=tmp_path / "out",
    )

    assert result.passed is True
    assert result.matched is True
    assert result.match_weight == pytest.approx(1.0)
    assert "PASS" in result.message


def test_validation_result_from_findings_detects_false_positive_leak(tmp_path: Path) -> None:
    entry = _ground_truth_entry(label=Label.FALSE_POSITIVE)
    finding = NormalizedFinding(
        id="finding-1",
        cwe_id="CWE-89",
        affected_files=("eval/synthetic/phase29/express/gt-277/repo.ts",),
        taint_source="req.body.name",
        taint_sink="db.query()",
        line_numbers=(9,),
    )

    result = validation_result_from_findings(
        entry,
        findings=[finding],
        fixture_root=tmp_path,
        output_dir=tmp_path / "out",
    )

    assert result.passed is False
    assert result.matched is True
    assert "unexpected detection" in result.message
