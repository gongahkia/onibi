from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.host import load_host_input
from piranesi.host.community import (
    HostCommunityError,
    evaluate_host_rule_pack,
    load_host_rule_pack,
    scaffold_host_rule,
)
from piranesi.host.community import (
    test_all_host_rule_packs as run_all_host_rule_packs,
)
from piranesi.host.community import (
    test_host_rule_pack as run_host_rule_pack,
)

ROOT = Path(__file__).resolve().parents[1]
RULE = ROOT / "rules" / "community" / "host" / "ssh-password-authentication.toml"
FIXTURE = ROOT / "tests" / "fixtures" / "host" / "debian-vulnerable"


def test_rule_pack_schema_validation() -> None:
    rule = load_host_rule_pack(RULE)

    assert rule.rule.id == "community.ssh.password-authentication-enabled"
    assert rule.match[0].evidence == "config.ssh.PasswordAuthentication"
    assert rule.remediation.text


def test_rule_execution_against_fixture_bundle() -> None:
    rule = load_host_rule_pack(RULE)
    snapshot = load_host_input(FIXTURE)

    findings = evaluate_host_rule_pack(rule, snapshot)

    assert len(findings) == 1
    assert findings[0].rule_id == rule.rule.id
    assert findings[0].source_tool == "community-rule"
    assert findings[0].evidence[0].value == "yes"


def test_rule_test_checks_expected_finding_ids() -> None:
    result = run_host_rule_pack(RULE, FIXTURE)

    assert result.passed is True
    assert result.missing_expected_finding_ids == []
    assert result.findings[0].id == "host-c4e5e125f8bbe7e4"


def test_rule_test_all_runs_community_directory() -> None:
    result = run_all_host_rule_packs(ROOT / "rules" / "community" / "host")

    assert result.tested == 1
    assert result.passed == 1
    assert result.failed == 0


def test_scaffold_writes_data_only_rule(tmp_path: Path) -> None:
    path = scaffold_host_rule("Disable risky service", output_dir=tmp_path)

    text = path.read_text(encoding="utf-8")
    assert path.name == "disable-risky-service.toml"
    assert "community.disable-risky-service" in text
    assert "python" not in text.lower()
    assert "shell" not in text.lower()


def test_community_rules_cannot_execute_shell_commands_or_import_python(tmp_path: Path) -> None:
    unsafe = tmp_path / "unsafe.toml"
    unsafe.write_text(
        """
[rule]
id = "community.unsafe.rule"
title = "Unsafe rule must fail"
category = "test"
severity = "low"

[[match]]
evidence = "config.ssh.PasswordAuthentication"
equals = "yes"
shell = "whoami"

[remediation]
text = "This rule should be rejected before execution."
""".strip(),
        encoding="utf-8",
    )

    with pytest.raises(HostCommunityError, match="unsafe"):
        load_host_rule_pack(unsafe)

    unsafe_import = tmp_path / "unsafe-import.toml"
    unsafe_import.write_text(
        """
[rule]
id = "community.unsafe.import"
title = "Unsafe import rule must fail"
category = "test"
severity = "low"
import = "os"

[[match]]
evidence = "config.ssh.PasswordAuthentication"
equals = "yes"

[remediation]
text = "This rule should be rejected before execution."
""".strip(),
        encoding="utf-8",
    )

    with pytest.raises(HostCommunityError, match="unsafe"):
        load_host_rule_pack(unsafe_import)
