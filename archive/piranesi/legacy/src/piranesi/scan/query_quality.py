from __future__ import annotations

from collections import Counter
from collections.abc import Iterable, Sequence

from piranesi.models.finding import (
    CandidateFinding,
    QueryQualityMetrics,
    QuerySpecDescriptor,
    QuerySpecUsage,
)
from piranesi.scan.specs import SinkSpec, SourceSpec

_DEFAULT_NOISY_SPEC_THRESHOLD = 5
_BUILTIN_SPEC_DEFINITION_FILE = "src/piranesi/scan/specs.py"
_CUSTOM_SPEC_DEFINITION_FILE = "piranesi.toml"


def build_query_quality_metrics(
    *,
    source_specs: Sequence[SourceSpec],
    sink_specs: Sequence[SinkSpec],
    candidate_findings: Sequence[CandidateFinding],
    noisy_candidate_threshold: int = _DEFAULT_NOISY_SPEC_THRESHOLD,
) -> QueryQualityMetrics:
    resolved_threshold = max(1, noisy_candidate_threshold)

    source_descriptors = tuple(_source_descriptor(spec) for spec in source_specs)
    sink_descriptors = tuple(_sink_descriptor(spec) for spec in sink_specs)
    source_descriptor_by_name = {descriptor.name: descriptor for descriptor in source_descriptors}
    sink_descriptor_by_name = {descriptor.name: descriptor for descriptor in sink_descriptors}

    source_counts = _source_candidate_counts(
        candidate_findings,
        source_descriptor_by_name=source_descriptor_by_name,
    )
    sink_counts = _sink_candidate_counts(
        candidate_findings,
        sink_descriptor_by_name=sink_descriptor_by_name,
    )

    source_usage = tuple(
        _usage_for_spec(descriptor, source_counts.get(descriptor.name, 0))
        for descriptor in source_descriptors
    )
    sink_usage = tuple(
        _usage_for_spec(descriptor, sink_counts.get(descriptor.name, 0))
        for descriptor in sink_descriptors
    )

    unmatched_source_specs = [usage.spec for usage in source_usage if not usage.matched]
    unmatched_sink_specs = [usage.spec for usage in sink_usage if not usage.matched]

    noisy_source_specs = _noisy_usage(source_usage, threshold=resolved_threshold)
    noisy_sink_specs = _noisy_usage(sink_usage, threshold=resolved_threshold)

    return QueryQualityMetrics(
        loaded_source_specs=len(source_descriptors),
        loaded_sink_specs=len(sink_descriptors),
        matched_source_specs=len(source_usage) - len(unmatched_source_specs),
        matched_sink_specs=len(sink_usage) - len(unmatched_sink_specs),
        noisy_candidate_threshold=resolved_threshold,
        source_specs=list(source_usage),
        sink_specs=list(sink_usage),
        unmatched_source_specs=unmatched_source_specs,
        unmatched_sink_specs=unmatched_sink_specs,
        noisy_source_specs=noisy_source_specs,
        noisy_sink_specs=noisy_sink_specs,
    )


def _usage_for_spec(descriptor: QuerySpecDescriptor, candidate_count: int) -> QuerySpecUsage:
    return QuerySpecUsage(
        spec=descriptor,
        candidate_count=candidate_count,
        matched=candidate_count > 0,
    )


def _source_candidate_counts(
    findings: Sequence[CandidateFinding],
    *,
    source_descriptor_by_name: dict[str, QuerySpecDescriptor],
) -> Counter[str]:
    counts: Counter[str] = Counter()
    for finding in findings:
        source_spec_name = _metadata_string(finding.metadata.get("source_spec_name"))
        if source_spec_name is None or source_spec_name not in source_descriptor_by_name:
            continue
        counts[source_spec_name] += 1
    return counts


def _sink_candidate_counts(
    findings: Sequence[CandidateFinding],
    *,
    sink_descriptor_by_name: dict[str, QuerySpecDescriptor],
) -> Counter[str]:
    counts: Counter[str] = Counter()
    for finding in findings:
        sink_spec_name = _metadata_string(finding.metadata.get("sink_spec_name"))
        if sink_spec_name is None or sink_spec_name not in sink_descriptor_by_name:
            continue
        counts[sink_spec_name] += 1
    return counts


def _noisy_usage(spec_usage: Iterable[QuerySpecUsage], *, threshold: int) -> list[QuerySpecUsage]:
    noisy = [usage for usage in spec_usage if usage.candidate_count >= threshold]
    return sorted(
        noisy,
        key=lambda usage: (-usage.candidate_count, usage.spec.name),
    )


def _source_descriptor(spec: SourceSpec) -> QuerySpecDescriptor:
    return QuerySpecDescriptor(
        spec_id=f"source:{spec.name}",
        name=spec.name,
        kind="source",
        category=spec.source_type.value,
        is_custom=spec.is_custom,
        definition_origin=_definition_origin(is_custom=spec.is_custom),
        definition_file=_definition_file(is_custom=spec.is_custom),
    )


def _sink_descriptor(spec: SinkSpec) -> QuerySpecDescriptor:
    return QuerySpecDescriptor(
        spec_id=f"sink:{spec.name}",
        name=spec.name,
        kind="sink",
        category=spec.sink_type.value,
        cwe_id=spec.cwe_id,
        severity=spec.severity,
        is_custom=spec.is_custom,
        definition_origin=_definition_origin(is_custom=spec.is_custom),
        definition_file=_definition_file(is_custom=spec.is_custom),
    )


def _definition_origin(*, is_custom: bool) -> str:
    return "config" if is_custom else "builtin"


def _definition_file(*, is_custom: bool) -> str:
    return _CUSTOM_SPEC_DEFINITION_FILE if is_custom else _BUILTIN_SPEC_DEFINITION_FILE


def _metadata_string(value: object) -> str | None:
    return value if isinstance(value, str) and value else None


__all__ = ["build_query_quality_metrics"]
