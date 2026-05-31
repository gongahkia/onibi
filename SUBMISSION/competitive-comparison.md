# Competitive Comparison

Public repository: `https://github.com/gongahkia/kelp-claw`

Legend: `done` = implemented in the public project or v3 anchor, `partial` = present but narrower or not fully benchmarked, `X` = not found in public materials reviewed on 2026-05-31.

Sources reviewed:

- KelpClaw SIFT Sentinel v3 local reruns: `.kelpclaw/findevil/sentinel-synthetic/`, `.kelpclaw/findevil/sentinel-cfreds/`, `.kelpclaw/findevil/benchmark/dfir-metric/`
- `https://github.com/marez8505/find-evil`
- `https://github.com/Juwon1405/agentic-dart`
- `https://findevil.devpost.com/rules`

| Feature | KelpClaw SIFT Sentinel | marez8505/find-evil | Juwon1405/agentic-dart |
|---|---|---|---|
| SANS Find Evil / Protocol SIFT alignment | done | done | done |
| Claude Code or agentic execution loop | done | done | done |
| Custom typed MCP forensic tool surface | partial | done | done |
| Evidence mounted read-only | done | done | done |
| Claim-level evidence references | done | partial | done |
| Claim-type verifier rules before confirmation | done | partial | partial |
| Targeted repair loop for weak claims | done | done | done |
| Precision / recall / F1 report | done | done | done |
| Three benchmark anchors in submission docs | done | X | partial |
| CFReDS public-image anchor | done | X | done |
| DFIR-Metric anchor | done | X | X |
| Hostile-evidence taint ledger | done | X | partial |
| Instruction firewall with measured corpus block rate | done | partial | done |
| Spoliation check with before/after hashes | done | partial | done |
| Signed audit bundle | done | partial | done |
| RFC3161 timestamp token | done | X | X |
| Deterministic replay hash | done | X | partial |
| Sigma mapping | done | X | partial |
| MITRE ATT&CK Navigator JSON export | done | X | partial |
| Docker runtime image | done | X | X |
| GitHub Actions CI | done | X | done |
| Web reviewer UI | done | done | partial |
| Broad SIFT adapters: Volatility, MFT, EVTX, Registry, Prefetch, YARA | partial | done | done |

Positioning: marez8505/find-evil and Agentic-DART are stronger typed-tool-surface projects. KelpClaw's v3 differentiation is the reviewer-grade validation layer around an agent run: strict claim confirmation, taint-aware firewall events, deterministic hashing, RFC3161 timestamping, Navigator export, and three explicit benchmark anchors.
