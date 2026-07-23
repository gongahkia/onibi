# Cloudflare Quick Setup

Cloudflare Quick is an opt-in, temporary public relay for a single-user Onibi cockpit. It is for short device tests when a private network path is unavailable, not a stable or account-managed tunnel.

## Requirements

- `cloudflared` is on `PATH`, or `ONIBI_CLOUDFLARED_BIN` points to it.
- The local cockpit starts before the Quick Tunnel and is available at `https://localhost:<port>`.
- No `config.yaml` is present in the `cloudflared` directory used by the Quick Tunnel. Cloudflare documents this Quick Tunnel limitation at <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/>.

Cloudflare assigns a random `https://*.trycloudflare.com` URL for the running process. It provides no uptime guarantee, has a 200 in-flight request limit, and does not support SSE.

## Start

```bash
onibi system doctor --transport=cloudflare-quick
onibi start --transport=cloudflare-quick
```

Onibi starts a child `cloudflared tunnel --url https://localhost:<port> --no-tls-verify` process, accepts only an HTTPS `trycloudflare.com` origin printed by that process, and stops when the child exits. It does not create, configure, or manage a Cloudflare account, domain, route, or token.

## Upgrade

`cloudflare-named` has been removed. Change `transport.mode` to `cloudflare-quick` or a private transport; Onibi rejects the old value before cockpit or tunnel startup and does not fall back. Previously configured Cloudflare API-token state is ignored and left untouched; remove it with the secret-store or environment-management tool that created it.

## Relay E2E

Cloudflare Quick always requires Onibi relay E2E. The QR URL carries its `#k=` bootstrap key only in the fragment; the relay never receives that fragment. Pair verification, terminal bytes, typed input, approvals, controls, HTTP payloads, and WebSocket frames fail closed without a valid key/verifier and encrypted frame.

Cloudflare can still observe the public hostname, request paths, timing, byte sizes, and connection metadata. See [SPEC-e2e.md](./SPEC-e2e.md) for threat boundaries.

## Phone smoke

1. Run `ONIBI_LIVE_CLOUDFLARE_QUICK=1 go test ./internal/web/transport -run LiveCloudflareQuick`.
2. Start `onibi start --transport=cloudflare-quick`, scan the new QR on iPhone Safari and Chrome over LTE, and verify terminal input, output, approval allow, and approval deny.
3. Open the pair URL after removing `#k=`; it must not attach.
4. Stop `cloudflared`; Onibi must stop on its next health check. Restart it, scan its new QR, and repeat a terminal command.
