from __future__ import annotations

import json
from pathlib import Path

import pytest
from scripts.validate_pff_fixtures import PffFixtureValidationError, validate_pff_fixture_corpus

PFF_FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "pff"


def test_pff_fixture_corpus_validates_against_schema() -> None:
    messages = validate_pff_fixture_corpus(PFF_FIXTURE_ROOT)

    assert messages == ["validated workspace-findings-v0.json"]


def test_pff_fixture_validation_rejects_invalid_documents(tmp_path: Path) -> None:
    corpus = tmp_path / "pff"
    corpus.mkdir()
    (corpus / "invalid.json").write_text(
        json.dumps({"schema_version": "piranesi.pff.v0", "producer": {"name": "piranesi"}}),
        encoding="utf-8",
    )

    with pytest.raises(PffFixtureValidationError, match="PFF document is invalid"):
        validate_pff_fixture_corpus(corpus)
