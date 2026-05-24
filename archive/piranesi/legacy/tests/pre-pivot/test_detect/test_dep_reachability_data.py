from __future__ import annotations

import json
from pathlib import Path

import pytest

from piranesi.detect import dep_reachability


def test_cve_function_data_migrates_lodash_template_regression() -> None:
    data = json.loads(dep_reachability._CVE_FUNCTION_DATA_PATH.read_text(encoding="utf-8"))

    assert data["cves"]["CVE-2021-23337"]["functions"] == ["template"]

    dep_reachability._load_cve_function_maps.cache_clear()
    try:
        assert dep_reachability._curated_targets_for_finding(
            "lodash",
            ["CVE-2021-23337"],
        ) == ("template",)
    finally:
        dep_reachability._load_cve_function_maps.cache_clear()


def test_cve_function_data_missing_file_falls_back_to_empty_map(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        dep_reachability,
        "_CVE_FUNCTION_DATA_PATH",
        tmp_path / "missing-cve-functions.json",
    )
    dep_reachability._load_cve_function_maps.cache_clear()

    try:
        assert (
            dep_reachability._curated_targets_for_finding(
                "lodash",
                ["CVE-2021-23337"],
            )
            == ()
        )
    finally:
        dep_reachability._load_cve_function_maps.cache_clear()
