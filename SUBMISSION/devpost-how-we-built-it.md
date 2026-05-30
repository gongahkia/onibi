# How We Built It

We kept KelpClaw's pre-existing governance foundation and built the Find Evil submission as a new DFIR layer around it.

The new `packages/findevil/` package contains the sentinel pipeline: claim extraction, evidence linking, verifier rules, repair loop, taint extraction, instruction firewall, spoliation hashing, and accuracy-report rendering. The CLI entry points live under `packages/cli/src/findevil/`.

The demo case is synthetic and repeatable. `examples/findevil-sift-sentinel/case.yml` points at a compact evidence tree under `examples/findevil-sift-sentinel/case-data/` and at the offline Protocol SIFT-style baseline trace under `fixtures/protocol-sift-baseline/baseline.jsonl`.

For the demo, the baseline report intentionally overclaims that `evil.exe` executed based only on timeline file presence. The sentinel flags that as unsupported, runs a targeted repair against execution artifacts, and confirms the claim only after linking direct Prefetch evidence. In the same run, hostile text in `ransom_note.txt` is prevented from becoming an operational instruction.

The signed audit bundle is produced by the sentinel itself under `.kelpclaw/findevil/sentinel/audit-bundle/` for the committed run, and by the Try It Out flow under `/tmp/kelpclaw-findevil-sentinel/audit-bundle/`.
