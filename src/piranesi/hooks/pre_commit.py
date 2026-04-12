from __future__ import annotations

from pathlib import Path

from piranesi.config import PiranesiConfig
from piranesi.observability import run_subprocess

_HOOK_MARKER = "# piranesi pre-commit hook"
_HOOK_FILENAME = "pre-commit"


class HookError(RuntimeError):
    """Raised when pre-commit hook operations fail."""


def render_pre_commit_hook_script(*, fail_severity: str, hook_timeout: int) -> str:
    return "\n".join(
        [
            "#!/bin/sh",
            _HOOK_MARKER,
            (
                "exec piranesi run . --incremental --staged-only "
                f'--fail-severity {fail_severity} --hook-timeout {hook_timeout} "$@"'
            ),
            "",
        ]
    )


def render_pre_commit_framework_manifest(*, fail_severity: str) -> str:
    return "\n".join(
        [
            "- id: piranesi",
            "  name: Piranesi Security Scan",
            (
                "  entry: piranesi run . --incremental --staged-only "
                f"--fail-severity {fail_severity}"
            ),
            "  language: python",
            "  pass_filenames: false",
            "  types_or: [typescript, javascript, python, go, java]",
            "  require_serial: true",
            "",
        ]
    )


def discover_staged_files(
    target_dir: Path,
    config: PiranesiConfig,
) -> list[Path]:
    repo_root = git_repo_root(target_dir)
    result = run_subprocess(
        ["git", "diff", "--cached", "--name-only", "--diff-filter=ACMR"],
        cwd=repo_root,
        timeout=10,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "git diff --cached failed"
        raise HookError(stderr)

    candidates: list[Path] = []
    for raw_path in result.stdout.splitlines():
        cleaned = raw_path.strip()
        if not cleaned:
            continue
        candidates.append((repo_root / cleaned).resolve(strict=False))

    from piranesi.pipeline import discover_scan_targets

    return discover_scan_targets(target_dir, config, candidate_paths=candidates)


def install_pre_commit_hook(
    start_dir: Path,
    *,
    fail_severity: str,
    hook_timeout: int,
) -> Path:
    hook_path = pre_commit_hook_path(start_dir)
    hook_path.parent.mkdir(parents=True, exist_ok=True)
    existing = hook_path.read_text(encoding="utf-8") if hook_path.exists() else None
    if existing is not None and _HOOK_MARKER not in existing:
        raise HookError(f"refusing to overwrite unmanaged hook at {hook_path}")

    hook_path.write_text(
        render_pre_commit_hook_script(
            fail_severity=fail_severity,
            hook_timeout=hook_timeout,
        ),
        encoding="utf-8",
    )
    current_mode = hook_path.stat().st_mode
    hook_path.chmod(current_mode | 0o111)
    return hook_path


def uninstall_pre_commit_hook(start_dir: Path) -> bool:
    hook_path = pre_commit_hook_path(start_dir)
    if not hook_path.exists():
        return False

    if not _is_managed_hook(hook_path):
        raise HookError(f"refusing to remove unmanaged hook at {hook_path}")

    hook_path.unlink()
    return True


def pre_commit_hook_status(start_dir: Path) -> tuple[bool, Path]:
    hook_path = pre_commit_hook_path(start_dir)
    return _is_managed_hook(hook_path), hook_path


def pre_commit_hook_path(start_dir: Path) -> Path:
    repo_root = git_repo_root(start_dir)
    result = run_subprocess(
        ["git", "rev-parse", "--git-path", "hooks"],
        cwd=repo_root,
        timeout=10,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "unable to resolve git hooks directory"
        raise HookError(stderr)

    raw_path = Path(result.stdout.strip())
    if raw_path.is_absolute():
        hooks_dir = raw_path.resolve(strict=False)
    else:
        hooks_dir = (repo_root / raw_path).resolve(strict=False)
    return hooks_dir / _HOOK_FILENAME


def git_repo_root(start_dir: Path) -> Path:
    cwd = start_dir.resolve(strict=False)
    result = run_subprocess(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=cwd,
        timeout=10,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "not inside a git repository"
        raise HookError(stderr)
    return Path(result.stdout.strip()).resolve(strict=False)


def _is_managed_hook(path: Path) -> bool:
    if not path.exists():
        return False
    try:
        return _HOOK_MARKER in path.read_text(encoding="utf-8")
    except OSError as exc:
        raise HookError(f"failed to read hook at {path}: {exc}") from exc


__all__ = [
    "HookError",
    "discover_staged_files",
    "install_pre_commit_hook",
    "pre_commit_hook_path",
    "pre_commit_hook_status",
    "render_pre_commit_framework_manifest",
    "render_pre_commit_hook_script",
    "uninstall_pre_commit_hook",
]
