from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path

from piranesi.llm.router import TokenBudgetExceededError
from piranesi.patch.generator import generate_patches
from tests._pipeline_fixtures import fixture_artifacts


@dataclass(slots=True)
class _Response:
    content: str


class _BudgetAwareProvider:
    def __init__(self) -> None:
        self.calls = 0

    def complete(self, **kwargs: object) -> _Response:
        _ = kwargs
        self.calls += 1
        if self.calls >= 2:
            raise TokenBudgetExceededError("token budget exhausted")
        return _Response(
            content=json.dumps(
                {
                    "patched_code": "const q = 'SELECT * FROM users WHERE id = ?';",
                    "explanation": "Use parameterized query",
                    "mitigation_type": "parameterization",
                },
                sort_keys=True,
            )
        )


def test_generate_patches_stops_when_token_budget_is_exhausted(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    verify_artifact = artifacts["verify"]
    findings = [*verify_artifact.findings, verify_artifact.findings[0]]
    provider = _BudgetAwareProvider()

    patches = generate_patches(
        findings=findings,
        provider=provider,  # type: ignore[arg-type]
        target_dir=tmp_path,
    )

    assert provider.calls == 2
    assert len(patches) == 1
    assert patches[0].patch_explanation == "Use parameterized query"
