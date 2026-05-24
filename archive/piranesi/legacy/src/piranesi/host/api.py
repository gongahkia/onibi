from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Literal, overload

from pydantic import BaseModel, ValidationError

from piranesi.host.analyze import analyze_snapshot
from piranesi.host.collect import (
    HostCollectionError,
    HostCollectionResult,
)
from piranesi.host.collect import (
    collect_host_evidence as _collect_host_evidence,
)
from piranesi.host.ingest import HostInputError, load_host_input
from piranesi.host.models import HostPostureReport, HostSnapshot

ApiFormat = Literal["model", "dict"]
HostAnalysis = Literal["deterministic", "llm", "both"]


class PiranesiHostApiError(RuntimeError):
    """Base exception for public host API failures."""


class HostAssessmentError(PiranesiHostApiError):
    """Raised when a host bundle cannot be assessed through the public API."""


class HostReportParseError(PiranesiHostApiError):
    """Raised when a saved host report cannot be parsed through the public API."""


class HostApiCollectionError(PiranesiHostApiError):
    """Raised when public host evidence collection fails."""


@overload
def assess_host_bundle(
    input_path: str | Path,
    *,
    analysis: HostAnalysis = "deterministic",
    format: Literal["model"] = "model",
    treat_private_as_public: bool = False,
) -> HostPostureReport: ...


@overload
def assess_host_bundle(
    input_path: str | Path,
    *,
    analysis: HostAnalysis = "deterministic",
    format: Literal["dict"],
    treat_private_as_public: bool = False,
) -> dict[str, Any]: ...


def assess_host_bundle(
    input_path: str | Path,
    *,
    analysis: HostAnalysis = "deterministic",
    format: ApiFormat = "model",
    treat_private_as_public: bool = False,
) -> HostPostureReport | dict[str, Any]:
    """Assess a host snapshot or evidence bundle without invoking the CLI."""
    _validate_api_format(format)
    _validate_analysis(analysis)
    try:
        snapshot = load_host_input(input_path)
        report = analyze_snapshot(
            snapshot,
            analysis=analysis,
            treat_private_as_public=treat_private_as_public,
        )
    except HostInputError as exc:
        raise HostAssessmentError(str(exc)) from exc
    except Exception as exc:
        raise HostAssessmentError(f"{type(exc).__name__}: {exc}") from exc
    return _format_model(report, format)


@overload
def collect_host_evidence(
    output_dir: str | Path,
    *,
    format: Literal["model"] = "model",
    include_trivy: bool = True,
    include_lynis: bool = False,
    include_openscap: bool = False,
    include_auth_evidence: bool = False,
    trivy_target: str | Path = Path("/"),
    timeout_seconds: int = 30,
    trivy_timeout_seconds: int = 300,
) -> HostCollectionResult: ...


@overload
def collect_host_evidence(
    output_dir: str | Path,
    *,
    format: Literal["dict"],
    include_trivy: bool = True,
    include_lynis: bool = False,
    include_openscap: bool = False,
    include_auth_evidence: bool = False,
    trivy_target: str | Path = Path("/"),
    timeout_seconds: int = 30,
    trivy_timeout_seconds: int = 300,
) -> dict[str, Any]: ...


def collect_host_evidence(
    output_dir: str | Path,
    *,
    format: ApiFormat = "model",
    include_trivy: bool = True,
    include_lynis: bool = False,
    include_openscap: bool = False,
    include_auth_evidence: bool = False,
    trivy_target: str | Path = Path("/"),
    timeout_seconds: int = 30,
    trivy_timeout_seconds: int = 300,
) -> HostCollectionResult | dict[str, Any]:
    """Collect local host evidence without CLI process exits."""
    _validate_api_format(format)
    try:
        result = _collect_host_evidence(
            output_dir,
            include_trivy=include_trivy,
            include_lynis=include_lynis,
            include_openscap=include_openscap,
            include_auth_evidence=include_auth_evidence,
            trivy_target=trivy_target,
            timeout_seconds=timeout_seconds,
            trivy_timeout_seconds=trivy_timeout_seconds,
        )
    except HostCollectionError as exc:
        raise HostApiCollectionError(str(exc)) from exc
    except Exception as exc:
        raise HostApiCollectionError(f"{type(exc).__name__}: {exc}") from exc
    return _format_model(result, format)


@overload
def load_host_report(
    path_or_payload: str | Path | dict[str, Any],
    *,
    format: Literal["model"] = "model",
) -> HostPostureReport: ...


@overload
def load_host_report(
    path_or_payload: str | Path | dict[str, Any],
    *,
    format: Literal["dict"],
) -> dict[str, Any]: ...


def load_host_report(
    path_or_payload: str | Path | dict[str, Any],
    *,
    format: ApiFormat = "model",
) -> HostPostureReport | dict[str, Any]:
    """Parse a saved host report with v0 compatibility normalization."""
    _validate_api_format(format)
    try:
        payload = _load_report_payload(path_or_payload)
        normalized = _normalize_host_report_payload(payload)
        report = HostPostureReport.model_validate(normalized)
    except (OSError, json.JSONDecodeError, ValidationError, TypeError, ValueError) as exc:
        raise HostReportParseError(str(exc)) from exc
    return _format_model(report, format)


def load_host_snapshot(path_or_payload: str | Path | dict[str, Any]) -> HostSnapshot:
    """Parse a host snapshot from a path, bundle, or JSON-like payload."""
    if isinstance(path_or_payload, dict):
        try:
            return HostSnapshot.model_validate(path_or_payload)
        except ValidationError as exc:
            raise HostAssessmentError(str(exc)) from exc
    try:
        return load_host_input(path_or_payload)
    except HostInputError as exc:
        raise HostAssessmentError(str(exc)) from exc


def _load_report_payload(path_or_payload: str | Path | dict[str, Any]) -> dict[str, Any]:
    if isinstance(path_or_payload, dict):
        return dict(path_or_payload)
    path = Path(path_or_payload).expanduser().resolve(strict=False)
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise TypeError("host report payload must be a JSON object")
    return payload


def _normalize_host_report_payload(payload: dict[str, Any]) -> dict[str, Any]:
    normalized = dict(payload)
    normalized.setdefault("schema_version", 1)
    normalized.setdefault("analysis_modes", ["deterministic"])
    normalized.setdefault("control_summary", {})
    normalized.setdefault("host_metadata", {})
    normalized.setdefault("top_actions", [])
    normalized.setdefault("findings", [])
    normalized.setdefault("evidence_inventory", {})
    normalized.setdefault("known_limitations", [])
    if "snapshot" not in normalized:
        target = str(normalized.get("target") or "unknown")
        normalized["snapshot"] = {
            "schema_version": 1,
            "identity": {"hostname": target},
        }
    return normalized


def _validate_api_format(format_name: str) -> None:
    if format_name not in {"model", "dict"}:
        raise ValueError("format must be 'model' or 'dict'")


def _validate_analysis(analysis: str) -> None:
    if analysis not in {"deterministic", "llm", "both"}:
        raise HostAssessmentError("analysis must be 'deterministic', 'llm', or 'both'")


@overload
def _format_model[T: BaseModel](model: T, format_name: Literal["model"]) -> T: ...


@overload
def _format_model[T: BaseModel](model: T, format_name: Literal["dict"]) -> dict[str, Any]: ...


@overload
def _format_model[T: BaseModel](model: T, format_name: ApiFormat) -> T | dict[str, Any]: ...


def _format_model[T: BaseModel](model: T, format_name: ApiFormat) -> T | dict[str, Any]:
    if format_name == "dict":
        return model.model_dump(mode="json")
    return model
