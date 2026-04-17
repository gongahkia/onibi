# Deployment — LAN mode

Use when your phone and Mac are on the **same Wi-Fi** and you accept plaintext HTTP over that network.

## 1. Configure the gateway

In Onibi → Settings → Mobile Access:

1. **Enable mobile gateway**.
2. **Enable remote control** (required for live terminal control).
3. **Network Binding** → pick **LAN (same Wi-Fi)**.
4. Copy one of the URLs shown under `Network Binding` (e.g. `http://192.168.1.20:8787`).
5. **Rotate Token** if you haven't, then **Show QR** or **Copy Deep Link**.

The gateway listens on `0.0.0.0:<port>` when bind mode is `lan` or `all`. macOS will prompt for firewall access on first run — allow it.

## 2. Serve the web UI on LAN

```zsh
cd OnibiWeb
npm run dev -- --host 0.0.0.0
```

Vite will print a `Network:` URL like `http://192.168.1.20:5173`.

If you'd rather serve the production bundle:

```zsh
cd OnibiWeb
npm run build
npx vite preview --host 0.0.0.0 --port 5173
```

## 3. Connect from the phone

1. Join the same Wi-Fi as the Mac.
2. Open `http://<mac-lan-ip>:5173` on your phone.
3. On the Connection screen: pick **LAN**, paste the Base URL from step 1, paste the token. Or tap **Paste Pairing Payload** after copying the deep link / JSON payload from the Mac.

## Tips

- **Stable IP:** set a DHCP reservation on your router so the Mac IP doesn't rotate.
- **mDNS:** if your Mac is reachable at `your-mac.local`, you can use that instead of the IP.
- **Firewall:** System Settings → Network → Firewall → allow the Onibi binary to accept incoming connections.
- **Client isolation:** many guest/coffee-shop Wi-Fi networks block peer-to-peer traffic — LAN mode won't work there. Use tunnel mode instead.
- **Sleep:** the Mac must stay awake. `caffeinate -dims` during debugging is handy.

## Troubleshooting

See [troubleshooting.md](./troubleshooting.md).
