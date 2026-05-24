from __future__ import annotations

import glob
import logging
import os
import re
import shutil
import subprocess
import tempfile
import tomllib
from collections.abc import Iterator, Sequence
from contextlib import contextmanager
from dataclasses import dataclass
from fnmatch import fnmatch
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError, model_validator

from piranesi import __version__
from piranesi.config import RulesConfig
from piranesi.observability import run_subprocess

logger = logging.getLogger(__name__)

DEFAULT_RULES_HOME = Path("~/.piranesi/rules").expanduser()
REPOSITORY_METADATA_FILE = "piranesi-rules.toml"
_CWE_PATTERN = re.compile(r"^CWE-\d+$")
_SAFE_NAME_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
_RULE_CATEGORIES = frozenset(
    {
        "authz",
        "crypto",
        "deserialization",
        "injection",
        "misconfiguration",
        "redirect",
        "secrets",
        "ssrf",
        "supply-chain",
        "traversal",
        "xss",
        "other",
    }
)
_SUPPORTED_RULE_SCHEMA_VERSIONS = frozenset({"1", "1.0"})


class RuleRegistryError(RuntimeError):
    """Raised when a rule repository or rule file is invalid."""


class RulePattern(BaseModel):
    model_config = ConfigDict(extra="forbid")

    pattern: str
    type: Literal["cpgql", "regex"]

    @model_validator(mode="after")
    def validate_pattern(self) -> RulePattern:
        if not self.pattern.strip():
            raise ValueError("pattern must not be empty")
        if self.type == "regex":
            try:
                re.compile(self.pattern)
            except re.error as exc:
                raise ValueError(f"invalid regex pattern: {exc}") from exc
        return self


class RuleSanitizers(BaseModel):
    model_config = ConfigDict(extra="forbid")

    patterns: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_patterns(self) -> RuleSanitizers:
        for pattern in self.patterns:
            if not pattern.strip():
                raise ValueError("sanitizer patterns must not be empty")
        return self


class RuleMessage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    template: str | None = None


class RuleInlineTest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fixture: str
    expect_finding: bool
    expect_cwe: str | None = None
    expect_source_line: int | None = None
    expect_sink_line: int | None = None
    description: str | None = None


class RuleDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str | None = None
    cwe_id: str | None = None
    severity: Literal["low", "medium", "high", "critical"] | None = None
    description: str | None = None
    category: str | None = None
    schema_version: str | None = "1"
    author: str | None = None
    version: str | None = None
    tags: list[str] = Field(default_factory=list)
    extends: str | None = None
    override_severity: Literal["low", "medium", "high", "critical"] | None = None
    source: RulePattern | None = None
    sink: RulePattern | None = None
    sanitizers: RuleSanitizers = Field(default_factory=RuleSanitizers)
    message: RuleMessage = Field(default_factory=RuleMessage)
    additional_sanitizers: RuleSanitizers = Field(default_factory=RuleSanitizers)

    @model_validator(mode="after")
    def validate_rule(self) -> RuleDefinition:
        if not _SAFE_NAME_PATTERN.fullmatch(self.id):
            raise ValueError(
                "rule.id must contain only letters, digits, dots, underscores, or hyphens"
            )
        if self.cwe_id is not None and not _CWE_PATTERN.fullmatch(self.cwe_id):
            raise ValueError("rule.cwe_id must use the form CWE-<number>")
        if self.category is not None:
            normalized = self.category.strip().lower()
            if normalized not in _RULE_CATEGORIES:
                allowed = ", ".join(sorted(_RULE_CATEGORIES))
                raise ValueError(f"rule.category must be one of: {allowed}")
        if self.schema_version is not None:
            normalized_schema = self.schema_version.strip()
            if normalized_schema not in _SUPPORTED_RULE_SCHEMA_VERSIONS:
                allowed = ", ".join(sorted(_SUPPORTED_RULE_SCHEMA_VERSIONS))
                raise ValueError(f"rule.schema_version must be one of: {allowed}")
        if self.extends is None:
            missing: list[str] = []
            if not self.name:
                missing.append("rule.name")
            if not self.cwe_id:
                missing.append("rule.cwe_id")
            if self.severity is None:
                missing.append("rule.severity")
            if self.source is None:
                missing.append("rule.source")
            if self.sink is None:
                missing.append("rule.sink")
            if missing:
                raise ValueError(f"missing required fields: {', '.join(missing)}")
        return self


class RuleDocument(BaseModel):
    model_config = ConfigDict(extra="forbid")

    rule: RuleDefinition
    tests: list[RuleInlineTest] = Field(default_factory=list)


class RuleRepositoryMetadata(BaseModel):
    model_config = ConfigDict(extra="ignore")

    name: str | None = None
    version: str | None = None
    description: str | None = None
    min_piranesi_version: str | None = None


@dataclass(frozen=True)
class DiscoveredRule:
    rule_id: str
    raw_rule_id: str
    namespace: str | None
    file_path: Path


@dataclass(frozen=True)
class InstalledRuleSet:
    name: str
    path: Path
    remote_url: str | None
    version: str | None
    description: str | None
    rules: tuple[DiscoveredRule, ...]

    @property
    def rule_count(self) -> int:
        return len(self.rules)


@dataclass(frozen=True)
class _RuleSearchLocation:
    root: Path
    namespace: str | None


def default_rules_home() -> Path:
    return DEFAULT_RULES_HOME.resolve(strict=False)


def namespaced_rule_id(namespace: str | None, rule_id: str) -> str:
    normalized_rule_id = rule_id.strip()
    if namespace is None:
        return normalized_rule_id
    return f"{namespace}:{normalized_rule_id}"


def derive_repository_name(git_url: str) -> str:
    stripped = git_url.strip().rstrip("/").removesuffix(".git")
    parts = [part for part in re.split(r"[/:]", stripped) if part]
    if not parts:
        raise RuleRegistryError(f"could not derive repository name from {git_url!r}")
    return _sanitize_repository_name(parts[-1])


def load_rule_document(path: Path) -> RuleDocument:
    try:
        with path.open("rb") as handle:
            payload = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise RuleRegistryError(f"failed to parse rule file {path}: {exc}") from exc
    try:
        return RuleDocument.model_validate(payload)
    except ValidationError as exc:
        raise RuleRegistryError(f"invalid rule file {path}: {exc}") from exc


def load_repository_metadata(repo_dir: Path) -> RuleRepositoryMetadata:
    metadata_path = repo_dir / REPOSITORY_METADATA_FILE
    if not metadata_path.is_file():
        return RuleRepositoryMetadata()
    try:
        with metadata_path.open("rb") as handle:
            payload = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise RuleRegistryError(
            f"failed to parse repository metadata {metadata_path}: {exc}"
        ) from exc
    if isinstance(payload.get("ruleset"), dict):
        payload = payload["ruleset"]
    try:
        return RuleRepositoryMetadata.model_validate(payload)
    except ValidationError as exc:
        raise RuleRegistryError(f"invalid repository metadata {metadata_path}: {exc}") from exc


def validate_rule_repository(
    repo_dir: Path,
    *,
    repo_name: str | None = None,
    rules_config: RulesConfig | None = None,
) -> InstalledRuleSet:
    rules_path = repo_dir.resolve(strict=False)
    if not rules_path.is_dir():
        raise RuleRegistryError(f"rule repository not found: {repo_dir}")
    namespace = _require_repository_name(repo_name or rules_path.name)
    metadata = load_repository_metadata(rules_path)
    _ensure_minimum_version(metadata, namespace)
    effective_config = rules_config or RulesConfig()
    if effective_config.require_signatures:
        verify_repository_signature(rules_path, metadata, effective_config.trusted_keys)

    rules = _load_rules_from_root(
        _RuleSearchLocation(root=rules_path, namespace=namespace),
        disabled_patterns=(),
    )
    if not rules:
        raise RuleRegistryError(f"no rule files found in {rules_path}")

    remote_url = _git_remote_url(rules_path)
    return InstalledRuleSet(
        name=namespace,
        path=rules_path,
        remote_url=remote_url,
        version=metadata.version,
        description=metadata.description,
        rules=tuple(rules),
    )


def list_installed_rule_sets(
    *,
    rules_root: Path | None = None,
    rules_config: RulesConfig | None = None,
) -> list[InstalledRuleSet]:
    root = (rules_root or default_rules_home()).expanduser().resolve(strict=False)
    if not root.exists():
        return []
    installed: list[InstalledRuleSet] = []
    for repo_dir in sorted(
        path for path in root.iterdir() if path.is_dir() and (path / ".git").exists()
    ):
        installed.append(validate_rule_repository(repo_dir, rules_config=rules_config))
    return installed


def install_rule_repository(
    git_url: str,
    *,
    name: str | None = None,
    rules_root: Path | None = None,
    rules_config: RulesConfig | None = None,
) -> InstalledRuleSet:
    repo_name = (
        _require_repository_name(name) if name is not None else derive_repository_name(git_url)
    )
    root = (rules_root or default_rules_home()).expanduser().resolve(strict=False)
    destination = root / repo_name
    if destination.exists():
        raise RuleRegistryError(f"rule repository already installed: {repo_name}")
    root.mkdir(parents=True, exist_ok=True)

    _run_checked(["git", "clone", git_url, str(destination)])
    try:
        return validate_rule_repository(destination, repo_name=repo_name, rules_config=rules_config)
    except Exception:
        shutil.rmtree(destination, ignore_errors=True)
        raise


def update_rule_repositories(
    *,
    name: str | None = None,
    rules_root: Path | None = None,
    rules_config: RulesConfig | None = None,
) -> list[InstalledRuleSet]:
    root = (rules_root or default_rules_home()).expanduser().resolve(strict=False)
    targets = (
        [_installed_repo_path(root, name)] if name is not None else _installed_repo_paths(root)
    )
    updated: list[InstalledRuleSet] = []
    for repo_dir in targets:
        _run_checked(["git", "-C", str(repo_dir), "pull", "--ff-only"])
        updated.append(validate_rule_repository(repo_dir, rules_config=rules_config))
    return updated


def remove_rule_repository(
    name: str,
    *,
    rules_root: Path | None = None,
) -> Path:
    root = (rules_root or default_rules_home()).expanduser().resolve(strict=False)
    repo_path = _installed_repo_path(root, name)
    shutil.rmtree(repo_path)
    return repo_path


def discover_rules(
    rules_config: RulesConfig,
    *,
    config_path: Path | None = None,
) -> list[DiscoveredRule]:
    locations = _expand_rule_search_locations(rules_config, config_path=config_path)
    discovered: list[DiscoveredRule] = []
    seen_rule_ids: set[str] = set()
    for location in locations:
        for rule in _load_rules_from_root(location, disabled_patterns=rules_config.disabled_rules):
            if rule.rule_id in seen_rule_ids:
                raise RuleRegistryError(f"duplicate rule id detected: {rule.rule_id}")
            seen_rule_ids.add(rule.rule_id)
            discovered.append(rule)
    return discovered


def is_rule_disabled(
    rule_id: str,
    *,
    raw_rule_id: str,
    disabled_patterns: Sequence[str],
) -> bool:
    return any(
        fnmatch(rule_id, pattern) or fnmatch(raw_rule_id, pattern) for pattern in disabled_patterns
    )


def verify_repository_signature(
    repo_dir: Path,
    metadata: RuleRepositoryMetadata,
    trusted_keys: Sequence[str],
) -> None:
    if not metadata.version:
        raise RuleRegistryError(
            f"signature verification for {repo_dir.name} requires version in "
            f"{REPOSITORY_METADATA_FILE}"
        )
    tags_at_head = {
        line.strip()
        for line in _run_checked(
            ["git", "-C", str(repo_dir), "tag", "--points-at", "HEAD"]
        ).stdout.splitlines()
        if line.strip()
    }
    expected_tags = (metadata.version, f"v{metadata.version}")
    tag = next((candidate for candidate in expected_tags if candidate in tags_at_head), None)
    if tag is None:
        raise RuleRegistryError(
            f"repository {repo_dir.name} must have a signed tag for version {metadata.version}"
        )

    with _trusted_key_environment(trusted_keys) as env:
        _run_checked(["git", "-C", str(repo_dir), "verify-tag", tag], env=env)


def _load_rules_from_root(
    location: _RuleSearchLocation,
    *,
    disabled_patterns: Sequence[str],
) -> list[DiscoveredRule]:
    rules: list[DiscoveredRule] = []
    seen_rule_ids: set[str] = set()
    for rule_path in _iter_rule_files(location.root):
        document = load_rule_document(rule_path)
        rule_id = namespaced_rule_id(location.namespace, document.rule.id)
        if is_rule_disabled(
            rule_id,
            raw_rule_id=document.rule.id,
            disabled_patterns=disabled_patterns,
        ):
            continue
        if rule_id in seen_rule_ids:
            raise RuleRegistryError(f"duplicate rule id detected: {rule_id}")
        seen_rule_ids.add(rule_id)
        rules.append(
            DiscoveredRule(
                rule_id=rule_id,
                raw_rule_id=document.rule.id,
                namespace=location.namespace,
                file_path=rule_path,
            )
        )
    return rules


def _expand_rule_search_locations(
    rules_config: RulesConfig,
    *,
    config_path: Path | None,
) -> list[_RuleSearchLocation]:
    base_dir = (
        config_path.expanduser().resolve(strict=False).parent
        if config_path is not None
        else Path.cwd().resolve(strict=False)
    )
    home_root = default_rules_home()
    locations: list[_RuleSearchLocation] = []
    seen_locations: set[tuple[Path, str | None]] = set()
    for raw_entry in rules_config.paths:
        for candidate in _expand_path_entry(raw_entry, base_dir):
            namespace = _namespace_for_path(candidate, home_root)
            location = _RuleSearchLocation(root=candidate, namespace=namespace)
            marker = (location.root, location.namespace)
            if marker in seen_locations:
                continue
            seen_locations.add(marker)
            locations.append(location)
    return locations


def _expand_path_entry(entry: str, base_dir: Path) -> list[Path]:
    expanded = os.path.expanduser(entry)
    candidate = Path(expanded)
    if not candidate.is_absolute():
        candidate = (base_dir / candidate).resolve(strict=False)
        expanded = str(candidate)
    if _contains_glob(entry):
        return [Path(path).resolve(strict=False) for path in sorted(glob.glob(expanded))]
    return [candidate.resolve(strict=False)]


def _contains_glob(value: str) -> bool:
    return any(token in value for token in ("*", "?", "["))


def _namespace_for_path(path: Path, home_root: Path) -> str | None:
    try:
        relative = path.resolve(strict=False).relative_to(home_root)
    except ValueError:
        return None
    if not relative.parts:
        return None
    return relative.parts[0]


def _iter_rule_files(root: Path) -> list[Path]:
    if root.is_file():
        return [root] if root.suffix == ".toml" and root.name != REPOSITORY_METADATA_FILE else []

    preferred_rules_dir = root / "rules"
    search_root = preferred_rules_dir if preferred_rules_dir.is_dir() else root
    files = [
        path
        for path in sorted(search_root.rglob("*.toml"))
        if path.is_file() and path.name != REPOSITORY_METADATA_FILE and ".git" not in path.parts
    ]
    return files


def _installed_repo_paths(root: Path) -> list[Path]:
    if not root.exists():
        return []
    return sorted(path for path in root.iterdir() if path.is_dir() and (path / ".git").exists())


def _installed_repo_path(root: Path, name: str) -> Path:
    repo_name = _require_repository_name(name)
    path = root / repo_name
    if not path.is_dir():
        raise RuleRegistryError(f"rule repository not installed: {repo_name}")
    return path


def _ensure_minimum_version(metadata: RuleRepositoryMetadata, repo_name: str) -> None:
    if metadata.min_piranesi_version is None:
        return
    if _compare_versions(__version__, metadata.min_piranesi_version) < 0:
        raise RuleRegistryError(
            f"rule repository {repo_name} requires piranesi>={metadata.min_piranesi_version}"
        )


def _compare_versions(left: str, right: str) -> int:
    left_parts = _parse_version(left)
    right_parts = _parse_version(right)
    width = max(len(left_parts), len(right_parts))
    left_normalized = left_parts + (0,) * (width - len(left_parts))
    right_normalized = right_parts + (0,) * (width - len(right_parts))
    if left_normalized < right_normalized:
        return -1
    if left_normalized > right_normalized:
        return 1
    return 0


def _parse_version(value: str) -> tuple[int, ...]:
    numbers = tuple(int(part) for part in re.findall(r"\d+", value))
    if not numbers:
        raise RuleRegistryError(f"invalid version string: {value!r}")
    return numbers


def _git_remote_url(repo_dir: Path) -> str | None:
    result = _run_optional(["git", "-C", str(repo_dir), "remote", "get-url", "origin"])
    if result is None or result.returncode != 0:
        return None
    remote_url = result.stdout.strip()
    return remote_url or None


def _require_repository_name(name: str) -> str:
    if not _SAFE_NAME_PATTERN.fullmatch(name):
        raise RuleRegistryError(
            "repository names must contain only letters, digits, dots, underscores, or hyphens"
        )
    return name


def _sanitize_repository_name(value: str) -> str:
    sanitized = re.sub(r"[^A-Za-z0-9._-]+", "-", value.strip())
    sanitized = sanitized.strip(".-")
    return _require_repository_name(sanitized)


def _run_checked(
    cmd: Sequence[str],
    *,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    result = _run_optional(cmd, cwd=cwd, env=env)
    if result is None:
        raise RuleRegistryError(f"required executable not found: {cmd[0]}")
    if result.returncode != 0:
        details = result.stderr.strip() or result.stdout.strip() or "command failed"
        raise RuleRegistryError(details)
    return result


def _run_optional(
    cmd: Sequence[str],
    *,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str] | None:
    try:
        return run_subprocess(cmd, cwd=cwd, env=env, logger=logger)
    except FileNotFoundError:
        return None


@contextmanager
def _trusted_key_environment(trusted_keys: Sequence[str]) -> Iterator[dict[str, str] | None]:
    if not trusted_keys:
        yield None
        return

    key_files = _collect_trusted_key_files(trusted_keys)
    if not key_files:
        raise RuleRegistryError("no GPG key files found in rules.trusted_keys")

    gnupg_home = Path(tempfile.mkdtemp(prefix="piranesi-gpg-")).resolve(strict=False)
    try:
        env = dict(os.environ)
        env["GNUPGHOME"] = str(gnupg_home)
        _run_checked(
            [
                "gpg",
                "--homedir",
                str(gnupg_home),
                "--batch",
                "--import",
                *[str(path) for path in key_files],
            ],
            env=env,
        )
        yield env
    finally:
        shutil.rmtree(gnupg_home, ignore_errors=True)


def _collect_trusted_key_files(trusted_keys: Sequence[str]) -> list[Path]:
    files: list[Path] = []
    for entry in trusted_keys:
        candidate = Path(entry).expanduser().resolve(strict=False)
        if candidate.is_file():
            files.append(candidate)
            continue
        if candidate.is_dir():
            files.extend(sorted(path for path in candidate.rglob("*") if path.is_file()))
    return files


__all__ = [
    "DEFAULT_RULES_HOME",
    "DiscoveredRule",
    "InstalledRuleSet",
    "RuleRegistryError",
    "default_rules_home",
    "derive_repository_name",
    "discover_rules",
    "install_rule_repository",
    "is_rule_disabled",
    "list_installed_rule_sets",
    "load_repository_metadata",
    "load_rule_document",
    "namespaced_rule_id",
    "remove_rule_repository",
    "update_rule_repositories",
    "validate_rule_repository",
    "verify_repository_signature",
]
