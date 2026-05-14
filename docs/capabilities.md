# Capability Matrix

Piranesi is an alpha local-first VM and Linux host posture CLI. The current
stable-alpha surface is Debian/Ubuntu host evidence collection and assessment.
The older source-code AppSec pipeline remains in the repository for compatibility
and migration work, but it is no longer the primary public workflow.

Machine-readable known limitations are tracked in [`docs/known-limitations.json`](./known-limitations.json). Generated reports also include active registry entries in `report.json` (`known_limitations`) and `report.md`.

## Maturity Levels

| Level | Meaning |
| --- | --- |
| Stable Alpha | Covered by regular tests and intended for normal use, but still pre-1.0. |
| Beta | Useful and tested, but expected to need tuning on real projects. |
| Experimental | Implemented for early validation; expect false positives, misses, or rough UX. |
| Pattern-Only | Lightweight syntactic or heuristic checks, not full semantic analysis. |

## Host Posture Coverage

| Area | Maturity | Current Scope | Main Limitations |
| --- | --- | --- | --- |
| Debian/Ubuntu local collection | Stable Alpha | `piranesi collect` gathers osquery JSON, optional Trivy JSON, and read-only command evidence under `raw/`. | Requires `osqueryi`; optional tools may be missing or permission-limited. |
| Multi-platform Linux assessment | Alpha | RHEL/CentOS/Rocky/Alma/Fedora, Amazon Linux, and Alpine raw bundles normalize into the same `HostSnapshot` and `HostPostureReport` contracts with platform metadata. | Collection helpers vary by distribution; unsupported distro-specific checks are surfaced as collection-health warnings. |
| Canonical host snapshots | Stable Alpha | `host_snapshot.json` is accepted directly and remains the stable interchange format. | Snapshot completeness depends on supplied evidence. |
| Public host API and schemas | Stable Alpha | `piranesi.host.api` exposes deterministic assessment, report parsing, typed exceptions, and `piranesi schema ...` exports for host report, snapshot, and fleet report contracts. | Stable-alpha compatibility is additive; use documented public modules rather than internal host implementation modules. |
| Policy-as-code gates | Stable Alpha | TOML policies validate required evidence, risk/severity thresholds, rule-specific gates, allowed exposure, suppression behavior, minimum score, and fleet policy summaries. | Deterministic only; no general-purpose scripting or remote policy server. |
| Remediation planning and host diff | Stable Alpha | `piranesi remediate plan`, `remediate checklist`, `host diff`, and `remediate verify` generate review-only plans and before/after closure tracking. | Does not auto-remediate or generate configuration-management changes. |
| Raw bundle ingestion | Stable Alpha | Accepts collector-style `raw/osquery`, `raw/trivy`, `raw/commands` and root-level `osquery`, `trivy`, `commands` layouts. | Raw JSON must match osquery, Trivy, or Piranesi command wrapper shapes. |
| Deterministic assessment | Stable Alpha | Exposed services, SSH hardening, firewall/update posture, unattended-upgrades, selected sysctl values, privileged accounts, Trivy CVEs, evidence coverage, `host_metadata`, and `top_actions`. | Linux-only and intentionally conservative when evidence is absent. |
| LLM host analysis | Beta | Optional evidence-bound host reasoning for `--analysis llm` or `--analysis both`. | Requires LiteLLM-compatible credentials and does not replace deterministic findings. |
| Smoke harness | Stable Alpha | `scripts/host_smoke_check.sh` validates real-VM collection and deterministic assessment structure. | Local-only; it does not create or boot VMs. |

## Application Security Coverage

The source-code analysis features below are maintained as compatibility surfaces
during the host-posture pivot.

| Area | Maturity | Current Scope | Main Limitations |
| --- | --- | --- | --- |
| TypeScript/JavaScript | Beta | Joern-backed transpile, source/sink specs, taint paths, sanitizer confidence, SARIF/report output. | Compatibility workflow during the host-posture pivot; real-world helper/wrapper patterns still create misses. |
| Express | Beta | `req.body`, `req.query`, `req.params`, headers/cookies, response output, redirects, file/shell/HTTP sinks. | Compatibility workflow during the host-posture pivot; route and helper aliasing still need stronger receiver/type disambiguation. |
| Fastify | Beta | Request sources, reply sinks, schema-validation sanitizer hints. | Framework-specific plugins and lifecycle hooks are incomplete. |
| Next.js | Beta | Pages API routes, app routes, server actions, request body/header/search-param sources. | Server/client boundary and middleware modeling are incomplete. |
| NestJS | Beta | Decorated controller parameter sources and redirect/body patterns. | Decorator lowering and dependency-injection flows remain brittle. |
| Go | Experimental | File discovery, Joern direct import, selected framework detection, dependency and crypto/transport patterns. | Taint coverage is not equivalent to JS/TS. |
| Python | Experimental | File discovery, direct import, selected framework detection, dependency and pattern checks. | Taint coverage is not equivalent to JS/TS. |
| Java/Spring Boot | Experimental | Request annotation sources, selected JDBC/JPA/Runtime sinks, crypto/transport checks. | Requires more real-project calibration. |
| PHP | Pattern-Only | Raw PHP, Laravel/Symfony/WordPress-oriented vulnerable pattern checks. | Mostly syntactic; no full Joern-backed PHP taint parity. |
| Ruby | Pattern-Only | Rails/Sinatra-style pattern checks for common injection classes. | Mostly syntactic; no full semantic taint parity. |

## Legacy Pipeline Feature Coverage

| Feature | Maturity | Notes |
| --- | --- | --- |
| `scan` / `detect` artifacts | Beta | Compatibility pipeline contract; emits inspectable JSON. |
| Deterministic no-LLM mode | Beta | Static scan/detect/report can run without API keys; triage passes reachable findings through. |
| LLM triage | Beta | Requires LiteLLM-compatible credentials; improves false-positive discrimination but should not be treated as authoritative. |
| Patch generation | Experimental | LLM-backed and skipped in deterministic mode. Generated patches require review. |
| Docker exploit verification | Experimental | Includes structured, safe-by-default templates for `CWE-89` (SQLi), `CWE-78` (command injection), `CWE-918` (SSRF loopback probes), `CWE-22` (path traversal), `CWE-601` (open redirect), `CWE-79` (reflected XSS), `CWE-502` (insecure deserialization markers), and weak crypto classes (`CWE-327`/`CWE-326`/`CWE-319`). `verify.proof_mode` defaults to `safe`, which excludes destructive templates; `unsafe` is explicit opt-in. Verification can also use reusable `verify.target_profiles` for startup/readiness/base URL reuse across runs. Attempts emit preconditions, proof mode, target profile, startup failures, launch log path, evidence strings, and skip/inconclusive reasons in `verify.json` and report explanations. |
| SARIF output | Beta | Suitable for CI/code-scanning ingestion in the legacy source-code pipeline. |
| JUnit/CSV/TUI output | Beta | Useful for integration and review workflows. |
| Baseline diff (`new`/`changed`/`fixed`/`existing`) | Beta | `piranesi baseline diff` (or compatibility `piranesi diff`) and `piranesi run --baseline ...` produce deterministic baseline comparisons plus PR-friendly `baseline-diff.md` / `baseline-diff.json` artifacts. |
| Finding clustering | Beta | Reports preserve individual findings while grouping related findings by CWE and sink location. |
| Compliance/legal mapping | Experimental | Produces technical evidence and framework-control mappings (with version, review date, reviewer/source, and confidence). Supports audits but does not certify compliance or replace legal review. `piranesi compliance bundle` creates redacted, checksum-manifested evidence bundles for audit workflows. |
| Custom rules and rule registry | Beta | Rule validation, fixture testing, install/update/list flows exist. First-party example packs are provided under `examples/rule-packs/` (see `docs/custom-rule-packs.md`) as authoring scaffolds. |
| Advisory/dependency analysis | Beta | Supports advisory ingestion and dependency finding artifacts, with explicit CLI workflows for advisory DB status/update/import/search (`docs/advisory-db-workflow.md`). |
| LSP/watch/pre-commit | Beta | Save/watch loops use incremental invalidation where feasible, deduplicate repeated diagnostics, and expose stable diagnostic metadata (`stable_id`, evidence level, severity, action). |

## Trust Boundary

Piranesi report artifacts now expose explicit evidence statuses:

- **`static_candidate`**: static analysis lead; may include false positives.
- **`triaged_active_candidate`**: candidate retained by model-assisted triage.
- **`unreachable_candidate`**: static candidate not reachable from known entry points.
- **`confirmed`**: dynamically verified exploit path with verification evidence.
- **`suppressed`**: finding intentionally suppressed with rationale.

Reports should be read with that distinction in mind. A candidate finding is a
lead for engineering review; a confirmed finding is materially stronger evidence.

Compliance framework mappings are maintenance-bound metadata. They are useful for
audit preparation, but they do not certify framework conformance or replace
formal assessor/legal review.

Suppression lifecycle metadata (`owner`, `reason_code`, `created`, `expires`,
`ticket`/`reference`, `scope`) is supported in `.piranesi-ignore`, with lifecycle
validation available via `piranesi suppressions validate`.

## Confidence Transparency

`report.json`, `report.md`, and `piranesi explain` now include explanation metadata
and a structured confidence breakdown (`model_version = v1`) with contributor
components:

- `static_reachability`
- `source_quality`
- `sink_quality`
- `sanitizer_signal`
- `triage_signal`
- `verification_signal`
- `suppression_signal`

`final_confidence` remains the original pipeline confidence for compatibility,
while `contextual_confidence` and component rationales explain why that finding
is strong or weak evidence.

## Composite Risk Scoring

`report.json`, `report.md`, and `piranesi explain` include additive composite risk
metadata (`model_version = v1`):

- `composite_risk_score` (`0..100`)
- `composite_risk_band` (`low`/`medium`/`high`/`critical`)
- `composite_risk` component points with rationale (severity, confidence, exposure,
  sink criticality, ownership signal, verification/exploitability/advisory signals,
  reachability, suppression)

This score is for transparent prioritization; it does not replace severity labels
or formal risk acceptance decisions.

## Plugin Stability

Plugin extension-point stability levels and versioning guidance are documented in
`docs/plugin-api.md`. The stable contract is also exported programmatically via
`piranesi.plugin.plugin_api_manifest()`.

## Public API Stability

Host posture library and schema compatibility rules are documented in
`docs/api.md`. Integrations should depend on `piranesi.host.api`,
`piranesi.host.models`, and `piranesi.schema`; other host modules are internal
unless they are explicitly promoted in that document.
