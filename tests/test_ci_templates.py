from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_ci_templates_are_present_and_referenced() -> None:
    github_template = ROOT / "docs" / "ci-templates" / "github-actions-piranesi.yml"
    gitlab_template = ROOT / "docs" / "ci-templates" / "gitlab-ci-piranesi.yml"
    integration_doc = (ROOT / "docs" / "ci-integration.md").read_text(encoding="utf-8")

    assert github_template.exists()
    assert gitlab_template.exists()
    assert "docs/ci-templates/github-actions-piranesi.yml" in integration_doc
    assert "docs/ci-templates/gitlab-ci-piranesi.yml" in integration_doc


def test_ci_templates_default_to_deterministic_mode() -> None:
    github_template = (ROOT / "docs" / "ci-templates" / "github-actions-piranesi.yml").read_text(
        encoding="utf-8"
    )
    gitlab_template = (ROOT / "docs" / "ci-templates" / "gitlab-ci-piranesi.yml").read_text(
        encoding="utf-8"
    )

    for payload in (github_template, gitlab_template):
        assert "piranesi ingest init" in payload
        assert "piranesi ingest nmap" in payload
        assert "piranesi ingest nuclei" in payload
        assert "piranesi report --workspace workspace --format json" in payload
        assert "piranesi sign --workspace workspace --verify" in payload
        assert "OPENAI_API_KEY" not in payload
        assert "ANTHROPIC_API_KEY" not in payload
