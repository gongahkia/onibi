from __future__ import annotations

import json
import logging
import re
import shutil
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from hashlib import sha256
from pathlib import Path
from typing import Literal, cast

from piranesi.detect.dep_reachability import apply_dependency_reachability
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.observability import log_error_context, run_subprocess

logger = logging.getLogger("piranesi.detect.dependencies")

SbomFormat = Literal["spdx", "cyclonedx"]

_CVE_PATTERN = re.compile(r"(CVE-\d{4}-\d+)", re.IGNORECASE)
_GHSA_PATTERN = re.compile(r"(GHSA-[0-9A-Za-z]{4}-[0-9A-Za-z]{4}-[0-9A-Za-z]{4})", re.IGNORECASE)
_SEVERITY_MAP = {
    "critical": "critical",
    "high": "high",
    "moderate": "medium",
    "medium": "medium",
    "low": "low",
    "info": "low",
    "none": "low",
}
_NPM_LOCKFILES = ("package-lock.json", "npm-shrinkwrap.json")
_PYTHON_PROJECT_FILES = ("pyproject.toml", "setup.py", "setup.cfg")
_JSON_DECODER = json.JSONDecoder()


@dataclass(frozen=True)
class DependencyScanResult:
    findings: tuple[CandidateFinding, ...] = ()
    sbom_artifacts: dict[str, str] = field(default_factory=dict)


def scan_dependency_findings(
    project_root: Path,
    *,
    output_dir: Path | None = None,
    sbom_format: SbomFormat | None = None,
    changed_files: set[Path] | None = None,
) -> DependencyScanResult:
    resolved_root = project_root.resolve(strict=False)
    findings: list[CandidateFinding] = []
    sbom_artifacts: dict[str, str] = {}
    scan_node = changed_files is None or _dependency_files_changed(
        resolved_root,
        changed_files,
        names=(*_NPM_LOCKFILES, "package.json"),
    )
    scan_python = changed_files is None or _dependency_files_changed(
        resolved_root,
        changed_files,
        names=(*_PYTHON_PROJECT_FILES, "requirements.txt"),
        prefix="requirements",
        suffix=".txt",
    )

    if scan_node and _is_node_project(resolved_root):
        findings.extend(_run_npm_audit(resolved_root))
        if sbom_format is not None and output_dir is not None:
            sbom_path = _generate_npm_sbom(resolved_root, output_dir, sbom_format)
            if sbom_path is not None:
                sbom_artifacts["npm"] = str(sbom_path)

    if scan_python and _is_python_project(resolved_root):
        findings.extend(_run_pip_audit(resolved_root))
        if sbom_format is not None and output_dir is not None:
            sbom_path = _generate_python_sbom(resolved_root, output_dir, sbom_format)
            if sbom_path is not None:
                sbom_artifacts["python"] = str(sbom_path)

    deduped_findings = tuple(_dedupe_findings(findings))
    annotated_findings = apply_dependency_reachability(resolved_root, deduped_findings)

    return DependencyScanResult(
        findings=annotated_findings,
        sbom_artifacts=sbom_artifacts,
    )


def _dependency_files_changed(
    project_root: Path,
    changed_files: set[Path],
    *,
    names: Sequence[str] = (),
    prefix: str | None = None,
    suffix: str | None = None,
) -> bool:
    normalized_names = set(names)
    for changed_file in changed_files:
        candidate = (
            changed_file.resolve(strict=False)
            if changed_file.is_absolute()
            else (project_root / changed_file).resolve(strict=False)
        )
        try:
            relative = candidate.relative_to(project_root)
        except ValueError:
            continue
        file_name = relative.name
        if file_name in normalized_names:
            return True
        if (
            prefix is not None
            and suffix is not None
            and file_name.startswith(prefix)
            and file_name.endswith(suffix)
        ):
            return True
    return False


def parse_npm_audit_payload(
    payload: object,
    *,
    project_root: Path,
) -> list[CandidateFinding]:
    if not isinstance(payload, Mapping):
        return []

    findings: list[CandidateFinding] = []
    versions = _load_npm_package_versions(project_root)
    manifest_path = _node_manifest_path(project_root)
    vulnerabilities = payload.get("vulnerabilities")
    if isinstance(vulnerabilities, Mapping):
        for raw_name, raw_vulnerability in vulnerabilities.items():
            if not isinstance(raw_name, str) or not isinstance(raw_vulnerability, Mapping):
                continue
            package_name = _string_value(raw_vulnerability.get("name")) or raw_name
            package_version = (
                versions.get(package_name)
                or _string_value(raw_vulnerability.get("version"))
                or _string_value(raw_vulnerability.get("range"))
                or "unknown"
            )
            fix_available = raw_vulnerability.get("fixAvailable")
            via_items = raw_vulnerability.get("via")
            advisories: Sequence[object]
            if isinstance(via_items, Sequence) and not isinstance(via_items, (str, bytes)):
                advisories = via_items
            elif via_items is None:
                advisories = ()
            else:
                advisories = (via_items,)
            for raw_advisory in advisories:
                if not isinstance(raw_advisory, Mapping):
                    continue
                findings.append(
                    _build_dependency_finding(
                        ecosystem="npm",
                        manifest_path=manifest_path,
                        package_name=package_name,
                        package_version=package_version,
                        severity=_normalize_severity(
                            raw_advisory.get("severity") or raw_vulnerability.get("severity")
                        ),
                        advisory_id=_npm_advisory_id(raw_advisory),
                        cve_id=_extract_cve_id(
                            [
                                raw_advisory.get("cve"),
                                raw_advisory.get("title"),
                                raw_advisory.get("url"),
                                raw_advisory.get("name"),
                            ]
                        ),
                        patched_version=_npm_patched_version(
                            raw_advisory,
                            fix_available=fix_available,
                        ),
                        vulnerable_range=_string_value(raw_advisory.get("range"))
                        or _string_value(raw_vulnerability.get("range")),
                        aliases=(),
                        title=_string_value(raw_advisory.get("title"))
                        or _string_value(raw_advisory.get("name"))
                        or package_name,
                        advisory_url=_string_value(raw_advisory.get("url")),
                    )
                )
        return _dedupe_findings(findings)

    legacy_advisories = payload.get("advisories")
    if not isinstance(legacy_advisories, Mapping):
        return []

    for raw_advisory_id, raw_advisory in legacy_advisories.items():
        if not isinstance(raw_advisory, Mapping):
            continue
        legacy_package_name = (
            _string_value(raw_advisory.get("module_name"))
            or _string_value(raw_advisory.get("name"))
            or _string_value(raw_advisory.get("module"))
        )
        if legacy_package_name is None:
            continue
        advisory_id = _string_value(raw_advisory_id) or _npm_advisory_id(raw_advisory)
        patched_versions = raw_advisory.get("patched_versions")
        cve_candidates: list[object] = []
        raw_cves = raw_advisory.get("cves")
        if isinstance(raw_cves, Sequence) and not isinstance(raw_cves, (str, bytes)):
            cve_candidates.extend(raw_cves)
        cve_candidates.extend([raw_advisory.get("title"), raw_advisory.get("url")])
        advisory_findings = raw_advisory.get("findings")
        if isinstance(advisory_findings, Sequence) and advisory_findings:
            for raw_finding in advisory_findings:
                if not isinstance(raw_finding, Mapping):
                    continue
                findings.append(
                    _build_dependency_finding(
                        ecosystem="npm",
                        manifest_path=manifest_path,
                        package_name=legacy_package_name,
                        package_version=_string_value(raw_finding.get("version"))
                        or versions.get(legacy_package_name)
                        or "unknown",
                        severity=_normalize_severity(raw_advisory.get("severity")),
                        advisory_id=advisory_id,
                        cve_id=_extract_cve_id(cve_candidates),
                        patched_version=_string_value(patched_versions),
                        vulnerable_range=_string_value(raw_advisory.get("vulnerable_versions")),
                        aliases=tuple(
                            str(item) for item in cve_candidates if isinstance(item, str)
                        ),
                        title=_string_value(raw_advisory.get("title")) or legacy_package_name,
                        advisory_url=_string_value(raw_advisory.get("url")),
                    )
                )
            continue

        findings.append(
            _build_dependency_finding(
                ecosystem="npm",
                manifest_path=manifest_path,
                package_name=legacy_package_name,
                package_version=versions.get(legacy_package_name) or "unknown",
                severity=_normalize_severity(raw_advisory.get("severity")),
                advisory_id=advisory_id,
                cve_id=_extract_cve_id(cve_candidates),
                patched_version=_string_value(patched_versions),
                vulnerable_range=_string_value(raw_advisory.get("vulnerable_versions")),
                aliases=tuple(str(item) for item in cve_candidates if isinstance(item, str)),
                title=_string_value(raw_advisory.get("title")) or legacy_package_name,
                advisory_url=_string_value(raw_advisory.get("url")),
            )
        )

    return _dedupe_findings(findings)


def parse_pip_audit_payload(
    payload: object,
    *,
    project_root: Path,
) -> list[CandidateFinding]:
    if not isinstance(payload, Mapping):
        return []

    manifest_path = _python_manifest_path(project_root)
    dependencies = payload.get("dependencies")
    if not isinstance(dependencies, Sequence):
        return []

    findings: list[CandidateFinding] = []
    for raw_dependency in dependencies:
        if not isinstance(raw_dependency, Mapping):
            continue
        package_name = _string_value(raw_dependency.get("name"))
        package_version = _string_value(raw_dependency.get("version")) or "unknown"
        if package_name is None:
            continue
        vulns = raw_dependency.get("vulns")
        if not isinstance(vulns, Sequence):
            continue
        for raw_vuln in vulns:
            if not isinstance(raw_vuln, Mapping):
                continue
            aliases = tuple(
                item
                for item in raw_vuln.get("aliases", ())
                if isinstance(item, str) and item.strip()
            )
            fix_versions = tuple(
                item
                for item in raw_vuln.get("fix_versions", ())
                if isinstance(item, str) and item.strip()
            )
            findings.append(
                _build_dependency_finding(
                    ecosystem="python",
                    manifest_path=manifest_path,
                    package_name=package_name,
                    package_version=package_version,
                    severity=_normalize_severity(None, default="medium"),
                    advisory_id=_string_value(raw_vuln.get("id")) or package_name,
                    cve_id=_extract_cve_id([raw_vuln.get("id"), *aliases]),
                    patched_version=fix_versions[0] if fix_versions else None,
                    vulnerable_range=None,
                    aliases=aliases,
                    title=_string_value(raw_vuln.get("description")) or package_name,
                    advisory_url=None,
                    fixed_versions=fix_versions,
                )
            )
    return _dedupe_findings(findings)


def _run_npm_audit(project_root: Path) -> list[CandidateFinding]:
    if shutil.which("npm") is None:
        logger.info(
            "skipping npm dependency audit because npm is not installed",
            extra={"event": "dependency_audit_tool_missing", "tool": "npm"},
        )
        return []
    payload = _run_json_command(["npm", "audit", "--json"], cwd=project_root, tool_name="npm audit")
    if payload is None:
        return []
    return parse_npm_audit_payload(payload, project_root=project_root)


def _run_pip_audit(project_root: Path) -> list[CandidateFinding]:
    if shutil.which("pip-audit") is None:
        logger.info(
            "skipping pip-audit because the binary is not installed",
            extra={"event": "dependency_audit_tool_missing", "tool": "pip-audit"},
        )
        return []
    cmd = ["pip-audit", "--format", "json", *_pip_audit_target_args(project_root)]
    payload = _run_json_command(cmd, cwd=project_root, tool_name="pip-audit")
    if payload is None:
        return []
    return parse_pip_audit_payload(payload, project_root=project_root)


def _generate_npm_sbom(
    project_root: Path,
    output_dir: Path,
    sbom_format: SbomFormat,
) -> Path | None:
    if shutil.which("npm") is None:
        return None
    json_fragment = _run_json_text_command(
        ["npm", "sbom", "--sbom-format", sbom_format],
        cwd=project_root,
        tool_name="npm sbom",
    )
    if json_fragment is None:
        return None
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / f"sbom.npm.{sbom_format}.json"
    output_path.write_text(f"{json_fragment}\n", encoding="utf-8")
    return output_path


def _generate_python_sbom(
    project_root: Path,
    output_dir: Path,
    sbom_format: SbomFormat,
) -> Path | None:
    if sbom_format != "cyclonedx" or shutil.which("pip-audit") is None:
        return None
    json_fragment = _run_json_text_command(
        ["pip-audit", "--format", "cyclonedx-json", *_pip_audit_target_args(project_root)],
        cwd=project_root,
        tool_name="pip-audit",
    )
    if json_fragment is None:
        return None
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / "sbom.python.cyclonedx.json"
    output_path.write_text(f"{json_fragment}\n", encoding="utf-8")
    return output_path


def _run_json_command(
    cmd: list[str],
    *,
    cwd: Path,
    tool_name: str,
) -> object | None:
    json_fragment = _run_json_text_command(cmd, cwd=cwd, tool_name=tool_name)
    if json_fragment is None:
        return None
    try:
        return cast(object, json.loads(json_fragment))
    except json.JSONDecodeError as exc:
        log_error_context(
            logger,
            event="dependency_audit_json_invalid",
            what="dependency_audit_json_parse",
            on_what=tool_name,
            why=str(exc),
            next_step="skipping_dependency_findings",
            debug=f"cwd={cwd}; payload={json_fragment[:500]!r}",
        )
        return None


def _run_json_text_command(
    cmd: list[str],
    *,
    cwd: Path,
    tool_name: str,
) -> str | None:
    try:
        result = run_subprocess(cmd, cwd=cwd, timeout=180, logger=logger)
    except FileNotFoundError:
        logger.info(
            "skipping %s because the binary is not installed",
            tool_name,
            extra={"event": "dependency_audit_tool_missing", "tool": cmd[0]},
        )
        return None

    raw_output = result.stdout.strip() or result.stderr.strip()
    if not raw_output:
        return None
    json_fragment = _extract_json_fragment(raw_output)
    if json_fragment is not None:
        return json_fragment
    log_error_context(
        logger,
        event="dependency_audit_output_invalid",
        what="dependency_audit_output_parse",
        on_what=tool_name,
        why="command output did not contain a JSON payload",
        next_step="skipping_dependency_findings",
        debug=f"cwd={cwd}; output={raw_output[:500]!r}",
    )
    return None


def _extract_json_fragment(raw_output: str) -> str | None:
    object_index = raw_output.find("{")
    array_index = raw_output.find("[")
    start_candidates = [index for index in (object_index, array_index) if index >= 0]
    if not start_candidates:
        return None
    payload = raw_output[min(start_candidates) :]
    try:
        _, end = _JSON_DECODER.raw_decode(payload)
    except json.JSONDecodeError:
        return None
    return payload[:end]


def _build_dependency_finding(
    *,
    ecosystem: str,
    manifest_path: Path,
    package_name: str,
    package_version: str,
    severity: str,
    advisory_id: str,
    cve_id: str | None,
    patched_version: str | None,
    vulnerable_range: str | None,
    aliases: Sequence[str],
    title: str,
    advisory_url: str | None,
    fixed_versions: Sequence[str] = (),
) -> CandidateFinding:
    normalized_manifest = manifest_path.resolve(strict=False)
    rendered_title = " ".join(title.split())[:240]
    summary = _dependency_summary(
        package_name=package_name,
        package_version=package_version,
        advisory_id=advisory_id,
        cve_id=cve_id,
        patched_version=patched_version,
        severity=severity,
    )
    metadata: dict[str, object] = {
        "ecosystem": ecosystem,
        "package": package_name,
        "package_version": package_version,
        "patched_version": patched_version,
        "severity": severity,
        "advisory_id": advisory_id,
        "cve_id": cve_id,
        "aliases": list(aliases),
        "vulnerable_range": vulnerable_range,
        "title": rendered_title,
        "advisory_url": advisory_url,
    }
    if fixed_versions:
        metadata["fixed_versions"] = list(fixed_versions)

    return CandidateFinding(
        id=_dependency_finding_id(
            ecosystem=ecosystem,
            package_name=package_name,
            package_version=package_version,
            advisory_id=advisory_id,
        ),
        vuln_class="CWE-1395",
        source=TaintSource(
            location=SourceLocation(
                file=str(normalized_manifest),
                line=1,
                column=1,
                snippet=f"{package_name}@{package_version}",
            ),
            source_type="dependency_manifest",
            data_categories=[],
            parameter_name=package_name,
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=str(normalized_manifest),
                line=1,
                column=1,
                snippet=summary,
            ),
            sink_type="dependency_vulnerability",
            api_name=advisory_id,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=1.0,
        severity=severity,
        metadata=metadata,
    )


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
    if cve_id is not None:
        parts.append(f"cve={cve_id}")
    if patched_version is not None:
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


def _normalize_severity(value: object, *, default: str = "medium") -> str:
    normalized = _string_value(value)
    if normalized is None:
        return default
    return _SEVERITY_MAP.get(normalized.lower(), default)


def _npm_advisory_id(advisory: Mapping[str, object]) -> str:
    cve_id = _extract_cve_id(
        [advisory.get("cve"), advisory.get("title"), advisory.get("url"), advisory.get("name")]
    )
    if cve_id is not None:
        return cve_id
    url = _string_value(advisory.get("url"))
    if url is not None:
        match = _GHSA_PATTERN.search(url)
        if match is not None:
            return match.group(1).upper()
    source = advisory.get("source")
    if source is not None:
        return str(source)
    return _string_value(advisory.get("name")) or "npm-advisory"


def _npm_patched_version(
    advisory: Mapping[str, object],
    *,
    fix_available: object,
) -> str | None:
    patched = _string_value(advisory.get("patched_version"))
    if patched is not None:
        return patched
    if isinstance(fix_available, Mapping):
        resolved = _string_value(fix_available.get("version"))
        if resolved is not None:
            return resolved
    return None


def _extract_cve_id(candidates: Sequence[object]) -> str | None:
    for candidate in candidates:
        if not isinstance(candidate, str):
            continue
        match = _CVE_PATTERN.search(candidate)
        if match is not None:
            return match.group(1).upper()
    return None


def _string_value(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    normalized = value.strip()
    return normalized or None


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for finding in findings:
        if finding.id in seen_ids:
            continue
        deduped.append(finding)
        seen_ids.add(finding.id)
    return deduped


def _is_node_project(project_root: Path) -> bool:
    return (project_root / "package.json").exists()


def _is_python_project(project_root: Path) -> bool:
    if any((project_root / name).exists() for name in _PYTHON_PROJECT_FILES):
        return True
    return any(project_root.glob("requirements*.txt"))


def _requirements_file(project_root: Path) -> Path | None:
    preferred = project_root / "requirements.txt"
    if preferred.exists():
        return preferred
    matches = sorted(project_root.glob("requirements*.txt"))
    if not matches:
        return None
    return matches[0]


def _pip_audit_target_args(project_root: Path) -> list[str]:
    requirements_file = _requirements_file(project_root)
    if requirements_file is not None:
        return ["-r", requirements_file.name]
    if _is_python_project(project_root):
        return ["."]
    return []


def _node_manifest_path(project_root: Path) -> Path:
    for filename in (*_NPM_LOCKFILES, "package.json"):
        candidate = project_root / filename
        if candidate.exists():
            return candidate
    return project_root / "package.json"


def _python_manifest_path(project_root: Path) -> Path:
    requirements_file = _requirements_file(project_root)
    if requirements_file is not None:
        return requirements_file
    for filename in _PYTHON_PROJECT_FILES:
        candidate = project_root / filename
        if candidate.exists():
            return candidate
    return project_root / "pyproject.toml"


def _load_npm_package_versions(project_root: Path) -> dict[str, str]:
    versions: dict[str, str] = {}
    for filename in _NPM_LOCKFILES:
        candidate = project_root / filename
        if not candidate.exists():
            continue
        try:
            payload = json.loads(candidate.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(payload, Mapping):
            continue
        packages = payload.get("packages")
        if isinstance(packages, Mapping):
            for package_path, raw_entry in packages.items():
                if not isinstance(package_path, str) or not isinstance(raw_entry, Mapping):
                    continue
                package_name = _package_name_from_lock_path(package_path)
                package_version = _string_value(raw_entry.get("version"))
                if package_name is None or package_version is None:
                    continue
                versions.setdefault(package_name, package_version)
        dependencies = payload.get("dependencies")
        if isinstance(dependencies, Mapping):
            _walk_lock_dependencies(dependencies, versions)
        if versions:
            return versions

    package_json = project_root / "package.json"
    if not package_json.exists():
        return versions
    try:
        payload = json.loads(package_json.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return versions
    if not isinstance(payload, Mapping):
        return versions
    for section in ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies"):
        raw_dependencies = payload.get(section)
        if not isinstance(raw_dependencies, Mapping):
            continue
        for raw_name, raw_version in raw_dependencies.items():
            if not isinstance(raw_name, str):
                continue
            normalized_version = _string_value(raw_version)
            if normalized_version is None:
                continue
            versions.setdefault(raw_name, normalized_version)
    return versions


def _walk_lock_dependencies(dependencies: Mapping[str, object], versions: dict[str, str]) -> None:
    for raw_name, raw_entry in dependencies.items():
        if not isinstance(raw_name, str) or not isinstance(raw_entry, Mapping):
            continue
        version = _string_value(raw_entry.get("version"))
        if version is not None:
            versions.setdefault(raw_name, version)
        nested = raw_entry.get("dependencies")
        if isinstance(nested, Mapping):
            _walk_lock_dependencies(nested, versions)


def _package_name_from_lock_path(package_path: str) -> str | None:
    normalized = package_path.replace("\\", "/")
    if "node_modules/" not in normalized:
        return None
    name = normalized.rsplit("node_modules/", 1)[1]
    stripped = name.strip("/")
    return stripped or None


__all__ = [
    "DependencyScanResult",
    "SbomFormat",
    "parse_npm_audit_payload",
    "parse_pip_audit_payload",
    "scan_dependency_findings",
]
