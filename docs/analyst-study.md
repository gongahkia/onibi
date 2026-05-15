# Analyst Study Protocol

This document defines the measured analyst study needed before Piranesi can make
claims about review-time reduction or triage-quality improvement. No participant
results are included yet.

## Study Goal

Measure whether Piranesi reports reduce review time or improve prioritization
quality compared with reviewing raw host evidence and standalone tool output.

## Task Set

Use synthetic or redacted fixtures only:

- Debian vulnerable host fixture;
- RHEL vulnerable host fixture;
- Amazon Linux update/privileged-user fixture;
- Alpine minimal fixture;
- optional raw osquery, Trivy, Lynis, or OpenSCAP output where available.

## Conditions

Each participant reviews comparable tasks under two conditions:

- baseline: raw evidence and standalone tool output;
- Piranesi: generated `host-report.json` and `host-report.md`.

Counterbalance the order to reduce learning effects.

## Metrics

Record:

- time to first correct critical/high priority;
- total review time;
- correct prioritization count;
- missed critical/high risks;
- false escalation count;
- analyst confidence;
- notes on confusing or missing evidence.

## Participant Instructions

Participants should:

- work from the supplied artifact bundle only;
- avoid internet research during timed tasks;
- write the top three actions they would assign;
- note any evidence they needed but could not find.

## Analysis Plan

For each condition, summarize median and range for time metrics, correctness,
missed critical/high risks, false escalations, and confidence. Include
limitations, participant background, sample size, and fixture scope.

Benchmark proxy outputs from `eval/host_benchmark.py` are not a substitute for
this study; they measure deterministic fixture matching, not human review.

