from __future__ import annotations

import shutil
import subprocess
from pathlib import Path
from typing import Any

import pytest

from piranesi.config import RulesConfig
from piranesi.rules.registry import (
    RuleRegistryError,
    discover_rules,
    install_rule_repository,
    remove_rule_repository,
    update_rule_repositories,
    validate_rule_repository,
)


def _write_rule(path: Path, rule_id: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(
            [
                "[rule]",
                f'id = "{rule_id}"',
                f'name = "{rule_id} name"',
                'cwe_id = "CWE-79"',
                'severity = "high"',
                "",
                "[rule.source]",
                'pattern = "req\\\\.query"',
                'type = "regex"',
                "",
                "[rule.sink]",
                'pattern = "res\\\\.send"',
                'type = "regex"',
                "",
            ]
        ),
        encoding="utf-8",
    )


def test_install_update_remove_rule_repository_lifecycle(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    template_repo = tmp_path / "template-repo"
    (template_repo / "rules").mkdir(parents=True)
    _write_rule(template_repo / "rules" / "shared.toml", "shared-rule")
    _write_rule(template_repo / "rules" / "noisy.toml", "noisy-rule-001")
    (template_repo / "piranesi-rules.toml").write_text(
        'name = "acme-rules"\nversion = "1.2.3"\ndescription = "Acme rules"\n',
        encoding="utf-8",
    )

    installed_root = tmp_path / "installed-rules"
    calls: list[list[str]] = []

    def fake_run_subprocess(
        cmd: Any,
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: Any = None,
    ) -> subprocess.CompletedProcess[str]:
        _ = (cwd, timeout, env, logger)
        args = list(cmd)
        calls.append(args)
        if args[:2] == ["git", "clone"]:
            destination = Path(args[-1])
            shutil.copytree(template_repo, destination)
            (destination / ".git").mkdir()
            return subprocess.CompletedProcess(args, 0, "", "")
        if args[:3] == ["git", "-C", str(installed_root / "acme-rules")] and args[3:] == [
            "remote",
            "get-url",
            "origin",
        ]:
            return subprocess.CompletedProcess(args, 0, "https://example.com/acme-rules.git\n", "")
        if args[:3] == ["git", "-C", str(installed_root / "acme-rules")] and args[3:] == [
            "pull",
            "--ff-only",
        ]:
            return subprocess.CompletedProcess(args, 0, "Already up to date.\n", "")
        raise AssertionError(f"unexpected command: {args}")

    monkeypatch.setattr("piranesi.rules.registry.run_subprocess", fake_run_subprocess)

    installed = install_rule_repository(
        "https://example.com/acme-rules.git",
        rules_root=installed_root,
        rules_config=RulesConfig(),
    )

    assert installed.name == "acme-rules"
    assert installed.path == (installed_root / "acme-rules")
    assert installed.rule_count == 2
    assert {rule.rule_id for rule in installed.rules} == {
        "acme-rules:shared-rule",
        "acme-rules:noisy-rule-001",
    }
    assert [
        "git",
        "clone",
        "https://example.com/acme-rules.git",
        str(installed_root / "acme-rules"),
    ] in calls

    updated = update_rule_repositories(rules_root=installed_root, rules_config=RulesConfig())

    assert len(updated) == 1
    assert updated[0].name == "acme-rules"
    assert [
        "git",
        "-C",
        str(installed_root / "acme-rules"),
        "pull",
        "--ff-only",
    ] in calls

    removed_path = remove_rule_repository("acme-rules", rules_root=installed_root)

    assert removed_path == installed_root / "acme-rules"
    assert not removed_path.exists()


def test_discover_rules_applies_namespacing_and_disabled_patterns(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_root = tmp_path / "project"
    local_rules_dir = project_root / "rules"
    installed_root = tmp_path / "installed-rules"
    installed_repo = installed_root / "org-rules"

    _write_rule(local_rules_dir / "local.toml", "local-only")
    _write_rule(local_rules_dir / "shared.toml", "shared-rule")
    _write_rule(installed_repo / "rules" / "shared.toml", "shared-rule")
    _write_rule(installed_repo / "rules" / "experimental.toml", "experimental-alpha")
    (installed_repo / ".git").mkdir(parents=True)

    monkeypatch.setattr("piranesi.rules.registry.DEFAULT_RULES_HOME", installed_root)

    config = RulesConfig(
        paths=["./rules", str(installed_root / "*")],
        disabled_rules=["local-only", "org-rules:experimental-*"],
    )

    discovered = discover_rules(config, config_path=project_root / "piranesi.toml")

    assert {rule.rule_id for rule in discovered} == {
        "shared-rule",
        "org-rules:shared-rule",
    }


def test_validate_rule_repository_requires_signed_tag_when_enabled(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    repo_dir = tmp_path / "signed-rules"
    (repo_dir / "rules").mkdir(parents=True)
    (repo_dir / ".git").mkdir()
    _write_rule(repo_dir / "rules" / "example.toml", "example-rule")
    (repo_dir / "piranesi-rules.toml").write_text(
        'version = "1.2.3"\nmin_piranesi_version = "0.1.0"\n',
        encoding="utf-8",
    )

    seen_verify_tag = False

    def fake_run_subprocess(
        cmd: Any,
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: Any = None,
    ) -> subprocess.CompletedProcess[str]:
        nonlocal seen_verify_tag
        _ = (cwd, timeout, env, logger)
        args = list(cmd)
        if args == ["git", "-C", str(repo_dir), "tag", "--points-at", "HEAD"]:
            return subprocess.CompletedProcess(args, 0, "v1.2.3\n", "")
        if args == ["git", "-C", str(repo_dir), "verify-tag", "v1.2.3"]:
            seen_verify_tag = True
            return subprocess.CompletedProcess(args, 0, "", "")
        if args == ["git", "-C", str(repo_dir), "remote", "get-url", "origin"]:
            return subprocess.CompletedProcess(
                args, 0, "https://example.com/signed-rules.git\n", ""
            )
        raise AssertionError(f"unexpected command: {args}")

    monkeypatch.setattr("piranesi.rules.registry.run_subprocess", fake_run_subprocess)

    installed = validate_rule_repository(
        repo_dir,
        rules_config=RulesConfig(require_signatures=True),
    )

    assert installed.name == "signed-rules"
    assert installed.rule_count == 1
    assert seen_verify_tag is True


def test_validate_rule_repository_rejects_duplicate_ids_in_same_repo(tmp_path: Path) -> None:
    repo_dir = tmp_path / "duplicate-rules"
    (repo_dir / "rules").mkdir(parents=True)
    _write_rule(repo_dir / "rules" / "one.toml", "duplicate-rule")
    _write_rule(repo_dir / "rules" / "two.toml", "duplicate-rule")

    with pytest.raises(RuleRegistryError, match="duplicate rule id detected"):
        validate_rule_repository(repo_dir)
