# TODO 4: Add Logs And Active Authentication Evidence

## Goal

Add host evidence support for logs and active/recent authentication state so Piranesi can assess brute-force exposure, stale sessions, privileged login patterns, and SSH risk with more context.

The proposal names logs and active auth sessions as part of the host evidence layer. Current Piranesi does not collect or analyze this surface.

## Current State

Relevant files:

- `src/piranesi/host/collect.py`
- `src/piranesi/host/ingest.py`
- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `src/piranesi/host/report.py`
- `tests/test_host_posture.py`
- `docs/host-posture.md`

Current evidence includes packages, ports, processes, users, services, SSH config, sudo evidence, firewall/update command evidence, and selected sysctls.

## Desired Evidence

Collect and ingest:

- Active login sessions:
  - `who`
  - `w`
  - osquery `logged_in_users` if available
- Recent successful logins:
  - `last -n 50`
  - osquery `last` equivalent if available on target
- Failed login summary:
  - `lastb -n 50` when permitted
  - auth log summaries from `/var/log/auth.log`, `/var/log/secure`, or journald
- SSH-specific auth failures:
  - failed password attempts by user/source IP
  - invalid users
  - root login attempts
- Sudo usage summary:
  - sudo log entries where available

Do not collect full raw logs by default. Collect compact summaries to reduce PII and secret leakage.

## Data Model

Add typed models:

```python
class LoginSession(BaseModel):
    username: str
    source: str | None = None
    tty: str | None = None
    started_at: str | None = None

class AuthEventSummary(BaseModel):
    event_type: Literal[
        "login_success",
        "login_failure",
        "ssh_failed_password",
        "ssh_invalid_user",
        "sudo_command",
    ]
    username: str | None = None
    source_ip: str | None = None
    count: int = 1
    first_seen: str | None = None
    last_seen: str | None = None
    evidence_source: str
```

Add to `HostSnapshot`:

```python
login_sessions: list[LoginSession] = Field(default_factory=list)
auth_event_summaries: list[AuthEventSummary] = Field(default_factory=list)
```

If schema churn is too large, store under `config["auth"]` first, but typed models are preferred.

## Collection

Add optional text commands in `collect.py`:

- `who_sessions`
- `w_sessions`
- `last_logins`
- `lastb_failures`
- `journalctl_sshd_auth_summary`
- `auth_log_sshd_summary`

Be careful with permissions:

- Missing or denied log access should be manifest warnings.
- Do not fail collection because auth logs are unavailable.
- Use timeouts.
- Keep command output bounded.

Suggested command forms:

- `who`
- `last -n 50`
- `lastb -n 50`
- `journalctl -u ssh --since -7 days --no-pager` or distribution-appropriate variants

If using `journalctl`, limit output. Do not dump unbounded logs.

## Redaction

Before writing raw auth summaries, redact:

- IP addresses only if configured? This is a tradeoff because IPs are useful evidence. At minimum, support a redaction mode.
- Hostnames in remote source fields if they look sensitive.
- Command arguments in sudo logs that may include secrets.
- Tokens, keys, passwords, and assignment-looking secrets.

Add a shared host redaction helper if todo7 has not already created one. If todo7 exists, use it.

## Deterministic Findings

Add findings such as:

- `host.auth.ssh_failed_password_spike`
  - high count of failed SSH password attempts.
  - severity medium or high if SSH is public and password auth is enabled.
- `host.auth.root_login_attempts`
  - root SSH login attempts detected.
- `host.auth.active_privileged_session`
  - active session for non-root privileged account.
  - severity informational or low unless combined with exposure.
- `host.auth.sudo_activity_present`
  - likely informational unless suspicious patterns are detected.

Avoid overclaiming. Log evidence is noisy and availability varies.

## Compound Risk

Use auth evidence to raise or add compound findings when multiple facts are present:

- Public SSH
- PasswordAuthentication yes
- Recent failed SSH attempts
- Privileged local accounts

This can become a `compound-risk` finding with high severity if evidence is strong.

## Reporting

Add to host metadata:

- `active_sessions_count`
- `auth_event_summary_count`
- `failed_ssh_attempt_count`

Add an "Auth Evidence" section to Markdown/PDF/dashboard output if todo1 exists. If todo1 has not landed, update Markdown only.

## Tests

Fixtures:

```text
tests/fixtures/host/auth-evidence/
  raw/commands/who_sessions.json
  raw/commands/last_logins.json
  raw/commands/lastb_failures.json
  raw/commands/journalctl_sshd_auth_summary.json
```

Tests:

- Parses active sessions.
- Parses failed SSH attempts.
- Missing auth log access creates collection health warning, not a finding.
- Public SSH plus password auth plus failed attempts creates compound risk.
- Redaction removes secrets from sudo/auth command excerpts.

## Acceptance Criteria

- Host snapshots can represent active sessions and auth summaries.
- Collection can gather bounded auth evidence when permitted.
- Deterministic assessment produces evidence-backed auth findings.
- Reports expose auth evidence counts and related findings.
- Existing host tests still pass.

## Out Of Scope

- Full SIEM ingestion.
- Long-term log retention.
- User behavior analytics.
- Remote log collection.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py
uv run piranesi collect --output /tmp/piranesi-auth-evidence --no-trivy
uv run piranesi assess /tmp/piranesi-auth-evidence --output /tmp/piranesi-auth-out
```

