# Release scenario matrix

Run the macOS release blocker:

```sh
make macos-scenario-gate
```

It writes `matrix.json`, `metadata.json`, `test.log`, and `summary.json` to
`artifacts/macos-scenario-gate`. A failed scenario remains in `summary.json` and
the gate exits non-zero.

| Scenario | Local macOS evidence | iPhone evidence |
| --- | --- | --- |
| Multi-host enrollment | signed owner enrollment, persisted node identity, hub link authentication, reconnect control | Pair each selected host as owner; verify its cockpit and fleet status. |
| LAN | lifecycle start, pair, loss, reconnect, shutdown | Safari: LAN, hotspot, isolated-Wi-Fi fallback. |
| Tailscale | lifecycle start, pair, loss, reconnect, shutdown | Safari over LTE with Funnel. |
| Tailscale private | lifecycle start, pair, loss, reconnect, shutdown | Authorized tailnet browser only. |
| WireGuard | lifecycle start, pair, loss, reconnect, shutdown | Authorized WireGuard peer only. |
| ZeroTier | lifecycle start, pair, loss, reconnect, shutdown | Authorized ZeroTier network only. |
| Cloudflare Quick | lifecycle start, pair, loss, reconnect, shutdown | Safari over LTE; E2E pair and terminal path. |
| Cloudflare Named | lifecycle start, pair, loss, reconnect, shutdown | Safari over LTE against configured hostname. |
| ngrok | lifecycle start, pair, loss, reconnect, shutdown | Safari over LTE against active HTTPS tunnel. |
| Session recovery | tmux restore, orphan retention, relay reattach, session-status replay | Background Safari, restore transport, confirm no duplicate input. |
| Approvals | Claude, Codex, Pi deny enforcement; WebSocket decision delivery | Trigger one approval; approve or deny reaches the host once. |
| Intervention | input, interrupt, handover, kill; duplicate command rejection | Send input or interrupt; confirm acknowledged command state. |

For every physical row, use `scripts/iphone-transport-smoke.sh` and retain its
0600 JSON artifact. It requires setup, pairing, approval, intervention,
reconnect, teardown, and failure-diagnostic outcomes. The local gate validates
the artifact contract only; it does not claim a real iPhone run occurred.
