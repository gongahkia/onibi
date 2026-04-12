from __future__ import annotations

import json
import logging
import os
import re
import shutil
import tempfile
from bisect import bisect_right
from collections import defaultdict
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path
from subprocess import CompletedProcess

from piranesi.observability import log_error_context, run_subprocess

logger = logging.getLogger("piranesi.scan.transpile")

_SOURCE_EXTENSIONS = {".js", ".jsx", ".ts", ".tsx"}
_IGNORED_TARGET_FILES = (".npmrc", ".node-version", ".nvmrc", ".tool-versions")
_BASE64_VLQ_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
_BASE64_VLQ_MAP = {char: index for index, char in enumerate(_BASE64_VLQ_ALPHABET)}
_FAILED_FILE_PATTERNS = (
    re.compile(r"^(?P<file>.+?)\(\d+,\d+\): error TS\d+:", re.MULTILINE),
    re.compile(r"^error TS\d+: File '(?P<file>.+?)'", re.MULTILINE),
    re.compile(r"^error TS\d+: Cannot read file '(?P<file>.+?)'", re.MULTILINE),
)


class TranspilationError(RuntimeError):
    """Raised when Piranesi cannot produce a usable JS transpilation."""


class TypeScriptCompilerNotFoundError(TranspilationError):
    """Raised when neither `tsc` nor `npx tsc` is available."""


@dataclass(frozen=True)
class TranspileWorkspace:
    root_dir: Path
    out_dir: Path
    tsconfig_path: Path
    npm_cache_dir: Path

    def cleanup(self) -> None:
        shutil.rmtree(self.root_dir, ignore_errors=True)


@dataclass
class TranspiledProject:
    target_dir: Path
    workspace: TranspileWorkspace
    source_map: SourceMap
    failed_files: tuple[Path, ...]
    compiler_cmd: tuple[str, ...]
    initial_result: CompletedProcess[str]
    retry_result: CompletedProcess[str] | None = None

    @property
    def out_dir(self) -> Path:
        return self.workspace.out_dir

    @property
    def tsconfig_path(self) -> Path:
        return self.workspace.tsconfig_path

    def cleanup(self) -> None:
        self.workspace.cleanup()


@dataclass(frozen=True)
class SourceMap:
    _generated_to_original: dict[tuple[Path, int], tuple[Path, int]]
    _original_to_generated: dict[tuple[Path, int], tuple[tuple[Path, int], ...]]
    _generated_lines: dict[Path, tuple[int, ...]]

    @classmethod
    def from_directory(cls, transpiled_root: Path) -> SourceMap:
        map_files = sorted(transpiled_root.rglob("*.map"))
        return cls.from_map_files(map_files)

    @classmethod
    def from_map_files(cls, map_files: Iterable[Path]) -> SourceMap:
        generated_to_original: dict[tuple[Path, int], tuple[Path, int]] = {}
        original_to_generated: defaultdict[tuple[Path, int], set[tuple[Path, int]]] = defaultdict(
            set
        )
        generated_lines: defaultdict[Path, set[int]] = defaultdict(set)

        for map_path in map_files:
            for generated_location, original_location in _parse_source_map_file(map_path):
                generated_to_original.setdefault(generated_location, original_location)
                original_to_generated[original_location].add(generated_location)
                generated_lines[generated_location[0]].add(generated_location[1])

        normalized_generated_lines = {
            js_file: tuple(sorted(lines)) for js_file, lines in generated_lines.items()
        }
        normalized_original_to_generated = {
            location: tuple(
                sorted(generated_locations, key=lambda value: (str(value[0]), value[1]))
            )
            for location, generated_locations in original_to_generated.items()
        }

        return cls(
            _generated_to_original=generated_to_original,
            _original_to_generated=normalized_original_to_generated,
            _generated_lines=normalized_generated_lines,
        )

    def resolve(self, js_file: str | Path, js_line: int) -> tuple[Path, int]:
        if js_line < 1:
            raise ValueError("js_line must be >= 1")

        normalized_file = Path(js_file).resolve(strict=False)
        direct = self._generated_to_original.get((normalized_file, js_line))
        if direct is not None:
            return direct

        candidate_lines = self._generated_lines.get(normalized_file)
        if candidate_lines is None or not candidate_lines:
            raise KeyError(f"no source map entries for {normalized_file}")

        index = bisect_right(candidate_lines, js_line)
        if index > 0:
            return self._generated_to_original[(normalized_file, candidate_lines[index - 1])]
        return self._generated_to_original[(normalized_file, candidate_lines[0])]

    def reverse_resolve(self, ts_file: str | Path, ts_line: int) -> tuple[tuple[Path, int], ...]:
        if ts_line < 1:
            raise ValueError("ts_line must be >= 1")
        normalized_file = Path(ts_file).resolve(strict=False)
        return self._original_to_generated.get((normalized_file, ts_line), ())

    def original_files(self) -> tuple[Path, ...]:
        return tuple(
            sorted(
                {path for path, _line in self._original_to_generated},
                key=str,
            )
        )


def prepare_transpile_workspace(
    target_dir: Path,
    *,
    changed_files: set[Path] | None = None,
    root_dir: Path | None = None,
    log: logging.Logger | None = None,
) -> TranspileWorkspace:
    active_logger = log or logger
    normalized_target = target_dir.resolve(strict=False)
    if not normalized_target.is_dir():
        raise ValueError(f"target_dir must be an existing directory: {normalized_target}")

    workspace_root = (
        root_dir.resolve(strict=False)
        if root_dir is not None
        else Path(tempfile.mkdtemp(prefix="piranesi-tsconfig-")).resolve(strict=False)
    )
    workspace_root.mkdir(parents=True, exist_ok=True)

    out_dir = workspace_root / "out"
    out_dir.mkdir(parents=True, exist_ok=True)

    npm_cache_dir = workspace_root / ".npm-cache"
    npm_cache_dir.mkdir(parents=True, exist_ok=True)

    selected_files = (
        None
        if changed_files is None
        else _normalize_changed_files(normalized_target, changed_files)
    )
    tsconfig = {
        "compilerOptions": {
            "target": "ES2020",
            "module": "commonjs",
            "rootDir": str(normalized_target),
            "outDir": str(out_dir),
            "declaration": False,
            "sourceMap": True,
            "allowJs": True,
            "esModuleInterop": True,
            "experimentalDecorators": True,
            "emitDecoratorMetadata": True,
            "resolveJsonModule": True,
            "strict": False,
            "skipLibCheck": True,
            "noEmit": False,
        },
        "exclude": [str(normalized_target / "node_modules" / "**")],
    }
    if selected_files is None:
        tsconfig["include"] = [
            str(normalized_target / "**" / "*.ts"),
            str(normalized_target / "**" / "*.tsx"),
            str(normalized_target / "**" / "*.js"),
            str(normalized_target / "**" / "*.jsx"),
        ]
    else:
        tsconfig["files"] = [str(path) for path in selected_files]

    tsconfig_path = workspace_root / "tsconfig.json"
    tsconfig_path.write_text(json.dumps(tsconfig, indent=2), encoding="utf-8")

    ignored_present = [
        entry
        for entry in ("tsconfig.json", *_IGNORED_TARGET_FILES)
        if (normalized_target / entry).exists()
    ]
    active_logger.debug(
        "prepared isolated transpile workspace %s",
        workspace_root,
        extra={
            "event": "transpile_workspace_prepared",
            "target_dir": str(normalized_target),
            "workspace": str(workspace_root),
            "out_dir": str(out_dir),
            "tsconfig": str(tsconfig_path),
            "ignored_target_files": ignored_present,
        },
    )

    return TranspileWorkspace(
        root_dir=workspace_root,
        out_dir=out_dir,
        tsconfig_path=tsconfig_path,
        npm_cache_dir=npm_cache_dir,
    )


def transpile_project(
    target_dir: Path,
    *,
    changed_files: set[Path] | None = None,
    timeout: int = 300,
    log: logging.Logger | None = None,
) -> TranspiledProject:
    active_logger = log or logger
    normalized_target = target_dir.resolve(strict=False)
    source_files = tuple(
        collect_transpilable_files(normalized_target)
        if changed_files is None
        else _normalize_changed_files(normalized_target, changed_files)
    )
    if not source_files:
        raise TranspilationError(
            f"no changed TypeScript or JavaScript files found under {normalized_target}"
            if changed_files is not None
            else f"no TypeScript or JavaScript files found under {normalized_target}"
        )

    workspace = prepare_transpile_workspace(
        normalized_target,
        changed_files=None if changed_files is None else set(source_files),
        log=active_logger,
    )
    compiler_env = _build_compiler_env(workspace)

    try:
        compiler_cmd, initial_result = _run_initial_compiler(
            workspace=workspace,
            env=compiler_env,
            timeout=timeout,
            log=active_logger,
        )

        retry_result: CompletedProcess[str] | None = None
        failed_files: tuple[Path, ...] = ()
        if initial_result.returncode != 0:
            failed_files = _extract_failed_files(
                output=_combine_output(initial_result),
                cwd=workspace.root_dir,
            )
            _log_failed_files(active_logger, failed_files)
            active_logger.warning(
                "TypeScript transpilation reported errors; retrying with forced emit flags",
                extra={
                    "event": "transpile_retry",
                    "cmd": " ".join(compiler_cmd),
                    "tsconfig": str(workspace.tsconfig_path),
                },
            )
            retry_cmd = [*compiler_cmd, "--skipLibCheck", "--noEmit", "false"]
            retry_result = run_subprocess(
                retry_cmd,
                cwd=workspace.root_dir,
                timeout=timeout,
                env=compiler_env,
                logger=active_logger,
            )
            failed_files = _merge_failed_files(
                failed_files,
                _extract_failed_files(
                    output=_combine_output(retry_result),
                    cwd=workspace.root_dir,
                ),
            )
            _log_failed_files(active_logger, failed_files)

        if not any(workspace.out_dir.rglob("*.js")):
            failed_result = retry_result if retry_result is not None else initial_result
            log_error_context(
                active_logger,
                event="transpile_failed",
                what="typescript_transpile",
                on_what=str(normalized_target),
                why=f"compiler exited {failed_result.returncode} and emitted no JavaScript",
                next_step="raising transpilation error",
                debug=(
                    f"tsconfig={workspace.tsconfig_path}; cmd={' '.join(compiler_cmd)}; "
                    f"stdout={failed_result.stdout!r}; stderr={failed_result.stderr!r}"
                ),
            )
            raise TranspilationError(
                "TypeScript transpilation failed and emitted no JavaScript. "
                "See logs for compiler diagnostics."
            )

        source_map = SourceMap.from_directory(workspace.out_dir)
        if not source_map._generated_to_original:
            log_error_context(
                active_logger,
                event="source_map_missing",
                what="source_map_build",
                on_what=str(workspace.out_dir),
                why="no .map files were produced by TypeScript",
                next_step="raising transpilation error",
                debug=f"out_dir={workspace.out_dir}",
            )
            raise TranspilationError(
                "TypeScript transpilation completed without usable source maps."
            )

        _warn_if_failure_ratio_exceeds_threshold(
            failed_files=failed_files,
            source_files=source_files,
            log=active_logger,
        )
        active_logger.info(
            "transpiled %d source files to %s",
            len(source_files),
            workspace.out_dir,
            extra={
                "event": "transpile_complete",
                "target_dir": str(normalized_target),
                "out_dir": str(workspace.out_dir),
                "failed_file_count": len(failed_files),
            },
        )

        return TranspiledProject(
            target_dir=normalized_target,
            workspace=workspace,
            source_map=source_map,
            failed_files=failed_files,
            compiler_cmd=tuple(compiler_cmd),
            initial_result=initial_result,
            retry_result=retry_result,
        )
    except Exception:
        workspace.cleanup()
        raise


def _run_initial_compiler(
    *,
    workspace: TranspileWorkspace,
    env: dict[str, str],
    timeout: int,
    log: logging.Logger,
) -> tuple[list[str], CompletedProcess[str]]:
    compiler_cmd = ["tsc", "--project", str(workspace.tsconfig_path)]
    try:
        result = run_subprocess(
            compiler_cmd,
            cwd=workspace.root_dir,
            timeout=timeout,
            env=env,
            logger=log,
        )
        return compiler_cmd, result
    except FileNotFoundError:
        log.warning(
            "TypeScript compiler not found on PATH; trying npx tsc",
            extra={
                "event": "transpile_tsc_missing",
                "cmd": " ".join(compiler_cmd),
            },
        )

    npx_cmd = ["npx", "tsc", "--project", str(workspace.tsconfig_path)]
    try:
        npx_result = run_subprocess(
            npx_cmd,
            cwd=workspace.root_dir,
            timeout=timeout,
            env=env,
            logger=log,
        )
    except FileNotFoundError as exc:
        raise _compiler_not_found_error() from exc

    if npx_result.returncode != 0:
        raise _compiler_not_found_error()
    return npx_cmd, npx_result


def _build_compiler_env(workspace: TranspileWorkspace) -> dict[str, str]:
    env = dict(os.environ)
    env["CI"] = "1"
    env["NPM_CONFIG_USERCONFIG"] = os.devnull
    env["npm_config_userconfig"] = os.devnull
    env["NPM_CONFIG_GLOBALCONFIG"] = os.devnull
    env["npm_config_globalconfig"] = os.devnull
    env["NPM_CONFIG_CACHE"] = str(workspace.npm_cache_dir)
    env["npm_config_cache"] = str(workspace.npm_cache_dir)
    return env


def _compiler_not_found_error() -> TypeScriptCompilerNotFoundError:
    return TypeScriptCompilerNotFoundError(
        "TypeScript compiler is required. Tried `tsc` and `npx tsc`, "
        "but neither produced a usable compiler. Install TypeScript with "
        "`npm install --save-dev typescript` in a trusted environment "
        "or `npm install -g typescript`, then rerun Piranesi."
    )


def collect_transpilable_files(target_dir: Path) -> list[Path]:
    source_files: list[Path] = []
    for path in target_dir.rglob("*"):
        if not path.is_file():
            continue
        if "node_modules" in path.parts:
            continue
        if path.suffix not in _SOURCE_EXTENSIONS:
            continue
        if path.name.endswith(".d.ts"):
            continue
        source_files.append(path.resolve(strict=False))
    return sorted(source_files)


def _normalize_changed_files(target_dir: Path, changed_files: set[Path]) -> list[Path]:
    normalized: set[Path] = set()
    for path in changed_files:
        candidate = path if path.is_absolute() else target_dir / path
        resolved = candidate.resolve(strict=False)
        if not resolved.is_file():
            continue
        if "node_modules" in resolved.parts:
            continue
        if resolved.suffix not in _SOURCE_EXTENSIONS:
            continue
        if resolved.name.endswith(".d.ts"):
            continue
        normalized.add(resolved)
    return sorted(normalized)


def _combine_output(result: CompletedProcess[str]) -> str:
    parts = [result.stdout, result.stderr]
    return "\n".join(part for part in parts if part)


def _extract_failed_files(*, output: str, cwd: Path) -> tuple[Path, ...]:
    failures: set[Path] = set()
    for pattern in _FAILED_FILE_PATTERNS:
        for match in pattern.finditer(output):
            raw_path = match.group("file").strip().strip("\"'")
            candidate = Path(raw_path)
            if not candidate.is_absolute():
                candidate = (cwd / candidate).resolve(strict=False)
            else:
                candidate = candidate.resolve(strict=False)
            if candidate.suffix in _SOURCE_EXTENSIONS:
                failures.add(candidate)
    return tuple(sorted(failures))


def _merge_failed_files(
    primary: tuple[Path, ...],
    secondary: tuple[Path, ...],
) -> tuple[Path, ...]:
    return tuple(sorted({*primary, *secondary}))


def _log_failed_files(log: logging.Logger, failed_files: tuple[Path, ...]) -> None:
    if not failed_files:
        return
    rendered_files = ", ".join(str(path) for path in failed_files)
    log.warning(
        "TypeScript transpilation reported %d failed files: %s",
        len(failed_files),
        rendered_files,
        extra={
            "event": "transpile_failed_files",
            "failed_files": [str(path) for path in failed_files],
        },
    )


def _warn_if_failure_ratio_exceeds_threshold(
    *,
    failed_files: tuple[Path, ...],
    source_files: tuple[Path, ...],
    log: logging.Logger,
) -> None:
    if not failed_files or not source_files:
        return

    source_set = set(source_files)
    relevant_failures = [path for path in failed_files if path in source_set]
    failure_ratio = len(relevant_failures) / len(source_files)
    if failure_ratio <= 0.2:
        return

    log.warning(
        "Transpilation gaps exceed 20%% of source files (%d/%d failed)",
        len(relevant_failures),
        len(source_files),
        extra={
            "event": "transpile_failure_threshold_exceeded",
            "failed_file_count": len(relevant_failures),
            "source_file_count": len(source_files),
            "failure_ratio": failure_ratio,
            "failed_files": [str(path) for path in relevant_failures],
        },
    )


def _parse_source_map_file(map_path: Path) -> list[tuple[tuple[Path, int], tuple[Path, int]]]:
    payload = json.loads(map_path.read_text(encoding="utf-8"))
    raw_sources = payload.get("sources")
    raw_mappings = payload.get("mappings")
    if not isinstance(raw_sources, list) or not isinstance(raw_mappings, str):
        raise TranspilationError(f"invalid source map payload in {map_path}")

    generated_file = map_path.with_suffix("").resolve(strict=False)
    source_root = payload.get("sourceRoot", "")
    if not isinstance(source_root, str):
        raise TranspilationError(f"invalid sourceRoot in {map_path}")

    resolved_sources = [
        _resolve_source_reference(
            map_path=map_path, source_root=source_root, source_reference=source
        )
        for source in raw_sources
        if isinstance(source, str)
    ]
    mappings: list[tuple[tuple[Path, int], tuple[Path, int]]] = []
    seen_lines: set[int] = set()
    for generated_line, source_index, original_line in _iter_source_map_lines(raw_mappings):
        if generated_line in seen_lines:
            continue
        if source_index < 0 or source_index >= len(resolved_sources):
            continue
        seen_lines.add(generated_line)
        mappings.append(
            (
                (generated_file, generated_line),
                (resolved_sources[source_index], original_line),
            )
        )
    return mappings


def _resolve_source_reference(*, map_path: Path, source_root: str, source_reference: str) -> Path:
    reference = Path(source_root) / source_reference if source_root else Path(source_reference)
    if reference.is_absolute():
        return reference.resolve(strict=False)
    return (map_path.parent / reference).resolve(strict=False)


def _iter_source_map_lines(mappings: str) -> Iterable[tuple[int, int, int]]:
    source_index = 0
    original_line = 0
    original_column = 0
    name_index = 0

    for generated_line, raw_line in enumerate(mappings.split(";"), start=1):
        generated_column = 0
        for segment in raw_line.split(","):
            if not segment:
                continue
            decoded = _decode_vlq_segment(segment)
            if len(decoded) == 1:
                generated_column += decoded[0]
                continue
            if len(decoded) not in {4, 5}:
                raise TranspilationError(f"unsupported source map segment: {segment}")

            generated_column += decoded[0]
            source_index += decoded[1]
            original_line += decoded[2]
            original_column += decoded[3]
            if len(decoded) == 5:
                name_index += decoded[4]

            _ = generated_column, original_column, name_index
            yield generated_line, source_index, original_line + 1


def _decode_vlq_segment(segment: str) -> list[int]:
    values: list[int] = []
    value = 0
    shift = 0

    for character in segment:
        try:
            digit = _BASE64_VLQ_MAP[character]
        except KeyError as exc:
            raise TranspilationError(f"invalid base64 VLQ character {character!r}") from exc

        continuation = digit & 32
        digit &= 31
        value += digit << shift
        if continuation:
            shift += 5
            continue

        is_negative = value & 1
        decoded_value = value >> 1
        values.append(-decoded_value if is_negative else decoded_value)
        value = 0
        shift = 0

    if shift != 0:
        raise TranspilationError(f"unterminated base64 VLQ segment: {segment}")
    return values


__all__ = [
    "SourceMap",
    "TranspilationError",
    "TranspileWorkspace",
    "TranspiledProject",
    "TypeScriptCompilerNotFoundError",
    "prepare_transpile_workspace",
    "transpile_project",
]
