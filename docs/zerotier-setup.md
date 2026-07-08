# ZeroTier Setup

ZeroTier mode is for users who already run a ZeroTier virtual network or want a user-controlled mesh alternative to Tailscale, WireGuard, Cloudflare, and ngrok.

## Requirements

- ZeroTier One is installed and running on the laptop.
- The phone has the ZeroTier app installed and connected to the same network.
- The laptop has `zerotier-cli` available on `PATH`, or `ONIBI_ZEROTIER_BIN` points at it.
- The laptop is authorized in a ZeroTier network and has a routed virtual IP before `onibi up` starts.

ZeroTier's upstream start guide documents `zerotier-cli info` and `zerotier-cli listnetworks`: <https://docs.zerotier.com/start/>.

## Laptop

Verify the daemon:

```bash
zerotier-cli info
```

The status must be `ONLINE`.

Join a network if needed:

```bash
zerotier-cli join <network-id>
```

Verify membership and authorization:

```bash
zerotier-cli listnetworks
zerotier-cli listnetworks -j
```

The selected network must report status `OK` and show an assigned IP.

When more than one network is joined, pin the one Onibi should use:

```bash
export ONIBI_ZEROTIER_NETWORK=<network-id-or-name>
```

Start Onibi:

```bash
onibi doctor --transport=zerotier
onibi up --transport=zerotier
```

Onibi binds the web listener to the selected ZeroTier IP only, then prints a pair URL over that IP.

## Phone

Install the official ZeroTier app, join the same network, authorize the phone in the ZeroTier controller, and activate the network before scanning the Onibi QR.

The Onibi local HTTPS profile is still required. Install and fully trust `onibi-local-ca.mobileconfig` once, then restart `onibi up` so the generated server certificate includes the active ZeroTier IP.

## Smoke

1. Run `onibi doctor --transport=zerotier`.
2. Run `onibi up --transport=zerotier`.
3. Confirm the printed URL uses the ZeroTier IP.
4. Open the QR from the phone while ZeroTier is active.
5. Use the phone terminal for a shell command and a full-screen program such as `vim`.

If pairing fails, first confirm the phone can reach the laptop ZeroTier IP with another service, and verify both devices are authorized in the ZeroTier network.
