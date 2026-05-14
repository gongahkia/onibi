# TODO 15: Add Policy-As-Code Gates And Organization Profiles

## Goal

Let teams encode what "acceptable host posture" means for their environment.
Wide adoption requires Piranesi to support different risk tolerances without
hard-coding one security policy.

## Current State

Host assessment has deterministic rules and `--fail-severity`, but there is no
first-class policy language for host posture, exceptions, required evidence, or
environment-specific gates.

## Desired Behavior

Add policy files:

```toml
[host.policy]
profile = "production-linux"

[[host.policy.gates]]
id = "no-public-ssh-password-auth"
rule_id = "host.ssh.password_authentication"
when = "public_ssh"
max_severity = "low"
action = "fail"

[[host.policy.required_evidence]]
name = "trivy"
required = true
action = "warn"
```

Add CLI:

```bash
piranesi policy validate piranesi-policy.toml
piranesi assess piranesi-evidence --policy piranesi-policy.toml
piranesi fleet assess fleet-evidence --policy production.toml
```

## Policy Features

Support:

- Required evidence by environment.
- Severity/risk thresholds.
- Rule-specific gates.
- Allowed exposure by port/service.
- Suppression expiry requirements.
- Minimum score requirements.
- Fleet-level thresholds.

## Reporting

Reports should include:

- Policy profile name.
- Gate pass/fail summary.
- Failed gates with finding IDs.
- Evidence requirements status.

## Implementation Notes

Create:

```text
src/piranesi/host/policy.py
```

Add models:

```python
class HostPolicyGate(BaseModel): ...
class HostPolicyResult(BaseModel): ...
```

Policy evaluation must be deterministic and independent of LLM analysis.

## Tests

Add tests for:

- Policy parser validation.
- Required evidence warnings.
- Failures from high-risk public SSH.
- Suppressed findings do not fail unless policy disallows suppression.
- Fleet-level policy summary.

## Documentation

Add:

```text
docs/policy-as-code.md
examples/policies/
```

Include starter profiles:

- `lab.toml`
- `production-linux.toml`
- `airgapped.toml`

## Acceptance Criteria

- Users can define host posture gates in version-controlled policy files.
- Assessment reports include policy results.
- CI can fail based on policy, not only raw severity.
- Policies are validated before execution.

## Out Of Scope

- General-purpose scripting language.
- Remote policy server.
- Automatic exception approval workflows.

## Validation Commands

```bash
uv run pytest tests/test_host_policy.py tests/test_host_posture.py
uv run piranesi policy validate examples/policies/production-linux.toml
```

