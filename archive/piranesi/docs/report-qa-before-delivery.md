# Report QA Before Delivery

Date: 2026-05-20

Status: recommended first supplementary build.

## Goal

Add a pre-delivery quality gate that catches incomplete, unsafe, or weak
deliverables before a report, handoff archive, or email draft leaves the local
workspace.

## Approach

Introduce `piranesi report qa` or `piranesi ci validate-delivery` as a local
validator over reports, handoff artifacts, evidence, findings, detection records,
and signatures. The validator should emit actionable errors and warnings in text
and JSON.

This is the lowest-risk next feature because it strengthens existing workflows
without requiring new external data sources.

## Proposed Checks

Required checks:

- every finding has evidence;
- every evidence reference exists;
- sensitive evidence is redacted in report outputs;
- every high or critical finding has remediation and retest guidance;
- every report artifact has a digest;
- handoff manifests include every referenced artifact;
- signed deliverables have a current manifest or clearly state unsigned status;
- no client handoff includes internal-only notes.

Optional checks:

- every procedure maps to ATT&CK where appropriate;
- every IOC has confidence and sensitivity;
- every measurable event has expected and actual response fields;
- every detection opportunity has expected telemetry.

## User Flow

1. Operator generates reports and handoff artifacts.
2. Operator runs delivery QA.
3. Piranesi prints blocking errors and non-blocking warnings.
4. Operator fixes issues and reruns QA.
5. Delivery proceeds only after blockers are cleared.

## Build Slices

1. Add reusable QA result model.
2. Add checks for findings, evidence references, and handoff manifests.
3. Add CLI command with JSON output.
4. Add CI integration docs.
5. Add web UI QA summary.

## Acceptance Criteria

- QA can run on an empty workspace and returns useful warnings.
- QA fails on missing evidence references.
- QA flags high or critical findings without retest guidance.
- QA validates email handoff manifests.
- Tests cover pass, warning, and failure cases.

## Non-Goals

- Rewriting reports automatically.
- Sending reports.
- Blocking local experimentation.
- Replacing human review.
