from __future__ import annotations

from piranesi.legal.frameworks import FRAMEWORK_BY_KEY, FRAMEWORKS


def test_framework_specs_include_review_metadata() -> None:
    assert FRAMEWORKS
    for framework in FRAMEWORKS:
        assert framework.version
        assert framework.mapping_last_reviewed
        assert framework.mapping_reviewer
        assert framework.mapping_source
        assert 0.0 <= framework.mapping_confidence <= 1.0


def test_framework_lookup_exposes_versioned_metadata() -> None:
    soc2 = FRAMEWORK_BY_KEY["SOC2"]
    assert soc2.version == "TSC 2022"
    assert soc2.mapping_source == "rules/soc2.toml"
    assert soc2.mapping_last_reviewed == "2026-04-16"
