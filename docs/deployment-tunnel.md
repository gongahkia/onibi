# Deployment — Tunnel mode

Use when your phone is **not on the same network** as the Mac, or when you need a secure context (HTTPS) for browser features.

## How it works

The Mac gateway keeps binding to loopback (`127.0.0.1`) — your tunnel daemon forwards traffic from a public `https://` URL to that loopback port. The phone talks to the tunnel, the tunnel talks to the gateway, both HTTP and WebSocket flow through.

```
phone ─ https ─▶ tunnel edge ─ https ─▶ cloudflared/ngrok on Mac ─ http ─▶ 127.0.0.1:8787
```

## 1. Configure the gateway

In Onibi → Settings → Mobile Access:

1. **Enable mobile gateway** and **Enable remote control**.
2. **Network Binding** → **Loopback only** (the tunnel is the only ingress you need).
3. **Rotate Token**.

## 2. Run a tunnel

Pick one:

### Cloudflare Quick Tunnel (no account)

```zsh
brew install cloudflared
cloudflared tunnel --url http://127.0.0.1:8787
```

Copy the `https://<random>.trycloudflare.com` URL from the output.

### Cloudflare Named Tunnel (stable URL)

```zsh
cloudflared login
cloudflared tunnel create onibi
# add to ~/.cloudflared/config.yml per Cloudflare docs
cloudflared tunnel route dns onibi onibi.yourdomain.com
cloudflared tunnel run onibi
```

URL becomes `https://onibi.yourdomain.com` and survives restarts.

### ngrok

```zsh
brew install ngrok
ngrok config add-authtoken <your-token>   # one-time
ngrok http 8787
```

Free tier gives a rotating `https://<random>.ngrok-free.app`. Paid plan supports reserved domains.

### Tailscale Funnel

Onibi already ships Tailscale-aware helpers — see the **Tailscale** section of Settings. Click **Enable Tailscale Serve** after `tailscale up` / `tailscale funnel 8787` from the CLI.

## 3. Register the URL in Onibi

Paste the tunnel's HTTPS URL into Settings → **Tunnel (optional) → Public URL**.

Benefits:
- The QR / deep link will prefer the tunnel URL when present.
- Diagnostics and advertised URL list include it.

## 4. Connect from the phone

1. Open the same tunnel URL (for the web UI) **or** keep the web UI on Vite preview + use the tunnel only for the API Base URL.
2. In the Connection screen: pick **Tunnel**, paste the Base URL, paste the token.

### One-tunnel vs two-tunnel setup

- **One tunnel (simpler):** point the tunnel at `127.0.0.1:8787` only. Host the web UI somewhere else (GitHub Pages, Vercel, or the Mac's Vite preview over LAN). Phone loads the SPA from that origin and talks to the tunnel for the API.
- **Two tunnels:** run a second tunnel for the Vite preview / static `dist` server. Phone loads SPA and API from HTTPS origins.

## Security notes

- The pairing token is your **only** authentication. Rotate it after each trial and when ending a session.
- Tunnel URLs get logged by the provider. Don't paste them into public channels.
- Free-tier tunnels enforce rate and bandwidth limits; long-lived WebSockets may be dropped after several hours.
- See [security.md](./security.md) for the threat model.
