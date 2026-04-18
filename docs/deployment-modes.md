# Deployment modes

The Onibi gateway runs on your Mac. To reach it from a phone or remote laptop, you pick how the gateway is exposed. Three supported modes, ranked from least to most setup:

| Mode | Reachable from | HTTPS | Stable URL | Setup |
| --- | --- | --- | --- | --- |
| **LAN** | Same Wi-Fi / Ethernet | ❌ (plain HTTP) | IP-dependent | Zero |
| **Cloudflare Quick Tunnel** | Anywhere | ✅ | ❌ (rotates) | One-click in-app |
| **Tailscale Funnel** | Anywhere | ✅ | ✅ (per device) | One-time tailnet setup |

Pick based on where you are and whether you need a stable URL.

---

## 1. LAN mode

**Use when:** phone and Mac are on the same trusted Wi-Fi.

**Setup:**
1. Onibi → Settings → Mobile Access → **Network Binding: LAN (same Wi-Fi)**.
2. Copy the `http://192.168.x.x:<port>` URL shown in the settings.
3. On the phone, open the deployed web UI (or serve it locally with `npm run dev -- --host 0.0.0.0` in `OnibiWeb/`).
4. Pick the **LAN** tab in the connect screen. Paste URL + token.

**Caveats:**
- The gateway serves **plain HTTP** on LAN. Anyone on the network who sniffs Wi-Fi can read the token. Don't use on public or guest Wi-Fi.
- If you're hosting the web UI on HTTPS (Vercel / Pages), browsers **block** mixed-content HTTP requests. Either serve the web UI over `http://` too, or use a tunnel mode.
- macOS firewall may block inbound LAN traffic even after binding succeeds — the app surfaces a hint with a link to System Settings when bind mode is LAN/all.

---

## 2. Cloudflare Quick Tunnel (one-click in-app)

**Use when:** you want HTTPS and public reachability without a Cloudflare account.

**Setup (one-time):**
1. Install Homebrew from [brew.sh](https://brew.sh) if you don't have it.
2. Onibi → Settings → Mobile Access → **Cloudflare Tunnel** → click **Install cloudflared**. The app runs `brew install cloudflared` for you and streams output.

**Per-session:**
1. Toggle **Enable mobile gateway** (keep bind mode **Loopback** — the tunnel handles the public exposure).
2. Click **Start Quick Tunnel**. The public URL (e.g. `https://foo-bar-baz.trycloudflare.com`) appears in the settings and is automatically written into the pairing QR payload.
3. Scan the QR or copy the deep link to the phone → connect.

**Caveats:**
- Quick Tunnels are **ephemeral** — the URL changes every time `cloudflared` restarts. Fine for ad-hoc use, painful for daily use. For a stable URL, use Tailscale Funnel (below) or upgrade to a [named tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/get-started/create-local-tunnel/) (not wizard-supported in-app — do it manually).
- Cloudflare terminates TLS on their edge and forwards plaintext to your Mac over their own transport. Cloudflare can read request bodies.
- The tunnel stops automatically when you disable the mobile gateway. Also verify by running `pgrep -fl cloudflared`.

---

## 3. Tailscale Funnel (stable public URL)

**Use when:** you want a permanent URL you can bookmark, and you're comfortable running Tailscale.

**One-time tailnet setup** (done once per tailnet, not once per device):
1. Install Tailscale from [tailscale.com](https://tailscale.com) and sign in on the Mac.
2. In your tailnet admin console, enable Funnel for this node in your ACL policy:
   ```jsonc
   // tailnet policy file
   {
     "nodeAttrs": [
       {
         "target": ["autogroup:admin"],
         "attr":   ["funnel"]
       }
     ]
   }
   ```
   (The exact policy depends on your org — see [Tailscale Funnel docs](https://tailscale.com/kb/1223/funnel).)
3. Confirm Funnel is allowed:
   ```
   tailscale funnel status
   ```

**Per-session:**
1. Onibi → Settings → Mobile Access → Tailscale → **Enable Tailscale Funnel (public)**.
2. The public URL is auto-populated into the pairing payload (e.g. `https://your-mac.tailnet.ts.net`).
3. Scan / paste on the phone.

**Caveats:**
- Funnel URLs are bound to your tailnet DNS name — if you rename the node, the URL changes.
- Funnel exposes the port on **port 443** by default. If another service is already funneled on 443 for this node, you'll collide — `tailscale funnel reset` clears all funnel rules.
- If the Enable button fails with a policy error, your tailnet ACL doesn't permit Funnel. Surface is via the `lastError` field in Mobile Access settings.

---

## When to use what

- **Home, same Wi-Fi**: LAN. Simplest.
- **Travelling, one-off test**: Cloudflare Quick Tunnel. One-click, rotates.
- **Daily driver, stable URL**: Tailscale Funnel. Set once, forget.
- **Production-ish, need auth in front**: named Cloudflare tunnel with Cloudflare Access — configure manually and paste the public URL into the Tunnel field. Out of scope for the in-app wizard.

---

## Security posture reminder

All three modes share the same auth model: a single pairing token gates every request. See [security.md](./security.md). Rotate the token whenever a tunnel URL leaks to an untrusted channel.
