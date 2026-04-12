from __future__ import annotations

import json
import re
import subprocess
import xml.etree.ElementTree as ET
from collections import defaultdict, deque
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import cast

import yaml

from piranesi.scan.framework import resolve_frameworks

_NODE_DEPENDENCY_KEYS = (
    "dependencies",
    "devDependencies",
    "peerDependencies",
    "optionalDependencies",
)
_CONTROL_FILES = frozenset(
    {
        "package.json",
        "pnpm-workspace.yaml",
        "turbo.json",
        "nx.json",
        "workspace.json",
        "lerna.json",
        "go.work",
        "pom.xml",
        "settings.gradle",
        "settings.gradle.kts",
    }
)
_SOURCE_DISCOVERY_EXCLUDE_PARTS = frozenset(
    {"node_modules", ".venv", "venv", "__pycache__", "dist", "build", "target", "vendor"}
)
_GO_MODULE_PATTERN = re.compile(r"^\s*module\s+(?P<name>\S+)\s*$")
_GO_REQUIRE_PATTERN = re.compile(r"^\s*(?P<name>\S+)\s+v[^\s]+(?:\s*//.*)?$")
_GRADLE_INCLUDE_PATTERN = re.compile(r'["\'](?P<project>:[^"\']+)["\']')
_GRADLE_PROJECT_DEP_PATTERN = re.compile(r'project\(\s*["\'](?P<project>:[^"\']+)["\']\s*\)')


@dataclass(frozen=True, slots=True)
class WorkspacePackage:
    name: str
    path: Path
    language: str
    internal_deps: list[str]
    frameworks: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class MonorepoManifest:
    root_path: Path
    packages: list[WorkspacePackage]
    dependency_edges: list[tuple[str, str]]
    detected_tool: str

    @property
    def tool(self) -> str:
        return self.detected_tool


@dataclass(frozen=True, slots=True)
class _PackageRecord:
    name: str
    path: Path
    dependency_names: tuple[str, ...]


def detect_monorepo(
    root_path: Path,
    *,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    root = root_path.resolve(strict=False)

    nx_manifest = _detect_nx_workspace(root, requested_frameworks=requested_frameworks)
    if nx_manifest is not None:
        return nx_manifest

    if (root / "turbo.json").is_file():
        records = _node_workspace_records(root)
        if records:
            return _build_manifest(
                root,
                records=records,
                detected_tool="turborepo",
                requested_frameworks=requested_frameworks,
            )

    if (root / "pnpm-workspace.yaml").is_file():
        records = _pnpm_workspace_records(root)
        if records:
            return _build_manifest(
                root,
                records=records,
                detected_tool="pnpm",
                requested_frameworks=requested_frameworks,
            )

    if (root / "lerna.json").is_file():
        records = _lerna_workspace_records(root)
        if records:
            return _build_manifest(
                root,
                records=records,
                detected_tool="lerna",
                requested_frameworks=requested_frameworks,
            )

    records = _node_workspace_records(root)
    if records:
        return _build_manifest(
            root,
            records=records,
            detected_tool=_node_workspace_tool(root),
            requested_frameworks=requested_frameworks,
        )

    go_manifest = _detect_go_workspace(root, requested_frameworks=requested_frameworks)
    if go_manifest is not None:
        return go_manifest

    maven_manifest = _detect_maven_workspace(root, requested_frameworks=requested_frameworks)
    if maven_manifest is not None:
        return maven_manifest

    gradle_manifest = _detect_gradle_workspace(root, requested_frameworks=requested_frameworks)
    if gradle_manifest is not None:
        return gradle_manifest

    return None


def topologically_sorted_packages(
    manifest: MonorepoManifest,
    *,
    selected_names: Iterable[str] | None = None,
) -> list[WorkspacePackage]:
    packages_by_name = {package.name: package for package in manifest.packages}
    selected = (
        set(packages_by_name)
        if selected_names is None
        else {name for name in selected_names if name in packages_by_name}
    )
    if not selected:
        return []

    indegree = dict.fromkeys(selected, 0)
    dependents: dict[str, list[str]] = defaultdict(list)
    for source_name, dependency_name in manifest.dependency_edges:
        if source_name not in selected or dependency_name not in selected:
            continue
        indegree[source_name] += 1
        dependents[dependency_name].append(source_name)

    ready = deque(sorted(name for name, count in indegree.items() if count == 0))
    ordered: list[str] = []
    while ready:
        current = ready.popleft()
        ordered.append(current)
        for dependent in sorted(dependents.get(current, ())):
            indegree[dependent] -= 1
            if indegree[dependent] == 0:
                ready.append(dependent)

    if len(ordered) != len(selected):
        remaining = sorted(selected - set(ordered))
        ordered.extend(remaining)

    return [packages_by_name[name] for name in ordered]


def detect_monorepo_manifest(
    root_path: Path,
    framework_overrides: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    return detect_monorepo(root_path, requested_frameworks=framework_overrides)


def topological_package_batches(
    manifest: MonorepoManifest,
    *,
    selected_names: Iterable[str] | None = None,
) -> list[list[WorkspacePackage]]:
    ordered_packages = topologically_sorted_packages(manifest, selected_names=selected_names)
    if not ordered_packages:
        return []

    remaining_deps = {
        package.name: {
            dependency_name
            for source_name, dependency_name in manifest.dependency_edges
            if source_name == package.name
        }
        for package in ordered_packages
    }
    processed: set[str] = set()
    batches: list[list[WorkspacePackage]] = []

    while len(processed) < len(ordered_packages):
        current_batch = [
            package
            for package in ordered_packages
            if package.name not in processed and remaining_deps[package.name] <= processed
        ]
        if not current_batch:
            break
        batches.append(current_batch)
        processed.update(package.name for package in current_batch)

    if len(processed) != len(ordered_packages):
        unresolved = [package for package in ordered_packages if package.name not in processed]
        batches.append(unresolved)

    return batches


def transitive_dependents(
    manifest: MonorepoManifest,
    package_names: Iterable[str],
) -> set[str]:
    reverse_edges: dict[str, set[str]] = defaultdict(set)
    for source_name, dependency_name in manifest.dependency_edges:
        reverse_edges[dependency_name].add(source_name)

    selected = {
        name for name in package_names if any(pkg.name == name for pkg in manifest.packages)
    }
    queue = deque(selected)
    while queue:
        current = queue.popleft()
        for dependent in sorted(reverse_edges.get(current, ())):
            if dependent in selected:
                continue
            selected.add(dependent)
            queue.append(dependent)
    return selected


def changed_package_names(
    manifest: MonorepoManifest,
    *,
    include_dependents: bool = True,
) -> set[str]:
    changed_files = _git_changed_files(manifest.root_path)
    if not changed_files:
        return set()

    package_paths = sorted(
        ((package.path.resolve(strict=False), package.name) for package in manifest.packages),
        key=lambda item: len(item[0].parts),
        reverse=True,
    )
    changed_packages: set[str] = set()
    all_packages = {package.name for package in manifest.packages}
    for relative_path in changed_files:
        resolved = (manifest.root_path / relative_path).resolve(strict=False)
        matched = False
        for package_path, package_name in package_paths:
            if _is_relative_to(resolved, package_path):
                changed_packages.add(package_name)
                matched = True
                break
        if matched:
            continue
        if relative_path.parts and relative_path.parts[0] in _CONTROL_FILES:
            changed_packages = set(all_packages)
            break

    if include_dependents:
        changed_packages = transitive_dependents(manifest, changed_packages)
    return changed_packages


def select_packages(
    manifest: MonorepoManifest,
    *,
    package_name: str | None = None,
    changed_only: bool = False,
) -> list[WorkspacePackage]:
    selected_names = {package.name for package in manifest.packages}
    if changed_only:
        selected_names &= changed_package_names(manifest)
    if package_name is not None:
        selector = package_name.strip()
        selected_names = {
            package.name
            for package in manifest.packages
            if _package_matches_selector(package, selector)
        } & selected_names
    return topologically_sorted_packages(manifest, selected_names=selected_names)


def package_for_path(
    manifest: MonorepoManifest,
    path: Path,
) -> WorkspacePackage | None:
    resolved = path.resolve(strict=False)
    matches = [
        package
        for package in manifest.packages
        if _is_relative_to(resolved, package.path.resolve(strict=False))
    ]
    if not matches:
        return None
    return max(matches, key=lambda package: len(package.path.parts))


def _detect_nx_workspace(
    root: Path,
    *,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    if not (root / "nx.json").is_file():
        return None

    workspace_config = _load_json(root / "workspace.json")
    records: list[_PackageRecord] = []
    if isinstance(workspace_config, dict):
        projects = workspace_config.get("projects")
        if isinstance(projects, dict):
            for project_name, raw_project in projects.items():
                if isinstance(raw_project, str):
                    project_root = root / raw_project
                    records.append(
                        _package_record_from_directory(project_root, default_name=project_name)
                    )
                elif isinstance(raw_project, dict):
                    raw_root = raw_project.get("root")
                    if isinstance(raw_root, str):
                        project_root = root / raw_root
                        records.append(
                            _package_record_from_directory(project_root, default_name=project_name)
                        )

    if not records:
        for project_json in sorted(root.rglob("project.json")):
            if any(part in _SOURCE_DISCOVERY_EXCLUDE_PARTS for part in project_json.parts):
                continue
            payload = _load_json(project_json)
            if not isinstance(payload, dict):
                continue
            raw_root = payload.get("root")
            default_name = payload.get("name") if isinstance(payload.get("name"), str) else None
            project_root = project_json.parent if not isinstance(raw_root, str) else root / raw_root
            records.append(_package_record_from_directory(project_root, default_name=default_name))

    cleaned_records = _dedupe_records(records)
    if not cleaned_records:
        return None
    return _build_manifest(
        root,
        records=cleaned_records,
        detected_tool="nx",
        requested_frameworks=requested_frameworks,
    )


def _detect_go_workspace(
    root: Path,
    *,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    go_work = root / "go.work"
    if not go_work.is_file():
        return None

    module_dirs = _parse_go_work_modules(go_work)
    records: list[_PackageRecord] = []
    for module_dir in module_dirs:
        gomod = module_dir / "go.mod"
        module_name = _go_module_name(gomod)
        if module_name is None:
            continue
        records.append(
            _PackageRecord(
                name=module_name,
                path=module_dir.resolve(strict=False),
                dependency_names=_go_internal_dependencies(gomod),
            )
        )
    if not records:
        return None
    return _build_manifest(
        root,
        records=records,
        detected_tool="go-work",
        requested_frameworks=requested_frameworks,
    )


def _detect_maven_workspace(
    root: Path,
    *,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    pom_path = root / "pom.xml"
    if not pom_path.is_file():
        return None

    try:
        pom_tree = ET.parse(pom_path)  # noqa: S314 - parses local project metadata.
    except (ET.ParseError, OSError):
        return None

    module_paths = [
        (root / module_text.strip()).resolve(strict=False)
        for module_text in _pom_modules(pom_tree.getroot())
        if module_text.strip()
    ]
    records: list[_PackageRecord] = []
    for module_path in module_paths:
        module_pom = module_path / "pom.xml"
        artifact_id = _pom_artifact_id(module_pom)
        if artifact_id is None:
            continue
        records.append(
            _PackageRecord(
                name=artifact_id,
                path=module_path,
                dependency_names=_pom_dependency_artifact_ids(module_pom),
            )
        )

    if not records:
        return None
    return _build_manifest(
        root,
        records=records,
        detected_tool="maven",
        requested_frameworks=requested_frameworks,
    )


def _detect_gradle_workspace(
    root: Path,
    *,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest | None:
    settings_path = root / "settings.gradle"
    if not settings_path.is_file():
        settings_path = root / "settings.gradle.kts"
    if not settings_path.is_file():
        return None

    included_projects = _gradle_included_projects(settings_path)
    if not included_projects:
        return None

    records: list[_PackageRecord] = []
    for project_name in included_projects:
        project_path = root / project_name.replace(":", "/").lstrip("/")
        if not project_path.is_dir():
            continue
        records.append(
            _PackageRecord(
                name=project_name.lstrip(":").replace(":", "/"),
                path=project_path.resolve(strict=False),
                dependency_names=_gradle_project_dependencies(project_path),
            )
        )

    if not records:
        return None
    return _build_manifest(
        root,
        records=records,
        detected_tool="gradle",
        requested_frameworks=requested_frameworks,
    )


def _build_manifest(
    root: Path,
    *,
    records: Sequence[_PackageRecord],
    detected_tool: str,
    requested_frameworks: Sequence[str] | None = None,
) -> MonorepoManifest:
    normalized_root = root.resolve(strict=False)
    deduped_records = _dedupe_records(records)
    package_names = {record.name for record in deduped_records}
    packages: list[WorkspacePackage] = []
    dependency_edges: list[tuple[str, str]] = []

    for record in deduped_records:
        package_path = record.path.resolve(strict=False)
        frameworks = resolve_frameworks(package_path, requested_frameworks)
        internal_deps = sorted(
            dependency_name
            for dependency_name in record.dependency_names
            if dependency_name in package_names and dependency_name != record.name
        )
        packages.append(
            WorkspacePackage(
                name=record.name,
                path=package_path,
                language=_detect_package_language(package_path, frameworks=frameworks),
                internal_deps=internal_deps,
                frameworks=frameworks,
            )
        )
        dependency_edges.extend((record.name, dependency_name) for dependency_name in internal_deps)

    ordered_packages = topologically_sorted_packages(
        MonorepoManifest(
            root_path=normalized_root,
            packages=packages,
            dependency_edges=dependency_edges,
            detected_tool=detected_tool,
        )
    )
    return MonorepoManifest(
        root_path=normalized_root,
        packages=ordered_packages,
        dependency_edges=sorted(set(dependency_edges)),
        detected_tool=detected_tool,
    )


def _pnpm_workspace_records(root: Path) -> list[_PackageRecord]:
    workspace_path = root / "pnpm-workspace.yaml"
    try:
        payload = yaml.safe_load(workspace_path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError):
        return []
    packages = payload.get("packages") if isinstance(payload, dict) else None
    patterns = (
        [pattern for pattern in packages if isinstance(pattern, str)]
        if isinstance(packages, list)
        else []
    )
    return _node_workspace_records_from_patterns(root, patterns)


def _lerna_workspace_records(root: Path) -> list[_PackageRecord]:
    payload = _load_json(root / "lerna.json")
    packages = payload.get("packages") if isinstance(payload, dict) else None
    patterns = (
        [pattern for pattern in packages if isinstance(pattern, str)]
        if isinstance(packages, list)
        else []
    )
    return _node_workspace_records_from_patterns(root, patterns)


def _node_workspace_records(root: Path) -> list[_PackageRecord]:
    payload = _load_json(root / "package.json")
    if not isinstance(payload, dict):
        return []
    raw_workspaces = payload.get("workspaces")
    patterns: list[str]
    if isinstance(raw_workspaces, list):
        patterns = [pattern for pattern in raw_workspaces if isinstance(pattern, str)]
    elif isinstance(raw_workspaces, dict):
        raw_packages = raw_workspaces.get("packages")
        patterns = (
            [pattern for pattern in raw_packages if isinstance(pattern, str)]
            if isinstance(raw_packages, list)
            else []
        )
    else:
        patterns = []
    return _node_workspace_records_from_patterns(root, patterns)


def _node_workspace_records_from_patterns(
    root: Path, patterns: Sequence[str]
) -> list[_PackageRecord]:
    package_dirs = _glob_workspace_directories(
        root,
        patterns=patterns,
        manifest_name="package.json",
    )
    records: list[_PackageRecord] = []
    for package_dir in package_dirs:
        package_json = _load_json(package_dir / "package.json")
        if not isinstance(package_json, dict):
            continue
        package_name = package_json.get("name")
        if not isinstance(package_name, str) or not package_name:
            package_name = package_dir.name
        records.append(
            _PackageRecord(
                name=package_name,
                path=package_dir.resolve(strict=False),
                dependency_names=_node_dependency_names(package_json),
            )
        )
    return records


def _package_record_from_directory(
    project_root: Path, default_name: str | None = None
) -> _PackageRecord:
    normalized_root = project_root.resolve(strict=False)
    package_json = _load_json(normalized_root / "package.json")
    if isinstance(package_json, dict):
        package_name = package_json.get("name")
        if not isinstance(package_name, str) or not package_name:
            package_name = default_name or normalized_root.name
        return _PackageRecord(
            name=package_name,
            path=normalized_root,
            dependency_names=_node_dependency_names(package_json),
        )

    pom_path = normalized_root / "pom.xml"
    artifact_id = _pom_artifact_id(pom_path)
    if artifact_id is not None:
        return _PackageRecord(
            name=artifact_id,
            path=normalized_root,
            dependency_names=_pom_dependency_artifact_ids(pom_path),
        )

    gomod = normalized_root / "go.mod"
    module_name = _go_module_name(gomod)
    if module_name is not None:
        return _PackageRecord(
            name=module_name,
            path=normalized_root,
            dependency_names=_go_internal_dependencies(gomod),
        )

    return _PackageRecord(
        name=default_name or normalized_root.name,
        path=normalized_root,
        dependency_names=(),
    )


def _glob_workspace_directories(
    root: Path,
    *,
    patterns: Sequence[str],
    manifest_name: str,
) -> list[Path]:
    include_patterns = [pattern for pattern in patterns if pattern and not pattern.startswith("!")]
    exclude_patterns = {pattern[1:] for pattern in patterns if pattern.startswith("!")}
    matched: set[Path] = set()
    for pattern in include_patterns:
        for candidate in root.glob(pattern):
            resolved_candidate = candidate.resolve(strict=False)
            candidate_dir = (
                resolved_candidate if resolved_candidate.is_dir() else resolved_candidate.parent
            )
            if not (candidate_dir / manifest_name).is_file():
                continue
            relative = candidate_dir.relative_to(root).as_posix()
            if any(
                candidate_dir.match(excluded) or relative == excluded
                for excluded in exclude_patterns
            ):
                continue
            matched.add(candidate_dir)
    return sorted(matched)


def _node_workspace_tool(root: Path) -> str:
    package_json = _load_json(root / "package.json")
    package_manager = package_json.get("packageManager") if isinstance(package_json, dict) else None
    if isinstance(package_manager, str) and package_manager.startswith("yarn@"):
        return "yarn-workspaces"
    if (root / "yarn.lock").is_file():
        return "yarn-workspaces"
    return "npm-workspaces"


def _node_dependency_names(payload: dict[str, object]) -> tuple[str, ...]:
    dependencies: set[str] = set()
    for key in _NODE_DEPENDENCY_KEYS:
        raw_section = payload.get(key)
        if not isinstance(raw_section, dict):
            continue
        for dependency_name, version in raw_section.items():
            if isinstance(dependency_name, str) and version:
                dependencies.add(dependency_name)
    return tuple(sorted(dependencies))


def _parse_go_work_modules(go_work_path: Path) -> list[Path]:
    root = go_work_path.parent.resolve(strict=False)
    modules: list[Path] = []
    in_use_block = False
    for raw_line in go_work_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.split("//", 1)[0].strip()
        if not line:
            continue
        if line.startswith("use ("):
            in_use_block = True
            continue
        if in_use_block and line == ")":
            in_use_block = False
            continue
        if line.startswith("use "):
            module_path = line.removeprefix("use").strip().strip('"')
            modules.append((root / module_path).resolve(strict=False))
            continue
        if in_use_block:
            modules.append((root / line.strip('"')).resolve(strict=False))
    return [module for module in modules if (module / "go.mod").is_file()]


def _go_module_name(gomod_path: Path) -> str | None:
    if not gomod_path.is_file():
        return None
    try:
        for line in gomod_path.read_text(encoding="utf-8").splitlines():
            match = _GO_MODULE_PATTERN.match(line)
            if match is not None:
                return match.group("name")
    except OSError:
        return None
    return None


def _go_internal_dependencies(gomod_path: Path) -> tuple[str, ...]:
    if not gomod_path.is_file():
        return ()
    dependencies: set[str] = set()
    try:
        lines = gomod_path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return ()

    in_require_block = False
    for raw_line in lines:
        line = raw_line.split("//", 1)[0].strip()
        if not line:
            continue
        if line.startswith("require ("):
            in_require_block = True
            continue
        if in_require_block and line == ")":
            in_require_block = False
            continue
        if line.startswith("require "):
            line = line.removeprefix("require").strip()
        match = _GO_REQUIRE_PATTERN.match(line)
        if match is not None:
            dependencies.add(match.group("name"))
    return tuple(sorted(dependencies))


def _pom_modules(root: ET.Element) -> list[str]:
    modules: list[str] = []
    for element in root.iter():
        if _xml_local_name(element.tag) != "module":
            continue
        if element.text is not None:
            modules.append(element.text)
    return modules


def _pom_artifact_id(pom_path: Path) -> str | None:
    if not pom_path.is_file():
        return None
    try:
        root = ET.parse(pom_path).getroot()  # noqa: S314 - parses local project metadata.
    except (ET.ParseError, OSError):
        return None
    project_root = root
    for child in project_root:
        if _xml_local_name(child.tag) == "artifactId" and child.text is not None:
            return child.text.strip()
    return None


def _pom_dependency_artifact_ids(pom_path: Path) -> tuple[str, ...]:
    if not pom_path.is_file():
        return ()
    try:
        root = ET.parse(pom_path).getroot()  # noqa: S314 - parses local project metadata.
    except (ET.ParseError, OSError):
        return ()
    dependencies: set[str] = set()
    in_dependency = False
    for element in root.iter():
        local_name = _xml_local_name(element.tag)
        if local_name == "dependency":
            in_dependency = True
            continue
        if in_dependency and local_name == "artifactId" and element.text is not None:
            dependencies.add(element.text.strip())
            in_dependency = False
    return tuple(sorted(dependencies))


def _gradle_included_projects(settings_path: Path) -> list[str]:
    try:
        content = settings_path.read_text(encoding="utf-8")
    except OSError:
        return []
    included: list[str] = []
    for line in content.splitlines():
        stripped = line.split("//", 1)[0].strip()
        if not stripped.startswith("include"):
            continue
        for match in _GRADLE_INCLUDE_PATTERN.finditer(stripped):
            included.append(match.group("project"))
    return included


def _gradle_project_dependencies(project_path: Path) -> tuple[str, ...]:
    dependency_names: set[str] = set()
    for filename in ("build.gradle", "build.gradle.kts"):
        build_file = project_path / filename
        if not build_file.is_file():
            continue
        try:
            content = build_file.read_text(encoding="utf-8")
        except OSError:
            continue
        for match in _GRADLE_PROJECT_DEP_PATTERN.finditer(content):
            dependency_names.add(match.group("project").lstrip(":").replace(":", "/"))
    return tuple(sorted(dependency_names))


def _load_json(path: Path) -> dict[str, object] | object:
    if not path.is_file():
        return {}
    try:
        return cast(dict[str, object] | object, json.loads(path.read_text(encoding="utf-8")))
    except (OSError, json.JSONDecodeError):
        return {}


def _git_changed_files(root: Path) -> set[Path]:
    if not _git_available(root):
        return set()

    commands = [
        ["git", "diff", "--name-only", "--relative", "HEAD", "--"],
        ["git", "diff", "--name-only", "--relative", "--cached", "--"],
        ["git", "diff", "--name-only", "--relative", "--"],
        ["git", "ls-files", "--others", "--exclude-standard"],
    ]
    changed_files: set[Path] = set()
    for command in commands:
        try:
            result = subprocess.run(
                command,
                cwd=root,
                capture_output=True,
                text=True,
                check=False,
            )
        except OSError:
            return set()
        if result.returncode not in {0, 1, 128}:
            continue
        changed_files.update(
            Path(line.strip()) for line in result.stdout.splitlines() if line.strip()
        )
    return changed_files


def _git_available(root: Path) -> bool:
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            cwd=root,
            capture_output=True,
            text=True,
            check=False,
        )
    except OSError:
        return False
    return result.returncode == 0


def _xml_local_name(tag: str) -> str:
    return tag.split("}", 1)[-1]


def _package_matches_selector(package: WorkspacePackage, selector: str) -> bool:
    normalized = selector.strip()
    if not normalized:
        return False
    aliases = {
        package.name,
        package.path.name,
        package.name.rsplit("/", 1)[-1],
    }
    return normalized in aliases


def _is_relative_to(path: Path, other: Path) -> bool:
    try:
        path.relative_to(other)
    except ValueError:
        return False
    return True


def _dedupe_records(records: Sequence[_PackageRecord]) -> list[_PackageRecord]:
    deduped: dict[tuple[str, Path], _PackageRecord] = {}
    for record in records:
        key = (record.name, record.path.resolve(strict=False))
        deduped[key] = _PackageRecord(
            name=record.name,
            path=record.path.resolve(strict=False),
            dependency_names=tuple(sorted(set(record.dependency_names))),
        )
    return sorted(deduped.values(), key=lambda record: (record.path.as_posix(), record.name))


def _detect_package_language(project_root: Path, *, frameworks: Sequence[str]) -> str:
    normalized_frameworks = {framework.lower() for framework in frameworks}
    if _package_has_suffix(project_root, (".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs")):
        return "javascript"
    if "springboot" in normalized_frameworks or _package_has_suffix(project_root, (".java",)):
        return "java"
    if normalized_frameworks & {"flask", "django", "fastapi"} or _package_has_suffix(
        project_root, (".py",)
    ):
        return "python"
    if normalized_frameworks & {"gin", "echo", "chi", "go-stdlib"} or _package_has_suffix(
        project_root, (".go",)
    ):
        return "go"
    return "javascript"


def _package_has_suffix(project_root: Path, suffixes: tuple[str, ...]) -> bool:
    for path in project_root.rglob("*"):
        if not path.is_file() or path.suffix not in suffixes:
            continue
        if any(part in _SOURCE_DISCOVERY_EXCLUDE_PARTS for part in path.parts):
            continue
        return True
    return False


__all__ = [
    "MonorepoManifest",
    "WorkspacePackage",
    "changed_package_names",
    "detect_monorepo",
    "detect_monorepo_manifest",
    "package_for_path",
    "select_packages",
    "topological_package_batches",
    "topologically_sorted_packages",
    "transitive_dependents",
]
