from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path


_HUNK_HEADER = re.compile(r"^@@ -(?P<start>\d+)(?:,(?P<count>\d+))? \+\d+(?:,\d+)? @@")
_COMMENT_PREFIXES = {
    ".py": "#",
    ".rb": "#",
    ".sh": "#",
    ".ts": "//",
    ".tsx": "//",
    ".js": "//",
    ".jsx": "//",
    ".go": "//",
    ".java": "//",
}


@dataclass(frozen=True, slots=True)
class FixtureExtractionResult:
    repo_dir: Path
    vulnerable_source: str
    patch_diff: str
    fixture_text: str


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Clone a repository, extract vulnerable code for a commit range, and write a fixture stub."
    )
    parser.add_argument("--repo", required=True, help="GitHub repository URL or local git path.")
    parser.add_argument("--vulnerable-commit", required=True, help="Last vulnerable commit SHA.")
    parser.add_argument("--fix-commit", required=True, help="Fix commit SHA.")
    parser.add_argument("--affected-file", required=True, help="File path to extract from the repository.")
    parser.add_argument("--cwe", required=True, help="CWE identifier, for example CWE-89.")
    parser.add_argument("--output", type=Path, required=True, help="Output stub file path.")
    parser.add_argument("--cve", help="Optional CVE identifier to include in the stub header.")
    parser.add_argument("--package", help="Optional package or project label for the stub header.")
    parser.add_argument(
        "--context-lines",
        type=int,
        default=8,
        help="Context lines to keep around changed hunks in the vulnerable source.",
    )
    return parser.parse_args(argv)


def _git(command: list[str], *, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *command],
        cwd=None if cwd is None else str(cwd),
        text=True,
        capture_output=True,
        check=False,
    )


def _run_git(command: list[str], *, cwd: Path | None = None) -> str:
    completed = _git(command, cwd=cwd)
    if completed.returncode != 0:
        message = completed.stderr.strip() or completed.stdout.strip()
        raise RuntimeError(f"git {' '.join(command)} failed: {message}")
    return completed.stdout


def clone_repository(repo: str, *, scratch_dir: Path) -> Path:
    destination = scratch_dir / "repo"
    cloned = _git(["clone", "--filter=blob:none", repo, str(destination)])
    if cloned.returncode == 0:
        return destination
    fallback = _git(["clone", repo, str(destination)])
    if fallback.returncode != 0:
        message = fallback.stderr.strip() or cloned.stderr.strip() or fallback.stdout.strip()
        raise RuntimeError(f"git clone failed: {message}")
    return destination


def ensure_commit(repo_dir: Path, sha: str) -> None:
    existing = _git(["cat-file", "-e", f"{sha}^{{commit}}"], cwd=repo_dir)
    if existing.returncode == 0:
        return
    fetch = _git(["fetch", "--depth=1", "origin", sha], cwd=repo_dir)
    if fetch.returncode != 0:
        message = fetch.stderr.strip() or fetch.stdout.strip()
        raise RuntimeError(f"unable to fetch commit {sha}: {message}")


def extract_changed_ranges(diff_text: str) -> list[tuple[int, int]]:
    ranges: list[tuple[int, int]] = []
    for line in diff_text.splitlines():
        match = _HUNK_HEADER.match(line)
        if match is None:
            continue
        start = int(match.group("start"))
        count = int(match.group("count") or "1")
        end = start + max(count, 1) - 1
        ranges.append((start, end))
    return ranges


def extract_context(
    source_text: str,
    ranges: list[tuple[int, int]],
    *,
    context_lines: int,
    gap_marker: str,
) -> str:
    lines = source_text.splitlines()
    if not lines:
        return ""
    if not ranges:
        return "\n".join(lines[: min(len(lines), 80)])

    windows: list[tuple[int, int]] = []
    for start, end in ranges:
        windows.append((max(1, start - context_lines), min(len(lines), end + context_lines)))
    windows.sort()

    merged: list[tuple[int, int]] = []
    for start, end in windows:
        if not merged or start > merged[-1][1] + 1:
            merged.append((start, end))
        else:
            merged[-1] = (merged[-1][0], max(merged[-1][1], end))

    excerpt: list[str] = []
    for index, (start, end) in enumerate(merged):
        if index:
            excerpt.append("")
            excerpt.append(gap_marker)
            excerpt.append("")
        excerpt.extend(lines[start - 1 : end])
    return "\n".join(excerpt)


def comment_prefix_for(path: Path, *, fallback_suffix: str | None = None) -> str:
    suffix = path.suffix.lower() or (fallback_suffix or "").lower()
    return _COMMENT_PREFIXES.get(suffix, "//")


def render_fixture_stub(
    *,
    output_path: Path,
    affected_file: str,
    cwe_id: str,
    cve_id: str | None,
    package: str,
    vulnerable_commit: str,
    fix_commit: str,
    vulnerable_excerpt: str,
    patch_diff: str,
) -> str:
    prefix = comment_prefix_for(output_path, fallback_suffix=Path(affected_file).suffix)
    header_lines = [
        f"{prefix} AUTO-GENERATED FIXTURE STUB -- requires manual reduction",
        f"{prefix} CVE: {cve_id or 'unknown'} | CWE: {cwe_id} | Package: {package}",
        f"{prefix} Vulnerable commit: {vulnerable_commit} | Fix commit: {fix_commit}",
        f"{prefix} Source file: {affected_file}",
        f"{prefix}",
        f"{prefix} TODO: reduce to minimal taint flow (target < 100 lines)",
        f"{prefix} TODO: add @piranesi-expect annotation",
        f"{prefix} TODO: create corresponding safe/fixed version",
        "",
        f"{prefix} --- VULNERABLE CODE (from commit {vulnerable_commit}) ---",
        vulnerable_excerpt.rstrip(),
        "",
        f"{prefix} --- FIX DIFF ---",
    ]
    diff_lines = [f"{prefix} {line}" if line else prefix for line in patch_diff.rstrip().splitlines()]
    return "\n".join([*header_lines, *diff_lines, ""])


def extract_fixture(
    *,
    repo: str,
    vulnerable_commit: str,
    fix_commit: str,
    affected_file: str,
    cwe_id: str,
    output_path: Path,
    cve_id: str | None = None,
    package: str | None = None,
    context_lines: int = 8,
) -> FixtureExtractionResult:
    with tempfile.TemporaryDirectory(prefix="extract-fixture-") as scratch:
        scratch_dir = Path(scratch)
        repo_dir = clone_repository(repo, scratch_dir=scratch_dir)
        ensure_commit(repo_dir, vulnerable_commit)
        ensure_commit(repo_dir, fix_commit)

        vulnerable_source = _run_git(["show", f"{vulnerable_commit}:{affected_file}"], cwd=repo_dir)
        patch_diff = _run_git(
            ["diff", f"--unified={context_lines}", vulnerable_commit, fix_commit, "--", affected_file],
            cwd=repo_dir,
        )
        changed_ranges = extract_changed_ranges(patch_diff)
        package_name = package or Path(repo.rstrip("/")).stem.replace(".git", "")
        prefix = comment_prefix_for(output_path, fallback_suffix=Path(affected_file).suffix)
        vulnerable_excerpt = extract_context(
            vulnerable_source,
            changed_ranges,
            context_lines=context_lines,
            gap_marker=f"{prefix} ...",
        )
        fixture_text = render_fixture_stub(
            output_path=output_path,
            affected_file=affected_file,
            cwe_id=cwe_id,
            cve_id=cve_id,
            package=package_name,
            vulnerable_commit=vulnerable_commit,
            fix_commit=fix_commit,
            vulnerable_excerpt=vulnerable_excerpt,
            patch_diff=patch_diff,
        )

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(fixture_text, encoding="utf-8")
    return FixtureExtractionResult(
        repo_dir=Path(repo),
        vulnerable_source=vulnerable_source,
        patch_diff=patch_diff,
        fixture_text=fixture_text,
    )


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
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


if __name__ == "__main__":
    raise SystemExit(main())
