# WORKON-PIVOT-ASAP

Pivot plan locked 2026-05-15. Piranesi reframes from "local-first host posture workbench" to a **pentest/red-team report engine** (Phase 1), with an audit evidence packager (Phase 2) and progressive ecosystem/integration/enterprise tracks beyond. Source-of-truth document; supersedes prior roadmaps/positioning until the README is rewritten.

---

## 0. Why we are pivoting

- Prior wedge ("report layer that composes free scanners") had no defensible pricing. Every wrapped tool (Lynis/OpenSCAP/Trivy/Wazuh/osquery/Semgrep/Snyk) is free and has its own UI; the README itself disclaims "Piranesi does not replace" any of them.
- Host-posture market is dominated by Wazuh (free + fleet-managed). Compliance market is dominated by Drata/Vanta (SaaS-with-agents). Piranesi cannot displace either.
- `docs/enterprise-readiness-assessment-2026-04-19.md` flagged Critical confidentiality leaks in LLM redaction and detection regressions — current breadth (104k LOC, 22 typer subapps, 97 CLI cmds, 12 compliance frameworks) cannot be supported by one maintainer.
- Pentest consultancies already pay $99–499/seat/mo for tools that solve a real pain (PlexTrac, Dradis, AttackForge, Hexway). ~60% of existing Piranesi code (adapters, report renderer, evidence model, redaction, suppressions, schema versioning) carries directly into that wedge. The deliverable IS the product.

## 1. Locked decisions (2026-05-15)

| # | Decision |
|---|---|
| 1 | Keep the **Piranesi** name. Domain/git/asset continuity. |
| 2 | New CLI surface: **5 verbs** — `ingest`, `report`, `retest`, `sign`, `serve`. Everything else hidden under `compat/` or removed. |
| 3 | MVP adapter trio: **nmap → nuclei → Burp Suite Pro**. Heavier matrix in Phase 1.1+. |
| 4 | "Delete-outright" list permitted (git history is the backup). Move-to-`legacy/` reserved for things we may revive. |
| 5 | First concrete action is writing this doc, not housekeeping. |

## 2. Phase summary

| Phase | Theme | Primary buyer | Status |
|---|---|---|---|
| 1 | Pentest report engine (MVP) | Solo pentesters, boutique offensive consultancies | Active |
| 2 | Audit evidence packager | Engineering teams prepping SOC2/ISO/PCI, regulated/airgapped shops | Planned |
| 3 | Workflow & ecosystem integration | Mid-size consultancies, internal red teams | Planned |
| 4 | Platform & open finding format | Tool ecosystem, plugin authors, community | Planned |
| 5 | Enterprise & scale | Larger MSSPs, regulated buyers, government | Planned |
| 6 | AI-native pentest co-pilot (aspirational) | Power users across all phases | Stretch |

Phases are **additive**, not destructive. Each phase preserves Phase 1's evidence-bound `Finding` contract + chain-of-custody invariants.

---

## 3. Phase 1 — Pentest report engine (MVP)

### 3.1 Goal

Single CLI that ingests output from the 3 most-used pentest tools, normalizes findings, and produces a customer-ready PDF + structured JSON + chain-of-custody-signed evidence bundle.

### 3.2 ICP

Freelance pentesters and 2–15-person offensive consultancies who today write deliverables in Word or pay $99–499/seat/mo for PlexTrac/Dradis/AttackForge.

### 3.3 Success criteria

- 5 design-partner consultancies running Piranesi on at least one real engagement.
- End-to-end nmap-XML → PDF deliverable runs in <5 min on a 1k-host scope.
- Local-only by default; zero outbound calls without explicit flag.
- Sign verb produces a verifiable hash chain reproducible from raw inputs.
- ≤500 lines in `cli.py` (down from 7,149).

### 3.4 CLI surface

```
piranesi ingest <tool> <file>      # nmap|nuclei|burp|nessus|zap|ffuf|sqlmap|sarif|host
piranesi report <workspace>        # --format pdf|json|md|all
piranesi retest <workspace> <new>  # diff, mark closed/open/regressed
piranesi sign <workspace>          # hash + timestamp + optional sigstore attestation
piranesi serve <workspace>         # 127.0.0.1 review/preview UI
```

Old verbs (`collect`, `assess`, `demo`, `doctor`, `fleet`, `host`, `container`, `k8s`, etc.) move under hidden `piranesi compat ...` until removed.

### 3.5 Adapter priority

Phase 1.0 (MVP):
1. nmap (XML)
2. nuclei (JSONL)
3. Burp Suite Pro (Issues XML — the format consultants actually ship)

Phase 1.1 (sellable):
4. Nessus (`.nessus`)
5. OWASP ZAP (JSON)
6. ffuf (JSON)
7. sqlmap (session/log)
8. Semgrep SARIF (already supported in `adapters/`)
9. Metasploit (DB export)

Phase 1.2 (stretch in P1):
10. Existing `host` collection (osquery/Trivy/Lynis/OpenSCAP) becomes one more adapter.
11. Nikto, Wapiti, testssl.sh, sslyze.
12. Bloodhound JSON, CrackMapExec output.

### 3.6 Hero artifact: the PDF

Sections (rendered by `report/renderer.py` rewritten around pentest template):
1. Cover page: engagement name, client logo, date range, consultant, version.
2. Executive summary: risk roll-up, posture decision, top-3 narrative.
3. Scope & methodology: in-scope assets, out-of-scope, tooling inventory, time window.
4. Findings (severity-ordered): title, severity, CVSS v4, affected component, evidence (cited from source tool), reproduction steps, remediation, retest status, references (CWE/OWASP), structured controls (CIS/NIST CSF appendix only).
5. Chain-of-custody appendix: SHA-256 of every raw input, signed finding hashes, optional sigstore attestation.
6. Optional: compliance mapping appendix (CIS + NIST CSF only).

### 3.7 What moves where

| Current path | Action | Target |
|---|---|---|
| `host/` | Demote — one adapter among many | `adapters/host/` |
| `report/renderer.py` | Rewrite around pentest template | stays in `report/` |
| `report/cwe.py` | Keep — used by finding metadata | stays |
| `models/` (Finding, etc.) | Keep — spine | stays |
| `adapters/` | Extend with new tool parsers | stays |
| `schema.py`, `audit.py`, `trace.py`, `support.py` | Keep | stays |
| `host/redaction.py` | Rename + expand to client-data sanitization | `core/sanitize.py` |
| `host/suppression.py` | Rename to retest tracking | `retest/` |
| `cli.py` | Rewrite to ≤500 lines / 5 verbs | stays |
| `ui_server/` | Repurpose as preview/team-review surface | stays |
| `rules/cis.toml`, `rules/nist_csf.toml` | Keep (appendix mapping only) | `rules/appendix/` |
| `scan/`, `detect/`, `triage/`, `verify/` | Move | `legacy/` |
| `infrastructure/` (container + k8s) | Move | `legacy/` |
| `legal/`, `intel/`, `graph/`, `patch/`, `hooks/`, `watch.py`, `lsp/`, `launcher_tui.py` | Move | `legacy/` |
| Compliance TOMLs (10 of 12) | Move | `legacy/rules/` |
| `verify/concolic.py` (102 KB Z3) | **Delete** | — |
| `detect/php_patterns.py`, `ruby_patterns.py`, `prototype_pollution.py`, `redos.py` | **Delete** | — |

### 3.8 Build sequence

Each step ≈ one PR-sized commit. Tests must remain green at every step.

1. **Housekeeping.** Create `legacy/`, git-mv targeted modules, fix imports, run `pytest`. No behavior change.
2. **CLI rewrite.** New typer app with 5 verbs. Old verbs aliased under hidden `compat`. Snapshot test for `--help`.
3. **Workspace concept.** A `workspace.json` schema that holds engagement metadata + ingested raw inputs + normalized findings + audit log. Lives on disk; versioned.
4. **Ingest pipeline v1.** `piranesi ingest nmap <file>` writes into a workspace. Add fixture + test.
5. **Report renderer v2.** Pentest-shaped PDF/JSON/MD from a workspace. Use Jinja2 + WeasyPrint (already deps-adjacent) or ReportLab. Fixture-driven golden tests.
6. **Adapter expansion.** `nuclei` and `burp` adapters with fixtures.
7. **`sign` verb.** Compute SHA-256 over normalized + raw inputs; write a manifest. Optional sigstore via `cosign` if available.
8. **`retest` verb.** Diff two workspaces; mark findings closed/open/regressed; render delta section in next report.
9. **`serve` verb.** 127.0.0.1 preview UI (reuse `ui_server/`, gut non-pentest screens).
10. **Docs rewrite.** README, capabilities.md, comparison.md, getting-started.md rewritten around pentest deliverable. Move old docs under `docs/legacy/`.

Estimate: ~2 focused weeks for steps 1–8; step 9 + 10 another 3–5 days. [Speculation] Depends on adapter parser quality of the 3 MVP inputs.

### 3.9 Non-goals (Phase 1)

- No multi-user, no auth, no team features.
- No SaaS hosting.
- No AI writeups (deferred to Phase 6).
- No new SAST work.
- No new compliance frameworks.
- No fleet management.

---

## 4. Phase 2 — Audit evidence packager

### 4.1 Goal

Re-use the Phase 1 chain-of-custody evidence bundle as an **assessor-friendly** deliverable for SOC2/ISO27001/PCI/ISO42001 prep, especially in regulated/airgapped environments where SaaS agents (Drata/Vanta) are unacceptable.

### 4.2 ICP

- Internal security/compliance engineers at mid-market companies preparing for first SOC2 Type II.
- Defense, banking, healthcare on-prem teams with no cloud egress.
- Auditors who want machine-readable evidence packets instead of screenshot folders.

### 4.3 Success criteria

- Generated evidence pack accepted by ≥1 real assessor as supplementary input.
- Continuous-collection mode runs as a cronjob, producing append-only signed history.
- Read-back tool verifies hash chain for any past evidence pack.

### 4.4 New surface (additive to Phase 1)

```
piranesi collect schedule         # continuous evidence collection (cron-friendly)
piranesi pack <workspaces...>     # build assessor-export bundle
piranesi verify <pack>            # verify hash chain + signatures
piranesi export <pack> --format aicpa|iso|pci  # framework-shaped projection
```

### 4.5 Reused infra

- Workspace model from Phase 1 (just longer-lived).
- `sign` verb (Phase 1) IS the chain-of-custody anchor.
- `adapters/host/` becomes primary input source.
- CIS + NIST CSF mappings expanded with versioned-control IDs (no more "broad family" disclaimers — properly cite exact controls).

### 4.6 New work

- Re-introduce 3–4 compliance frameworks from `legacy/rules/` (SOC2, ISO27001, PCI-DSS first). This time with assessor-reviewed control mappings, not auto-generated broad-family ones.
- "Evidence requirement" model: each control declares what evidence (config, log, screenshot, attestation) satisfies it. Workspace declares which it has.
- Append-only ledger (SQLite + signed log) so a year of monthly collections is one queryable artifact.
- Optional integration with public timestamping services (RFC 3161 TSAs) for hash anchoring.

### 4.7 Non-goals (Phase 2)

- Not a Drata/Vanta replacement. Not chasing continuous-monitoring dashboards.
- Not chasing FedRAMP/StateRAMP certification.
- Not building a multi-tenant SaaS.

---

## 5. Phase 3 — Workflow & ecosystem integration

### 5.1 Goal

Stop being a CLI; start being a **workflow tool** the consultant lives in for the duration of an engagement. Integrate with where pentesters and red teams already work.

### 5.2 ICP

Mid-size consultancies (10–50 consultants), internal red teams at F500.

### 5.3 Adapter expansion — offensive ecosystem

Beyond Phase 1.1 inputs, add the tools red-team operators actually run:

- **C2/post-exploitation:** Cobalt Strike beacon logs, Sliver, Mythic, Caldera, Empire export.
- **Active Directory:** Bloodhound JSON (already partial), ADRecon, PingCastle, PurpleKnight, certipy, pyKerbrute output.
- **Cloud:** ScoutSuite, Prowler, Pacu, CloudSploit; Azure Stormspotter; GCP Hayat.
- **Web:** dirsearch, gobuster, wfuzz, kiterunner, nikto, wapiti.
- **Wireless / RF:** Aircrack-ng, Kismet, Bettercap.
- **Mobile:** MobSF JSON.
- **Hardware/IoT:** binwalk, firmwalker reports.

Each is one adapter; the spine doesn't change.

### 5.4 Workflow integrations

- **Ticketing for remediation:** Jira, Linear, GitHub Issues, Azure DevOps. Push individual findings as tickets with structured metadata. Sync back closed/open status into retest workflow.
- **Engagement management:** template library (web app pentest, internal network, red team, AD assessment, cloud review). Methodology cards per engagement type. Scope tracker.
- **Client portal (paid SaaS):** customer-facing read-only preview of in-progress findings + final deliverable, with comment + acknowledgement workflow.
- **Knowledge base:** reusable finding templates across engagements ("here's our standard CVE-2024-xxxx writeup with remediation"). Org-wide library.
- **Time tracking integration:** Toggl, Harvest, Clockify — tie consultant time to findings for billing reports.
- **PSA/CRM:** ConnectWise Manage, HubSpot — auto-create engagement from signed SOW.
- **Slack/Discord/Teams:** notify on critical finding, on retest result, on client portal comment.

### 5.5 Team collaboration

- Multi-consultant assignment per finding.
- Peer-review workflow (one consultant authors, another reviews before client-visible).
- Engagement-level RBAC: lead vs. consultant vs. read-only client.
- Audit log of who edited what (extends existing `audit.py`).

### 5.6 New CLI / SaaS surface

CLI stays at 5 verbs; new verbs hidden under namespaces:

```
piranesi engagement create|list|archive
piranesi template list|use|publish
piranesi ticket push|sync
piranesi client preview|publish|revoke
```

SaaS web app starts here (optional, OSS CLI remains feature-complete for solo users).

### 5.7 Pricing inflection point

This is where freemium → paid becomes natural:
- OSS CLI: free forever.
- Paid team SaaS: $49–199/consultant/mo for client portal, retest queue, knowledge base sharing, ticketing sync, audit log retention.

---

## 6. Phase 4 — Platform & open finding format

### 6.1 Goal

Make Piranesi the **default open data format** for pentest findings, like Pcap for packets or SARIF for static analysis. Become a platform other tools emit into.

### 6.2 Deliverables

- **Piranesi Finding Format (PFF) spec** — public, versioned, JSON Schema-defined, governed under an open spec process. SARIF-inspired but covers full pentest deliverable (evidence, retest history, chain-of-custody, scope, methodology, not just static findings).
- **Adapter SDK** — Python + Go + TS libraries so third-party tool authors can emit PFF directly.
- **Plugin API** — community parsers/renderers/exporters loadable without forking core (existing `plugin.py` is the seed).
- **Community marketplace** — finding-template library, methodology library, custom report-template library. Hosted, signed, version-pinned.
- **CI/CD primitives** — official GitHub Action / GitLab template / Jenkins shared lib for emitting PFF from any pipeline.
- **PFF validators** — `piranesi pff lint` for tool authors, schema linting in editors.

### 6.3 Why it matters

Open formats win adoption (SARIF, CycloneDX, SPDX). If PFF becomes the lingua franca of "I scanned X and found Y," Piranesi becomes the default tool that reads/writes it — and the SaaS becomes the obvious place to host shared finding libraries.

### 6.4 Non-goals

- No closed/proprietary format lock-in.
- No "Piranesi cloud-only" features for PFF — the spec must work entirely offline.

---

## 7. Phase 5 — Enterprise & scale

### 7.1 Goal

Make Piranesi acquirable / sellable to MSSPs and regulated enterprises.

### 7.2 Deliverables

- Multi-tenancy with strict tenant isolation.
- SSO (SAML 2.0, OIDC), SCIM for user provisioning.
- RBAC with audit log of all admin actions.
- Customer-managed encryption keys (CMEK) for sensitive engagement data.
- On-prem deploy: Helm chart, Docker Compose, airgap install bundle (tarball + signed manifests).
- Audit log export to SIEM (CEF/LEEF/JSON over syslog).
- Support tiers, SLA, on-prem support contracts.
- Compliance certifications attainable for the SaaS itself (SOC2 Type II first; ISO27001 next; FedRAMP only if a customer is paying for it).
- Data residency options (US/EU/APAC).
- Backup/DR primitives, point-in-time restore of engagement workspaces.
- White-label option for MSSPs reselling.

### 7.3 Non-goals

- Not building a SOC or MDR offering. Stay in the report-and-evidence layer.

---

## 8. Phase 6 — AI-native pentest co-pilot (aspirational)

### 8.1 Goal

Given a normalized workspace, draft the parts of the deliverable a consultant would copy-paste anyway, **without leaking client data**.

### 8.2 Capabilities

- Auto-draft remediation prose per finding.
- Auto-draft exec summary narrative.
- Suggest next-step probes (extends current `host/probe.py` adaptive-probing infra).
- Anomaly detection across tool outputs: "nmap says port 443 open but nuclei found nothing — investigate or confirm probe failure."
- Methodology adherence checker: "engagement type 'web app' usually includes auth testing; no auth findings observed — confirm tested."
- Cross-engagement insight: "this CVE pattern appears in 4 of 7 of your client engagements this quarter."

### 8.3 Privacy invariants (non-negotiable)

- BYOK only; never call external LLM without an explicit, per-engagement opt-in.
- Local-LLM option (Ollama-compatible) for fully offline engagements.
- Redaction layer applied **before** any prompt leaves the host (extends `host/redaction.py` Phase 1 rename).
- Trace log of every LLM call, what was sent (redacted), what was returned. Reviewable.

### 8.4 Non-goals

- Not autonomous testing.
- Not generating exploit payloads.
- Not "AI replaces the consultant" — co-pilot only.

---

## 9. Definitive delete list (do not restore)

| Path | Reason |
|---|---|
| `verify/concolic.py` | 102 KB Z3 concolic engine, never reached production quality, off-path. |
| `verify/sandbox.py` | Docker exploit sandbox, niche, off-path. |
| `verify/constraints.py`, `verify/solver.py`, `verify/preconditions.py`, `verify/confirm.py`, `verify/launch.py`, `verify/reproducer.py`, `verify/evidence.py`, `verify/validation.py` | Verify subsystem entire; not pentest-report path. |
| `detect/php_patterns.py` | Pattern-only PHP SAST, low-value, off-path. |
| `detect/ruby_patterns.py` | Same as PHP. |
| `detect/prototype_pollution.py` | Niche JS, off-path. |
| `detect/redos.py` | Niche, off-path. |
| `eval/active_learn.py`, `eval/calibrate.py`, `eval/active_learn*` | Old SAST ML calibration, off-path. |

Everything else moves to `legacy/` instead of being deleted, so it remains importable for the rare user who still relies on it.

## 10. Definitive `legacy/` quarantine list

| Current path | Reason |
|---|---|
| `scan/` | Joern-backed SAST not part of Phase 1. |
| `detect/` (remaining files after deletes) | Source-code taint analysis off-path. |
| `triage/` | LLM triage, off-path. |
| `infrastructure/` | Container + k8s, deferred to Phase 1.2 as adapter. |
| `legal/` | Compliance memo generation, off-path. |
| `intel/`, `graph/` | Threat intel graph, off-path. |
| `patch/` | LLM patch generation, off-path. |
| `hooks/` | Pre-commit hook, off-path. |
| `watch.py` | File-system watch loop, off-path. |
| `lsp/` | Language server, off-path. |
| `launcher_tui.py` | Textual TUI, redundant with `serve`. |
| `rules/ccpa.toml`, `hipaa.toml`, `gdpr.toml`, `pdpa.toml`, `mas_trm.toml`, `eu_ai_act.toml`, `nis2.toml`, `pci_dss.toml`, `soc2.toml`, `iso27001.toml` | 10 of 12 compliance frameworks. CIS + NIST CSF stay in `rules/appendix/`. |

## 11. Tests that must stay green at every step

- `tests/test_host_posture.py` (98 tests) — host adapter contract.
- `tests/test_cli.py` (54 tests) — CLI surface.
- `tests/test_first_run_golden_path.py`, `tests/test_release_hygiene.py`, `tests/test_preflight.py`, `tests/test_ci_templates.py`.
- `tests/test_report_renderer.py` — renderer contract.
- `tests/test_exporters.py` — exporter contract.
- Legacy tests for moved modules move with the modules into `tests/legacy/`. They must still pass.

[Speculation] Full-suite run on a fast machine completed within ~5 min pre-pivot; expect same or better after `legacy/` quarantine reduces import surface.

## 12. Branding & messaging changes (do before launch)

- README headline: "Local-first pentest report engine. Ingest your tools, ship customer-ready deliverables, prove chain-of-custody."
- `description` in `pyproject.toml`: replace "CLI-native VM and Linux host security posture assessment tool" with pentest-report wording.
- Logo + asset stays.
- New tagline on cover-page PDF template.
- `docs/comparison.md` rewritten — competitors become **PlexTrac, Dradis, AttackForge, Hexway Apiary**, not Lynis/OpenSCAP/Trivy.

## 13. Risks & open questions

| Risk | Mitigation |
|---|---|
| PDF rendering quality varies by template — pentest buyers are picky. | Hire/commission a designer once 1 paying customer signs. Until then, prioritize structure over polish. |
| Burp Suite Pro Issues XML schema is loosely documented and drifts across versions. | Keep adapter version-pinned + fixture-tested per Burp version. |
| Chain-of-custody signing without a real PKI is theater. | Phase 1.0 uses self-signed hashes; Phase 1.1 adds RFC 3161 timestamping; Phase 2 adds sigstore. Acknowledge the trust gradient in docs. |
| Pentest consultancies are conservative about new tools. | First 5 design partners via personal network / OSS-friendly Discord (Bugcrowd, HackerOne, NetSec). Free OSS forever lowers adoption friction. |
| Phase 3 SaaS work distracts from Phase 1 quality. | Hard rule: no SaaS code until 5 design partners are running Phase 1 OSS on real engagements. |
| Single-maintainer pace. | Cut surface aggressively (this doc) before adding anything. Hire/co-maintain when revenue covers it. |

| Open question | Owner | Decide by |
|---|---|---|
| Self-host PDF renderer (WeasyPrint vs ReportLab vs Typst)? | maintainer | before Step 5 |
| Workspace storage format (single JSON vs SQLite vs filesystem tree)? | maintainer | before Step 3 |
| Sigstore vs RFC 3161 vs both for `sign`? | maintainer | before Step 7 |
| Open finding format (PFF) governance — single-owner or BDFL-then-foundation? | maintainer | before Phase 4 |
| Pricing tier names for Phase 3 SaaS? | maintainer | before first paying customer |

## 14. Immediate next action

Execute **Step 1 — housekeeping** from §3.8:

1. `git checkout -b pivot/phase1-housekeeping`
2. `mkdir -p legacy/ legacy/rules/ legacy/tests/`
3. `git mv` modules per §10 into `legacy/`.
4. `git mv` deleted modules per §9 → `git rm` (rely on git history).
5. Fix imports across the codebase.
6. Move corresponding test files into `tests/legacy/`.
7. Run full pytest. Must be green.
8. One commit, PR title `chore: quarantine off-path modules under legacy/ ahead of phase-1 pivot`.

After that PR lands, proceed to Step 2 (CLI rewrite).

---

*Document owner: maintainer. Living document — update as decisions resolve. Supersedes prior roadmaps in `docs/` until those docs are rewritten in Phase 1 Step 10.*
