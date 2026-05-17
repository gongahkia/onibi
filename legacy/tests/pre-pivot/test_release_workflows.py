from __future__ import annotations

from pathlib import Path

from scripts.check_release_tag import expected_version
from scripts.check_release_tag import main as check_release_tag_main

ROOT = Path(__file__).resolve().parents[1]


def test_release_workflows_define_distribution_channels() -> None:
    pypi = (ROOT / ".github" / "workflows" / "publish-pypi.yml").read_text(encoding="utf-8")
    container = (ROOT / ".github" / "workflows" / "publish-container.yml").read_text(
        encoding="utf-8"
    )
    smoke = (ROOT / ".github" / "workflows" / "release-smoke.yml").read_text(encoding="utf-8")

    assert "pypa/gh-action-pypi-publish@release/v1" in pypi
    assert "actions/attest-build-provenance@v2" in pypi
    assert "docker/build-push-action@v6" in container
    assert "ghcr.io/${{ github.repository }}" in container
    assert "actions/attest-build-provenance@v2" in container
    assert "pipx install --force dist/*.whl" in smoke
    assert "shasum -a 256 -c dist/SHA256SUMS" in smoke


def test_dockerfile_uses_non_root_version_pinned_runtime() -> None:
    dockerfile = (ROOT / "Dockerfile").read_text(encoding="utf-8")

    assert "FROM python:3.12.12-slim-bookworm" in dockerfile
    assert "USER piranesi" in dockerfile
    assert 'ENTRYPOINT ["piranesi"]' in dockerfile


def test_release_tag_checker_matches_pyproject_version() -> None:
    version = expected_version(ROOT)

    assert check_release_tag_main([f"v{version}"]) == 0
    assert check_release_tag_main(["v0.0.0"]) == 1
