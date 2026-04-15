from __future__ import annotations

import argparse
import io
import json
import re
import sys
import time
import urllib.error
import urllib.request
import zipfile
from collections.abc import Iterable, Iterator, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "src"
if SRC.exists():
    sys.path.insert(0, str(SRC))

DATA_PATH = ROOT / "data" / "cve_functions.json"
OSV_BULK_URLS = {
    "npm": "https://osv-vulnerabilities.storage.googleapis.com/npm/all.zip",
    "PyPI": "https://osv-vulnerabilities.storage.googleapis.com/PyPI/all.zip",
}
USER_AGENT = "piranesi-cve-functions-refresh/1.0"

try:
    from piranesi.detect.dep_reachability import (  # type: ignore[attr-defined]
        _BACKTICK_TOKEN_RE,
        _FUNCTION_TOKEN_RE,
        _normalize_target_candidate,
    )
except Exception:  # pragma: no cover - used when running outside an installed checkout.
    _BACKTICK_TOKEN_RE = re.compile(r"`(?P<token>[A-Za-z_$][\w$./-]*(?:\(\))?)`")
    _FUNCTION_TOKEN_RE = re.compile(r"\b(?P<token>[A-Za-z_$][\w$./-]*)\s*\(\)")

    def _normalize_target_candidate(candidate: str, *, package_name: str) -> str | None:
        normalized = candidate.strip().strip("`'\"()[]{}")
        normalized = re.sub(r"\(\)$", "", normalized.replace("::", ".").strip("."))
        if not normalized:
            return None
        lowered = normalized.lower()
        package = package_name.strip().lower().replace("-", "_")
        for prefix in (f"{package}.", f"{package}/", "_."):
            if lowered.startswith(prefix):
                normalized = normalized[len(prefix) :]
                break
        parts = [part for part in re.split(r"[./]", normalized) if part]
        if not parts:
            return None
        target = parts[-1]
        if not re.fullmatch(r"[A-Za-z_$][\w$-]*", target):
            return None
        return target.strip().lower().replace("-", "_")


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Refresh data/cve_functions.json from OSV.dev bulk advisory data."
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Maximum number of OSV advisory JSON files to process across all ecosystems.",
    )
    parser.add_argument(
        "--ecosystem",
        action="append",
        choices=sorted(OSV_BULK_URLS),
        dest="ecosystems",
        help="OSV ecosystem to refresh. Defaults to npm and PyPI.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=DATA_PATH,
        help="Output JSON path. Defaults to data/cve_functions.json.",
    )
    args = parser.parse_args(argv)

    ecosystems = args.ecosystems or sorted(OSV_BULK_URLS)
    existing = _load_existing_data(args.output)
    cves = _cve_entries(existing)
    packages = _package_entries(existing)

    processed = 0
    for ecosystem in ecosystems:
        if args.limit is not None and processed >= args.limit:
            break
        remaining = None if args.limit is None else args.limit - processed
        for advisory in _download_osv_advisories(ecosystem, limit=remaining):
            processed += 1
            for package_name, cve_ids, functions in _extract_advisory_targets(advisory):
                if not cve_ids or not functions:
                    continue
                _merge_package_functions(packages, package_name, functions)
                for cve_id in cve_ids:
                    existing_entry = cves.get(cve_id)
                    if existing_entry and existing_entry.get("source") == "manual":
                        continue
                    cves[cve_id] = {
                        "package": package_name,
                        "functions": sorted(functions),
                        "source": "osv",
                    }
        time.sleep(1.0)

    output = {
        "cves": dict(sorted(cves.items())),
        "packages": {package: packages[package] for package in sorted(packages)},
        "generated_at": datetime.now(UTC).date().isoformat(),
        "source": "manual+osv",
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(output, indent=2, sort_keys=False) + "\n", encoding="utf-8")
    print(f"processed={processed} cves={len(cves)} packages={len(packages)} output={args.output}")
    return 0


def _download_osv_advisories(ecosystem: str, *, limit: int | None) -> Iterator[dict[str, Any]]:
    url = OSV_BULK_URLS[ecosystem]
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})  # noqa: S310
    try:
        with urllib.request.urlopen(request, timeout=120) as response:  # noqa: S310
            payload = response.read()
    except urllib.error.URLError as exc:
        raise SystemExit(f"failed to download OSV data for {ecosystem}: {exc}") from exc

    emitted = 0
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        for name in sorted(archive.namelist()):
            if limit is not None and emitted >= limit:
                break
            if not name.endswith(".json"):
                continue
            try:
                advisory = json.loads(archive.read(name).decode("utf-8"))
            except (KeyError, UnicodeDecodeError, json.JSONDecodeError):
                continue
            if isinstance(advisory, dict):
                emitted += 1
                yield advisory


def _extract_advisory_targets(
    advisory: dict[str, Any],
) -> Iterator[tuple[str, tuple[str, ...], set[str]]]:
    cve_ids = _cve_aliases(advisory)
    if not cve_ids:
        return

    text = "\n".join(
        value for key in ("summary", "details") if isinstance((value := advisory.get(key)), str)
    )
    affected = advisory.get("affected")
    if not isinstance(affected, list):
        return

    for entry in affected:
        if not isinstance(entry, dict):
            continue
        package_name = _affected_package_name(entry)
        if package_name is None:
            continue
        functions = _affected_functions(entry, package_name=package_name)
        if not functions:
            functions = _text_functions(text, package_name=package_name)
        if functions:
            yield package_name, cve_ids, functions


def _cve_aliases(advisory: dict[str, Any]) -> tuple[str, ...]:
    aliases: list[str] = []
    raw_id = advisory.get("id")
    if isinstance(raw_id, str) and raw_id.upper().startswith("CVE-"):
        aliases.append(raw_id.upper())
    raw_aliases = advisory.get("aliases")
    if isinstance(raw_aliases, list):
        aliases.extend(alias.upper() for alias in raw_aliases if isinstance(alias, str))
    return tuple(sorted({alias for alias in aliases if alias.startswith("CVE-")}))


def _affected_package_name(entry: dict[str, Any]) -> str | None:
    raw_package = entry.get("package")
    if not isinstance(raw_package, dict):
        return None
    ecosystem = raw_package.get("ecosystem")
    if ecosystem not in OSV_BULK_URLS:
        return None
    name = raw_package.get("name")
    if not isinstance(name, str) or not name.strip():
        return None
    return name.strip()


def _affected_functions(entry: dict[str, Any], *, package_name: str) -> set[str]:
    ecosystem_specific = entry.get("ecosystem_specific")
    if not isinstance(ecosystem_specific, dict):
        return set()
    raw_functions = ecosystem_specific.get("affected_functions")
    if not isinstance(raw_functions, list):
        return set()
    return _normalize_functions(
        (item for item in raw_functions if isinstance(item, str)),
        package_name=package_name,
    )


def _text_functions(text: str, *, package_name: str) -> set[str]:
    if not text:
        return set()
    candidates: list[str] = []
    candidates.extend(match.group("token") for match in _BACKTICK_TOKEN_RE.finditer(text))
    candidates.extend(match.group("token") for match in _FUNCTION_TOKEN_RE.finditer(text))
    package_member_re = re.compile(
        rf"\b{re.escape(package_name)}[./](?P<token>[A-Za-z_$][\w$-]*)\s*\(",
        re.IGNORECASE,
    )
    candidates.extend(
        f"{package_name}.{match.group('token')}" for match in package_member_re.finditer(text)
    )
    return _normalize_functions(candidates, package_name=package_name)


def _normalize_functions(candidates: Iterable[str], *, package_name: str) -> set[str]:
    functions: set[str] = set()
    for candidate in candidates:
        normalized = _normalize_target_candidate(candidate, package_name=package_name)
        if normalized:
            functions.add(normalized)
    return functions


def _load_existing_data(path: Path) -> dict[str, Any]:
    try:
        raw_data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return raw_data if isinstance(raw_data, dict) else {}


def _cve_entries(existing: dict[str, Any]) -> dict[str, dict[str, Any]]:
    raw_cves = existing.get("cves")
    if not isinstance(raw_cves, dict):
        return {}
    all_manual = existing.get("source") == "manual"
    cves: dict[str, dict[str, Any]] = {}
    for raw_cve, raw_entry in raw_cves.items():
        if not isinstance(raw_cve, str) or not isinstance(raw_entry, dict):
            continue
        source = "manual" if raw_entry.get("source") == "manual" or all_manual else "osv"
        package_name = raw_entry.get("package")
        raw_functions = raw_entry.get("functions")
        if not isinstance(package_name, str) or not isinstance(raw_functions, list):
            continue
        functions = [item for item in raw_functions if isinstance(item, str)]
        if functions:
            cves[raw_cve.upper()] = {
                "package": package_name,
                "functions": functions,
                "source": source,
            }
    return cves


def _package_entries(existing: dict[str, Any]) -> dict[str, list[str]]:
    raw_packages = existing.get("packages")
    if not isinstance(raw_packages, dict):
        return {}
    packages: dict[str, list[str]] = {}
    for raw_package, raw_functions in raw_packages.items():
        if not isinstance(raw_package, str) or not isinstance(raw_functions, list):
            continue
        functions = sorted({item for item in raw_functions if isinstance(item, str)})
        if functions:
            packages[raw_package] = functions
    return packages


def _merge_package_functions(
    packages: dict[str, list[str]],
    package_name: str,
    functions: set[str],
) -> None:
    current = set(packages.get(package_name, []))
    current.update(functions)
    packages[package_name] = sorted(current)


if __name__ == "__main__":
    raise SystemExit(main())
