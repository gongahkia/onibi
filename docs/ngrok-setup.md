# ngrok Setup

ngrok is an optional public relay for a single-user Onibi cockpit. Run it only when a private LAN, tailnet, WireGuard, or ZeroTier path is unsuitable.

## Requirements

- `ngrok` is on `PATH`, or `ONIBI_NGROK_BIN` points to it.
- An ngrok authtoken is available when the selected account or reserved domain requires one.
- The ngrok Agent API remains on a loopback HTTP origin. Onibi accepts `localhost`, `127.0.0.1`, or `::1` only; this API has no authentication.

ngrok documents the local Agent API and `ngrok http https://localhost:8443` upstream form at <https://ngrok.com/docs/agent/api> and <https://ngrok.com/docs/agent>.

## Laptop

Store the authtoken if needed:

```bash
onibi ngrok setup --authtoken ...
```

Optionally select a reserved HTTPS domain:

```bash
export ONIBI_NGROK_DOMAIN=example.ngrok.app
```

Preflight and start:

```bash
onibi doctor --transport=ngrok
onibi up --transport=ngrok
```

Onibi starts only `ngrok http https://localhost:<port>`, accepts only the matching Agent API tunnel with an HTTPS public origin, and rechecks that tunnel while `onibi up` waits. It deletes that Agent API tunnel and stops its own ngrok process during cleanup; it does not manage account endpoints, domains, or credentials beyond the selected authtoken/domain.

## Relay E2E

ngrok is a public relay and always uses the same required E2E relay protocol as Cloudflare Quick. The QR URL has a `#k=` fragment bootstrap key; fragments are not sent to the relay. Pair verification, terminal bytes, typed input, approvals, controls, HTTP payloads, and WebSocket frames fail closed without a valid key/verifier and encrypted frame.

The relay can still observe public host, request paths, timing, sizes, and connection metadata. See [SPEC-e2e.md](./SPEC-e2e.md) for the protocol and threat boundaries.

## Phone smoke

1. Run the live tunnel check: `ONIBI_LIVE_NGROK=1 go test ./internal/web/transport -run LiveNgrok`.
2. Start `onibi up --transport=ngrok`, scan the new QR on iPhone Safari and Chrome over LTE, and open a terminal.
3. Verify typed input, output, one approval allow, and one approval deny.
4. Open the pair URL without `#k=` and verify it cannot attach.
5. Stop the Agent API tunnel; Onibi must stop on its next health check. Restart `onibi up`, scan its new URL, and repeat a terminal command.
