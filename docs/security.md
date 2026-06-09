# Security

Onibi is local-first software that exposes an approval protocol to devices you pair. Treat access to the bearer token as equivalent to access to approve, deny, or edit local agent actions.

## Trust Boundaries

Onibi has four important boundaries:

| Boundary | Assets | Default control |
| --- | --- | --- |
| Local host process | PTY output, approval requests, SQLite state | Loopback server, OS user permissions |
| Adapter hooks | Agent tool payloads, long-poll decisions | Bearer token and schema validation |
| Mobile PWA | Pending approvals, terminal mirror, decisions, remote pane input | Pairing QR, bearer token, HTTPS transport |
| Public transport | Tailscale Funnel, Cloudflare Tunnel, LAN TLS | TLS plus mandatory bearer token |

The v1.5 launch deliberately avoids accounts, hosted relay infrastructure, and telemetry. There is no Onibi cloud service.

## Threat Model

### Adversary On The Local Network

A device on the same Wi-Fi can discover or guess the LAN host and attempt to call the HTTPS listener. LAN mode uses a self-signed certificate and publishes the SHA256 fingerprint in the pairing payload; the mobile device still has to trust the certificate at the OS level before browser HTTPS succeeds. Every state-changing endpoint also requires `Authorization: Bearer <token>`.

Risk that remains: if the full token is copied from the QR, shell history, screenshots, or `~/.config/onibi/token.txt`, the attacker can call the API until the token is rotated.

### Read-Only Spectator Tokens

`onibi token spectator` creates a one-time pairing payload with `scope = "read-only"`. The token can pair one device once, then remains valid only for read endpoints and realtime observation. Read-only tokens can fetch pending/history/status data and subscribe to `/v1/realtime`, but the server rejects decisions, emergency stop, remote pane input, PTY/control, hook, and config mutation routes with `403 Forbidden`.

### Adversary On The Public Internet

Tailscale Funnel and Cloudflare Tunnel publish the local daemon to a public HTTPS URL. The URL is not treated as a secret. Tailscale names are per tailnet and Cloudflare Quick Tunnel names are random, but the real control is the bearer token. The daemon rejects unauthenticated API and WebSocket calls.

The launch hardening pass adds:

- `Strict-Transport-Security` on tunnel-bound HTTPS responses.
- A 10 request/second per-source-IP limiter on `POST /v1/approval/request`.
- Body-size limits for high-risk JSON endpoints.
- A 256 KB WebSocket message cap.

### Malicious Local App On The Same Machine

The preferred token store is the OS keychain: macOS Keychain, Linux Secret Service, or the platform backend exposed through the `keyring` crate. If no keychain is available, Onibi falls back to `~/.config/onibi/token.txt` with mode `0600`.

Fallback file storage is a supportable Pi/headless compromise, not the best security posture. On multi-user hosts, use a real keychain, keep the Onibi config directory owned by the daemon user, and rotate the token after demos.

### Malicious Agent Or Hook Injection

Adapters can POST arbitrary JSON. Onibi does not evaluate or execute any received payload. It stores payloads as JSON, validates protocol envelopes before accepting requests, and only returns structured decisions. The PTY manager and transport modules are separate from the approval store; approval JSON is never interpreted as shell code by the daemon.

Risk that remains: a legitimate user can approve a dangerous tool call. Onibi is a gate, not a sandbox.

### Remote Pane Input

The PWA remote-input surface writes literal bytes to an existing Onibi PTY. `send-text` never assembles shell commands; it writes the submitted text and an optional Enter byte. `send-keys` accepts only server-owned preset keys from the closed allowlist. Destructive presets and `approval-required` sessions require an explicit confirmation before dispatch. Every accepted dispatch is recorded in the approval audit log as `RemoteKeystroke` with pane/session target, payload, token fingerprint, trust mode, and timestamp.

Preset keys are configured on the daemon, not supplied by the client:

```toml
[[remote_control.presets]]
key = "interrupt"
label = "Interrupt"
keys = ["Ctrl+C"]
destructive = true
```

## Implemented Hardening

| Control | Status | Implementation |
| --- | --- | --- |
| Bearer token required | Done | `app/src-tauri/src/server/auth.rs` |
| Approval rate limit | Done | `tower_governor`, 10 burst / 100 ms refill on `/v1/approval/request` |
| Approval body limit | Done | 1 MB `DefaultBodyLimit` |
| PTY output body limit | Done | 5 MB `DefaultBodyLimit` |
| WebSocket message limit | Done | 256 KB frame/message limit on `/v1/realtime` |
| HSTS | Done | Tunnel-bound responses only; LAN self-signed flow is excluded |
| PWA CSP | Done | Header for `/m/*` and meta CSP in `mobile/index.html` |
| Read-only spectator scope | Done | One-time spectator pairing plus route-level mutation block |
| Remote pane input safety | Done | Literal-only text route, preset allowlist, confirmation gate, audit rows |
| Release HTTP errors | Done | Internal error bodies are generic in non-debug builds |
| Token fallback permissions | Done | File fallback written `0600` on Unix |

## Hardening Checklist

Before tagging a release:

- Run `cargo audit` from `app/src-tauri`.
- Run `pnpm audit --prod` at the repo root.
- Run `cargo test --workspace` from `app/src-tauri`.
- Run `pnpm --filter onibi-app test` and `pnpm --filter onibi-mobile test`.
- Confirm `onibi doctor` reports no failed checks on macOS, Linux, and Pi.
- Rotate the bearer token after public demos: `onibi token rotate`.
- Remove stale Cloudflare Quick Tunnel URLs from docs, screenshots, and issue comments.
- Confirm the LAN certificate fingerprint shown in the pairing UI matches the phone trust profile.

## Audit Log

Audit commands are recorded in `docs/dry-run-log.md` during the final QA pass. High and critical CVEs block the v1.5.0 tag.

## Disclosure

Report security issues privately through GitHub Security Advisories if enabled on the repository. If advisories are not available, open a minimal issue asking for a private contact and do not include exploit details in the public thread.
