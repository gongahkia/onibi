# TODO 20: Build Community Rule, Fixture, And Benchmark Ecosystem

## Goal

Encourage wide adoption by making it easy for users to contribute host checks,
fixtures, policies, mappings, and benchmark cases without changing core code.

Piranesi should become a practical local-first posture workbench with a growing
community knowledge base.

## Current State

The repository already has custom rule-pack ideas for the legacy source-code
pipeline and host fixtures for Debian clean/vulnerable cases. Host posture does
not yet have a mature community contribution path.

## Desired Behavior

Add host-oriented contribution workflows:

```bash
piranesi host rule scaffold "Disable risky service"
piranesi host rule test rules/community/host/my-rule.toml tests/fixtures/host/my-fixture
piranesi host fixture validate tests/fixtures/host/my-fixture
piranesi host benchmark submit --fixture tests/fixtures/host/my-fixture
```

## Contribution Types

Support:

- Host deterministic rule packs.
- Host evidence fixtures.
- Ground-truth benchmark labels.
- Policy profiles.
- Control mappings.
- Remediation templates.
- Platform support adapters.

## Rule Pack Shape

Example:

```toml
[rule]
id = "community.ssh.disable-password-auth"
title = "SSH password authentication should be disabled"
category = "ssh"
severity = "medium"

[[match]]
evidence = "config.ssh.passwordauthentication"
equals = "yes"

[remediation]
text = "Set PasswordAuthentication no and restart sshd after validating key access."
```

Keep the first version intentionally constrained. Avoid arbitrary code execution
in community rules.

## Quality Gates

Community contributions should include:

- Fixture evidence.
- Expected finding IDs.
- False-positive notes where relevant.
- Documentation link.
- Mapping confidence when controls are referenced.

Add validation commands that can run in CI.

## Registry

Create:

```text
rules/community/host/
examples/policies/community/
eval/host-community/
```

Add an index file that records:

- Rule ID.
- Maintainer.
- Platform support.
- Fixture coverage.
- Last validation date.

## Tests

Add tests for:

- Rule pack schema validation.
- Rule execution against fixture bundles.
- Fixture validation.
- Benchmark metadata validation.
- Community rules cannot execute shell commands or import Python.

## Documentation

Add:

```text
docs/contributing-host-rules.md
docs/community-benchmarks.md
```

Include a contributor guide with examples and review criteria.

## Acceptance Criteria

- Users can add host checks without modifying core code.
- Community rules are constrained, testable, and safe.
- Fixtures and benchmark labels improve product credibility over time.
- CI can validate community contributions.

## Out Of Scope

- Marketplace payments.
- Unreviewed remote rule execution.
- Arbitrary Python plugin execution for host checks.

## Validation Commands

```bash
uv run pytest tests/test_host_rule_packs.py tests/test_host_benchmark.py
uv run piranesi host fixture validate tests/fixtures/host/debian-vulnerable
uv run piranesi host rule test-all rules/community/host
```

