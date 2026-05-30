# Find Evil! Hackathon — Final Plan for KelpClaw Pivot

## Context

KelpClaw is a TypeScript pnpm monorepo currently positioned as a workflow-governance control plane. The user wants to retarget it for **SANS Find Evil!** (findevil.devpost.com), the first hackathon for autonomous AI incident response. Three pivot proposals exist (IDEA1/2/3). The user has a team of 4–5, ~16 days remaining (deadline 2026-06-15), no Protocol SIFT setup yet (willing to spin up a VM), and explicit permission to gut or rebrand whatever doesn't serve the submission.

This plan recommends a single coherent project that fuses the strongest parts of IDEA 2 and IDEA 3, declares which parts of the existing repo to keep vs. shelve, and lays out a 16-day execution path.

## Verified Hackathon Facts (sources at end)

- **Mission**: make Protocol SIFT (the MCP bridge to SANS SIFT Workstation's ~200 tools) into a fully agentic IR platform.
- **Required agentic framework**: Claude Code OR OpenClaw. KelpClaw is *not* an agentic framework — it is a wrapper. **Position Claude Code as the agentic framework, KelpClaw as the verification + containment harness.** That is honest and defensible.
- **Required runtime**: must run on / integrate with the SANS SIFT Workstation.
- **Pre-existing code**: allowed as foundation; "novel contribution must be clearly documented" and the work submitted must be "substantially new."
- **Six confirmed judging criteria**: autonomous execution quality · IR accuracy · hallucination management · architectural guardrails · audit trail quality · documentation.
- **Prizes**: $10K / $7.5K / $3K + Summit packages. Winning code is reviewed for integration into Protocol SIFT.
- **Required artifacts**: code repo, demo video, architecture diagram, evidence dataset doc, accuracy report, agent execution logs, try-it-out instructions for SIFT Workstation, written description.
- **Timeline**: submit by 2026-06-15 11:45 PM EDT. Judging 2026-06-19 → 2026-07-03. Winners 2026-07-08.

## Verified Repo Reality

- 2026-05-30T11:10:40+0800: Phase 0 shelve completed; non-DFIR surfaces moved under `legacy/` and workspace narrowed to retained DFIR foundation packages.

[Inference, per Explore agent.] KelpClaw has these mature subsystems that map directly to hackathon criteria:

| KelpClaw asset | Maturity | Maps to judging criterion |
|---|---|---|
| `packages/evidence` — Ed25519 signing, audit bundles, attestation profiles (reviewer/regulator/ci) | Complete + tests | Audit trail · IR accuracy |
| `packages/policy` — evaluator + 11 policy packs, allow/deny/require-approval/log-only | Complete + tests | Architectural guardrails |
| `packages/agent-hooks` — Claude Code hook normalizer (PreToolUse/PostToolUse → JSONL) | Complete + tests | Autonomous execution · Audit trail |
| `packages/nanoclaw` — run manifests + replay (full DAG, per-node IO, hashes, policy decisions) | Complete | Audit trail · Hallucination mgmt (replayability) |
| `packages/codegen` — content-addressed SHA256 artifact store | Complete | Audit trail · Evidence integrity |
| `packages/cli` — `kelp-claw` binary, 40+ subcommands incl. `export-audit-bundle`, `verify-audit-bundle`, `replay-diff` | Complete | All criteria via tooling |

These are exactly the primitives the hackathon rewards. Keep them.

KelpClaw assets that do **not** serve the submission and should be shelved (commented out of workspace or moved to `legacy/`) to make the novel-contribution story sharp:
- `apps/kelpclaw` (React workflow editor UI) — not relevant to a DFIR CLI submission.
- `apps/api` (Fastify workflow CRUD API) — same.
- `packages/web-intel` (Exa/TinyFish search) — irrelevant.
- `packages/adapters` SaaS adapters (Gmail/Sheets/GitHub/Slack/Discord/Notion/Linear/Jira/Airtable) — irrelevant; keep only `mcp-adapter.ts` as the Protocol SIFT MCP client.
- `packages/skill-registry` — leave if cheap to keep, otherwise shelve.
- `apps/mcp-server` web-gateway stub — shelve (Protocol SIFT *is* the MCP server we wrap).

## Recommendation: KelpClaw SIFT Sentinel

**One coherent project**: an evidence-preserving, self-correcting verification harness around Protocol SIFT. Three layers, each owned by one team-member subset:

### Layer A — Claim Verifier + Repair Loop  *(IDEA 2 core; primary, ~50% of effort)*
1. Wrap a Protocol SIFT run via Claude Code (using existing `agent-hooks` normalizer to capture JSONL trace).
2. Parse the final Protocol SIFT report into atomic claims using a deterministic schema (`Claim {id, text, type, severity, confidence, evidenceRefs, missingEvidence, status, repairAction}`).
3. Evidence Linker maps each claim to concrete artifacts from the case output (timeline rows, Prefetch, Amcache, Sysmon, event log entries — start with **3 artifact types**; expand if time permits).
4. Verifier applies strict per-claim-type rules (e.g., `program_execution` cannot be `confirmed` from file presence alone — requires Prefetch / Amcache / ShimCache / Sysmon).
5. Status set ∈ {`confirmed`, `inferred`, `unsupported`, `contradicted`, `unverifiable`}.
6. For each `unsupported` / `contradicted` claim, generate a **targeted repair prompt**; rerun Claude Code → Protocol SIFT with bounded `--max-iterations`.
7. Diff baseline vs. repaired report; emit `accuracy-report.md`.

### Layer B — Evidence Integrity Guard  *(IDEA 3 lite; ~25% of effort)*
1. Hash every case-evidence file (SHA256) **before** the agent starts.
2. Mount evidence root read-only at the OS level (Linux bind-mount `-o ro` on the SIFT Workstation).
3. Re-hash all evidence files **after** every run; emit `spoliation-check.json` proving no original evidence was modified.
4. Reject agent tool calls that write into the evidence root via the existing `packages/policy` evaluator (add a `dfir-spoliation-strict` policy pack).

### Layer C — Evidence Taint Containment  *(IDEA 3 sharp; ~25% of effort)*
1. Label every case-derived text span (filenames, log messages, timeline rows, registry values, doc contents, memory strings) as `taint: case-data` with locator + source-artifact hash in a `taint-ledger.jsonl`.
2. Add an **instruction firewall** rule to `packages/policy`: block a tool-call argument when its content is dominated by tainted text matching imperative patterns (`ignore previous`, `delete`, `rm -rf`, `curl ... | sh`, `Run cleanup.sh`, etc.).
3. On block, write a `firewall-events.jsonl` entry and emit a `safe_reanalysis` repair task that quotes the hostile text as evidence only.

### Why this hybrid wins
Each layer maps directly to a different judging criterion, and none of them duplicate each other. Together they hit **all six** named criteria:

| Criterion | Layer that nails it |
|---|---|
| Autonomous execution quality | A (verifier loop + repair) + C (safe reanalysis) |
| IR accuracy | A (claim-to-evidence rules) |
| Hallucination management | A (claim status taxonomy + repair) |
| Architectural guardrails | B (read-only mounts + policy denial) + C (instruction firewall) |
| Audit trail quality | All three: claim ledger, repair trace, spoliation check, firewall events, signed bundle |
| Documentation | Case manifest, accuracy report, try-it-out, novel-contribution doc |

### Why not IDEA 1 (typed SIFT MCP bridge)
- Directly overlaps with Protocol SIFT itself (the hackathon's flagship framework). Reframing as a typed-tool replacement competes with the host project rather than enhancing it.
- Lowest reuse of mature KelpClaw code (MCP server is only a stub).
- High crowd risk — many teams will wrap forensic tools.
- The benefit of typed safety boundaries is captured cheaply in Layer B + Layer C via policy enforcement around the existing Protocol SIFT MCP, without reinventing it.

### Why not "pivot entirely"
The user offered to gut the repo, but the Explore audit shows KelpClaw's mature subsystems (evidence vault + signing + policy + run manifests + hook normalization) are precisely what the hackathon rewards. Throwing them away costs more than it saves. The right move is to shelve the workflow-editor and SaaS-adapter surfaces, keep the governance primitives, and add the DFIR-specific layers.

## Concrete File / Module Plan

**New top-level structure for submission**:
```
packages/
  evidence/        keep — signing, audit bundles
  policy/          keep — extend with dfir-spoliation-strict + tainted-instruction-block packs
  agent-hooks/     keep — Claude Code wrapper is the agentic-framework integration point
  nanoclaw/        keep — run manifests for replay/diff
  codegen/         keep — SHA256 content-addressed store
  cli/             keep — extend with `findevil` subcommand group
  findevil/        NEW — claim schema, extractor, evidence linker, verifier rules, repair loop, taint tracker, instruction firewall, spoliation guard
examples/
  findevil-sift-sentinel/   NEW — case.yml, hostile-evidence fixtures, sample Protocol SIFT trace
fixtures/
  protocol-sift-baseline/   NEW — captured Protocol SIFT run output for offline demo reliability
legacy/             NEW (or just .gitignored) — shelve apps/kelpclaw, apps/api, packages/web-intel, packages/adapters, packages/skill-registry, apps/mcp-server
```

**New CLI surface**:
```
kelp-claw findevil run    --case <case.yml> --max-iterations 3 --out .kelpclaw/findevil/
kelp-claw findevil verify --sift-run <dir>  --case <case.yml> --max-iterations 3
kelp-claw findevil firewall --case <case.yml> --sift-command "./run-protocol-sift.sh"
```
Single `kelp-claw findevil sentinel` umbrella command runs all three layers in sequence for the headline demo.

**Required outputs** in `.kelpclaw/findevil/sentinel/`:
- `agent-execution.jsonl` — full tool-event trace (reuse `agent-hooks` normalizer)
- `claim-ledger.json` — Layer A
- `repair-trace.jsonl` — Layer A
- `accuracy-report.md` — Layer A baseline-vs-repaired diff
- `taint-ledger.jsonl` — Layer C
- `firewall-events.jsonl` — Layer C
- `spoliation-check.json` — Layer B (before/after hashes)
- `evidence-manifest.json` — case data inventory with hashes
- `audit-bundle/index.html` + signed `manifest.json` + `attestation.json` — reuse existing `kelp-claw export-audit-bundle`

## 16-Day Execution Path (team of 4–5)

**Days 1–2: Foundation + Repo Reshape**
- Person 1: spin up SIFT Workstation VM on Mac (UTM/QEMU or VirtualBox); install Protocol SIFT; capture one baseline run against a public case dataset (e.g., the Lone Wolf scenario from DFIR.training or any SANS DFIR sample); save as `fixtures/protocol-sift-baseline/`.
- Person 2: shelve unused packages/apps (move to `legacy/` or comment out of `pnpm-workspace.yaml`); rewrite README to lead with "KelpClaw SIFT Sentinel" framing; document novel contribution.
- Person 3: scaffold `packages/findevil/` with claim schema + Vitest harness.
- Person 4 (if available): scaffold `examples/findevil-sift-sentinel/case.yml` and hostile-evidence fixtures (filename injection, log-line injection, ransom-note imperatives).

**Days 3–7: Layer A (Claim Verifier)**
- Claim extractor (LLM-assisted with deterministic schema enforcement).
- Evidence linker for 3 artifact types: timeline CSV rows, Prefetch entries, Amcache entries.
- Verifier rules for `program_execution`, `persistence`, `network_connection`.
- Repair prompt generator; bounded loop with `--max-iterations`.
- Vitest fixtures + unit tests for each rule.

**Days 8–10: Layer B (Spoliation Guard)**
- Pre/post hashing utility (reuse `packages/codegen` SHA256 helpers).
- `dfir-spoliation-strict` policy pack denying writes into evidence root.
- Read-only bind-mount instructions for SIFT Workstation try-it-out.
- `spoliation-check.json` schema + emitter.

**Days 11–13: Layer C (Instruction Firewall)**
- Taint ledger schema + emitter (locator + source-artifact hash).
- Imperative-pattern detector (regex + simple classifier; explicit allow-list for safe quoting).
- `tainted-instruction-block` policy pack.
- `firewall-events.jsonl` + safe-reanalysis repair prompt.

**Days 14–15: Demo, Docs, Submission Artifacts**
- Record demo video (≤5 min): show baseline overclaim → verifier flags → repair → corrected report → hostile-evidence block → safe reanalysis → spoliation check passes → signed audit bundle opens in browser.
- Architecture diagram (excalidraw or similar).
- Accuracy report writeup.
- Try-it-out instructions for SIFT Workstation.
- Novel-contribution markdown.

**Day 16: Buffer — submit by 2026-06-15 11:45 PM EDT.**

## Risks & Mitigations

- **Protocol SIFT setup risk**: if VM setup takes longer than 2 days, switch demo to offline `--trace fixtures/protocol-sift-baseline.jsonl` mode (already a documented fallback in IDEA 3). Live integration becomes a stretch goal.
- **Claim-extraction LLM variance**: enforce deterministic JSON schema with retries; cache by report hash so judges replaying see identical claims.
- **Scope creep into IDEA 1**: explicitly defer typed-MCP wrapping unless all three layers ship by Day 13.
- **License**: confirm MIT or Apache 2.0 on the repo before submission.
- **Eligibility framing**: in the novel-contribution doc, explicitly enumerate "what existed before 2026-04-15" (the evidence/policy/agent-hooks primitives) vs. "what is new in this submission" (everything under `packages/findevil/` + the three policy packs + the CLI `findevil` subcommands).

## Verification (end-to-end)

1. `pnpm install && pnpm test` — all existing test suites must remain green after the repo reshape.
2. `pnpm test --filter @kelpclaw/findevil` — new Vitest suite covers each layer.
3. On the SIFT Workstation VM:
   ```
   kelp-claw findevil sentinel \
     --case examples/findevil-sift-sentinel/case.yml \
     --sift-command "./run-protocol-sift.sh" \
     --max-iterations 3 \
     --out .kelpclaw/findevil/sentinel
   ```
4. Confirm all required output files exist; confirm `kelp-claw verify-audit-bundle --profile reviewer` validates the bundle.
5. Record one *real* self-correction (verifier flags an overclaim → repair pass fixes it) on a known dataset.
6. Confirm before/after hashes for every case-data file match in `spoliation-check.json`.

## Sources

- https://findevil.devpost.com/
- https://findevil.devpost.com/rules
- https://www.sans.org/blog/sans-launches-first-hackathon-autonomous-incident-response
- https://www.sans.org/blog/protocol-sift-experimental-research-initiative-ai-assisted-dfir
- https://robtlee73.substack.com/p/registration-is-open-find-evil-hackathon
- https://robtlee73.substack.com/p/introducing-protocol-sift-meeting
- https://www.sans.org/press/announcements/two-words-changed-cybersecurity-find-evil-builders-answer-call-defend-infrastructure
- https://github.com/marez8505/find-evil  (competing submission — typed-MCP path)
- https://github.com/Yamato-Security/hayabusa  (Sigma-based EVTX scanner; referenced by Phase 6B)
- https://github.com/WithSecureLabs/chainsaw  (MITRE-mapped EVTX analyzer)
- https://attack.mitre.org/techniques/enterprise/  (Phase 7A catalog source)
- arxiv:2602.08082  Spectral Guardrails for Tool Use Hallucination (Phase 7C inspiration)
- arxiv:2601.06818  AgentHallu hallucination attribution benchmark

---

# v2 Expansion (Phases 5–8) — added 2026-05-30

## Why v2 exists

The v1 pipeline (Phases 0–4) ships a working but thin demo: one claim, one repair, one firewall block, one spoliation pass. Audit confirmed it works end-to-end. With ~3 weeks of runway remaining before 2026-06-15, the gap between "works" and "wins" is:

1. **Visible breadth.** Reviewers will compare against other submissions. A public competitor (`github.com/marez8505/find-evil`) already ships broad artifact coverage (MFT, Registry, Volatility 3, YARA, netscan) on the typed-MCP path. Without more artifact linkers, Kelp's "depth not breadth" framing reads as "narrow, not focused."
2. **Defensible numbers.** A single-claim accuracy report does not look like a benchmark. Reviewers reward precision/recall they can replicate.
3. **A real audit experience.** The current `audit-bundle/index.html` is a 685-byte placeholder. The signed primitives are excellent but reviewers cannot walk them.
4. **Hallucination story.** "We catch hallucinations" is the explicit hackathon criterion. The single-model extractor cannot make a research-grade claim about this. A multi-model committee can.
5. **Real SIFT Workstation runtime.** Offline `--trace` mode is documented, but live integration is the rule the hackathon names explicitly.

## Competitive intelligence (2026-05-30 web research)

- **`github.com/marez8505/find-evil`** — typed MCP server with broad forensic tool wrappers (Amcache, Prefetch, MFT/mactime, Registry, EvtxECmd, Volatility 3 plugins, netscan, YARA), persistent self-correcting loop, architectural guardrails against shell exec and evidence writes. **Lacks**: claim verifier, instruction firewall, spoliation detection. **Lacks**: signed audit bundle. **Lacks**: any threat-intel or MITRE ATT&CK mapping.
- **1,100+ registered participants** — assume 200+ will submit. Most will follow the typed-MCP path. Kelp's verification + containment + signed-proof angle is rare.
- **Protocol SIFT itself**: "has not been validated for forensic soundness or evidentiary reliability and is not court-admissible." This is the killer positioning line — Kelp explicitly adds the validation layer Protocol SIFT lacks.
- **Hackathon explicit requirement**: "Each Project must demonstrate self-correction... and accuracy validation — all findings are traceable to specific artifacts, files, offsets, or log entries." Kelp's `evidenceRefs` schema is the literal answer to "traceable to specific artifacts." Quote this verbatim in submission docs.
- **DFIR community vocabulary** in 2026: MITRE ATT&CK technique IDs (T-numbers) are the lingua franca; Sigma rules (Hayabusa/Chainsaw) are the de-facto detection format; Sysmon Event ID 1 + Security 4688/4624/7045 are the must-have event IDs.

## Updated positioning (use in Phase 8 submission rewrite)

Old one-liner (v1):
> Kelp turns Protocol SIFT into a defensible autonomous DFIR agent…

New one-liner (v2):
> Protocol SIFT runs at machine speed but is "not validated for forensic soundness." Kelp is the validation layer Protocol SIFT lacks: every claim mapped to specific artifacts with MITRE ATT&CK tags, every overclaim caught by multi-model committee verification, every hostile evidence string blocked from steering the agent, every original artifact hashed and signed.

## Updated layer roadmap

| Layer | Source | Status after v1 | v2 expansion |
|---|---|---|---|
| A: Claim Verifier + Repair | IDEA 2 core | shipped (1 claim demo) | 5A richer fixture, 7A ATT&CK tagging, 7B benchmark, 7C committee |
| B: Spoliation Guard | IDEA 3 lite | shipped | unchanged |
| C: Instruction Firewall + Taint | IDEA 3 sharp | shipped | unchanged |
| D: Artifact Coverage | new in v2 | — | 6A Sysmon, 6B EVTX, 6C ShimCache+SRUM, 6D PCAP |
| E: Reviewer Experience | new in v2 | placeholder HTML | 7D interactive reviewer UI |
| F: Live SIFT Integration | mentioned, deferred | offline only | 5C live `--sift-command` mode |
| G: Distribution | mentioned, missing | bin not on PATH | 5B installable CLI |

## Updated 3-week schedule (today 2026-05-30, deadline 2026-06-15)

| Days | Phase(s) | Mode | People |
|---|---|---|---|
| 0.5 | 5A + 5B | parallel | 2 |
| 1.5 | 5C | sequential | 1 |
| 1 | 6A + 6B + 6C + 6D | **parallel** | 4 |
| 0.5 | 7A | sequential | 1 |
| 1 | 7B + 7C + 7D | parallel | 3 |
| 2 | 8 (rerun + SUBMISSION rewrite + demo video) | sequential | 1–2 |
| buffer ~10 days | risk + polish + dry runs | — | all |

Total critical-path work in v2: ~6.5 days. With a team of 4–5 and ~16 days of runway (2026-05-30 → 2026-06-15), the buffer absorbs Protocol SIFT VM friction, demo recording retakes, and any judging-criteria adjustments.

## Updated risks (v2)

- **Phase 5C VM friction.** Protocol SIFT install on the SIFT Workstation may break for opaque reasons. Time-box to 2 days; fall back to a recorded transcript + offline `--trace` mode for the live demo.
- **Phase 6 conflicts on `linker/index.ts`.** Four agents inserting into one dispatch table. The Phase 1 stub should already have inserted a `// PHASE 6 INSERT POINT` marker; if not, Phase 5A's agent must add it before Phase 6 starts.
- **Phase 7A schema migration.** Adding `attackTechniques` to the Claim schema is a Zod-level change that ripples through extractor/linker/verifier. Run the full test suite after the schema edit before letting 7B/7C/7D start.
- **Phase 7C model cost.** Multi-model committee can burn budget. Default to single-model when `KELP_FINDEVIL_MODELS` is unset; treat committee as opt-in for the headline demo only.
- **Demo video timing.** With reviewer UI, ATT&CK coverage, and benchmark table the 5-minute video gets crowded. Phase 8 demo-script must aggressively cut anything non-essential.
