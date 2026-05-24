from __future__ import annotations

import argparse
import json
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

import yaml

try:
    from eval.ground_truth.schema import GroundTruthEntry
except ImportError:  # pragma: no cover - supports `python eval/ground_truth_enrich.py`
    from ground_truth.schema import GroundTruthEntry  # type: ignore[import-not-found,no-redef]

_DEFAULT_FIELDS = (
    "language",
    "framework",
    "taint_step_count",
    "taint_field_path",
    "field_sensitive_label",
)
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

_TAINT_FIELD_PATH_OVERRIDES = {
    "gt-114": "body.search",
    "gt-115": "body.cmd_input",
    "gt-117": "body.file_path",
    "gt-118": "body.proxy_url",
    "gt-119": "body.expr_input",
}


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


def _sanitize_field_segment(value: str) -> str:
    sanitized = re.sub(r"[^A-Za-z0-9_]+", "_", value.strip())
    sanitized = sanitized.strip("_")
    if not sanitized:
        return "value"
    return sanitized


def _normalize_field_path(path: str) -> str:
    parts = [_sanitize_field_segment(part) for part in path.split(".") if part.strip()]
    if not parts:
        return ""
    return ".".join(parts)


def _build_field_path(prefix: str, field: str, *, lowercase: bool = False) -> str:
    value = _normalize_field_path(field)
    if not value:
        value = "value"
    if lowercase:
        value = value.lower()
    return f"{prefix}.{value}"


def _extract_python_typed_param(prefix: str, source_text: str) -> str | None:
    pattern = re.compile(rf"\b([A-Za-z_][A-Za-z0-9_]*)\s*:\s*[^=]+?=\s*{prefix}\(")
    match = pattern.search(source_text)
    if match:
        return match.group(1)
    return None


def infer_taint_field_path(payload: dict[str, Any]) -> str | None:
    entry_id = str(payload.get("id") or "")
    override = _TAINT_FIELD_PATH_OVERRIDES.get(entry_id)
    if override is not None:
        return override

    taint_source = payload.get("taint_source")
    if not isinstance(taint_source, str):
        return None
    source_text = taint_source.strip()
    if not source_text:
        return None

    matchers: list[tuple[re.Pattern[str], Callable[[re.Match[str]], str]]] = [
        (
            re.compile(
                r"(?:^|\b)(?:req|request)\.(body|query|params|cookies|headers|files)\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"
            ),
            lambda match: _build_field_path(
                match.group(1),
                match.group(2),
                lowercase=match.group(1) == "headers",
            ),
        ),
        (
            re.compile(r"(?:^|\b)req\.file\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"),
            lambda match: _build_field_path("files", f"file.{match.group(1)}"),
        ),
        (
            re.compile(r"(?:^|\b)req\.file\b"),
            lambda _match: "files.file",
        ),
        (
            re.compile(r"(?:^|\b)params\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"(?:^|\b)searchParams\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"route\.snapshot\.queryParams\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(
                r"(?:^|\b)(body|query|params|headers|cookies|files)\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)"
            ),
            lambda match: _build_field_path(
                match.group(1),
                match.group(2),
                lowercase=match.group(1) == "headers",
            ),
        ),
        (
            re.compile(r"(?:^|\b)(?:req|request)\.headers\[['\"]([^'\"]+)['\"]\]"),
            lambda match: _build_field_path("headers", match.group(1), lowercase=True),
        ),
        (
            re.compile(
                r"(?:^|\b)(?:req|request)\.(body|query|params|cookies|headers|files)\[['\"]([^'\"]+)['\"]\]"
            ),
            lambda match: _build_field_path(
                match.group(1),
                match.group(2),
                lowercase=match.group(1) == "headers",
            ),
        ),
        (
            re.compile(r"request\.nextUrl\.searchParams\.get\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"formData\.get\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"request\.(GET|args)\[['\"]([^'\"]+)['\"]\]", re.IGNORECASE),
            lambda match: _build_field_path("query", match.group(2)),
        ),
        (
            re.compile(r"request\.(POST|form)\[['\"]([^'\"]+)['\"]\]", re.IGNORECASE),
            lambda match: _build_field_path("body", match.group(2)),
        ),
        (
            re.compile(r"request\.(args|GET)\.get\(['\"]([^'\"]+)['\"]\)", re.IGNORECASE),
            lambda match: _build_field_path("query", match.group(2)),
        ),
        (
            re.compile(r"request\.(form|POST)\.get\(['\"]([^'\"]+)['\"]\)", re.IGNORECASE),
            lambda match: _build_field_path("body", match.group(2)),
        ),
        (
            re.compile(r"request\.(json|data)\[['\"]([^'\"]+)['\"]\]"),
            lambda match: _build_field_path("body", match.group(2)),
        ),
        (
            re.compile(r"request\.(json|data)\.get\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(2)),
        ),
        (
            re.compile(r"request\.get_json\(\)\[['\"]([^'\"]+)['\"]\]"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"request\.getParameter\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"@Body\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"@Param\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"@Query\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"@Headers\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("headers", match.group(1), lowercase=True),
        ),
        (
            re.compile(r"@CookieValue\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("cookies", match.group(1)),
        ),
        (
            re.compile(r"@RequestHeader\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("headers", match.group(1), lowercase=True),
        ),
        (
            re.compile(r"@RequestParam\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"@PathVariable\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"c\.Query\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"c\.QueryParam\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"ctx\.QueryParam\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"r\.URL\.Query\(\)\.Get\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"c\.PostForm\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"c\.FormValue\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"r\.FormValue\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"chi\.URLParam\(r, ['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"c\.Param\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"c\.GetHeader\(['\"]([^'\"]+)['\"]\)"),
            lambda match: _build_field_path("headers", match.group(1), lowercase=True),
        ),
        (
            re.compile(r"\$_GET\[['\"]([^'\"]+)['\"]\]"),
            lambda match: _build_field_path("query", match.group(1)),
        ),
        (
            re.compile(r"\$_POST\[['\"]([^'\"]+)['\"]\]"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
        (
            re.compile(r"params\[:([A-Za-z_][A-Za-z0-9_]*)\]"),
            lambda match: _build_field_path("params", match.group(1)),
        ),
        (
            re.compile(r"document\.getElementById\(['\"]([^'\"]+)['\"]\)\.value"),
            lambda match: _build_field_path("body", match.group(1)),
        ),
    ]

    for pattern, resolver in matchers:
        match = pattern.search(source_text)
        if match:
            inferred = resolver(match)
            if inferred:
                return inferred

    if "@RequestParam" in source_text:
        return "query.param"
    if "@PathVariable" in source_text:
        return "params.id"
    if "@RequestBody" in source_text:
        return "body.payload"

    typed_query_name = _extract_python_typed_param("Query", source_text)
    if typed_query_name is not None:
        return _build_field_path("query", typed_query_name)

    typed_body_name = _extract_python_typed_param("Body", source_text)
    if typed_body_name is not None:
        return _build_field_path("body", typed_body_name)

    if source_text in {"req.body", "request.body", "request.data", "request.json"}:
        return "body.*"

    if source_text.startswith("request.get_data"):
        return "body.raw"

    return None


def infer_field_sensitive_label(
    payload: dict[str, Any],
    *,
    inferred_taint_field_path: str | None = None,
) -> str | None:
    label = payload.get("label")
    if not isinstance(label, str):
        return None
    normalized_label = label.strip()
    if normalized_label not in {"true_positive", "false_positive"}:
        return None

    taint_field_path_raw = payload.get("taint_field_path")
    taint_field_path: str | None = None
    if isinstance(taint_field_path_raw, str) and taint_field_path_raw.strip():
        taint_field_path = taint_field_path_raw.strip()
    elif inferred_taint_field_path is not None and inferred_taint_field_path.strip():
        taint_field_path = inferred_taint_field_path.strip()
    if taint_field_path is None:
        return None
    if "*" in taint_field_path:
        return None
    return normalized_label


def _entry_id(payload: dict[str, Any], fallback: str) -> str:
    value = payload.get("id")
    if isinstance(value, str) and value.strip():
        return value
    return fallback


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
    taint_field_candidates_only: bool,
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

        entry_id = _entry_id(payload, path.stem)
        inferred_language: str | None = (
            str(payload.get("language")) if _is_present(payload.get("language")) else None
        )

        if "language" in fields and not _is_present(payload.get("language")):
            language_candidate = infer_language(payload)
            if language_candidate is None:
                unresolved["language"].append(entry_id)
            else:
                updates["language"] = language_candidate
                inferred_language = language_candidate

        if "framework" in fields and not _is_present(payload.get("framework")):
            framework_candidate = infer_framework(payload, inferred_language=inferred_language)
            if framework_candidate is None:
                unresolved["framework"].append(entry_id)
            else:
                updates["framework"] = framework_candidate

        if "taint_step_count" in fields and not _is_present(payload.get("taint_step_count")):
            step_count_candidate = infer_taint_step_count(payload)
            if step_count_candidate is None:
                unresolved["taint_step_count"].append(entry_id)
            else:
                updates["taint_step_count"] = step_count_candidate

        if "taint_field_path" in fields and not _is_present(payload.get("taint_field_path")):
            field_path_candidate = infer_taint_field_path(payload)
            if field_path_candidate is None:
                if not taint_field_candidates_only:
                    unresolved["taint_field_path"].append(entry_id)
            else:
                updates["taint_field_path"] = field_path_candidate

        if "field_sensitive_label" in fields and not _is_present(
            payload.get("field_sensitive_label")
        ):
            field_sensitive_candidate = infer_field_sensitive_label(
                payload,
                inferred_taint_field_path=str(updates.get("taint_field_path"))
                if _is_present(updates.get("taint_field_path"))
                else None,
            )
            if field_sensitive_candidate is None:
                unresolved["field_sensitive_label"].append(entry_id)
            else:
                updates["field_sensitive_label"] = field_sensitive_candidate

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
        help=(
            "Field to enrich (repeatable). "
            "Defaults to language, framework, taint_step_count, taint_field_path, "
            "field_sensitive_label."
        ),
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
        "--taint-field-candidates-only",
        action="store_true",
        help=(
            "When enriching taint_field_path, only enforce unresolved checks on entries with "
            "explicitly inferable field-access sources."
        ),
    )
    parser.add_argument(
        "--fail-on-unresolved",
        action="store_true",
        help="Return exit code 1 if unresolved entries remain for selected fields.",
    )
    parser.add_argument(
        "--fail-on-updates",
        action="store_true",
        help=(
            "Return exit code 2 if enrichment would update any fields. "
            "Use this as a CI freshness gate in dry-run mode."
        ),
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
        taint_field_candidates_only=args.taint_field_candidates_only,
    )

    if args.json:
        print(json.dumps(summary.to_dict(), indent=2))
    else:
        print(render_summary(summary))

    if args.fail_on_unresolved and any(summary.unresolved.values()):
        return 1
    if args.fail_on_updates and summary.updated_fields > 0:
        return 2
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
