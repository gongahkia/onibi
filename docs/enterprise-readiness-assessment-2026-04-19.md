# Piranesi Enterprise Readiness Assessment (2026-04-19)

## Scope And Method
This assessment combines:
- Repository architecture and capability review.
- Evaluation harness and ground-truth corpus analysis.
- Local test sweep (`python3 -m pytest`) and failure triage.
- Security/data-boundary review of redaction, evidence, verify sandbox, and advisory workflows.
- Cross-check against current external standards and references.

## Executive Summary
Piranesi is strong as an alpha CLI for JS/TS-centric AppSec workflows, but it is not yet enterprise-ready for broad organizational rollout.

Primary blockers are not only coverage. They include:
- Data-boundary regressions (secret redaction and evidence handling).
- Detection correctness regressions in key flow paths.
- Reliability and contract drift across CLI/report/schema behavior.
- CI environment contract instability for optional capabilities.

## What We Learned
1. Capability maturity is uneven by stage and language; JS/TS has the strongest signal.
2. Ground truth is sizable but highly imbalanced and partially non-runnable.
3. The highest current risk is confidentiality leakage through insufficient redaction.
4. Several test failures indicate real product regressions rather than environment-only issues.
5. Verification hardening exists but needs stricter policy and boundary enforcement.
6. Advisory and prioritization features are useful but should be trust-strengthened for enterprise use.

## Quantitative Baseline
- Ground-truth entries: `602`.
- Runnable local fixture mapping: `488/602` (`81.1%`).
- Non-runnable entries: `114/602` (`18.9%`), all from manual data.
- Label mix: `463` TP (`76.9%`), `139` FP (`23.1%`).
- Coverage gaps at `min-count=8`:
  - `cwe+language`: `125` gaps.
  - `cwe+framework`: `138` gaps.
  - `language+framework`: `15` gaps.
  - `cwe+language+framework`: `194` gaps.
- Test baseline (local, with optional suites excluded): `992 passed`, `16 failed`, `6 skipped`.

## Severity-Ranked Findings

### Critical
1. LLM redaction misses secret material because key regexes are over-escaped in code.
2. Sensitive data can leak into verification/compliance artifacts due incomplete redaction behavior.

### High
1. Evidence artifact path construction is vulnerable to unsafe filename inputs from finding IDs.
2. Confirmed verification payload/response artifacts can retain high-sensitivity raw content.
3. Detection precision/coverage regressions reduce trustworthiness of findings.

### Medium
1. Verify runtime controls are documented but not fully threaded into execution policy.
2. Advisory trust currently emphasizes freshness, not cryptographic source verification.
3. Report/schema/help contract drift introduces downstream integration instability.
4. Corpus skew lowers generalization confidence outside dominant slices.

## Standards Alignment Snapshot
- OWASP ASVS `5.0.0`: requires stronger repeatable verification and control assurance than current baseline.
- OWASP Top 10 and CWE Top 25 (2025): long-tail weak classes/slices remain under-covered.
- NIST SSDF SP 800-218 v1.1 and NIST CSF 2.0: governance, repeatability, and evidence integrity need strengthening.
- CVSS v4 / EPSS v4 / KEV integration direction is correct but policy controls need hardening.
- SBOM standards evolved (SPDX 3.0, CycloneDX 1.7); provenance/signing posture should be tightened.

## Actionable Improvement Program
Improvements are organized into six implementation tracks:
- Track A: Security and Data Boundary Hardening.
- Track B: Detection Correctness and Signal Quality.
- Track C: CI, Test Architecture, and Release Reliability.
- Track D: Ground Truth and Coverage Expansion.
- Track E: Advisory and Supply-Chain Trust.
- Track F: Enterprise Rollout Controls and Operations.
- Track G: Graph Intelligence, Cross-Tooling, and Agent Harness.
- Track F implementation docs:
  - [Enterprise Rollout Controls](/Users/gongahkia/Desktop/coding/projects/piranesi/docs/enterprise-rollout-controls.md)
  - [Incident Response Playbooks](/Users/gongahkia/Desktop/coding/projects/piranesi/docs/incident-response-playbooks.md)
  - [Rollout Governance And SLOs](/Users/gongahkia/Desktop/coding/projects/piranesi/docs/rollout-governance-and-slos.md)
- Track G implementation docs:
  - [Intel Integration](/Users/gongahkia/Desktop/coding/projects/piranesi/docs/intel-integration.md)

## Suggested Sequencing
1. Complete Track A before any broad rollout.
2. Run Tracks B and C in parallel once Track A P0 items are green.
3. Start Track D immediately as a continuing program after Track A starts stabilizing.
4. Execute Track E before policy-driven production onboarding.
5. Use Track F to gate organizational adoption by environment and risk tier.
6. Use Track G to add optional enrichment and agent orchestration after Track F controls are stable.

## Exit Criteria For Enterprise-Candidate Status
1. Zero known secret redaction regressions in required paths.
2. Stable detector regression suite with explicit precision/recall trend controls.
3. Deterministic CI core lane consistently green; integration drift bounded by policy.
4. Coverage expansion milestones achieved for sparse high-priority slices.
5. Signed/admissible evidence and advisory trust policies enforced.
