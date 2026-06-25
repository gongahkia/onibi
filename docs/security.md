# Security

Onibi gives a paired phone browser control over local coding-agent sessions. Treat it like remote terminal access to your user account.

## Threat Model

| # | adversary | capability | mitigation |
|---|---|---|---|
| T1 | local network attacker | probes the web server or tries to steal a pair URL | HTTPS, short-lived single-use pair tokens, owner cookie, WebSocket token checks |
| T2 | untrusted Wi-Fi | blocks or interferes with device-to-device traffic | hotspot fallback, local-only server, QR regenerated per run |
| T3 | stolen paired phone | can use an active owner browser session | stop `onibi up`, clear browser data, rotate local state if needed |
| T4 | same-user local malware | can read files, socket, hooks, and agent output | out of scope; hook hashes and doctor checks are detection only |
| T5 | hook tampering | redirects approval data or bypasses Onibi | hook hashes, `hooks --show`, provider trust review |
| T6 | stale approval replay | repeats an old decision | terminal approval states are final and idempotent |
| T7 | edited JSON abuse | changes tool inputs before approval | JSON validation, provider schemas where available, audit records |
| T8 | Unix socket impersonation | injects fake local events | state dir/socket perms plus peer UID checks |
| T9 | local CA misuse | user trusts the wrong certificate profile | profile is generated locally; install only the path printed by `onibi up` |

## Enforcements

- Pairing tokens are TTL-bound and single-use.
- Owner identity is stored in an HttpOnly Secure cookie.
- WebSocket connections require cookie auth and token auth.
- Approval decisions update pending rows atomically.
- Edited approval input must be valid JSON.
- Hook installers record SHA-256 hashes.
- `onibi doctor` reports hook drift and state permission problems.
- `/control` actions operate on the hosted PTY process, not arbitrary system processes.
- `onibi mcp` exposes local stdio tools through the same Unix socket and peer-UID checks as hooks.

## Non-Defenses

- Same-user local compromise. If your OS user account is owned, Onibi is owned.
- A stolen unlocked phone with an active paired browser session.
- Malicious coding agents or provider-side model behavior.
- Malicious commands you approve.
- Apple notarization, GitHub release, package registry, or Homebrew compromise.

## Setup Checklist

- [ ] Install only the `onibi-local-ca.mobileconfig` printed by your own `onibi up`.
- [ ] Enable full trust for that CA only when you intend to use the phone cockpit.
- [ ] Use iPhone hotspot when managed Wi-Fi blocks local peer traffic.
- [ ] Verify hook commands with `./bin/onibi hooks --show --agent claude`.
- [ ] Review Claude `/hooks` before trusting Onibi hooks.
- [ ] Keep state dir permissions restricted.
- [ ] Stop `onibi up` when you are done.

## Reporting

Report vulnerabilities privately through GitHub Security Advisories for this repository. Do not open a public issue for a live exploit.
