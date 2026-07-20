# Threat Model

Onibi gives a paired phone browser control over local coding-agent sessions. Treat it like remote terminal access to your user account.

Detailed controls live in [`docs/security.md`](docs/security.md). Relay E2E details live in [`docs/SPEC-e2e.md`](docs/SPEC-e2e.md#threat-model).

## Adversaries

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
| T10 | Linux host without Secret Service | steals a powered-off disk and reads the SQLite master-key fallback file | not a defense; `<config-dir>/onibi/store.key` is 0600 but unencrypted at rest. Prefer an active credential store and full-disk encryption |
| T11 | cross-site requester on a relay hostname | tries to drive owner-only writes with ambient cookies | owner mutating routes require `X-Onibi-CSRF`, minted via `/session-info` and bound to the owner session |

## Caveats

- Chat providers such as Telegram and Slack can see message plaintext unless the provider path has real end-to-end encryption. Do not route terminal secrets or approval payloads through plaintext chat transports.
- Cloudflare relay E2E hides PTY bytes, typed input, approval payloads, and control bodies from Cloudflare, but relay metadata remains visible: host, timing, byte lengths, connection count, request paths, `session_id`, stream id, channel, direction, and sequence numbers.
- Hook failures are provider-specific. Blocking approval hooks fail closed when the provider honors non-zero exit or deny JSON. Notify-only hooks and providers that ignore hook failures can fail open; use `onibi hooks --show` and provider trust review to confirm behavior.

## Enforcements

- Pairing tokens are TTL-bound and single-use.
- Owner identity is stored in an HttpOnly Secure cookie.
- Pairing tokens and web owner-cookie rows are encrypted at rest in SQLite with AES-GCM and row-scoped authenticated data.
- The SQLite encryption master key is stored in the OS credential backend where available, with a 0600 fallback file for hosts without Keychain or Secret Service.
- WebSocket connections require cookie auth and token auth.
- Approval decisions update pending rows atomically.
- Edited approval input must be valid JSON.
- Hook installers record SHA-256 hashes.
- `onibi doctor` reports hook drift and state permission problems.
- `onibi doctor` verifies the encrypted store key is present and can decrypt existing encrypted SQLite rows.
- `/control` actions operate on the hosted PTY process, not arbitrary system processes.
- `onibi mcp` exposes local stdio tools through the same Unix socket and peer-UID checks as hooks.

## Non-Defenses

- Same-user local compromise. If your OS user account is owned, Onibi is owned.
- A stolen unlocked phone with an active paired browser session.
- Malicious coding agents or provider-side model behavior.
- Malicious commands you approve.
- Plaintext chat-provider delivery without E2EE.
- Relay traffic metadata.
- Hook surfaces that notify but cannot block provider execution.
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

Report vulnerabilities privately through GitHub Security Advisories:
<https://github.com/gongahkia/onibi/security/advisories/new>

Do not open a public issue for exploitable bugs, token leaks, auth bypasses, or terminal-injection paths.
