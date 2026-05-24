> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Policy As Code

Piranesi host policies let teams define deterministic pass/fail gates for host
posture reports. Policies are TOML files that can be committed with application
or infrastructure code and applied during `assess` or `fleet assess`.

Policy evaluation is deterministic and independent of LLM analysis. Suppressions
are applied before policy evaluation. By default, suppressed findings do not fail
policy gates unless the policy sets `allow_suppressed = false`.

## Validate A Policy

```bash
piranesi policy validate examples/policies/production-linux.toml
```

## Apply To One Host

```bash
piranesi assess piranesi-evidence \
  --output piranesi-output \
  --policy examples/policies/production-linux.toml
```

## Apply To A Fleet

```bash
piranesi fleet assess fleet-evidence \
  --output fleet-output \
  --policy examples/policies/production-linux.toml
```

Assessment exits `1` when policy fails unless `--no-fail` is set. Reports still
write before the process exits.

## Policy Shape

```toml
[host.policy]
profile = "production-linux"
minimum_score = 85
max_severity = "medium"
max_risk = 75
allow_suppressed = true
suppression_expiry_required = true

[host.policy.fleet]
max_failed_hosts = 0
max_policy_failures = 0
minimum_passing_hosts_percent = 100

[[host.policy.required_evidence]]
name = "trivy"
required = true
action = "fail"

[[host.policy.gates]]
id = "no-public-ssh-password-auth"
rule_id = "host.ssh.password_authentication"
when = "public_ssh"
max_severity = "low"
action = "fail"
```

## Supported Gates

Global policy fields:

- `minimum_score`: fail when the posture score is below this value.
- `max_severity`: fail when any unsuppressed finding is above this severity.
- `max_risk`: fail when any unsuppressed finding has a risk score above this value.
- `allow_suppressed`: when `false`, any suppressed finding fails policy.

Rule gates support:

- `rule_id`: match a specific deterministic rule ID.
- `category`: match a finding category such as `exposure` or `coverage`.
- `when = "always"`: evaluate whenever the matching finding exists.
- `when = "public_ssh"`: evaluate only when public SSH exposure exists.
- `when = "public_listener"`: evaluate public listener findings.
- `max_severity`: fail or warn when matching findings are above this severity.
- `max_risk`: fail or warn when matching findings exceed this risk score.
- `action = "fail"` or `action = "warn"`.

Allowed exposure entries can permit known listener exposure by port or service:

```toml
[[host.policy.allowed_exposure]]
port = 22
reason = "SSH is permitted in this lab network."
```

## Required Evidence

Required evidence checks verify that expected evidence is present in the report:

```toml
[[host.policy.required_evidence]]
name = "trivy"
required = true
action = "warn"
```

Common evidence names include:

- `trivy`
- `users`
- `firewall`
- `updates`
- `sysctl`
- `auth_evidence`
- `network`
- `listening_ports`

Missing evidence can warn or fail depending on `action`.

## Report Fields

Host and fleet JSON reports include:

- `policy_profile`
- `policy_summary`
- `policy_gate_results`
- `required_evidence_status`

Markdown reports include a `Policy` section with the same pass/fail summary,
failed gate IDs, affected finding IDs, and evidence requirement statuses.

## Starter Profiles

Starter policies live under `examples/policies/`:

- `lab.toml`: permissive lab evaluation with warnings.
- `production-linux.toml`: stricter production Linux gates.
- `airgapped.toml`: offline-friendly profile that warns on missing optional
  scanner evidence.
