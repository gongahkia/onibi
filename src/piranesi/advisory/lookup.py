from __future__ import annotations

import json
import re
import xml.etree.ElementTree as ET
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path

from piranesi.advisory.db import AdvisoryDB
from piranesi.advisory.epss import epss_label
from piranesi.advisory.models import Advisory, AffectedPackage, ExploitStatus, severity_rank
from piranesi.advisory.version_match import is_vulnerable
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource

_YARN_KEY_RE = re.compile(r'^("?)(.+?)\1:$')
_REQ_RE = re.compile(r"^\s*([A-Za-z0-9_.-]+)(?:\[[A-Za-z0-9_,.-]+\])?\s*==\s*([^\s;]+)")
_GEMFILE_LOCK_SPEC_RE = re.compile(r"^\s{4}(?P<name>[A-Za-z0-9_.-]+)\s+\((?P<version>[^)]+)\)\s*$")


@dataclass(frozen=True)
class ResolvedDependency:
    ecosystem: str
    name: str
    version: str
    manifest_path: Path


def lookup_dependencies(
    db: AdvisoryDB,
    project_root: Path,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for dependency in parse_lockfiles(project_root):
        advisories = db.find_advisories_for_package(dependency.ecosystem, dependency.name)
        for advisory in advisories:
            matched_package = _matching_package(advisory, dependency.ecosystem, dependency.name)
            if matched_package is None:
                continue
            effective_ranges = _effective_ranges(
                matched_package.vulnerable_ranges, matched_package.fixed_versions
            )
            if effective_ranges and not is_vulnerable(
                dependency.version,
                list(effective_ranges),
                dependency.ecosystem,
            ):
                continue
            finding = _build_lookup_finding(dependency, advisory, effective_ranges)
            if finding.id in seen_ids:
                continue
            seen_ids.add(finding.id)
            findings.append(finding)
    return findings


def parse_lockfiles(project_root: Path) -> list[ResolvedDependency]:
    dependencies: list[ResolvedDependency] = []
    dependencies.extend(_parse_package_lock(project_root))
    dependencies.extend(_parse_yarn_lock(project_root))
    dependencies.extend(_parse_pipfile_lock(project_root))
    dependencies.extend(_parse_requirements(project_root))
    dependencies.extend(_parse_go_sum(project_root))
    dependencies.extend(_parse_pom_xml(project_root))
    dependencies.extend(_parse_gemfile_lock(project_root))
    deduped: dict[tuple[str, str, str, str], ResolvedDependency] = {}
    for dependency in dependencies:
        key = (
            dependency.ecosystem,
            dependency.name,
            dependency.version,
            str(dependency.manifest_path.resolve(strict=False)),
        )
        deduped.setdefault(key, dependency)
    return list(deduped.values())


def _parse_package_lock(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "package-lock.json"
    if not path.exists():
        return []
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return []
    versions = _load_npm_package_versions(payload)
    return [
        ResolvedDependency(
            ecosystem="npm",
            name=name,
            version=version,
            manifest_path=path,
        )
        for name, version in sorted(versions.items())
    ]


def _parse_yarn_lock(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "yarn.lock"
    if not path.exists():
        return []
    try:
        content = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []

    dependencies: list[ResolvedDependency] = []
    selectors: list[str] = []
    version: str | None = None
    for line in [*content, ""]:
        if not line.strip():
            if version is not None:
                for selector in selectors:
                    name = _package_name_from_selector(selector)
                    if name is None:
                        continue
                    dependencies.append(
                        ResolvedDependency(
                            ecosystem="npm",
                            name=name,
                            version=version,
                            manifest_path=path,
                        )
                    )
            selectors = []
            version = None
            continue
        if not line.startswith(" "):
            match = _YARN_KEY_RE.match(line)
            if match is None:
                selectors = []
                version = None
                continue
            selectors = [part.strip().strip('"') for part in match.group(2).split(",")]
            continue
        stripped = line.strip()
        if stripped.startswith("version "):
            version = stripped[len("version ") :].strip().strip('"')
    return dependencies


def _parse_pipfile_lock(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "Pipfile.lock"
    if not path.exists():
        return []
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return []
    dependencies: list[ResolvedDependency] = []
    for section in ("default", "develop"):
        items = payload.get(section)
        if not isinstance(items, Mapping):
            continue
        for name, raw_entry in items.items():
            if not isinstance(name, str) or not isinstance(raw_entry, Mapping):
                continue
            version = raw_entry.get("version")
            if not isinstance(version, str):
                continue
            dependencies.append(
                ResolvedDependency(
                    ecosystem="pypi",
                    name=name,
                    version=version.lstrip("="),
                    manifest_path=path,
                )
            )
    return dependencies


def _parse_requirements(project_root: Path) -> list[ResolvedDependency]:
    paths = sorted(project_root.glob("requirements*.txt"))
    dependencies: list[ResolvedDependency] = []
    for path in paths:
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except OSError:
            continue
        for line in lines:
            match = _REQ_RE.match(line)
            if match is None:
                continue
            dependencies.append(
                ResolvedDependency(
                    ecosystem="pypi",
                    name=match.group(1),
                    version=match.group(2),
                    manifest_path=path,
                )
            )
    return dependencies


def _parse_go_sum(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "go.sum"
    if not path.exists():
        return []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []
    dependencies: dict[tuple[str, str], ResolvedDependency] = {}
    for line in lines:
        parts = line.split()
        if len(parts) < 2:
            continue
        module, version = parts[0], parts[1]
        if module.endswith("/go.mod") or version.endswith("/go.mod"):
            continue
        key = (module, version)
        dependencies.setdefault(
            key,
            ResolvedDependency(
                ecosystem="go",
                name=module,
                version=version,
                manifest_path=path,
            ),
        )
    return list(dependencies.values())


def _parse_pom_xml(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "pom.xml"
    if not path.exists():
        return []
    try:
        tree = ET.parse(path)  # noqa: S314 - parses local project metadata, not remote XML.
    except (OSError, ET.ParseError):
        return []
    root = tree.getroot()
    namespace = _xml_namespace(root.tag)
    properties = _extract_pom_properties(root, namespace)
    dependencies: list[ResolvedDependency] = []
    for dependency in root.findall(f".//{namespace}dependency"):
        group_id = _xml_text(dependency.find(f"{namespace}groupId"), properties)
        artifact_id = _xml_text(dependency.find(f"{namespace}artifactId"), properties)
        version = _xml_text(dependency.find(f"{namespace}version"), properties)
        if not group_id or not artifact_id or not version:
            continue
        dependencies.append(
            ResolvedDependency(
                ecosystem="maven",
                name=f"{group_id}:{artifact_id}",
                version=version,
                manifest_path=path,
            )
        )
    return dependencies


def _parse_gemfile_lock(project_root: Path) -> list[ResolvedDependency]:
    path = project_root / "Gemfile.lock"
    if not path.exists():
        return []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []

    dependencies: dict[tuple[str, str], ResolvedDependency] = {}
    in_specs = False
    for line in lines:
        if line.strip() == "specs:":
            in_specs = True
            continue
        if in_specs and line and not line.startswith(" "):
            in_specs = False
        if not in_specs:
            continue
        match = _GEMFILE_LOCK_SPEC_RE.match(line)
        if match is None:
            continue
        key = (match.group("name"), match.group("version"))
        dependencies.setdefault(
            key,
            ResolvedDependency(
                ecosystem="rubygems",
                name=match.group("name"),
                version=match.group("version"),
                manifest_path=path,
            ),
        )
    return list(dependencies.values())


def _matching_package(advisory: Advisory, ecosystem: str, name: str) -> AffectedPackage | None:
    for package in advisory.affected_packages:
        if package.ecosystem == ecosystem and package.name == name:
            return package
    return None


def _effective_ranges(
    vulnerable_ranges: Sequence[str],
    fixed_versions: Sequence[str],
) -> tuple[str, ...]:
    if vulnerable_ranges:
        return tuple(vulnerable_ranges)
    if fixed_versions:
        return tuple(f"<{version}" for version in fixed_versions)
    return ()


def _build_lookup_finding(
    dependency: ResolvedDependency,
    advisory: Advisory,
    vulnerable_ranges: Sequence[str],
) -> CandidateFinding:
    rendered_severity = advisory.severity
    adjusted = _adjusted_severity(advisory.severity, advisory.exploit_status)
    summary = _dependency_summary(
        package_name=dependency.name,
        package_version=dependency.version,
        advisory_id=advisory.advisory_id,
        cve_id=advisory.cve_id,
        patched_version=advisory.fix_version,
        severity=rendered_severity,
    )
    metadata: dict[str, object] = {
        "ecosystem": dependency.ecosystem,
        "package": dependency.name,
        "package_version": dependency.version,
        "patched_version": advisory.fix_version,
        "severity": rendered_severity,
        "advisory_id": advisory.advisory_id,
        "cve_id": advisory.cve_id,
        "ghsa_id": advisory.ghsa_id,
        "vulnerable_range": " || ".join(vulnerable_ranges) if vulnerable_ranges else None,
        "title": advisory.title,
        "advisory_url": advisory.references[0] if advisory.references else None,
        "advisory_sources": list(advisory.sources),
        "epss_score": advisory.epss_score,
        "epss_percentile": advisory.epss_percentile,
        "epss_label": epss_label(advisory.epss_score, advisory.epss_percentile),
        "exploit_status": advisory.exploit_status.value,
        "exploit_sources": list(advisory.exploit_sources),
        "adjusted_severity": adjusted,
        "cvss_score": advisory.cvss_score,
        "cvss_vector": advisory.cvss_vector,
        "fix_available": advisory.fix_available,
        "fix_version": advisory.fix_version,
        "cwe_ids": list(advisory.cwe_ids),
        "references": list(advisory.references),
    }
    location = SourceLocation(
        file=str(dependency.manifest_path.resolve(strict=False)),
        line=1,
        column=1,
        snippet=f"{dependency.name}@{dependency.version}",
    )
    return CandidateFinding(
        id=_dependency_finding_id(
            ecosystem=dependency.ecosystem,
            package_name=dependency.name,
            package_version=dependency.version,
            advisory_id=advisory.advisory_id,
        ),
        vuln_class="CWE-1395",
        source=TaintSource(
            location=location,
            source_type="dependency_manifest",
            data_categories=[],
            parameter_name=dependency.name,
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=str(dependency.manifest_path.resolve(strict=False)),
                line=1,
                column=1,
                snippet=summary,
            ),
            sink_type="dependency_vulnerability",
            api_name=advisory.advisory_id,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=1.0,
        severity=rendered_severity,
        metadata=metadata,
    )


def _adjusted_severity(severity: str, exploit_status: ExploitStatus) -> str:
    rank = severity_rank(severity)
    if exploit_status in {
        ExploitStatus.IN_THE_WILD,
        ExploitStatus.WEAPONIZED,
    } and rank < severity_rank("high"):
        return "high"
    if exploit_status is ExploitStatus.IN_THE_WILD and rank == severity_rank("high"):
        return "critical"
    if exploit_status is ExploitStatus.POC_AVAILABLE and rank < severity_rank("medium"):
        return "medium"
    return severity


def _dependency_summary(
    *,
    package_name: str,
    package_version: str,
    advisory_id: str,
    cve_id: str | None,
    patched_version: str | None,
    severity: str,
) -> str:
    parts = [
        f"package={package_name}",
        f"version={package_version}",
        f"advisory={advisory_id}",
        f"severity={severity}",
    ]
    if cve_id:
        parts.append(f"cve={cve_id}")
    if patched_version:
        parts.append(f"patched={patched_version}")
    return " ".join(parts)


def _dependency_finding_id(
    *,
    ecosystem: str,
    package_name: str,
    package_version: str,
    advisory_id: str,
) -> str:
    digest = sha256(
        f"{ecosystem}:{package_name}:{package_version}:{advisory_id}".encode()
    ).hexdigest()[:16]
    return f"dep-{digest}"


def _load_npm_package_versions(payload: object) -> dict[str, str]:
    if not isinstance(payload, Mapping):
        return {}
    versions: dict[str, str] = {}
    packages = payload.get("packages")
    if isinstance(packages, Mapping):
        for package_path, raw_entry in packages.items():
            if not isinstance(package_path, str) or not isinstance(raw_entry, Mapping):
                continue
            package_name = _package_name_from_lock_path(package_path)
            version = _string_value(raw_entry.get("version"))
            if package_name and version:
                versions.setdefault(package_name, version)
    dependencies = payload.get("dependencies")
    if isinstance(dependencies, Mapping):
        _walk_lock_dependencies(dependencies, versions)
    return versions


def _walk_lock_dependencies(dependencies: Mapping[str, object], versions: dict[str, str]) -> None:
    for name, raw_entry in dependencies.items():
        if not isinstance(name, str) or not isinstance(raw_entry, Mapping):
            continue
        version = _string_value(raw_entry.get("version"))
        if version:
            versions.setdefault(name, version)
        nested = raw_entry.get("dependencies")
        if isinstance(nested, Mapping):
            _walk_lock_dependencies(nested, versions)


def _package_name_from_lock_path(package_path: str) -> str | None:
    normalized = package_path.replace("\\", "/")
    if "node_modules/" not in normalized:
        return None
    name = normalized.rsplit("node_modules/", 1)[1]
    return name.strip("/") or None


def _package_name_from_selector(selector: str) -> str | None:
    stripped = selector.strip().strip('"')
    at_index = stripped.rfind("@")
    if at_index <= 0:
        return stripped or None
    return stripped[:at_index] or None


def _xml_namespace(tag: str) -> str:
    if tag.startswith("{") and "}" in tag:
        return tag.split("}", 1)[0] + "}"
    return ""


def _extract_pom_properties(root: ET.Element, namespace: str) -> dict[str, str]:
    properties: dict[str, str] = {}
    properties_node = root.find(f"{namespace}properties")
    if properties_node is None:
        return properties
    for child in list(properties_node):
        tag = child.tag.split("}", 1)[-1]
        text = child.text.strip() if child.text else ""
        if text:
            properties[tag] = text
    return properties


def _xml_text(node: ET.Element | None, properties: Mapping[str, str]) -> str | None:
    if node is None or node.text is None:
        return None
    value = node.text.strip()
    if value.startswith("${") and value.endswith("}"):
        return properties.get(value[2:-1])
    return value or None


def _string_value(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None
