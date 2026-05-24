from __future__ import annotations

import json
from pathlib import Path

import pytest

from piranesi.evidence import EvidenceError, add_evidence_file, load_evidence_index
from piranesi.workspace import EVIDENCE_FILE, create_workspace


def test_add_evidence_file_preserves_raw_artifact_and_metadata(tmp_path: Path) -> None:
    workspace = create_workspace(tmp_path / "workspace")
    source = tmp_path / "operator note.txt"
    source.write_text("initial access note\n", encoding="utf-8")

    index, record = add_evidence_file(
        workspace.root,
        file_path=source,
        kind="note",
        title="Initial access note",
        source="operator",
        sensitivity="internal",
        tags=["initial-access"],
    )

    assert len(index.evidence) == 1
    assert record.kind == "note"
    assert record.title == "Initial access note"
    assert record.raw_path.startswith("raw/note/")
    assert (workspace.root / record.raw_path).read_text(encoding="utf-8") == "initial access note\n"

    reloaded = load_evidence_index(workspace.root)
    assert reloaded.evidence[0].id == record.id
    assert reloaded.evidence[0].tags == ["initial-access"]


def test_add_evidence_file_reingest_dedupes_by_digest(tmp_path: Path) -> None:
    workspace = create_workspace(tmp_path / "workspace")
    source = tmp_path / "terminal.txt"
    source.write_text("whoami\n", encoding="utf-8")

    first = add_evidence_file(workspace.root, file_path=source, kind="transcript")[1]
    second = add_evidence_file(workspace.root, file_path=source, kind="transcript")[1]

    index = load_evidence_index(workspace.root)
    assert first.id == second.id
    assert len(index.evidence) == 1


def test_load_evidence_index_rejects_invalid_schema(tmp_path: Path) -> None:
    workspace = create_workspace(tmp_path / "workspace")
    path = workspace.root / EVIDENCE_FILE
    payload = json.loads(path.read_text(encoding="utf-8"))
    payload["schema_version"] = "piranesi.evidence.v999"
    path.write_text(json.dumps(payload), encoding="utf-8")

    with pytest.raises(EvidenceError, match="unsupported evidence schema version"):
        load_evidence_index(workspace.root)
