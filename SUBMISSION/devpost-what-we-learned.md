# What We Learned

Claim verification needs domain-specific rules. For `program_execution`, file presence should not carry the same weight as Prefetch, Amcache, ShimCache, Sysmon, Security 4688, or Volatility process evidence. That rule is why the synthetic run keeps 5 expected findings unconfirmed even when the report sounds plausible.

Agent safety also needs DFIR-specific framing. Case evidence is adversarial input, but analysts still need to quote it, search it, and preserve it. The useful boundary is not "never read hostile text"; it is "never let hostile case text become authority." The firewall corpus now measures that boundary: 46 of 46 malicious payloads blocked and 9 of 9 legitimate quotes allowed.

Benchmarking changed the story. A self-correction demo proves the loop once; three anchors expose the trade-offs: synthetic precision 1.000 / recall 0.500 / F1 0.667, CFReDS precision 0.000 / recall 0.000 / F1 0.000 by design, and DFIR-Metric blind subset-10 precision 0.000 / recall 0.000 / F1 0.000 because benchmark answers are withheld from evidence and trace claims.

Finally, auditability is easier to trust when every artifact is boring and inspectable: JSONL traces, JSON manifests, Markdown reports, hashes, Navigator JSON, Ed25519 signatures, and RFC3161 timestamp tokens.
