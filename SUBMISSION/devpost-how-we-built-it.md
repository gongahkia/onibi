# How We Built It

We kept KelpClaw's existing governance foundation and built the Find Evil submission as a new DFIR validation and read-only tool layer around Claude Code and Protocol SIFT.

Layer A extracts incident-response claims, tags them with ATT&CK techniques, links them to evidence, scores them against ground truth, and runs targeted repair when claims are unsupported or contradicted.

Layer B hashes the evidence tree before and after analysis and emits `spoliation-check.json`. The v3 synthetic run hashed 13 files before and after with 0 added, 0 removed, and 0 changed files. The CFReDS run hashed the pinned 309,818,835-byte E01 before and after with the same 0-change result.

Layer C treats case text as tainted input. The v3 synthetic run blocked 1 tainted operational use, while the Phase 12B firewall corpus blocked 46 of 46 malicious payloads and allowed all 9 legitimate quote controls.

Layer D expands artifact linkers across Prefetch, Amcache, MFT-style rows, registry, Sysmon, EVTX-style JSON, ShimCache, SRUM, memory-provider outputs, PCAP/Zeek flow summaries, YARA hits, and hash-backed evidence refs.

Layer E is the purpose-built MCP surface. `kelp-claw findevil mcp` exposes typed read-only tools: evidence inventory, file hashing, `mmls` partition parsing, `fls` filesystem listing, `icat` inode extraction, and literal recovered-artifact search. It does not expose arbitrary shell, delete, write, mount-remount, or package-install tools.

Layer F is the reviewer experience: a signed audit bundle with `index.html`, agent trace, claim ledger, repair trace, firewall events, spoliation check, evidence manifest, ATT&CK layer, policy decisions, and signature material.

Layer G adds Sigma mapping plus MITRE ATT&CK Navigator export. Each sentinel run writes `attack-navigator-layer.json`; the synthetic anchor covers 6 techniques and the CFReDS anchor covers 5 techniques.

Layer H packages the runtime with Docker. The v3 anchors were run through `kelp:v3`, built from `Dockerfile.kelp`, with case evidence mounted read-only and outputs mounted under `.kelpclaw/findevil/`.

Layer I adds deterministic replay and timestamp anchoring. Phase 12C replay logs the stable claim-ledger hash `sha256:8f99da2da7cb45a9e28d0c6db0c89fe6d08cbcf36fa0d2a710cd9552a10ee666`, and the v3 sentinel bundles include RFC3161 `evidence-manifest.tsr` tokens for `openssl ts -verify`.

The three v3 anchors show different operating modes: a synthetic self-correction case with precision 1.000 / recall 0.500 / F1 0.667, a conservative CFReDS container run with no unsupported worksheet prompt promoted to confirmed, and a DFIR-Metric blind subset-10 run where 14 non-empty expected answers are withheld from the evidence path and remain unconfirmed.

The CFReDS Hacking Case pilot adds the public real-image path judges asked for: fetch the official E01/E02 parts, verify the EWF image on SIFT, run the SIFT triage wrapper, and score 8 artifact-backed findings instead of promoting worksheet answers.
