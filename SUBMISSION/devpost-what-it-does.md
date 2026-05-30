# What It Does

KelpClaw SIFT Sentinel runs an offline Protocol SIFT-style trace or a live SIFT command and produces a reviewable DFIR audit package.

It:

- Parses the agent's final report into atomic incident-response claims.
- Links claims to concrete evidence artifacts such as timeline rows, Prefetch entries, and Amcache records.
- Applies claim-specific verification rules, including the rule that file presence alone does not prove program execution.
- Generates targeted repair prompts for unsupported or contradicted claims.
- Tracks case-derived text in a taint ledger.
- Blocks tainted imperative text when it crosses into an operational tool argument.
- Reanalyzes blocked hostile evidence as quoted evidence, not as an instruction.
- Hashes evidence before and after the run and reports spoliation status.
- Exports a signed audit bundle with the normalized agent trace, claim ledger, repair trace, firewall event, spoliation check, manifest, and HTML index.

The current Phase 3 sentinel run confirms one intentionally overclaimed execution claim after repair, blocks one hostile evidence instruction, and reports zero evidence changes.
