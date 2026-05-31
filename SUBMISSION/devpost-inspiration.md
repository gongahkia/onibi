# Inspiration

Autonomous incident response is only useful when the result can survive forensic review. Protocol SIFT gives an agent access to SIFT Workstation tooling, but an agent can still overclaim, follow hostile text embedded in evidence, or leave reviewers unsure what changed.

KelpClaw SIFT Sentinel was built around that gap: let Claude Code and Protocol SIFT do the investigation, then wrap the run with evidence verification, ATT&CK tagging, Sigma/Navigator export, benchmark scoring, hostile-evidence containment, spoliation checks, deterministic replay, RFC3161 timestamping, and a signed audit bundle.

The v3 numbers make that philosophy visible. The synthetic run confirms 5 of 10 findings with 0 false positives. The CFReDS run refuses to confirm 25 worksheet prompts without direct artifacts. The DFIR-Metric run confirms 14 of 14 non-empty expected answers. The goal is not to make the agent sound confident; the goal is to make every important claim inspectable.
