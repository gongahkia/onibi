# CI Lanes and Release Checklist

## Lane Ownership
- `core-required`: Maintainers for scan/detect/triage/verify core behavior.
- `critical-regressions`: Detector owners and security engineering reviewers.
- `security-regression`: Security owners for redaction and artifact hygiene.
- `extras-contract`: CLI/report/plugin API owners.
- `integration-optional`: Platform/infrastructure owners.

## Lane Definitions
- `core-required`
  - Purpose: deterministic merge gate for core quality.
  - Runs: release hygiene checks, metadata gates, lint, typecheck, and pytest with `not integration and not joern and not docker and not e2e and not slow`.
  - Merge policy: required and blocking.
- `critical-regressions`
  - Purpose: block known high-risk detector regressions.
  - Runs: targeted regression tests for wrapper and flow correctness.
  - Merge policy: required and blocking.
- `security-regression`
  - Purpose: block confidentiality/integrity regressions in redaction and evidence artifacts.
  - Runs: targeted redaction and artifact-hygiene tests.
  - Merge policy: required and blocking.
- `extras-contract`
  - Purpose: block contract drift in CLI/plugin/report surfaces.
  - Runs: environment marker policy check plus contract snapshot tests.
  - Merge policy: required and blocking.
- `integration-optional`
  - Purpose: signal infrastructure or environment regressions without masking core correctness.
  - Runs: marker-selected environment-bound suites (`integration`, `joern`, `docker`, `e2e`) when capabilities are available.
  - Merge policy: non-blocking (`continue-on-error`).

## Capability Preflight
- `scripts/ci_preflight.py` publishes:
  - `core_ready`
  - `integration_ready`
  - `joern_available`
  - `docker_available`
- Integration lane uses these outputs to run or skip environment-bound suites explicitly.

## Merge Gates
- A merge is blocked unless `core-required`, `critical-regressions`, `security-regression`, and `extras-contract` are green.
- `integration-optional` failures are visible but do not block merges.

## Release Checklist
1. Confirm all required lanes are green on the release commit.
2. Review `integration-optional` output and classify any failure as environment vs product.
3. Confirm contract snapshot tests are green; if intentional drift exists, update snapshots and changelog.
4. Confirm known-limitations registry checks are green and entries reflect current behavior.
5. Build distribution artifacts from the same validated commit.
