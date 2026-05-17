# Community Contribution Workflow

Community contributions should be deterministic, reviewable, and safe to run in
CI. Piranesi accepts data-only host rules, redacted host fixtures, policy
profiles, control mappings, and benchmark metadata. It does not accept arbitrary
remote rule execution or Python/shell plugins through the community rule path.

## Host Rule Checklist

Before opening a pull request:

- scaffold with `piranesi host rule scaffold "Rule title"`;
- keep the rule in `rules/community/host/`;
- use constrained TOML match blocks only;
- include remediation text, maintainer, platform support, fixture path,
  expected finding IDs, false-positive notes, and last validation date;
- update `rules/community/host/index.json` when adding or changing indexed
  rules.

Validation:

```bash
piranesi host rule test rules/community/host/my-rule.toml tests/fixtures/host/my-fixture
piranesi host rule test-all rules/community/host
```

Review criteria:

- the rule is deterministic and does not execute commands;
- the finding has a stable `rule.id`, clear severity, and actionable remediation;
- fixture coverage proves both the matching path and known safe exceptions where
  practical;
- any control mapping is confidence-scored and does not overclaim compliance.

## Fixture Checklist

Fixtures may be canonical `host_snapshot.json` bundles or raw collector-style
evidence bundles. They must be synthetic, redacted, and small enough for normal
CI.

Validation:

```bash
piranesi host fixture validate tests/fixtures/host/my-fixture
```

Review criteria:

- no real hostnames, production IPs, usernames, secrets, tokens, or private keys;
- `collection-manifest.json` documents collected and skipped evidence;
- `ground_truth.json` records expected findings, expected-absent findings,
  allowed extras, and false-positive notes;
- platform family and caveats are clear.

## Benchmark Submission Checklist

Community benchmark submissions should include validation output and index
metadata:

```bash
piranesi host benchmark submit --fixture tests/fixtures/host/my-fixture
python eval/host_benchmark.py --fixtures tests/fixtures/host --output /tmp/piranesi-host-benchmark
```

Review criteria:

- precision, recall, F1, and finding matrix output remain explainable;
- expected false positives or false negatives are documented rather than hidden;
- new distro coverage updates `eval/host-community/index.json` when it represents
  a community benchmark entry.

## Policy And Mapping Checklist

Policy profiles and control mappings should be conservative:

- include a named owner or maintainer;
- document the target environment and intended gate;
- use stable rule IDs and schema versions;
- include confidence and rationale for control mappings;
- avoid saying a mapping certifies compliance unless a formal review has happened.

## Pull Request Checklist

Run the narrow validation first, then the broader local gates relevant to the
change:

```bash
uv run pytest tests/test_host_rule_packs.py tests/test_host_benchmark.py
uv run piranesi host rule test-all rules/community/host
uv run piranesi host fixture validate tests/fixtures/host/debian-vulnerable
uv run piranesi host benchmark submit --fixture tests/fixtures/host/debian-vulnerable
uv run pytest
uv build
```

Maintainers should reject contributions that require hidden credentials, depend
on live infrastructure for normal CI, include sensitive evidence, or make
unsupported compliance claims.

