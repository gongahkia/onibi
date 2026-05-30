# How We Built It

We kept KelpClaw's pre-existing governance foundation and built the Find Evil submission as a new DFIR layer around it.

The new `packages/findevil/` package contains the sentinel pipeline: claim extraction, ATT&CK tagging, evidence linking, verifier rules, benchmark scoring, repair loop, optional multi-model committee voting, taint extraction, instruction firewall, spoliation hashing, reviewer HTML generation, live SIFT command execution, and accuracy-report rendering. The CLI entry points live under `packages/cli/src/findevil/`.

The public repository is `https://github.com/gongahkia/kelp-claw`. The demo case is synthetic and repeatable. `examples/findevil-sift-sentinel/case.yml` points at a compact evidence tree under `examples/findevil-sift-sentinel/case-data/`, the offline Protocol SIFT-style baseline trace under `fixtures/protocol-sift-baseline/baseline.jsonl`, and the live SIFT command contract through `siftIntegration`.

For the demo, the baseline report intentionally mixes supported findings with overclaims: file-presence-only execution, TaskCache-only persistence, DNS-only networking, no-evidence lateral movement, and a YARA-style family claim without a full hash chain. The sentinel flags weak claims, runs targeted repair against direct artifacts, confirms only the claims backed by accepted evidence, and downgrades or leaves unsupported the claims that still lack proof.

The canonical run confirms 3 of 10 ground-truth findings with precision 1.000, recall 0.300, and F1 0.462. It also tags 6 MITRE ATT&CK techniques, blocks 1 hostile evidence instruction, proves 13 evidence files were unchanged, and writes a self-contained reviewer UI under `.kelpclaw/findevil/sentinel/audit-bundle/index.html`.

The signed audit bundle is produced by the sentinel itself under `.kelpclaw/findevil/sentinel/audit-bundle/` for the canonical run, and by the Try It Out flow under `/tmp/kelpclaw-findevil-sentinel/audit-bundle/`.
