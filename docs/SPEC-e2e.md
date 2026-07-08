# Onibi E2E Relay Protocol v1

Status: frozen for Q0 implementation.

## Scope

This protocol protects Onibi traffic when a third-party HTTPS relay is used, starting with Cloudflare Quick Tunnel. The relay may terminate TLS and proxy requests, but it must not learn terminal bytes, approval payloads, control messages, or typed input.

Local LAN and Tailscale modes can use the same framing, but E2E is mandatory for `--transport=cloudflare-quick`.

References:

- MDN Web Crypto `SubtleCrypto.deriveKey()` documents HKDF key derivation into AES-GCM keys: <https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/deriveKey>.
- MDN Web Crypto `AesGcmParams` documents AES-GCM IV uniqueness, 96-bit IV guidance, authentication tags, and additional authenticated data: <https://developer.mozilla.org/en-US/docs/Web/API/AesGcmParams>.
- Cloudflare Quick Tunnels create random `trycloudflare.com` subdomains and proxy that public hostname to a local service: <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/>.

## URL Bootstrap

QR shape:

```text
https://<id>.trycloudflare.com/pair/<pair-token>#k=<key>
```

`<key>` is base64url without padding over 32 random bytes. Encoded length is 43 characters.

The key lives only in the URL fragment. Browsers do not send fragments in HTTP requests, so Cloudflare and the Onibi HTTP handler do not receive `k` via URL, headers, logs, or request bodies. The frontend must parse `location.hash`, import the raw key into WebCrypto, then immediately remove the fragment with `history.replaceState`.

The server may keep the raw key only in volatile memory while the pair token is live and while the bound owner session is active. SQLite stores only commitments/verifiers, never the raw key.

## Key Schedule

Inputs:

- `K_pair`: 32-byte random bootstrap key from `#k=`.
- `session_id`: owner web session id created after successful `/pair/<pair-token>`.
- `stream_id`: 16-byte random value per HTTP request or WebSocket connection, encoded base64url.
- `dir`: `c2s` or `s2c`.
- `channel`: one of `http:<METHOD>:<PATH>`, `ws:pty`, `ws:events`.

Session base:

```text
K_session = HKDF-SHA256(
  IKM  = K_pair,
  salt = session_id,
  info = "onibi-e2e-v1",
  L    = 32
)
```

Per-stream AES key:

```text
K_stream = HKDF-SHA256(
  IKM  = K_session,
  salt = stream_id,
  info = "onibi-e2e-stream-v1:" || channel || ":" || dir,
  L    = 32
)
```

Per-stream nonce prefix:

```text
nonce_prefix = HKDF-SHA256(
  IKM  = K_session,
  salt = stream_id,
  info = "onibi-e2e-nonce-v1:" || channel || ":" || dir,
  L    = 4
)
```

Frame IV:

```text
iv = nonce_prefix || uint64_be(seq)
```

`seq` starts at 0 for each `(session_id, stream_id, channel, dir)` and increments by 1 per encrypted frame. Reusing a `(stream_id, dir, seq)` tuple is a protocol error because it reuses an AES-GCM IV.

Pair-confirm traffic is pre-session traffic. It derives a pre-session base key:

```text
K_pair_confirm = HKDF-SHA256(
  IKM  = K_pair,
  salt = pair_token,
  info = "onibi-e2e-pair-confirm-v1",
  L    = 32
)
```

For pair-confirm only, replace `K_session` with `K_pair_confirm` in the `K_stream` and `nonce_prefix` derivations above. Pair-confirm uses `session_id = pair_token`, `channel = http:POST:/pair/confirm`, a fresh `stream_id`, `dir = c2s`, and the same IV/frame rules below.

## Frame

All encrypted WebSocket frames are sent as text frames containing JSON. Encrypted HTTP request and response bodies use the same JSON frame with `Content-Type: application/onibi-e2e+json`.

```json
{
  "v": "onibi.e2e.v1",
  "sid": "<session_id>",
  "st": "<stream_id>",
  "ch": "ws:pty",
  "dir": "c2s",
  "seq": 0,
  "iv": "<base64url 12-byte IV>",
  "t": "binary",
  "ct": "<base64url ciphertext+tag>"
}
```

Fields:

- `v`: protocol version.
- `sid`: owner session id.
- `st`: stream id.
- `ch`: channel.
- `dir`: direction.
- `seq`: uint64 sequence number.
- `iv`: 12-byte AES-GCM IV, base64url without padding.
- `t`: plaintext type, `text` or `binary`.
- `ct`: AES-256-GCM output, ciphertext followed by the 16-byte authentication tag, base64url without padding.

AAD is canonical UTF-8:

```text
v || "\n" ||
sid || "\n" ||
st || "\n" ||
ch || "\n" ||
dir || "\n" ||
decimal(seq) || "\n" ||
iv || "\n" ||
t
```

Receivers must reconstruct AAD from frame fields, not trust any duplicated in-band value. Receivers must decode `iv`, require exactly 12 bytes, and reject frames where `iv != nonce_prefix || uint64_be(seq)`. AES-GCM authentication failure closes the WebSocket with policy violation or returns HTTP 400.

## Replay Defense

For WebSockets, each side keeps the next expected `seq` for `(session_id, stream_id, channel, dir)`. A frame is accepted only when `seq == expected`; accepted frames increment `expected`. Lower, repeated, skipped, or non-numeric sequence values are rejected and close the socket.

For HTTP requests, clients generate a fresh `stream_id` per request and use `seq=0`. The server keeps a bounded replay cache of recently accepted `(session_id, stream_id, channel, dir, seq)` tuples for at least 10 minutes. A duplicate tuple is rejected with HTTP 409. Implementation: `internal/web/e2e.go` enforces this in `Server.acceptE2EHTTPReplay` with `e2eHTTPReplayTTL = 10 * time.Minute` and `e2eHTTPReplayLimit = 4096`.

For HTTP responses, the server uses the request `stream_id`, `dir=s2c`, and `seq=0`.

## Pairing And Verifier Flow

1. Server mints `<pair-token>` with existing single-use token semantics and generates `K_pair` in memory.
2. Server stores only:
   - `pair_commit = HMAC-SHA256(K_pair, "onibi relay key commitment v1")`
   - `pair_verifier = HKDF-SHA256(K_pair, salt=<pair-token>, info="onibi-e2e-pair-verifier-v1", L=32)`
3. QR encodes `/pair/<pair-token>#k=<base64url(K_pair)>`.
4. Client opens `/pair/<pair-token>`, reads `K_pair` from fragment, derives `pair_verifier`, and sends it inside the encrypted pair-confirm request body.
5. Server consumes the single-use pair token, constant-time compares the verifier, creates `session_id`, and binds `K_pair` to that session in volatile memory.
6. Server stores:
   - `session_commit = HMAC-SHA256(K_pair, "onibi relay key commitment v1:" || session_id)`
   - `session_verifier = HKDF-SHA256(K_pair, salt=session_id, info="onibi-e2e-session-verifier-v1", L=32)`
7. Client sends `session_verifier` in the first encrypted WebSocket hello. Server constant-time compares it before accepting PTY/events traffic.

The raw key is not persisted. If the daemon restarts, Cloudflare Quick Tunnel E2E sessions must be re-paired.

## Channel Bindings

HTTP channels:

- `http:POST:/control`
- `http:POST:/approval/<id>`
- future state-changing endpoints must add explicit channel strings before shipping.

WebSocket channels:

- `ws:pty`
- `ws:events`

Plain `/healthz` may report whether E2E is required and the hex session verifier commitment. It must not expose raw keys, stream ids, ciphertext plaintext, or decrypted payloads.

## Failure Behavior

- Missing `#k=`: Cloudflare Quick Tunnel UI refuses to attach and asks the user to rescan the QR.
- Bad key length: client refuses before any network send.
- Bad verifier: server rejects pairing or WS attach with 401.
- AES-GCM auth failure: reject frame/request, audit sanitized reason, never surface decrypted partial data.
- Replay or sequence gap: reject and require reconnect.
- Volatile key missing after daemon restart: require fresh `onibi pair`.

## Threat Model

Protected against:

- Cloudflare relay reading PTY bytes, typed input, approval payloads, or control requests.
- Passive network observers reading Onibi payloads.
- Relay replay of already accepted request/frame tuples.
- Server-side persistence leaking raw relay keys.

Not protected against:

- Same-user local malware or a debugger on the laptop.
- A malicious browser extension reading page memory after pairing.
- A stolen unlocked phone with an active paired session.
- Traffic metadata: host, timing, byte lengths, connection count, and request paths remain visible to Cloudflare.
- Relay dropping, delaying, reordering, or denying traffic.
- Bugs in browser WebCrypto or Go crypto implementations.

## Implementation Gates

- [x] Fragment-keyed bootstrap (`#k=`) stays out of relay requests: `internal/cli/up.go` appends `#k=<base64url>`, `frontend/src/e2e.ts` reads `location.hash`, imports exactly 32 bytes, and removes the fragment with `history.replaceState`; `internal/cli/up_test.go` checks the pre-fragment URL does not contain the key.
- [x] Volatile-only raw key storage: `internal/web/relay_keys.go` stores raw keys only in `RelayKeys.pairs` and `RelayKeys.sessions`; SQLite stores `relay_key_commitment:*` strings and `web_sessions.key_verifier_enc`; `internal/web/e2e_test.go` checks the commitment does not contain the raw key.
- [x] HKDF-SHA256 key schedule: `internal/e2e/e2e.go` derives `K_session`; `internal/envelope/relay.go` derives per-stream keys/nonces; `frontend/src/e2e.ts` mirrors both with WebCrypto HKDF.
- [x] Per-frame IV is `nonce_prefix || uint64_be(seq)`: `internal/envelope/relay.go` builds and verifies the IV; `frontend/src/e2e.ts` mirrors `uint64BE`.
- [x] AAD is reconstructed from frame fields: `internal/envelope/relay.go` uses `RelayAAD(frame)` on seal/open; `frontend/src/e2e.ts` reconstructs the same field list before encrypt/decrypt.
- [x] HTTP replay cache uses a 10 minute TTL: `internal/web/e2e.go` has `e2eHTTPReplayTTL = 10 * time.Minute`; `internal/web/e2e_test.go` covers replay, expiry, and cache bounds.
- [x] Sequence gaps/replay are rejected: `internal/web/e2e.go` tracks expected WebSocket sequence; `internal/envelope/relay.go` rejects unexpected `seq`; `internal/web/e2e_test.go` covers replay rejection.
- [x] Bad verifier rejects attach with unauthorized: `internal/web/auth.go` constant-time compares `verify_token`; `internal/web/e2e_test.go` and `internal/web/ws_pty_test.go` cover bad verifier rejection.
- [x] Tagged-release relay E2E gate and no plaintext bypass: `internal/cli/up_test.go` verifies Cloudflare Quick is a relay mode and the removed `--unsafe-cloudflare-no-e2e` flag is rejected; `scripts/release-e2e-gate.sh` is wired as the release assertion.
- [x] Spec drift filed: encrypted pair-confirm (`http:POST:/pair/confirm`) is specified above but the current implementation binds on `GET /pair/<token>` before WebSocket verifier attach. Tracked in GitHub issue #188.
