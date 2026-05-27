# Security model

Onibi's mobile gateway gives a remote client the ability to **read terminal output** and **inject keystrokes** into running Ghostty sessions. Treat that as equivalent to handing over your shell.

## Auth

The only credential is the **pairing token** — a random high-entropy string stored in the macOS Keychain. Every HTTP request must send `Authorization: Bearer <token>` and every WebSocket must send an `{"type":"auth","token":"..."}` frame first.

Rotate the token (Settings → Pairing → **Rotate Token**):

- after granting access to a new device
- after any tunnel or QR leaks to a non-trusted channel
- on a routine cadence if the mobile gateway is left enabled

Old tokens are invalidated immediately; in-flight WebSockets get a `1008 unauthorized` close.

## Bind modes

| Mode | Reachable from | Recommended when |
| --- | --- | --- |
| **Loopback** | Only this Mac | You're pairing it with a tunnel (Cloudflare, ngrok, Tailscale Funnel) |
| **LAN** | Same Wi-Fi / Ethernet | Quick phone tests on a network you control |
| **All interfaces** | Every bound interface including VPNs | Rarely — only on trusted networks |

Even in LAN mode the gateway serves **plaintext HTTP** on that network. Anyone who sniffs the Wi-Fi can see the token. Don't use LAN mode on public or guest Wi-Fi.

## Tunnel threat model

The tunnel provider (Cloudflare, ngrok, Tailscale) terminates TLS on its edge and forwards plaintext to the Mac over its own encrypted transport. Implications:

- Provider can read request URLs and bodies; trust accordingly.
- The tunnel URL is effectively public. Anyone who obtains it can hammer the login endpoint.
- Rate-limit protection is provided by the tunnel (Cloudflare/ngrok) — Onibi itself does not throttle.
- Scoping: prefer **named tunnels with auth** (Cloudflare Access, ngrok OAuth) if you leave the tunnel running long-term. Quick tunnels are fine for ad-hoc use.

## What the gateway does *not* have

- TLS termination. There is no built-in HTTPS. Tunnels provide HTTPS externally.
- Per-user identity. The token is shared across all paired clients.
- Audit trail of issued commands. Keystrokes are forwarded and not individually persisted. Session output is buffered only in-memory (`sessionOutputBufferLineLimit`).
- Rate limiting / brute-force protection on the token.

## Recommended posture

1. **Loopback bind + Cloudflare Quick Tunnel** for short-lived sessions.
2. Rotate token when you stop using it.
3. Watch Console.app / `log stream` for `com.onibi.gateway` auth failures — repeated 401s imply a scanner has found your URL.
4. Kill the tunnel process (`cloudflared` / `ngrok`) the moment you're done. The gateway alone on loopback is safe.

## Known limitations

- `/api/v2/health` is intentionally unauthenticated so tunnels can liveness-probe. It returns `{"status":"ok"}` only — no sensitive state.
- The WebSocket auth token travels in the first frame, not the URL. However, some reverse proxies log WebSocket upgrade headers — verify your provider's behavior.
