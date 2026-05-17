from __future__ import annotations

import argparse
import json
import re
import sys
import time
from datetime import UTC, datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import requests

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "src") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "src"))

from eval.extract_fixture import extract_fixture


NVD_API_URL = "https://services.nvd.nist.gov/rest/json/cves/2.0"
_GITHUB_REPO = re.compile(r"https://github\.com/(?P<owner>[^/]+)/(?P<repo>[^/#?]+)")
_LANGUAGE_HINTS = {
    "typescript": ("typescript", "javascript", "node", "npm", "express", "nestjs", "next.js"),
    "javascript": ("javascript", "node", "npm", "express", "nestjs", "next.js"),
    "python": ("python", "django", "flask", "fastapi", "pypi", "pip"),
    "go": ("golang", "go ", " gin", " echo", "/go"),
    "java": ("java", "spring", "maven", "gradle"),
}


class NvdRateLimiter:
    def __init__(self, *, has_api_key: bool) -> None:
        self.window = 30.0
        self.max_requests = 50 if has_api_key else 5
        self.timestamps: list[float] = []

    def wait(self) -> None:
        now = time.monotonic()
        self.timestamps = [timestamp for timestamp in self.timestamps if now - timestamp < self.window]
        if len(self.timestamps) >= self.max_requests:
            sleep_time = self.window - (now - self.timestamps[0]) + 0.1
            time.sleep(max(sleep_time, 0.0))
        self.timestamps.append(time.monotonic())


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Query NVD and extract candidate CVE fixtures.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    query = subparsers.add_parser("query", help="Query NVD for CVE candidates.")
    query.add_argument("--cwe", required=True, help="CWE identifier, for example CWE-89.")
    query.add_argument(
        "--keywords",
        required=True,
        help="Comma-separated keywords such as sequelize,typeorm,prisma.",
    )
    query.add_argument("--language", required=True, help="Target language filter.")
    query.add_argument("--since", help="Published-on or after YYYY-MM-DD.")
    query.add_argument("--min-cvss", type=float, default=5.0, help="Minimum CVSS v3.1 score.")
    query.add_argument("--output", type=Path, required=True, help="Output JSON path.")
    query.add_argument("--api-key", help="Optional NVD API key.")
    query.add_argument("--results-per-page", type=int, default=100)
    query.add_argument("--max-results", type=int, help="Optional cap after filtering.")

    extract = subparsers.add_parser("extract", help="Extract a fixture stub for one CVE candidate.")
    extract.add_argument("--repo", required=True, help="GitHub repository URL or local git path.")
    extract.add_argument("--vulnerable-commit", required=True, help="Last vulnerable commit SHA.")
    extract.add_argument("--fix-commit", required=True, help="Fix commit SHA.")
    extract.add_argument("--affected-file", required=True, help="File to extract from the repository.")
    extract.add_argument("--cwe", required=True, help="CWE identifier, for example CWE-89.")
    extract.add_argument("--output", type=Path, required=True, help="Output stub path.")
    extract.add_argument("--cve", help="Optional CVE identifier.")
    extract.add_argument("--package", help="Optional package label.")
    extract.add_argument("--context-lines", type=int, default=8)

    batch = subparsers.add_parser("batch", help="Batch-extract fixtures from an enriched candidate list.")
    batch.add_argument("--input", type=Path, required=True, help="Candidate JSON file.")
    batch.add_argument("--output", type=Path, required=True, help="Output directory for fixture stubs.")
    batch.add_argument("--max", type=int, default=50, help="Maximum candidates to process.")
    batch.add_argument("--context-lines", type=int, default=8)
    return parser.parse_args(argv)


def _published_timestamp(value: str | None) -> str | None:
    if value is None:
        return None
    value = value.strip()
    if not value:
        return None
    if "T" in value:
        return value
    return f"{value}T00:00:00.000"


def _keyword_query(keywords: str) -> str:
    parts = [part.strip() for part in keywords.split(",") if part.strip()]
    return " ".join(parts)


def _description_text(cve: dict[str, Any]) -> str:
    descriptions = cve.get("descriptions")
    if not isinstance(descriptions, list):
        return ""
    for description in descriptions:
        if isinstance(description, dict) and description.get("lang") == "en":
            value = description.get("value")
            if isinstance(value, str):
                return value
    return ""


def _cwe_ids(cve: dict[str, Any]) -> list[str]:
    identifiers: list[str] = []
    weaknesses = cve.get("weaknesses")
    if not isinstance(weaknesses, list):
        return identifiers
    for weakness in weaknesses:
        descriptions = weakness.get("description")
        if not isinstance(descriptions, list):
            continue
        for description in descriptions:
            value = description.get("value") if isinstance(description, dict) else None
            if isinstance(value, str) and value.startswith("CWE-"):
                identifiers.append(value)
    return sorted(set(identifiers))


def _cvss_score(cve: dict[str, Any]) -> float | None:
    metrics = cve.get("metrics")
    if not isinstance(metrics, dict):
        return None
    for key in ("cvssMetricV31", "cvssMetricV30", "cvssMetricV2"):
        values = metrics.get(key)
        if not isinstance(values, list):
            continue
        for item in values:
            data = item.get("cvssData") if isinstance(item, dict) else None
            score = data.get("baseScore") if isinstance(data, dict) else None
            if isinstance(score, int | float):
                return float(score)
    return None


def _references(cve: dict[str, Any]) -> list[dict[str, str]]:
    refs = cve.get("references")
    if not isinstance(refs, list):
        return []
    extracted: list[dict[str, str]] = []
    for ref in refs:
        if not isinstance(ref, dict):
            continue
        url = ref.get("url")
        if not isinstance(url, str):
            continue
        tags = ref.get("tags")
        ref_type = "reference"
        if isinstance(tags, list) and tags:
            ref_type = str(tags[0]).lower()
        elif "advisories" in url:
            ref_type = "advisory"
        elif "/pull/" in url or "/commit/" in url:
            ref_type = "patch"
        extracted.append({"url": url, "type": ref_type})
    return extracted


def _github_repo_url(refs: list[dict[str, str]]) -> str | None:
    for ref in refs:
        match = _GITHUB_REPO.match(ref["url"])
        if match is not None:
            return f"https://github.com/{match.group('owner')}/{match.group('repo')}"
    return None


def _affected_package(repo_url: str | None, description: str, keywords: str) -> str | None:
    keyword_parts = [part.strip() for part in keywords.split(",") if part.strip()]
    normalized_description = description.casefold()
    for keyword in keyword_parts:
        if keyword.casefold() in normalized_description:
            return keyword
    if repo_url is not None:
        parsed = urlparse(repo_url)
        repo_name = parsed.path.rstrip("/").split("/")[-1]
        return repo_name.replace(".git", "")
    return keyword_parts[0] if keyword_parts else None


def _ecosystem(language: str) -> str:
    mapping = {
        "typescript": "npm",
        "javascript": "npm",
        "python": "pypi",
        "go": "go",
        "java": "maven",
    }
    return mapping.get(language.casefold(), "unknown")


def _matches_language(cve: dict[str, Any], refs: list[dict[str, str]], *, language: str) -> bool:
    if not language:
        return True
    tokens = _LANGUAGE_HINTS.get(language.casefold(), ())
    haystack_parts = [_description_text(cve), " ".join(ref["url"] for ref in refs)]

    configurations = cve.get("configurations")
    if isinstance(configurations, list):
        for config in configurations:
            nodes = config.get("nodes") if isinstance(config, dict) else None
            if not isinstance(nodes, list):
                continue
            for node in nodes:
                matches = node.get("cpeMatch") if isinstance(node, dict) else None
                if not isinstance(matches, list):
                    continue
                for match in matches:
                    criteria = match.get("criteria") if isinstance(match, dict) else None
                    if isinstance(criteria, str):
                        haystack_parts.append(criteria)

    haystack = " ".join(part.casefold() for part in haystack_parts)
    return any(token.casefold() in haystack for token in tokens)


def _candidate_from_vulnerability(
    vulnerability: dict[str, Any],
    *,
    language: str,
    keywords: str,
) -> dict[str, Any] | None:
    cve = vulnerability.get("cve")
    if not isinstance(cve, dict):
        return None
    refs = _references(cve)
    github_refs = [ref for ref in refs if "github.com" in ref["url"]]
    if not github_refs:
        return None
    if not _matches_language(cve, refs, language=language):
        return None

    description = _description_text(cve)
    repo_url = _github_repo_url(github_refs)
    cve_id = cve.get("id")
    if not isinstance(cve_id, str):
        return None

    return {
        "cve_id": cve_id,
        "cwe_ids": _cwe_ids(cve),
        "cvss_v31_score": _cvss_score(cve),
        "description": description,
        "published": vulnerability.get("published"),
        "references": github_refs,
        "repo_url": repo_url,
        "affected_package": _affected_package(repo_url, description, keywords),
        "ecosystem": _ecosystem(language),
        "status": "candidate",
    }


def query_nvd(
    *,
    cwe_id: str,
    keywords: str,
    language: str,
    since: str | None,
    min_cvss: float,
    api_key: str | None,
    results_per_page: int,
    max_results: int | None,
) -> dict[str, Any]:
    limiter = NvdRateLimiter(has_api_key=bool(api_key))
    headers = {"apiKey": api_key} if api_key else {}
    session = requests.Session()
    all_candidates: list[dict[str, Any]] = []
    total_results = 0
    start_index = 0

    while True:
        limiter.wait()
        params = {
            "cweId": cwe_id,
            "keywordSearch": _keyword_query(keywords),
            "resultsPerPage": results_per_page,
            "startIndex": start_index,
        }
        published = _published_timestamp(since)
        if published is not None:
            params["pubStartDate"] = published
        response = session.get(NVD_API_URL, params=params, headers=headers, timeout=30)
        response.raise_for_status()
        payload = response.json()
        total_results = int(payload.get("totalResults", 0))
        vulnerabilities = payload.get("vulnerabilities")
        if not isinstance(vulnerabilities, list) or not vulnerabilities:
            break

        for vulnerability in vulnerabilities:
            candidate = _candidate_from_vulnerability(
                vulnerability,
                language=language,
                keywords=keywords,
            )
            if candidate is None:
                continue
            score = candidate.get("cvss_v31_score")
            if isinstance(score, int | float) and score < min_cvss:
                continue
            all_candidates.append(candidate)
            if max_results is not None and len(all_candidates) >= max_results:
                break
        if max_results is not None and len(all_candidates) >= max_results:
            break
        start_index += len(vulnerabilities)
        if start_index >= total_results:
            break

    return {
        "query": {
            "cwe": cwe_id,
            "keywords": keywords,
            "language": language,
            "since": since,
            "min_cvss": min_cvss,
        },
        "queried_at": datetime.now(UTC).isoformat(),
        "total_results": total_results,
        "candidates": all_candidates,
    }


def run_query_command(args: argparse.Namespace) -> int:
    payload = query_nvd(
        cwe_id=args.cwe,
        keywords=args.keywords,
        language=args.language,
        since=args.since,
        min_cvss=args.min_cvss,
        api_key=args.api_key,
        results_per_page=args.results_per_page,
        max_results=args.max_results,
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(args.output)
    return 0


def run_extract_command(args: argparse.Namespace) -> int:
    extract_fixture(
        repo=args.repo,
        vulnerable_commit=args.vulnerable_commit,
        fix_commit=args.fix_commit,
        affected_file=args.affected_file,
        cwe_id=args.cwe,
        output_path=args.output,
        cve_id=args.cve,
        package=args.package,
        context_lines=args.context_lines,
    )
    print(args.output)
    return 0


def run_batch_command(args: argparse.Namespace) -> int:
    payload = json.loads(args.input.read_text(encoding="utf-8"))
    candidates = payload.get("candidates")
    if not isinstance(candidates, list):
        raise ValueError("candidate file must contain a top-level candidates list")

    extracted: list[dict[str, Any]] = []
    skipped: list[dict[str, Any]] = []
    for candidate in candidates[: args.max]:
        if not isinstance(candidate, dict):
            continue
        required = ("repo_url", "vulnerable_commit", "fix_commit", "affected_file", "cwe_ids", "cve_id")
        if any(candidate.get(field) in (None, "", []) for field in required):
            skipped.append(
                {
                    "cve_id": candidate.get("cve_id"),
                    "reason": "missing repo_url, vulnerable_commit, fix_commit, affected_file, or cwe_ids",
                }
            )
            continue

        cwe_ids = candidate.get("cwe_ids")
        if not isinstance(cwe_ids, list) or not cwe_ids:
            skipped.append({"cve_id": candidate.get("cve_id"), "reason": "missing cwe_ids"})
            continue

        output_name = f"{candidate['cwe_ids'][0].lower()}-{candidate['cve_id']}.fixture{Path(candidate['affected_file']).suffix or '.txt'}"
        output_path = args.output / output_name
        extract_fixture(
            repo=str(candidate["repo_url"]),
            vulnerable_commit=str(candidate["vulnerable_commit"]),
            fix_commit=str(candidate["fix_commit"]),
            affected_file=str(candidate["affected_file"]),
            cwe_id=str(candidate["cwe_ids"][0]),
            output_path=output_path,
            cve_id=str(candidate["cve_id"]),
            package=str(candidate.get("affected_package") or ""),
            context_lines=args.context_lines,
        )
        extracted.append({"cve_id": candidate["cve_id"], "output": str(output_path)})

    report = {
        "processed_at": datetime.now(UTC).isoformat(),
        "input": str(args.input),
        "extracted": extracted,
        "skipped": skipped,
    }
    report_path = args.output / "batch-report.json"
    args.output.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print(report_path)
    return 0


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.command == "query":
        return run_query_command(args)
    if args.command == "extract":
        return run_extract_command(args)
    if args.command == "batch":
        return run_batch_command(args)
    raise ValueError(f"unsupported command: {args.command}")


if __name__ == "__main__":
    raise SystemExit(main())
