# Community Host Benchmarks

Community benchmarks are host evidence fixtures with ground-truth labels. They
help measure whether deterministic rules detect real posture issues without
inflating false positives.

For the complete contributor and maintainer checklist, see
[Community Contribution Workflow](community-contribution-workflow.md).

## Validate A Fixture

```bash
piranesi host fixture validate tests/fixtures/host/my-fixture
```

A fixture can be either a canonical `host_snapshot.json` bundle or a raw evidence
bundle using the existing host collector layout. If `ground_truth.json` is
present, Piranesi validates it with the host benchmark schema.

## Submit Metadata

```bash
piranesi host benchmark submit --fixture tests/fixtures/host/my-fixture
```

This command performs local validation only. It prints a submission metadata
payload containing:

- fixture path
- target name
- platform family
- evidence inventory
- expected finding count
- expected-absent count

No network call is made. Include the printed metadata in pull request context and
update `eval/host-community/index.json`.

## Ground Truth

`ground_truth.json` should include:

- `expected_findings` for findings that must appear
- `expected_absent` for findings that must not appear
- `allowed_extra` for known acceptable extra findings
- `clean_fixture = true` for intentionally clean bundles
- notes explaining coverage and false-positive boundaries

Prefer stable constraints such as `rule_id`, `instance_key`, and severity. Title
matching is useful as a fallback but should not be the only assertion for
security-critical benchmark cases.

## Review Criteria

Benchmark fixture pull requests should include:

- redacted evidence only
- no secrets, real hostnames, personal data, or production IP addresses
- ground-truth labels with false-positive notes
- platform support notes
- the output of `piranesi host fixture validate`
- any community rules that the fixture is meant to cover
