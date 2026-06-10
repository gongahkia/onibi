# Security

Onibi gives a Telegram chat controlled access to local coding-agent sessions. Treat it like remote terminal control.

## Threat Model

| # | adversary | capability | mitigation |
|---|---|---|---|
| T1 | bot token thief | can impersonate the bot, consume updates, or set webhooks | OS keychain by default, `.env` fallback warning, log redaction, token rotation, doctor checks |
| T2 | owner Telegram account compromised | becomes the owner and can approve/inject | setup requires Telegram 2-step verification acknowledgement, approvals expire, optional TOTP/paranoid mode |
| T3 | local malware as same user | can read user files, Keychain after unlock, socket, hooks | out of scope; hook hash checks provide detection only |
| T4 | network MITM | can try to read/modify Telegram traffic | stdlib TLS, TLS 1.2 minimum, no proxy for `api.telegram.org` by default |
| T5 | first-message owner race | attacker pairs before real user | single-use deeplink pairing; no first-message ownership fallback |
| T6 | bot username typo/squat | phishing future users | setup suggests a random username suffix |
| T7 | stale approval hijack | approve old queued requests | approval TTL is enforced by the state machine |
| T8 | duplicate callback replay | repeat an old decision | terminal approval states are final and idempotent |
| T9 | hook tampering | redirect notify events or capture approval data | installed hook hashes are recorded and checked by `onibi doctor` |
| T10 | edited JSON abuse | change tool inputs through Telegram | JSON validation, audit log, short approval lifetime |
| T11 | Unix socket impersonation | inject fake local events | state dir/socket perms plus peer UID checks |
| T12 | token paste capture | clipboard/shoulder-surf token leak | `--token-stdin`, no argv token, immediate log scrubbing |
| T13 | token committed to a repo | public bot takeover | `.gitignore`, pre-commit template, keychain storage by default |
| T14 | SMS OTP interception | Telegram account takeover | setup recommends email recovery for Telegram 2-step verification |

## Enforcements

- Every Telegram update passes one owner check before handlers run.
- Pairing tokens are single-use, TTL-bound, and constant-time compared.
- Approval decisions are stored with atomic `WHERE state='pending'` transitions.
- Bot tokens are never accepted as positional args and are redacted from logs.
- The daemon uses outbound HTTPS to Telegram only; it does not expose an inbound network service.
- Unix socket/state paths are permission-checked by `onibi doctor`.
- Telegram send calls are rate-limited below Telegram's documented soft limits.
- Hook installers record SHA-256 hashes; `onibi doctor` reports mismatches.

## Non-Defenses

- Same-user local malware. If the user account is owned, Onibi is owned.
- Full compromise of the owner Telegram account.
- Telegram cloud confidentiality. Telegram can see bot messages.
- Apple notarization or GitHub release infrastructure compromise.
- Hosted agent API insider attacks.

## Setup Checklist

- [ ] Bot username has a random suffix.
- [ ] Bot token is in the OS keychain, not `.env`.
- [ ] Telegram 2-step verification is enabled.
- [ ] Telegram recovery uses email, not SMS.
- [ ] LaunchAgent/systemd service is loaded.
- [ ] State dir is `0700`; socket is `0600`.
- [ ] Hook hashes match `onibi doctor`.
- [ ] Bot token was rotated within the last 6 months.

## Reporting

Report vulnerabilities privately through GitHub Security Advisories for this repository. Do not open a public issue for a live exploit or token exposure.
