from __future__ import annotations

from piranesi.legal.engine import Fact, FactPattern, ForwardChainingEngine, Rule


def test_engine_with_no_rules_or_facts_derives_no_obligations() -> None:
    engine = ForwardChainingEngine()

    engine.run()

    assert engine.query("obligation") == []


def test_single_rule_with_unsatisfied_preconditions_derives_nothing() -> None:
    engine = ForwardChainingEngine()
    engine.add_fact(Fact(predicate="finding", args={"finding_id": "f-1", "category": "public"}))
    engine.add_rule(
        Rule(
            preconditions=[
                FactPattern(
                    predicate="finding",
                    args={"finding_id": "?finding_id", "category": "health"},
                )
            ],
            conclusions=[
                Fact(
                    predicate="obligation",
                    args={"finding_id": "?finding_id", "action": "notify_regulator"},
                )
            ],
        )
    )

    engine.run()

    assert engine.query("obligation") == []


def test_forward_chaining_derives_facts_across_multiple_rules() -> None:
    engine = ForwardChainingEngine()
    engine.add_fact(
        Fact(
            predicate="finding",
            args={"finding_id": "f-1", "category": "health", "severity": "HIGH"},
        )
    )
    engine.add_rule(
        Rule(
            preconditions=[
                FactPattern(
                    predicate="finding",
                    args={"finding_id": "?finding_id", "category": "health"},
                )
            ],
            conclusions=[
                Fact(
                    predicate="sensitive_finding",
                    args={"finding_id": "?finding_id", "tier": 1},
                )
            ],
        )
    )
    engine.add_rule(
        Rule(
            preconditions=[
                FactPattern(
                    predicate="sensitive_finding",
                    args={"finding_id": "?finding_id", "tier": 1},
                ),
                FactPattern(
                    predicate="finding",
                    args={"finding_id": "?finding_id", "severity": "HIGH"},
                ),
            ],
            conclusions=[
                Fact(
                    predicate="obligation",
                    args={"finding_id": "?finding_id", "action": "notify_regulator"},
                )
            ],
        )
    )

    engine.run()

    assert engine.query("sensitive_finding") == [
        Fact(predicate="sensitive_finding", args={"finding_id": "f-1", "tier": 1})
    ]
    assert engine.query("obligation") == [
        Fact(
            predicate="obligation",
            args={"finding_id": "f-1", "action": "notify_regulator"},
        )
    ]


def test_engine_reaches_fixed_point_and_is_idempotent() -> None:
    engine = ForwardChainingEngine()
    engine.add_fact(Fact(predicate="data_category", args={"value": "nric"}))
    engine.add_rule(
        Rule(
            preconditions=[FactPattern(predicate="data_category", args={"value": "nric"})],
            conclusions=[Fact(predicate="tier", args={"value": 1})],
        )
    )

    engine.run()
    first_pass = engine.query("tier")

    engine.run()
    second_pass = engine.query("tier")

    assert first_pass == [Fact(predicate="tier", args={"value": 1})]
    assert second_pass == first_pass


def test_engine_does_not_loop_on_mutually_recursive_rules() -> None:
    engine = ForwardChainingEngine()
    engine.add_fact(Fact(predicate="a", args={"id": "seed"}))
    engine.add_rule(
        Rule(
            preconditions=[FactPattern(predicate="a", args={"id": "?id"})],
            conclusions=[Fact(predicate="b", args={"id": "?id"})],
        )
    )
    engine.add_rule(
        Rule(
            preconditions=[FactPattern(predicate="b", args={"id": "?id"})],
            conclusions=[Fact(predicate="a", args={"id": "?id"})],
        )
    )

    engine.run()

    assert engine.query("a") == [Fact(predicate="a", args={"id": "seed"})]
    assert engine.query("b") == [Fact(predicate="b", args={"id": "seed"})]


def test_engine_honors_max_iterations_safety_bound() -> None:
    engine = ForwardChainingEngine()
    engine.add_fact(Fact(predicate="seed", args={"id": "f-1"}))
    engine.add_rule(
        Rule(
            preconditions=[FactPattern(predicate="seed", args={"id": "?id"})],
            conclusions=[Fact(predicate="derived_one", args={"id": "?id"})],
        )
    )
    engine.add_rule(
        Rule(
            preconditions=[FactPattern(predicate="derived_one", args={"id": "?id"})],
            conclusions=[Fact(predicate="derived_two", args={"id": "?id"})],
        )
    )

    engine.run(max_iterations=1)

    assert engine.query("derived_one") == [Fact(predicate="derived_one", args={"id": "f-1"})]
    assert engine.query("derived_two") == []
