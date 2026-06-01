# What It Does

KelpClaw SIFT Sentinel runs an offline Protocol SIFT-style trace or a live SIFT command and produces a reviewable DFIR audit package.

It:

- Parses the agent's final report into atomic incident-response claims.
- Tags claims with deterministic MITRE ATT&CK technique IDs.
- Links claims to concrete evidence artifacts such as timeline rows, Prefetch entries, Amcache records, MFT-style rows, registry exports, PCAP/Zeek flow summaries, Sysmon JSON, EVTX-style JSON, ShimCache, SRUM, memory-provider outputs, and YARA hits.
- Applies claim-specific verification rules, including the rule that file presence alone does not prove program execution.
- Scores confirmed claims against case ground truth with precision, recall, and F1.
- Generates targeted repair prompts for unsupported or contradicted claims.
- Supports opt-in multi-provider extraction through Anthropic, OpenAI, Azure OpenAI, and Google Gemini provider adapters.
- Tracks case-derived text in a taint ledger.
- Blocks tainted imperative text when it crosses into an operational tool argument.
- Reanalyzes blocked hostile evidence as quoted evidence, not as an instruction.
- Hashes evidence before and after the run and reports spoliation status.
- Exports Sigma/ATT&CK coverage as a MITRE ATT&CK Navigator JSON layer.
- Exports a signed audit bundle with normalized agent trace, claim ledger, repair trace, firewall events, spoliation check, benchmarked accuracy report, manifest, RFC3161 timestamp token, and reviewer HTML UI.

v3 reports three anchors: synthetic precision 1.000 / recall 0.500 / F1 0.667; CFReDS precision 0.000 / recall 0.000 / F1 0.000 because no worksheet prompt is promoted without artifact proof; and DFIR-Metric blind subset-10 precision 0.000 / recall 0.000 / F1 0.000 because expected answers are used only by the scorer.
