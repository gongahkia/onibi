# Product Hardening Roadmap

KelpClaw remains focused on agent skill governance: policy decisions before and during runs, replayable execution evidence, signed audit bundles, and reviewer-friendly governance outputs. Piranesi's useful role is the local evidence workspace inside that flow: custody, normalized findings, QA, retest comparison, and portable evidence views.

## External Anchors

- Singapore IMDA's [Model AI Governance Framework for Agentic AI](https://www.imda.gov.sg/resources/press-releases-factsheets-and-speeches/press-releases/2026/new-model-ai-governance-framework-for-agentic-ai), launched on 22 January 2026, should remain the primary SG positioning anchor for autonomy, human accountability, traceability, and safe deployment.
- IMDA's [AI Agents Sandbox insights](https://www.imda.gov.sg/resources/press-releases-factsheets-and-speeches/factsheets/2026/ai-agents-insights-from-the-singapore-government-and-google-sandbox), published on 20 May 2026, support KelpClaw's focus on computer-use agents, distributed safeguards, runtime evidence, and governance at platform, organisational, and end-user levels.
- The [AI Verify / IMDA Generative AI governance work](https://www.imda.gov.sg/resources/press-releases-factsheets-and-speeches/factsheets/2024/gen-ai-and-digital-foss-ai-governance-playbook) remains useful for model and application governance language, but KelpClaw should differentiate around tool-using agent runs rather than generic model evaluation.
- [SLSA](https://slsa.dev/spec/v1.0/levels) and [OpenSSF SLSA](https://openssf.org/projects/slsa/) are the right references for release provenance and artifact trust.
- [NIST SP 800-218 SSDF](https://csrc.nist.gov/pubs/sp/800/218/final) is the right reference for secure software development controls when selling into multinational enterprises.

## Implemented Hardening

- `kelp-claw help` returns adoption workflows and command groups as structured JSON.
- `kelp-claw doctor` checks local demo readiness: Node.js, workspace writes, policy packs, Git, optional Codex CLI, Exa, TinyFish, and API environment.
- `kelp-claw demo governance` generates a complete local handoff with a sample skill, input, evidence workspace, imported SARIF finding, signed bundle, governance report, controls, SARIF, and strict verification.
- Signed audit bundles and evidence workspaces are portable enough for security and compliance review without running KelpClaw.
- SG/APAC policy packs and governance reports are first-class, with US/UK/EU expansion left as later coverage.

## Next Product Directions

1. Release and install hardening
   - Publish a stable npm package and Homebrew tap.
   - Add signed release artifacts, provenance, SBOMs, and a release verification command.
   - Add `kelp-claw version --json` with build metadata and policy pack versions.

2. Live enforcement depth
   - Expand Codex wrapper enforcement from JSONL classification to stronger command/file/network mediation where the agent runtime exposes hooks.
   - Add comparable wrappers for Claude Code and Goose.
   - Track unsupported agent actions as explicit fail-closed findings under `--enforce-policy`.

3. Reviewer portal
   - Add a static bundle reviewer UI with search, filters, decision timeline, evidence previews, policy explanations, and redaction status.
   - Keep it static-first so reviewers can open the bundle without deploying KelpClaw.

4. Policy pack marketplace
   - Add pack metadata, changelogs, compatibility ranges, severity mappings, and local override layers.
   - Prioritize SG PDPA, MAS model risk, ASEAN GenAI, procurement/vendor review, and enterprise no-destructive-shell packs before US/UK/EU-specific packs.

5. Evidence connector breadth
   - Add imports for GitHub code scanning, Dependabot, Semgrep, Trivy, Snyk, Jira, Linear, and ticket exports.
   - Keep connectors evidence-oriented; KelpClaw should not become a general vulnerability scanner.

6. Enterprise operations
   - Add retention policies, redaction profiles, key rotation, external signing keys, approval workflows, and audit bundle verification policies.
   - Add API endpoints for bundle ingestion and inventory dashboards after the static workflow is stable.

7. Corpus and benchmarks
   - Grow the public `SKILL.md` compatibility corpus with real public skills.
   - Snapshot compatibility, policy explain, governance, replay diff, and bundle verification outputs.
   - Add benchmark runs for parser drift, policy pack regressions, and wrapper event normalization.

8. Replay diff visualization
   - Render cross-agent replay diffs as timeline views inside static bundles.
   - Show tool sequence, normalized args, output hashes, policy decisions, and unclassified events side by side.

## Near-Term Priority

The highest-leverage next work is release hardening plus reviewer experience: make KelpClaw trivial to install, run, verify, and forward to a reviewer. That aligns tightly with the value prop and avoids drifting into a broad agent platform.
