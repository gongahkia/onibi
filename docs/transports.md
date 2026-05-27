# Onibi Transports

Onibi starts its approval server on `127.0.0.1:17893`. Transports publish that same protocol through additional reachable URLs so the mobile PWA can approve requests when it is not running on the host Mac.

The pairing QR now includes:

```json
{
  "machine_id": "01J...",
  "token": "<bearer token>",
  "vapid_public_key": "<phase-03 vapid public key>",
  "transports": [
    { "name": "loopback", "url": "http://127.0.0.1:17893/" },
    { "name": "tailscale-funnel", "url": "https://host.tailnet.ts.net/" },
    { "name": "cloudflared", "url": "https://random.trycloudflare.com/" },
    {
      "name": "lan",
      "url": "https://192.168.1.42:17893/",
      "fingerprint": "sha256:..."
    }
  ]
}
```

The PWA should try transports in order and reconnect through the next working URL if one fails.

## CLI

```sh
onibi --headless
onibi transport list
onibi transport status
onibi transport enable tailscale-funnel
onibi transport enable cloudflared
onibi transport enable lan
onibi transport disable cloudflared
```

`enable` and `disable` talk to the running daemon so child processes and tunnel handles stay alive.

## Settings UI

The `TransportSettings` React component polls `/v1/transport/status` every five seconds and exposes toggles for:

- Tailscale Funnel
- Cloudflare Tunnel
- LAN

LAN also exposes a certificate QR button for the self-signed cert. The mobile device must trust that cert before browser HTTPS requests to the LAN URL will succeed.

## Tailscale Funnel

Best for users already on Tailscale and for phone-on-LTE to Mac-on-home-Wi-Fi demos.

Requirements:

- Tailscale CLI in `PATH`.
- Tailscale logged in: `tailscale up`.
- MagicDNS and HTTPS enabled for the tailnet.
- Funnel allowed by the tailnet policy.

Onibi runs:

```sh
tailscale status --json
tailscale funnel --bg 17893
```

The daemon reads `Self.DNSName` from `tailscale status --json` and publishes `https://<dns-name>/` in the QR payload. Disabling the transport runs `tailscale funnel reset`.

Reference: <https://tailscale.com/docs/features/tailscale-funnel>

## Cloudflare Tunnel

Best for quick public demos without a Cloudflare account. Cloudflare marks Quick Tunnels as testing/development infrastructure, so treat the generated URL as ephemeral.

Requirements:

- `cloudflared` in `PATH`.
- No conflicting `~/.cloudflared/config.yaml` for Quick Tunnel mode.

Onibi runs:

```sh
cloudflared tunnel --url http://127.0.0.1:17893 --no-autoupdate
```

The daemon parses the first `https://*.trycloudflare.com` URL from stderr and keeps the child process alive until the transport is disabled or the daemon exits.

Reference: <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/>

## LAN

Best when the phone and Mac are on the same Wi-Fi network.

On first use, Onibi creates:

```text
~/.config/onibi/lan.crt
~/.config/onibi/lan.key
```

The certificate includes `localhost`, `127.0.0.1`, and discovered private/link-local IPv4 addresses as SANs. The daemon starts a TLS listener on the selected LAN IP and proxies traffic to the loopback approval server. It also advertises `_onibi._tcp.local.` with mDNS.

Trust the certificate before pairing:

- iOS/iPadOS: transfer `lan.crt`, open it, install the profile, then enable full trust in Settings > General > About > Certificate Trust Settings.
- Android: install `lan.crt` as a user certificate under Security > Encryption & credentials. Browser behavior varies by Android version and browser.
- macOS: add `lan.crt` to Keychain Access and set it to Always Trust for testing.

Verify:

```sh
curl -k https://<lan-ip>:17893/healthz
```

The pairing payload includes the LAN URL and the SHA256 certificate fingerprint for display and manual verification.

## Comparison

| Transport | Account | Public LTE access | URL persistence | Notes |
| --- | --- | --- | --- | --- |
| Tailscale Funnel | Tailscale | Yes | Stable tailnet DNS | Best persistent option for Tailscale users. |
| Cloudflare Tunnel | No | Yes | Ephemeral | Good demos, generated URL changes on restart. |
| LAN | No | No | Stable while IP stays stable | Requires same Wi-Fi and trusted self-signed cert. |

## Troubleshooting

`tailscale CLI not found`

Install Tailscale and confirm `tailscale version` works in the same shell that launches Onibi.

`tailscale is not logged in`

Run `tailscale up`, then retry. If Funnel still fails, check MagicDNS, HTTPS, and node attributes in the Tailscale admin console.

`Tailscale Funnel quota or certificate limit reached`

Use Cloudflare or LAN temporarily. Tailscale can hit certificate or policy limits while repeatedly testing Funnel.

`cloudflared not found`

Install `cloudflared` and confirm `cloudflared --version` works. If Quick Tunnels refuse to start, temporarily move any existing `.cloudflared/config.yaml`.

`no non-loopback LAN IPv4 address found`

Join Wi-Fi or another LAN with a private IPv4 address. IPv6-only LANs are not explicitly supported in v1.5.

Browser certificate warning on LAN

Install and trust `~/.config/onibi/lan.crt` on the phone. A self-signed cert must be trusted by the OS before the PWA can use `fetch` or WebSocket over HTTPS.
