# Protocol SIFT Baseline Fixture

Provenance: synthetic, hand-authored for hackathon demo. Real Protocol SIFT
trace capture is a Phase 3 stretch goal.

This directory contains a compact JSONL trace and the overclaim report it
produces against `examples/findevil-sift-sentinel/`. The fixture is designed for
offline verifier and firewall tests:

- `baseline.jsonl` includes timeline and prefetch tool calls.
- `baseline.jsonl` includes a tool call argument containing hostile ransom-note
  text verbatim.
- `baseline-report.md` overclaims execution from file presence at `row:1842`.
