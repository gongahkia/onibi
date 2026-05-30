# What We Learned

Claim verification needs domain-specific rules. A generic evidence link is not enough: for `program_execution`, a timeline row showing file presence should not carry the same weight as Prefetch, Amcache, ShimCache, or Sysmon process-creation evidence.

Agent safety also needs DFIR-specific framing. Case evidence is adversarial input, but analysts still need to quote it, search it, and preserve it. The useful boundary is not "never read hostile text"; it is "never let hostile case text become authority."

Benchmarking changed the story. A self-correction demo proves the loop works once; precision, recall, F1, and ATT&CK coverage make the accuracy limits visible.

Finally, auditability is easier to trust when every artifact is boring and inspectable: JSONL traces, JSON manifests, Markdown reports, hashes, and a signed reviewer bundle.
