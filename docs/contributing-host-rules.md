# Contributing Host Rules

Community host rules let contributors add deterministic checks without changing
Piranesi core code. The format is constrained TOML data, not Python, shell, or a
plugin runtime.

The community host rule TOML format is stable-alpha. See
[Stability Guarantees](stability.md) for the additive-field, schema-version, and
deprecation policy.

For the full rule, fixture, benchmark, policy, mapping, and review checklist,
see [Community Contribution Workflow](community-contribution-workflow.md).

## Create A Rule

```bash
piranesi host rule scaffold "Disable risky service"
```

This writes a template under `rules/community/host/`. A minimal rule looks like:

```toml
[rule]
id = "community.ssh.password-authentication-enabled"
title = "SSH password authentication should be disabled"
category = "ssh"
severity = "medium"
confidence = 0.9
platform_support = ["debian", "ubuntu"]

[[match]]
evidence = "config.ssh.PasswordAuthentication"
equals = "yes"

[remediation]
text = "Set PasswordAuthentication no after confirming key-based access."

[metadata]
maintainer = "your-github-handle"
fixture = "tests/fixtures/host/debian-vulnerable"
expected_finding_ids = ["host-c4e5e125f8bbe7e4"]
false_positive_notes = "Document compensating controls or safe exceptions."
last_validation_date = 2026-05-14
```

## Match Language

Rules can only inspect normalized host snapshot fields using dotted evidence
paths. Supported operators are:

- `equals`
- `not_equals`
- `contains`
- `exists`
- `in`

Multiple `[[match]]` blocks are combined with logical AND. List fields are
searched element-by-element, so `packages.name = "redis-server"` can match any
package in the normalized inventory.

Unsafe keys such as `shell`, `command`, `python`, `import`, `exec`, `eval`, and
`subprocess` are rejected before model validation. Rules never execute external
commands.

## Test Locally

```bash
piranesi host rule test rules/community/host/my-rule.toml tests/fixtures/host/my-fixture
piranesi host rule test-all rules/community/host
```

`test-all` requires each committed rule to declare `metadata.fixture`. If
`metadata.expected_finding_ids` is present, generated finding IDs must match.

## Review Criteria

Contributions should include:

- a constrained TOML rule under `rules/community/host/`
- a fixture or existing fixture reference
- expected finding IDs
- false-positive notes
- remediation and verification text
- documentation link when available
- community index update with maintainer, platform support, fixture coverage, and
  last validation date

Control mappings should include a confidence value and avoid claiming exact
framework coverage unless the mapping has been reviewed.
