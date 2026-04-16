from __future__ import annotations

import re
import sys
import tomllib
from pathlib import Path

try:
    from scripts.check_known_limitations import collect_known_limitations_errors
except ModuleNotFoundError:  # pragma: no cover - supports direct script execution.
    from check_known_limitations import collect_known_limitations_errors

ROOT = Path(__file__).resolve().parents[1]


def collect_release_hygiene_errors(root: Path = ROOT) -> list[str]:
    version = _pyproject_version(root)
    errors: list[str] = []

    init_version = _init_version(root)
    if init_version != version:
        errors.append(
            f"version mismatch: pyproject.toml has {version}, "
            f"src/piranesi/__init__.py has {init_version}"
        )

    changelog_version = _latest_changelog_version(root)
    if changelog_version != version:
        errors.append(
            f"CHANGELOG.md latest entry is {changelog_version or 'missing'}, expected {version}"
        )

    readme = _read(root / "README.md")
    if f"`v{version}`" not in readme and f"v{version}" not in readme:
        errors.append(f"README.md does not mention v{version}")
    if "docs/capabilities.md" not in readme:
        errors.append("README.md must link docs/capabilities.md")

    security = _read(root / "SECURITY.md")
    release_line = _release_line(version)
    if f"`{release_line}`" not in security:
        errors.append(f"SECURITY.md does not mark {release_line} as supported")
    if f"`< {version}`" not in security:
        errors.append(f"SECURITY.md does not mark versions before {version} unsupported")

    capabilities_path = root / "docs" / "capabilities.md"
    if not capabilities_path.exists():
        errors.append("docs/capabilities.md is missing")
    else:
        capabilities = _read(capabilities_path)
        if "known-limitations.json" not in capabilities:
            errors.append("docs/capabilities.md must reference docs/known-limitations.json")

    stale_patterns = _stale_version_patterns(version)
    for relative_path in _release_doc_paths(root):
        if relative_path.name == "CHANGELOG.md":
            continue
        text = _read(relative_path)
        for pattern in stale_patterns:
            if pattern.search(text):
                errors.append(f"{relative_path.relative_to(root)} contains stale release text")
                break

    known_limitations_errors = collect_known_limitations_errors(root)
    errors.extend(f"known limitations registry: {error}" for error in known_limitations_errors)

    return errors


def main() -> int:
    errors = collect_release_hygiene_errors(ROOT)
    if not errors:
        print("release hygiene checks passed")
        return 0
    print("release hygiene checks failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    return 1


def _pyproject_version(root: Path) -> str:
    with (root / "pyproject.toml").open("rb") as handle:
        data = tomllib.load(handle)
    version = data.get("project", {}).get("version")
    if not isinstance(version, str) or not version:
        raise RuntimeError("pyproject.toml is missing [project].version")
    return version


def _init_version(root: Path) -> str | None:
    text = _read(root / "src" / "piranesi" / "__init__.py")
    match = re.search(r'^__version__\s*=\s*["\']([^"\']+)["\']', text, flags=re.MULTILINE)
    return match.group(1) if match else None


def _latest_changelog_version(root: Path) -> str | None:
    text = _read(root / "CHANGELOG.md")
    match = re.search(r"^## \[([^\]]+)\]", text, flags=re.MULTILINE)
    return match.group(1) if match else None


def _release_line(version: str) -> str:
    major, minor, *_ = version.split(".")
    return f"{major}.{minor}.x"


def _stale_version_patterns(current_version: str) -> tuple[re.Pattern[str], ...]:
    major, minor, *_ = current_version.split(".")
    current_line = f"{major}.{minor}.x"
    return (
        re.compile(r"\bv0\.1\.0\b"),
        re.compile(r"\b0\.1\.x\b"),
        re.compile(r"`0\.1\.x`"),
        re.compile(r"latest `0\.1\.x`"),
        re.compile(rf"Only the latest `(?!{re.escape(current_line)})\d+\.\d+\.x`"),
    )


def _release_doc_paths(root: Path) -> list[Path]:
    paths = [
        root / "README.md",
        root / "SECURITY.md",
        root / "CHANGELOG.md",
    ]
    paths.extend(sorted((root / "docs").rglob("*.md")))
    return [path for path in paths if path.exists()]


def _read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


if __name__ == "__main__":
    raise SystemExit(main())
