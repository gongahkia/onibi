from __future__ import annotations

import tomllib
from collections.abc import Iterable, Mapping, Sequence
from itertools import product
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field

from piranesi.legal.engine import Fact, FactPattern, ForwardChainingEngine, Rule


class RegulatoryRuleSpec(BaseModel):
    model_config = ConfigDict(extra="forbid")

    rule_id: str
    framework: str
    section: str
    obligation_text: str
    consequences: list[str]
    penalty_range: str
    notification_timeline: str | None = None
    enforcement_precedents: list[str] = Field(default_factory=list)
    cross_references: list[str] = Field(default_factory=list)
    severity_modifier: str | None = None
    vuln_classes: list[str] = Field(default_factory=list)
    data_categories: list[str] = Field(default_factory=list)
    requires_rule_ids: list[str] = Field(default_factory=list)
    requires_boolean_facts: list[str] = Field(default_factory=list)
    affected_individuals_gte: int | None = None


class RegulatoryRuleDocument(BaseModel):
    model_config = ConfigDict(extra="forbid")

    rules: list[RegulatoryRuleSpec]


def default_rules_path(filename: str) -> Path:
    return Path(__file__).resolve().parents[4] / "rules" / filename


def load_rule_specs(path: Path) -> list[RegulatoryRuleSpec]:
    with path.open("rb") as handle:
        data = tomllib.load(handle)
    document = RegulatoryRuleDocument.model_validate(data)
    return document.rules


def compile_rule_specs(rule_specs: Sequence[RegulatoryRuleSpec]) -> list[Rule]:
    compiled: list[Rule] = []
    for rule_spec in rule_specs:
        compiled.extend(compile_rule_spec(rule_spec))
    return compiled


def compile_rule_spec(rule_spec: RegulatoryRuleSpec) -> list[Rule]:
    fixed_preconditions: list[FactPattern] = [
        FactPattern(
            predicate="rule_fired",
            args={"finding_id": "?finding_id", "rule_id": required_rule_id},
        )
        for required_rule_id in rule_spec.requires_rule_ids
    ]
    fixed_preconditions.extend(
        FactPattern(
            predicate=boolean_fact,
            args={"finding_id": "?finding_id", "value": True},
        )
        for boolean_fact in rule_spec.requires_boolean_facts
    )
    if rule_spec.affected_individuals_gte is not None:
        fixed_preconditions.append(
            FactPattern(
                predicate="affected_individuals_threshold",
                args={
                    "finding_id": "?finding_id",
                    "threshold": rule_spec.affected_individuals_gte,
                    "value": True,
                },
            )
        )

    variable_dimensions: list[list[FactPattern]] = []
    if rule_spec.vuln_classes:
        variable_dimensions.append(
            [
                FactPattern(
                    predicate="vuln_class",
                    args={"finding_id": "?finding_id", "value": vuln_class},
                )
                for vuln_class in rule_spec.vuln_classes
            ]
        )
    if rule_spec.data_categories:
        variable_dimensions.append(
            [
                FactPattern(
                    predicate="data_category",
                    args={"finding_id": "?finding_id", "value": data_category},
                )
                for data_category in rule_spec.data_categories
            ]
        )

    conclusions = _build_conclusions(rule_spec)
    dimension_product = product(*variable_dimensions) if variable_dimensions else [()]
    return [
        Rule(
            preconditions=[*fixed_preconditions, *variant_preconditions],
            conclusions=conclusions,
        )
        for variant_preconditions in dimension_product
    ]


def _build_conclusions(rule_spec: RegulatoryRuleSpec) -> list[Fact]:
    obligation = Fact(
        predicate="obligation",
        args={
            "finding_id": "?finding_id",
            "rule_id": rule_spec.rule_id,
            "framework": rule_spec.framework,
            "section": rule_spec.section,
            "obligation_text": rule_spec.obligation_text,
            "consequences": list(rule_spec.consequences),
            "penalty_range": rule_spec.penalty_range,
            "notification_timeline": rule_spec.notification_timeline,
            "enforcement_precedents": list(rule_spec.enforcement_precedents),
            "cross_references": list(rule_spec.cross_references),
            "severity_modifier": rule_spec.severity_modifier,
        },
    )
    conclusions = [
        Fact(
            predicate="rule_fired",
            args={"finding_id": "?finding_id", "rule_id": rule_spec.rule_id},
        ),
        obligation,
    ]
    conclusions.extend(
        Fact(
            predicate="consequence",
            args={
                "finding_id": "?finding_id",
                "rule_id": rule_spec.rule_id,
                "action": consequence,
            },
        )
        for consequence in rule_spec.consequences
    )
    return conclusions


def build_finding_facts(
    *,
    finding_id: str,
    vuln_class: str,
    data_categories: Iterable[str],
    severity: str | None = None,
    affected_individuals: int | None = None,
    boolean_facts: Mapping[str, bool] | None = None,
    thresholds: Iterable[int] | None = None,
) -> list[Fact]:
    facts = [
        Fact(
            predicate="vuln_class",
            args={"finding_id": finding_id, "value": vuln_class},
        )
    ]

    seen_categories: set[str] = set()
    for data_category in data_categories:
        normalized = data_category.strip().lower()
        if not normalized or normalized in seen_categories:
            continue
        seen_categories.add(normalized)
        facts.append(
            Fact(
                predicate="data_category",
                args={"finding_id": finding_id, "value": normalized},
            )
        )

    if severity is not None:
        facts.append(
            Fact(
                predicate="severity",
                args={"finding_id": finding_id, "value": severity},
            )
        )

    if affected_individuals is not None:
        facts.append(
            Fact(
                predicate="affected_individuals",
                args={"finding_id": finding_id, "count": affected_individuals},
            )
        )
        for threshold in sorted(set(thresholds or (500,))):
            if affected_individuals >= threshold:
                facts.append(
                    Fact(
                        predicate="affected_individuals_threshold",
                        args={
                            "finding_id": finding_id,
                            "threshold": threshold,
                            "value": True,
                        },
                    )
                )

    for predicate, enabled in sorted((boolean_facts or {}).items()):
        if enabled:
            facts.append(
                Fact(
                    predicate=predicate,
                    args={"finding_id": finding_id, "value": True},
                )
            )

    return facts


def add_finding_facts(
    engine: ForwardChainingEngine,
    *,
    finding_id: str,
    vuln_class: str,
    data_categories: Iterable[str],
    severity: str | None = None,
    affected_individuals: int | None = None,
    boolean_facts: Mapping[str, bool] | None = None,
    thresholds: Iterable[int] | None = None,
) -> None:
    for fact in build_finding_facts(
        finding_id=finding_id,
        vuln_class=vuln_class,
        data_categories=data_categories,
        severity=severity,
        affected_individuals=affected_individuals,
        boolean_facts=boolean_facts,
        thresholds=thresholds,
    ):
        engine.add_fact(fact)


def query_obligations(
    engine: ForwardChainingEngine,
    *,
    finding_id: str | None = None,
) -> list[Fact]:
    obligations = engine.query("obligation")
    if finding_id is None:
        return obligations
    return [fact for fact in obligations if fact.args.get("finding_id") == finding_id]


def query_consequences(
    engine: ForwardChainingEngine,
    *,
    finding_id: str | None = None,
) -> list[Fact]:
    consequences = engine.query("consequence")
    if finding_id is None:
        return consequences
    return [fact for fact in consequences if fact.args.get("finding_id") == finding_id]


__all__ = [
    "RegulatoryRuleSpec",
    "add_finding_facts",
    "build_finding_facts",
    "compile_rule_spec",
    "compile_rule_specs",
    "default_rules_path",
    "load_rule_specs",
    "query_consequences",
    "query_obligations",
]
