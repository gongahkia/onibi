# Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Settings shows "Port 8787 is already in use" | Another process holds the port (often a stale `swift run` or a previous Onibi) | Pick a different port in Settings, or `lsof -iTCP:8787 -sTCP:LISTEN` to find + kill the holder |
| Settings shows "Permission denied binding port …" | Trying to bind below 1024 without privileges | Pick a port ≥ 1024 |
| Phone says "Host request failed (connection refused)" | Gateway bind mode is Loopback but phone is on LAN | Switch bind mode to LAN in Settings, or use a tunnel |
| Phone says "Pairing token rejected" | Token rotated on Mac since phone saved it | Tap **Clear Saved Connection** on phone, re-paste latest token or rescan QR |
| Realtime stays in "connecting" forever on LAN | macOS firewall blocking inbound | System Settings → Network → Firewall → allow incoming for the Onibi binary |
| Works on Mac `http://localhost` but not on phone LAN | Bind mode still Loopback | Switch to LAN; rescan interfaces |
| Page on `https://…` can't talk to `http://…` API | Browser mixed-content block | Use tunnel mode (HTTPS) for the API, or serve the SPA over HTTP too |
| WebSocket closes with code 1006 | Network hiccup / tunnel idle timeout | Client auto-reconnects with backoff. Check Debug drawer for the last close reason |
| WebSocket closes with code 1008 `unauthorized` | Token invalid | Re-pair; check redacted token preview in Mac logs matches what phone sent |
| `cloudflared --url` errors about certs | `cloudflared` version too old | `brew upgrade cloudflared` |
| Vite dev server only reachable on `localhost` | Started without `--host` | `npm run dev -- --host 0.0.0.0` |
| LAN IPs empty in Settings | Mac has no active non-loopback IPv4 (Wi-Fi off, Ethernet down) | Connect to a network; click **Rescan interfaces** |

## Log channels

macOS app emits structured `os.log` under subsystem `com.onibi.gateway` with these categories:

- `bind` — listener state / bind errors
- `auth` — 401s, token rotations (token value redacted to first 4 chars + length)
- `http` — HTTP method/path/status for each request
- `ws` — WebSocket upgrades and close events

View in Console.app: **Filter by subsystem: `com.onibi.gateway`**, or via CLI:

```zsh
log stream --predicate 'subsystem == "com.onibi.gateway"' --level debug
```

## Web debug drawer

The web UI has a **Debug** drawer (bottom-right). Click to expand: shows realtime connection state, reconnect attempts, last WebSocket close code, and the most recent 200 client events. Click **Copy** to grab everything as plain text — paste into a bug report.
