# What It Does

KelpClaw SIFT Sentinel runs an offline Protocol SIFT-style trace or a live SIFT command and produces a reviewable DFIR audit package.

It:

- Parses the agent's final report into atomic incident-response claims.
- Tags claims with deterministic MITRE ATT&CK technique IDs.
- Links claims to concrete evidence artifacts such as timeline rows, Prefetch entries, Amcache records, registry exports, PCAP/Zeek-style flow summaries, Sysmon JSON, EVTX-style JSON, ShimCache, and SRUM.
- Applies claim-specific verification rules, including the rule that file presence alone does not prove program execution.
- Scores confirmed claims against the case manifest's ground truth.
- Generates targeted repair prompts for unsupported or contradicted claims.
- Supports opt-in multi-model committee extraction through `KELP_FINDEVIL_MODELS`.
- Tracks case-derived text in a taint ledger.
- Blocks tainted imperative text when it crosses into an operational tool argument.
- Reanalyzes blocked hostile evidence as quoted evidence, not as an instruction.
- Hashes evidence before and after the run and reports spoliation status.
- Exports a signed audit bundle with the normalized agent trace, claim ledger, repair trace, firewall event, spoliation check, benchmarked accuracy report, manifest, and reviewer HTML UI.

The current sentinel run evaluates 10 claims, confirms 3 ground-truth findings, reports precision 1.000 / recall 0.300 / F1 0.462, blocks 1 hostile evidence instruction, and reports zero evidence changes across 13 files.
