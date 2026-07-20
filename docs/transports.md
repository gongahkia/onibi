# Transports

Onibi is a single-user phone web cockpit. Use a local network path that reaches the Mac running Onibi; pairing and owner authentication remain required on every path.

## LAN and hotspot

`onibi up --transport=lan` starts the local HTTPS cockpit and prints a single-use pair URL. A phone on the same LAN can open that URL after trusting the generated local CA profile. When managed Wi-Fi blocks peer traffic, use the iPhone hotspot and start Onibi again to obtain a new URL.

## Tailscale Private

`onibi up --transport=tailscale-private` exposes the same cockpit only through the authenticated tailnet. Tailnet reachability is not authorization: the phone still needs the pair URL and owner session.

`tailscale` has been removed because it exposed the cockpit publicly. Existing `transport.mode: tailscale` configuration fails before cockpit startup; select `tailscale-private`, LAN, WireGuard, ZeroTier, Cloudflare Quick, or ngrok instead.

## Other web paths

WireGuard, ZeroTier, Cloudflare, and ngrok are web transport implementations with their own setup and operational constraints. Public relays require Onibi relay E2E; see [Cloudflare Quick setup](./cloudflare-quick-setup.md) and [ngrok setup](./ngrok-setup.md). They do not add a hosted Onibi account, multi-user control plane, remote host management, chat bridge, or notification provider.

Run `onibi doctor --transport=<mode>` before a non-LAN start and use [`transport-smoke.md`](./transport-smoke.md) for device validation.
