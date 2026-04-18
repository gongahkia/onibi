from __future__ import annotations

import argparse
import json
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

try:
    from eval.ground_truth.schema import GroundTruthEntry
except ImportError:  # pragma: no cover - supports `python eval/ground_truth_enrich.py`
    from ground_truth.schema import GroundTruthEntry  # type: ignore[import-not-found,no-redef]

_DEFAULT_FIELDS = ("language", "framework", "taint_step_count")
_ALLOWED_FIELDS = frozenset(_DEFAULT_FIELDS)
_SHOW_LIMIT_DEFAULT = 10

_LANGUAGE_BY_EXTENSION = {
    ".js": "javascript",
    ".mjs": "javascript",
    ".cjs": "javascript",
    ".ts": "typescript",
    ".tsx": "typescript",
    ".py": "python",
    ".go": "go",
    ".java": "java",
    ".php": "php",
    ".rb": "ruby",
}

_LANGUAGE_BY_SOURCE_PROJECT = {
    "adm-zip": "typescript",
    "bestzip": "typescript",
    "crypto-es": "javascript",
    "crypto-js": "javascript",
    "dvna": "javascript",
    "ejs": "javascript",
    "engine.io-client": "javascript",
    "express": "javascript",
    "fastify-reply-from": "javascript",
    "goof": "javascript",
    "koa": "javascript",
    "koa-remove-trailing-slashes": "javascript",
    "knex": "typescript",
    "mustache.js": "typescript",
    "node-jsonwebtoken": "javascript",
    "node-tar": "typescript",
    "owasp-juice-shop": "typescript",
    "owasp-nodegoat": "javascript",
    "piranesi-cross-language-fixture": "mixed",
    "piranesi-crypto-transport-fixtures": "mixed",
    "piranesi-owasp-fixture": "mixed",
    "prisma": "typescript",
    "remarkable": "typescript",
    "send": "typescript",
    "sequelize": "javascript",
    "serialize-javascript": "typescript",
    "serve": "typescript",
    "serve-static": "typescript",
    "simple-git": "typescript",
    "spring-petclinic": "java",
    "synthetic-go-chi": "go",
    "synthetic-go-echo": "go",
    "synthetic-go-gin": "go",
    "synthetic-go-stdlib": "go",
    "systeminformation": "typescript",
    "unzipper": "typescript",
    "xmlhttprequest-ssl": "javascript",
}

_FRAMEWORK_BY_SOURCE_PROJECT = {
    "adm-zip": "node",
    "bestzip": "node",
    "crypto-es": "node",
    "crypto-js": "node",
    "dvna": "express",
    "ejs": "node",
    "engine.io-client": "node",
    "express": "express",
    "fastify-reply-from": "fastify",
    "goof": "express",
    "koa": "koa",
    "koa-remove-trailing-slashes": "koa",
    "knex": "node",
    "mustache.js": "node",
    "node-jsonwebtoken": "node",
    "node-tar": "node",
    "owasp-juice-shop": "express",
    "owasp-nodegoat": "express",
    "piranesi-cross-language-fixture": "general",
    "piranesi-crypto-transport-fixtures": "general",
    "piranesi-owasp-fixture": "general",
    "prisma": "node",
    "remarkable": "node",
    "send": "node",
    "sequelize": "node",
    "serialize-javascript": "node",
    "serve": "node",
    "serve-static": "node",
    "simple-git": "node",
    "spring-petclinic": "spring-boot",
    "synthetic": "general",
    "synthetic-go-chi": "chi",
    "synthetic-go-echo": "echo",
    "synthetic-go-gin": "gin",
    "synthetic-go-stdlib": "go-stdlib",
    "systeminformation": "node",
    "unzipper": "node",
    "xmlhttprequest-ssl": "node",
}

_FRAMEWORK_FROM_LANGUAGE = {
    "javascript": "node",
    "typescript": "node",
    "python": "python",
    "go": "go",
    "java": "java",
    "php": "php",
    "ruby": "ruby",
    "mixed": "general",
}

_PATH_FRAMEWORK_HINTS = (
    ("/django/", "django"),
    ("/flask/", "flask"),
    ("/fastapi/", "fastapi"),
    ("/nestjs/", "nestjs"),
    ("/express/", "express"),
    ("/spring-boot/", "spring-boot"),
    ("/spring/", "spring"),
    ("/servlet/", "servlet"),
    ("/gin/", "gin"),
    ("/echo/", "echo"),
    ("/chi/", "chi"),
    ("/go-stdlib/", "go-stdlib"),
    ("/laravel/", "laravel"),
    ("/wordpress/", "wordpress"),
    ("/symfony/", "symfony"),
)


@dataclass(frozen=True, slots=True)
class EnrichmentSummary:
    gt_dir: str
    total_entries: int
    considered_entries: int
    fields: tuple[str, ...]
    write: bool
    files_written: int
    updated_entries: int
    updated_fields: int
    unresolved: dict[str, tuple[str, ...]]

    def to_dict(self) -> dict[str, Any]:
        unresolved_payload = {
            field: {
                "count": len(entry_ids),
                "entry_ids": list(entry_ids),
            }
            for field, entry_ids in self.unresolved.items()
        }
        return {
            "gt_dir": self.gt_dir,
            "total_entries": self.total_entries,
            "considered_entries": self.considered_entries,
            "fields": list(self.fields),
            "write": self.write,
            "files_written": self.files_written,
            "updated_entries": self.updated_entries,
            "updated_fields": self.updated_fields,
            "unresolved": unresolved_payload,
        }


def _is_present(value: object) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, list | tuple | set | dict):
        return bool(value)
    return True


def build_filter_predicate(expressions: list[str]) -> tuple[tuple[str, str], ...]:
    pairs: list[tuple[str, str]] = []
    for expression in expressions:
        if "=" not in expression:
            raise ValueError(f"invalid filter expression: {expression}")
        key, value = expression.split("=", 1)
        normalized_key = key.strip()
        normalized_value = value.strip()
        if not normalized_key:
            raise ValueError(f"invalid filter expression: {expression}")
        pairs.append((normalized_key, normalized_value))
    return tuple(pairs)


def _matches_filters(payload: dict[str, Any], filters: tuple[tuple[str, str], ...]) -> bool:
    if not filters:
        return True
    for key, expected in filters:
        current = payload.get(key)
        if current is None or str(current) != expected:
            return False
    return True


def _language_from_extensions(affected_files: list[str]) -> str | None:
    counts: Counter[str] = Counter()
    for raw_path in affected_files:
        suffix = Path(str(raw_path)).suffix.lower()
        language = _LANGUAGE_BY_EXTENSION.get(suffix)
        if language is not None:
            counts[language] += 1

    if not counts:
        return None

    most_common = counts.most_common()
    if len(most_common) == 1:
        return most_common[0][0]
    if most_common[0][1] > most_common[1][1]:
        return most_common[0][0]
    return None


def infer_language(payload: dict[str, Any]) -> str | None:
    files = [str(value) for value in payload.get("affected_files", []) if isinstance(value, str)]
    language = _language_from_extensions(files)
    if language is not None:
        return language
    source_project = str(payload.get("source_project") or "")
    return _LANGUAGE_BY_SOURCE_PROJECT.get(source_project)


def _framework_from_paths(affected_files: list[str]) -> str | None:
    normalized_paths = [str(value).replace("\\", "/").lower() for value in affected_files]
    for marker, framework in _PATH_FRAMEWORK_HINTS:
        if any(marker in path for path in normalized_paths):
            return framework
    return None


def infer_framework(payload: dict[str, Any], *, inferred_language: str | None) -> str | None:
    files = [str(value) for value in payload.get("affected_files", []) if isinstance(value, str)]
    framework = _framework_from_paths(files)
    if framework is not None:
        return framework

    source_project = str(payload.get("source_project") or "")
    framework = _FRAMEWORK_BY_SOURCE_PROJECT.get(source_project)
    if framework is not None:
        return framework

    if inferred_language is None:
        return None
    return _FRAMEWORK_FROM_LANGUAGE.get(inferred_language)


def infer_taint_step_count(payload: dict[str, Any]) -> int | None:
    taint_path = payload.get("taint_path")
    if not isinstance(taint_path, list):
        return None
    return len(taint_path)


def _render_scalar(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, str):
        return value
    raise TypeError(f"unsupported scalar value type for YAML write: {type(value).__name__}")


def _apply_scalar_updates_to_text(original_text: str, updates: dict[str, Any]) -> str:
    lines = original_text.splitlines()
    for key, value in updates.items():
        replacement = f"{key}: {_render_scalar(value)}"
        prefix = f"{key}:"
        replaced = False
        for index, line in enumerate(lines):
            if line.startswith(prefix):
                lines[index] = replacement
                replaced = True
                break
        if not replaced:
            lines.append(replacement)
    trailing_newline = "\n" if original_text.endswith("\n") else ""
    return "\n".join(lines) + trailing_newline


def load_ground_truth_payloads(gt_dir: Path) -> list[tuple[Path, dict[str, Any]]]:
    entries: list[tuple[Path, dict[str, Any]]] = []
    seen_ids: set[str] = set()
    for path in sorted(gt_dir.glob("*.yaml")):
        payload = yaml.safe_load(path.read_text(encoding="utf-8"))
        if not isinstance(payload, dict):
            raise ValueError(f"invalid YAML object in {path}")
        entry = GroundTruthEntry.model_validate(payload)
        if entry.id in seen_ids:
            raise ValueError(f"duplicate ground truth id: {entry.id}")
        seen_ids.add(entry.id)
        entries.append((path, payload))
    return entries


def _normalize_fields(fields: list[str] | None) -> tuple[str, ...]:
    if not fields:
        return _DEFAULT_FIELDS
    normalized: list[str] = []
    for field in fields:
        key = field.strip()
        if key not in _ALLOWED_FIELDS:
            allowed = ", ".join(sorted(_ALLOWED_FIELDS))
            raise ValueError(f"unsupported field '{field}'. allowed: {allowed}")
        if key not in normalized:
            normalized.append(key)
    return tuple(normalized)


def enrich_ground_truth(
    entries: list[tuple[Path, dict[str, Any]]],
    *,
    gt_dir: Path,
    fields: tuple[str, ...],
    filters: tuple[tuple[str, str], ...],
    write: bool,
    show_limit: int,
) -> EnrichmentSummary:
    unresolved: dict[str, list[str]] = {field: [] for field in fields}
    files_written = 0
    updated_entries = 0
    updated_fields = 0
    considered_entries = 0

    for path, payload in entries:
        if not _matches_filters(payload, filters):
            continue
        considered_entries += 1
        updates: dict[str, Any] = {}

        inferred_language = str(payload.get("language")) if _is_present(payload.get("language")) else None

        if "language" in fields and not _is_present(payload.get("language")):
            candidate = infer_language(payload)
            if candidate is None:
                unresolved["language"].append(str(payload.get("id") or path.stem))
            else:
                updates["language"] = candidate
                inferred_language = candidate

        if "framework" in fields and not _is_present(payload.get("framework")):
            candidate = infer_framework(payload, inferred_language=inferred_language)
            if candidate is None:
                unresolved["framework"].append(str(payload.get("id") or path.stem))
            else:
                updates["framework"] = candidate

        if "taint_step_count" in fields and not _is_present(payload.get("taint_step_count")):
            candidate = infer_taint_step_count(payload)
            if candidate is None:
                unresolved["taint_step_count"].append(str(payload.get("id") or path.stem))
            else:
                updates["taint_step_count"] = candidate

        if not updates:
            continue

        updated_entries += 1
        updated_fields += len(updates)
        payload.update(updates)
        GroundTruthEntry.model_validate(payload)

        if write:
            original_text = path.read_text(encoding="utf-8")
            path.write_text(
                _apply_scalar_updates_to_text(original_text, updates),
                encoding="utf-8",
            )
            files_written += 1

    unresolved_limited = {
        field: tuple(entry_ids[:show_limit])
        for field, entry_ids in unresolved.items()
        if entry_ids
    }

    return EnrichmentSummary(
        gt_dir=str(gt_dir),
        total_entries=len(entries),
        considered_entries=considered_entries,
        fields=fields,
        write=write,
        files_written=files_written,
        updated_entries=updated_entries,
        updated_fields=updated_fields,
        unresolved=unresolved_limited,
    )


def render_summary(summary: EnrichmentSummary) -> str:
    lines = [
        "Ground Truth Enrichment",
        f"- Directory: {summary.gt_dir}",
        f"- Entries considered: {summary.considered_entries}/{summary.total_entries}",
        f"- Fields: {', '.join(summary.fields)}",
        f"- Updated entries: {summary.updated_entries}",
        f"- Updated fields: {summary.updated_fields}",
        f"- Files written: {summary.files_written}",
    ]
    if summary.unresolved:
        lines.append("- Unresolved:")
        for field in summary.fields:
            ids = summary.unresolved.get(field)
            if ids:
                lines.append(f"  - {field}: {len(ids)} sample_ids={', '.join(ids)}")
    else:
        lines.append("- Unresolved: none")
    return "\n".join(lines)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Infer and backfill ground-truth metadata fields.",
    )
    parser.add_argument(
        "--gt-dir",
        type=Path,
        default=Path("eval/ground_truth"),
        help="Ground-truth directory.",
    )
    parser.add_argument(
        "--field",
        action="append",
        default=[],
        help="Field to enrich (repeatable). Defaults to language, framework, taint_step_count.",
    )
    parser.add_argument(
        "--filter",
        action="append",
        default=[],
        help="Filter entries by key=value before enrichment.",
    )
    parser.add_argument(
        "--show-limit",
        type=int,
        default=_SHOW_LIMIT_DEFAULT,
        help="Maximum unresolved entry IDs to include per field.",
    )
    parser.add_argument("--write", action="store_true", help="Persist enriched values to YAML files.")
    parser.add_argument(
        "--fail-on-unresolved",
        action="store_true",
        help="Return exit code 1 if unresolved entries remain for selected fields.",
    )
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON output.")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    fields = _normalize_fields(args.field)
    filters = build_filter_predicate(args.filter)
    entries = load_ground_truth_payloads(args.gt_dir)
    summary = enrich_ground_truth(
        entries,
        gt_dir=args.gt_dir,
        fields=fields,
        filters=filters,
        write=args.write,
        show_limit=max(1, args.show_limit),
    )

    if args.json:
        print(json.dumps(summary.to_dict(), indent=2))
    else:
        print(render_summary(summary))

    if args.fail_on_unresolved and any(summary.unresolved.values()):
        return 1
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
