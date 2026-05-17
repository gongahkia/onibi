from __future__ import annotations

from collections.abc import Iterable
from typing import Any

from pydantic import BaseModel, ConfigDict

Bindings = dict[str, object]


def _canonicalize(value: object) -> Any:
    if isinstance(value, BaseModel):
        return _canonicalize(value.model_dump(mode="python"))
    if isinstance(value, dict):
        return tuple((key, _canonicalize(nested)) for key, nested in sorted(value.items()))
    if isinstance(value, (list, tuple)):
        return tuple(_canonicalize(item) for item in value)
    if isinstance(value, set):
        return tuple(_canonicalize(item) for item in sorted(value, key=repr))
    if value is None:
        return ("none", "")
    if isinstance(value, bool):
        return ("bool", value)
    if isinstance(value, int):
        return ("int", value)
    if isinstance(value, float):
        return ("float", value)
    if isinstance(value, str):
        return ("str", value)
    return ("repr", repr(value))


def _canonical_items(args: dict[str, object]) -> tuple[tuple[str, object], ...]:
    return tuple((key, _canonicalize(value)) for key, value in sorted(args.items()))


def _pattern_variable(value: object) -> str | None:
    if isinstance(value, str) and value.startswith("?") and len(value) > 1:
        return value[1:]
    return None


class Fact(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    predicate: str
    args: dict[str, object]

    def __hash__(self) -> int:
        return hash((self.predicate, _canonical_items(self.args)))

    def sort_key(self) -> tuple[str, tuple[tuple[str, object], ...]]:
        return (self.predicate, _canonical_items(self.args))


class FactPattern(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    predicate: str
    args: dict[str, object]


class Rule(BaseModel):
    model_config = ConfigDict(extra="forbid")

    preconditions: list[FactPattern]
    conclusions: list[Fact]


class ForwardChainingEngine:
    def __init__(self) -> None:
        self._rules: list[Rule] = []
        self._facts: set[Fact] = set()

    def add_rule(self, rule: Rule) -> None:
        self._rules.append(rule)

    def add_fact(self, fact: Fact) -> None:
        self._facts.add(fact)

    def run(self, max_iterations: int | None = None) -> None:
        iterations = 0

        while True:
            if max_iterations is not None and iterations >= max_iterations:
                return

            snapshot = tuple(self._facts)
            new_facts: set[Fact] = set()

            for rule in self._rules:
                for bindings in self._bindings_for_rule(rule.preconditions, snapshot):
                    for conclusion in rule.conclusions:
                        instantiated = self._instantiate_fact(conclusion, bindings)
                        if instantiated not in self._facts and instantiated not in new_facts:
                            new_facts.add(instantiated)

            if not new_facts:
                return

            self._facts.update(new_facts)
            iterations += 1

    def query(self, predicate: str) -> list[Fact]:
        matches = [fact for fact in self._facts if fact.predicate == predicate]
        return sorted(matches, key=Fact.sort_key)

    def _bindings_for_rule(
        self,
        preconditions: Iterable[FactPattern],
        facts: tuple[Fact, ...],
    ) -> list[Bindings]:
        bindings_list: list[Bindings] = [{}]

        for precondition in preconditions:
            next_bindings: list[Bindings] = []
            for bindings in bindings_list:
                for fact in facts:
                    matched = self._match_pattern(precondition, fact, bindings)
                    if matched is not None:
                        next_bindings.append(matched)
            if not next_bindings:
                return []
            bindings_list = self._dedupe_bindings(next_bindings)

        return bindings_list

    def _match_pattern(
        self,
        pattern: FactPattern,
        fact: Fact,
        bindings: Bindings,
    ) -> Bindings | None:
        if pattern.predicate != fact.predicate:
            return None
        if not set(pattern.args).issubset(fact.args):
            return None

        matched = dict(bindings)
        for key, pattern_value in pattern.args.items():
            fact_value = fact.args[key]
            variable_name = _pattern_variable(pattern_value)
            if variable_name is None:
                if _canonicalize(pattern_value) != _canonicalize(fact_value):
                    return None
                continue

            if variable_name in matched:
                if _canonicalize(matched[variable_name]) != _canonicalize(fact_value):
                    return None
            else:
                matched[variable_name] = fact_value

        return matched

    def _instantiate_fact(self, template: Fact, bindings: Bindings) -> Fact:
        return Fact(
            predicate=template.predicate,
            args={key: self._substitute(value, bindings) for key, value in template.args.items()},
        )

    def _substitute(self, value: object, bindings: Bindings) -> object:
        variable_name = _pattern_variable(value)
        if variable_name is not None:
            if variable_name not in bindings:
                raise ValueError(f"unbound variable '?{variable_name}' in rule conclusion")
            return bindings[variable_name]
        if isinstance(value, dict):
            return {key: self._substitute(nested, bindings) for key, nested in value.items()}
        if isinstance(value, list):
            return [self._substitute(item, bindings) for item in value]
        if isinstance(value, tuple):
            return tuple(self._substitute(item, bindings) for item in value)
        return value

    def _dedupe_bindings(self, bindings_list: list[Bindings]) -> list[Bindings]:
        unique: dict[tuple[tuple[str, object], ...], Bindings] = {}
        for bindings in bindings_list:
            key = _canonical_items(bindings)
            unique.setdefault(key, bindings)
        return list(unique.values())


__all__ = ["Fact", "FactPattern", "ForwardChainingEngine", "Rule"]
