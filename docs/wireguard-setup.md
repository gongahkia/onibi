# WireGuard Setup

WireGuard mode is for users who already run their own VPN or want a self-hosted alternative to Tailscale, Cloudflare, and ngrok.

## Requirements

- WireGuard is configured and connected on the laptop.
- The phone has a WireGuard peer profile installed and connected.
- The laptop has `wg` available on `PATH`, or `ONIBI_WIREGUARD_BIN` points at it.
- The WireGuard interface has a routable VPN IP before `onibi up` starts.

WireGuard's upstream quick start documents `wg show` and `wg show interfaces`: <https://www.wireguard.com/quickstart/>.

## Laptop

Verify the active interface:

```bash
wg show interfaces
```

Verify the address:

```bash
ifconfig wg0
```

On Linux, use `ip addr show wg0` if `ifconfig` is not installed.

When more than one interface is active, pin the one Onibi should use:

```bash
export ONIBI_WIREGUARD_INTERFACE=wg0
```

Start Onibi:

```bash
onibi doctor --transport=wireguard
onibi up --transport=wireguard
```

Onibi binds the web listener to the selected WireGuard IP only, then prints a pair URL over that IP.

## Phone

Install the official WireGuard app, import the phone peer profile, and activate the tunnel before scanning the Onibi QR.

The Onibi local HTTPS profile is still required. Install and fully trust `onibi-local-ca.mobileconfig` once, then restart `onibi up` so the generated server certificate includes the active WireGuard IP.

## Smoke

1. Run `onibi doctor --transport=wireguard`.
2. Run `onibi up --transport=wireguard`.
3. Confirm the printed URL uses the WireGuard IP.
4. Open the QR from the phone while the tunnel is active.
5. Use the phone terminal for a shell command and a full-screen program such as `vim`.

If pairing fails, first confirm the phone can reach the laptop WireGuard IP with another service. WireGuard itself does not expose a connected control session; `wg show` and the latest handshake/transfer counters are the useful local diagnostics.
