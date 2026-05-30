# Protocol SIFT Baseline Fixture

Provenance: synthetic, hand-authored for hackathon demo. Real Protocol SIFT
trace capture is a Phase 3 stretch goal.

This directory contains a ten-claim JSONL trace, repair injections, and the
overclaim report it produces against `examples/findevil-sift-sentinel/`. The
fixture is designed for offline verifier and firewall tests:

- `baseline.jsonl` covers program execution, persistence, network connection,
  credential access, lateral movement, and malware identification claims.
- `baseline.jsonl` includes a tool call argument containing hostile ransom-note
  text verbatim.
- `baseline-report.md` intentionally overclaims file presence, DNS-only,
  TaskCache-only, YARA-only, and no-evidence conclusions.
- `repair-injections.jsonl` contains synthetic targeted-analysis outputs for
  repairable high-severity claims.
