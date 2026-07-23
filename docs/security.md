# Security

See the top-level [`THREAT-MODEL.md`](../THREAT-MODEL.md) for the visible summary. This document keeps extended implementation detail.

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
- Pairing tokens and web owner-cookie rows are encrypted at rest in SQLite with AES-GCM and row-scoped authenticated data.
- The SQLite encryption master key is stored in the OS credential backend where available, with a 0600 fallback file for hosts without Keychain or Secret Service.
- WebSocket connections require cookie auth and token auth.
- Approval decisions update pending rows atomically.
- Edited approval input must be valid JSON.
- Hook installers record SHA-256 hashes.
- `onibi doctor` reports hook drift and state permission problems.
- `onibi doctor` verifies the encrypted store key is present and can decrypt existing encrypted SQLite rows.
- `/control` actions operate on the hosted PTY process, not arbitrary system processes.

## At-Rest State

> **Linux fallback warning:** On Linux hosts without an active Secret Service daemon, such as minimal containers or headless servers without `gnome-keyring` or KWallet, Onibi's SQLite master key is stored as a base64 string in `<config-dir>/onibi/store.key` with 0600 permissions. This file is **not encrypted at rest**. A stolen powered-off disk exposes the key. Prefer running Onibi on hosts with an active credential store.

Onibi encrypts local SQLite values that are useful for pairing or browser takeover: pair tokens, owner-cookie material, and user-agent/device labels. Lookup uses non-secret SHA-256 indexes over high-entropy random tokens, while the original values are sealed through `internal/store.CryptBox` with per-row AAD (`table`, row id, column). Audit rows keep payload hashes instead of raw approval payloads.

The master key is `onibi.store.key.v1`. On macOS it is stored through the native Keychain backend provided by `99designs/keyring`; on Linux it uses Secret Service when available; otherwise Onibi stores a base64 key in `<config-dir>/onibi/store.key` with 0600 permissions. `onibi store rekey` rotates this key, re-seals encrypted rows, and revokes every web session with reason `store-rekey`. Active WebSocket sessions poll session validity and close with `store-rekey` after the next check instead of keeping old in-memory session stream keys alive. Run `onibi store rekey --dry-run` to see active web sessions, web session rows, and pairing token rows affected before rotating.

This is defense in depth, not protection from a fully compromised user account. A stolen powered-off laptop should not expose those encrypted SQLite values without the OS credential store or fallback key file. A stolen unlocked laptop, same-user malware, a debugger, swap, crash dumps, shell history, terminal scrollback, and the Onibi process memory while running can still leak live session data or decrypted values.

Keychain caveat: the `zalando/go-keyring` issue tracker documents macOS concerns around implementations that shell out to the `security` CLI, including issue #110. Onibi currently uses `99designs/keyring`'s native Keychain backend instead, but users should still treat the OS account boundary as the real trust boundary and should keep the Onibi binary signed/notarized for distribution builds.

## Non-Defenses

- Same-user local compromise. If your OS user account is owned, Onibi is owned.
- A stolen unlocked phone with an active paired browser session.
- Malicious coding agents or provider-side model behavior.
- Malicious commands you approve.
- Apple notarization, GitHub release, package registry, or Homebrew compromise.

## Setup Checklist

- [ ] Install only the trust file printed by your own `onibi up`: `onibi-local-ca.mobileconfig` on iPhone/iPad or `onibi-local-ca.crt` on Android.
- [ ] Enable full trust for that CA only when you intend to use the phone cockpit.
- [ ] Use a phone hotspot when managed Wi-Fi blocks local peer traffic.
- [ ] Verify hook commands with `./bin/onibi hooks --show --agent claude`.
- [ ] Review Claude `/hooks` before trusting Onibi hooks.
- [ ] Keep state dir permissions restricted.
- [ ] Stop `onibi up` when you are done.

## Reporting

Report vulnerabilities privately through GitHub Security Advisories for this repository. Do not open a public issue for a live exploit.
