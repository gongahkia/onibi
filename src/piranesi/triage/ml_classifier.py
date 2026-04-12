from __future__ import annotations

import json
import logging
import pickle
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from piranesi.detect.sanitizer_validation import SanitizerEffectiveness, validate_sanitizer
from piranesi.models import CandidateFinding

_logger = logging.getLogger(__name__)
_DEFAULT_MODEL_PATH = Path("models") / "fp_classifier.pkl"
_CWE_PATTERN = re.compile(r"CWE-\d+")
_COMPLEXITY_PATTERN = re.compile(r"\b(?:if|else|for|while|switch|case|catch)\b|&&|\|\||\?")

_TOP_CWES = (
    "CWE-79",
    "CWE-89",
    "CWE-78",
    "CWE-22",
    "CWE-918",
    "CWE-502",
    "CWE-94",
    "CWE-77",
    "CWE-611",
    "CWE-943",
    "CWE-200",
    "CWE-352",
    "CWE-287",
    "CWE-384",
    "CWE-601",
)
_SOURCE_TYPES = (
    "req.body",
    "req.query",
    "req.params",
    "req.headers",
    "req.cookies",
    "env",
    "file",
    "db",
)
_SINK_TYPES = ("query", "exec", "render", "write", "redirect", "eval", "deserialize")
_FRAMEWORKS = ("express", "nestjs", "fastify", "nextjs", "django", "flask", "gin", "spring")
_EXTENSIONS = (".ts", ".tsx", ".js", ".jsx", ".py", ".go", ".java")
_Z3_RESULTS = ("SAT", "UNSAT", "UNKNOWN", "SKIPPED")
_SEVERITY_MAP = {"informational": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}


@dataclass(slots=True)
class MLClassifier:
    model: Any
    ordered_features: tuple[str, ...]
    metadata: dict[str, Any]

    def predict_probability(self, finding: CandidateFinding) -> float:
        return self.predict_probabilities([finding])[0]

    def predict_probabilities(self, findings: list[CandidateFinding]) -> list[float]:
        if not findings:
            return []
        rows = [
            feature_vector(finding, ordered_features=self.ordered_features) for finding in findings
        ]
        raw_probabilities = self.model.predict_proba(rows)
        return [_positive_class_probability(row) for row in raw_probabilities]

    @property
    def recommended_threshold(self) -> float:
        value = self.metadata.get("recommended_threshold")
        if isinstance(value, (int, float)):
            return float(value)
        return 0.5


@dataclass(slots=True, frozen=True)
class MLPrediction:
    finding: CandidateFinding
    true_positive_probability: float


def default_model_path() -> Path:
    return _DEFAULT_MODEL_PATH


def feature_names() -> list[str]:
    names: list[str] = []
    for cwe_id in _TOP_CWES:
        names.append(f"cwe_{cwe_id}")
    names.append("cwe_other")
    names.extend(
        [
            "confidence",
            "taint_path_length",
            "has_sanitizer_on_path",
            "sanitizer_cwe_match",
        ]
    )
    for source_type in _SOURCE_TYPES:
        names.append(f"src_{source_type}")
    names.append("src_other")
    for sink_type in _SINK_TYPES:
        names.append(f"sink_{sink_type}")
    names.append("sink_other")
    for framework in _FRAMEWORKS:
        names.append(f"fw_{framework}")
    names.append("fw_unknown")
    for extension in _EXTENSIONS:
        names.append(f"ext_{extension}")
    names.append("ext_other")
    names.extend(
        [
            "function_depth",
            "is_reachable",
            "is_dep_reachable",
            "field_sensitive_taint",
            "path_condition_count",
        ]
    )
    for result in _Z3_RESULTS:
        names.append(f"z3_{result}")
    names.extend(
        [
            "code_complexity",
            "has_test_coverage",
            "commit_age_days",
            "severity_ordinal",
            "has_path_condition_unsat",
        ]
    )
    return names


def extract_features(finding: CandidateFinding) -> dict[str, float]:
    feats: dict[str, float] = {}

    cwe_id = _parse_cwe(finding.vuln_class)
    for candidate_cwe in _TOP_CWES:
        feats[f"cwe_{candidate_cwe}"] = 1.0 if cwe_id == candidate_cwe else 0.0
    feats["cwe_other"] = 0.0 if cwe_id in _TOP_CWES else 1.0

    feats["confidence"] = float(finding.confidence)
    feats["taint_path_length"] = float(len(finding.taint_path))
    feats["has_sanitizer_on_path"] = float(
        any(step.sanitizer_applied is not None for step in finding.taint_path)
    )
    feats["sanitizer_cwe_match"] = float(_check_sanitizer_cwe_match(finding, cwe_id))

    source_type = _normalize_source_type(finding.source.source_type)
    for candidate_source_type in _SOURCE_TYPES:
        feats[f"src_{candidate_source_type}"] = 1.0 if source_type == candidate_source_type else 0.0
    feats["src_other"] = 0.0 if source_type in _SOURCE_TYPES else 1.0

    sink_type = _normalize_sink_type(finding.sink.sink_type, finding.sink.api_name)
    for candidate_sink_type in _SINK_TYPES:
        feats[f"sink_{candidate_sink_type}"] = 1.0 if sink_type == candidate_sink_type else 0.0
    feats["sink_other"] = 0.0 if sink_type in _SINK_TYPES else 1.0

    framework = _normalize_framework(finding.metadata.get("framework"))
    for candidate_framework in _FRAMEWORKS:
        feats[f"fw_{candidate_framework}"] = 1.0 if framework == candidate_framework else 0.0
    feats["fw_unknown"] = 0.0 if framework in _FRAMEWORKS else 1.0

    extension = Path(finding.source.location.file).suffix.lower()
    for candidate_extension in _EXTENSIONS:
        feats[f"ext_{candidate_extension}"] = 1.0 if extension == candidate_extension else 0.0
    feats["ext_other"] = 0.0 if extension in _EXTENSIONS else 1.0

    through_functions = {
        step.through_function for step in finding.taint_path if step.through_function is not None
    }
    feats["function_depth"] = float(len(through_functions))
    feats["is_reachable"] = 1.0 if finding.reachability == "reachable" else 0.0
    feats["is_dep_reachable"] = float(
        _metadata_bool(finding.metadata.get("dep_reachable"), default=True)
    )
    feats["field_sensitive_taint"] = float(
        _metadata_bool(finding.metadata.get("field_sensitive"), default=False)
    )
    feats["path_condition_count"] = float(len(finding.path_conditions))

    z3_result = _normalize_z3_result(finding.metadata.get("z3_result"))
    for candidate_result in _Z3_RESULTS:
        feats[f"z3_{candidate_result}"] = 1.0 if z3_result == candidate_result else 0.0

    feats["code_complexity"] = float(
        len(_COMPLEXITY_PATTERN.findall(finding.sink.location.snippet))
    )
    feats["has_test_coverage"] = float(_has_test_file(finding.sink.location.file))
    feats["commit_age_days"] = float(
        _metadata_float(finding.metadata.get("commit_age_days"), default=-1.0)
    )
    feats["severity_ordinal"] = float(_SEVERITY_MAP.get(finding.severity.lower(), 2))
    feats["has_path_condition_unsat"] = 1.0 if z3_result == "UNSAT" else 0.0

    return feats


def feature_vector(
    finding: CandidateFinding,
    *,
    ordered_features: tuple[str, ...] | list[str] | None = None,
) -> list[float]:
    names = list(ordered_features) if ordered_features is not None else feature_names()
    features = extract_features(finding)
    return [float(features.get(name, 0.0)) for name in names]


def predict(
    findings: list[CandidateFinding],
    *,
    classifier: MLClassifier | None = None,
    model_path: Path | str | None = None,
) -> list[MLPrediction]:
    if not findings:
        return []

    loaded_classifier = classifier if classifier is not None else load_model(model_path)
    if loaded_classifier is None:
        return [
            MLPrediction(finding=finding, true_positive_probability=0.5) for finding in findings
        ]

    probabilities = loaded_classifier.predict_probabilities(findings)
    return [
        MLPrediction(finding=finding, true_positive_probability=probabilities[index])
        for index, finding in enumerate(findings)
    ]


def filter_findings(
    findings: list[CandidateFinding],
    *,
    classifier: MLClassifier | None = None,
    model_path: Path | str | None = None,
    threshold: float | None = None,
) -> tuple[list[CandidateFinding], list[CandidateFinding]]:
    if not findings:
        return [], []

    loaded_classifier = classifier if classifier is not None else load_model(model_path)
    if loaded_classifier is None:
        return list(findings), []

    effective_threshold = (
        threshold if threshold is not None else loaded_classifier.recommended_threshold
    )
    escalate: list[CandidateFinding] = []
    likely_fp: list[CandidateFinding] = []
    for prediction in predict(findings, classifier=loaded_classifier):
        if prediction.true_positive_probability >= effective_threshold:
            escalate.append(prediction.finding)
        else:
            likely_fp.append(prediction.finding)
    return escalate, likely_fp


def load_model(model_path: Path | str | None = None) -> MLClassifier | None:
    path = _resolve_model_path(model_path)
    if not path.exists():
        _logger.debug("ML classifier model not found at %s", path)
        return None

    try:
        with path.open("rb") as handle:
            payload = pickle.load(handle)  # noqa: S301 - local trusted model artifact
    except ModuleNotFoundError:
        _logger.debug("ML classifier dependencies unavailable; skipping model load from %s", path)
        return None
    except Exception:
        _logger.exception("Failed to load ML classifier model from %s", path)
        return None

    if isinstance(payload, dict) and "model" in payload:
        metadata = _coerce_metadata(payload.get("metadata"))
        ordered_features = tuple(
            item for item in payload.get("feature_names", feature_names()) if isinstance(item, str)
        )
        if not ordered_features:
            ordered_features = tuple(feature_names())
        return MLClassifier(
            model=payload["model"],
            ordered_features=ordered_features,
            metadata=metadata,
        )

    metadata = load_model_metadata(path) or {}
    return MLClassifier(
        model=payload,
        ordered_features=tuple(feature_names()),
        metadata=metadata,
    )


def load_model_metadata(model_path: Path | str | None = None) -> dict[str, Any] | None:
    metadata_path = _resolve_metadata_path(model_path)
    if metadata_path is None or not metadata_path.exists():
        return None
    try:
        return _coerce_metadata(json.loads(metadata_path.read_text(encoding="utf-8")))
    except json.JSONDecodeError:
        _logger.exception("Failed to parse ML classifier metadata from %s", metadata_path)
        return None


def _resolve_model_path(model_path: Path | str | None) -> Path:
    raw_path = default_model_path() if model_path is None else Path(model_path)
    return raw_path.expanduser()


def _resolve_metadata_path(model_path: Path | str | None) -> Path | None:
    path = _resolve_model_path(model_path)
    candidates = [
        path.with_suffix(".json"),
        path.parent / "fp_classifier.json",
    ]
    if path.exists():
        resolved = path.resolve(strict=False)
        candidates.append(resolved.with_suffix(".json"))
    for candidate in candidates:
        if candidate.exists():
            return candidate
    return candidates[0] if candidates else None


def _coerce_metadata(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return dict(value)
    return {}


def _parse_cwe(vuln_class: str) -> str:
    match = _CWE_PATTERN.search(vuln_class)
    if match is None:
        return "CWE-0"
    return match.group(0)


def _check_sanitizer_cwe_match(finding: CandidateFinding, cwe_id: str) -> bool:
    for step in finding.taint_path:
        if step.sanitizer_applied is None:
            continue
        effectiveness = validate_sanitizer(step.sanitizer_applied, cwe_id)
        if effectiveness == SanitizerEffectiveness.EFFECTIVE:
            return True
    return False


def _normalize_source_type(value: str) -> str:
    lowered = value.lower()
    source_mappings = {
        "req.body": ("req.body", "request.body", "body."),
        "req.query": ("req.query", "request.query", "query."),
        "req.params": ("req.params", "request.params", "params."),
        "req.headers": ("req.headers", "request.headers", "headers."),
        "req.cookies": ("req.cookies", "request.cookies", "cookies."),
        "env": ("process.env", "env", "environment"),
        "file": ("file", "upload", "multipart", "fs."),
        "db": ("db", "database", "mongo", "sql row"),
    }
    for normalized, tokens in source_mappings.items():
        if any(token in lowered for token in tokens):
            return normalized
    return value


def _normalize_sink_type(sink_type: str, api_name: str) -> str:
    lowered = f"{sink_type} {api_name}".lower()
    sink_mappings = {
        "query": ("query", "sql", "findone", "find(", "where", "raw"),
        "exec": ("exec", "spawn", "system", "subprocess", "child_process"),
        "render": ("render", "template", "html", "innerhtml"),
        "write": ("write", "sendfile", "appendfile", "fs.", "file"),
        "redirect": ("redirect", "location"),
        "eval": ("eval", "function(", "vm.", "spel", "expression"),
        "deserialize": (
            "deserialize",
            "yaml.load",
            "pickle",
            "objectmapper",
            "json.parse",
            "unmarshal",
        ),
    }
    for normalized, tokens in sink_mappings.items():
        if any(token in lowered for token in tokens):
            return normalized
    return sink_type


def _normalize_framework(value: object) -> str:
    if isinstance(value, str):
        lowered = value.lower()
        for framework in _FRAMEWORKS:
            if framework in lowered:
                return framework
        return "unknown"
    if isinstance(value, list):
        for item in value:
            normalized = _normalize_framework(item)
            if normalized != "unknown":
                return normalized
    return "unknown"


def _normalize_z3_result(value: object) -> str:
    if isinstance(value, str):
        normalized = value.upper()
        if normalized in _Z3_RESULTS:
            return normalized
    return "SKIPPED"


def _metadata_bool(value: object, *, default: bool) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in {"1", "true", "yes", "y"}:
            return True
        if lowered in {"0", "false", "no", "n"}:
            return False
    return default


def _metadata_float(value: object, *, default: float) -> float:
    if isinstance(value, bool):
        return default
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value)
        except ValueError:
            return default
    return default


def _has_test_file(file_path: str) -> bool:
    path = Path(file_path)
    candidates = [
        path.with_name(f"{path.stem}.test{path.suffix}"),
        path.with_name(f"{path.stem}.spec{path.suffix}"),
        path.parent / "__tests__" / path.name,
        path.parent / "tests" / path.name,
    ]
    return any(candidate.exists() for candidate in candidates)


def _positive_class_probability(raw_row: Any) -> float:
    if isinstance(raw_row, (list, tuple)):
        if len(raw_row) >= 2:
            return float(raw_row[1])
        if len(raw_row) == 1:
            return float(raw_row[0])
    try:
        return float(raw_row[1])
    except Exception:
        return float(raw_row)


__all__ = [
    "MLClassifier",
    "MLPrediction",
    "default_model_path",
    "extract_features",
    "feature_names",
    "feature_vector",
    "filter_findings",
    "load_model",
    "load_model_metadata",
    "predict",
]
