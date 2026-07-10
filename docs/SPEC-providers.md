# Onibi Provider Parity Contract

Status: contract for E1-E12 provider parity work.

This spec defines the target `chatout.Provider` shape for chat and notify
providers. Existing providers do not all satisfy the full contract today. The
matrix below is the baseline; E1-E7 and future provider issues close gaps
against this document.

## Provider Interface

The target Go surface lives in `internal/chatout/contract.go`:

```go
type Provider interface {
    Name() string
    Capabilities() []Capability

    SendApproval(context.Context, ApprovalRequest) (msgID string, err error)
    OnDecision(msgID string, handler func(Decision)) error

    SendText(context.Context, text string) error
    OnInboundText(handler func(text string, from Sender)) error
    TailStream(context.Context, sessionID string, chunk <-chan []byte) error

    Connect(context.Context) error
    Reconnect(context.Context) error
    Close() error

    RecordInteraction(context.Context, AuditInteraction) error
    RateLimit() RateLimitPolicy
}
```

`Capabilities()` is authoritative. A provider that does not support an axis must
return `ErrUnsupported` from that method path once concrete implementations add
the shared error type.

## Types

`ApprovalRequest` contains:

- `id`: Onibi approval id.
- `session_id`: Onibi session id.
- `agent`: source agent name, for example `claude`.
- `tool`: requested tool name.
- `input_json`: scrubbed or policy-filtered tool input.
- `diff`: optional unified diff for file edits.
- `risk_level`: `low`, `medium`, or `high`.

`Decision` contains:

- `approval_id`: Onibi approval id.
- `verdict`: `approve`, `deny`, `edit`, or `expire`.
- `updated_input`: replacement JSON for edit decisions.
- `sender`: provider user/channel identity.
- `message_id`: provider message or callback id.

`Sender` contains provider-scoped `id`, display name, and channel id. Providers
must treat provider user ids as audit identifiers, not as Onibi authentication.

## Rate Limits

Every provider reports a two-bucket policy:

```go
type RateLimitPolicy struct {
    PerSecond RateLimitBucket
    PerMinute RateLimitBucket
}

type RateLimitBucket struct {
    Limit  int
    Burst  int
    Window time.Duration
}
```

`PerSecond.Window` should be `time.Second`. `PerMinute.Window` should be
`time.Minute`. `Limit <= 0` means the provider has no local static limit for
that bucket and relies on remote `429` or `Retry-After` handling. Providers that
receive a remote retry hint must sleep at least that long before retrying the
same request. Retries must be context-cancelable.

Chunking must happen before rate-limit scheduling. The chunk id is not an audit
id; all chunks for one logical approval or tail update share one Onibi event id
in audit detail.

## Reconnect

Reconnect uses exponential backoff with jitter:

- initial delay: `1s`
- multiplier: `2`
- jitter: random `0.5x` to `1.5x`
- cap: `5m`
- reset: next successful `Connect`

`Connect(ctx)` establishes a fresh provider connection. `Reconnect(ctx)` may
reuse provider resume tokens, cursors, or socket URLs when the provider exposes
them. If resume fails, the provider falls back to `Connect(ctx)` without losing
Onibi's local approval queue. `Close()` must be idempotent.

Providers with polling cursors must persist the last consumed cursor before
handling user text when possible. Providers with push sockets must acknowledge
provider envelopes only after Onibi accepts the event or has recorded an
intentional ignore reason.

## Audit Shape

Provider audit rows must use `internal/store.AuditAppend`:

| SQLite column | Source |
| --- | --- |
| `ts` | write time from `AuditAppend` |
| `action` | provider action string |
| `session_id` | Onibi session id, empty if global |
| `payload_hash` | SHA-256 of payload supplied to `AuditAppend` |
| `decided_by_chat` | provider user numeric id when available, else `0` |
| `detail` | compact key/value detail string |

Action names use `provider.event` shape:

- `approval.request`
- `approval.decided`
- `provider.<name>.send`
- `provider.<name>.send_error`
- `provider.<name>.inbound_text`
- `provider.<name>.ignored`
- `notify.<name>.sent`
- `notify.<name>.receipt.created`
- `notify.<name>.receipt.acknowledged`
- `notify.<name>.receipt.expired`

`detail` must include provider, message id when present, verdict for decisions,
and retry/rate-limit state when relevant. Raw provider payloads, access tokens,
room secrets, and full terminal text must not be written to `detail`.

## Capability Matrix

Legend: `yes` means shipped enough for normal use; `partial` means present but
below the target contract; `no` means not implemented today.

<!-- markdownlint-disable MD013 -->

| Provider | Approval send | Decision callbacks | Text out | Text in | Tail stream | Reconnect | Notify receipt | Audit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Telegram | yes | yes | yes | yes | partial | partial | no | partial |
| Matrix | yes | partial | yes | yes | partial | partial | no | partial |
| Slack | yes | yes | yes | yes | partial | partial | no | partial |
| Discord | partial | partial | yes | yes | partial | partial | no | partial |
| Zulip | partial | partial | yes | yes | partial | partial | no | partial |
| IRC | partial | partial | yes | yes | partial | partial | no | partial |
| Signal | partial | partial | yes | yes | partial | partial | no | partial |
| Pushover | yes | partial | notify-only | no | no | no | yes | partial |
| ntfy | yes | partial | notify-only | no | yes | partial | no | partial |
| Gotify | yes | partial | notify-only | no | yes | partial | no | partial |
| APNs | yes | no | notify-only | no | no | no | no | partial |
| SMS | partial | partial | notify-only | no | no | partial | no | partial |
| Email | partial | partial | notify-only | no | no | partial | no | partial |

<!-- markdownlint-enable MD013 -->

## Existing Provider Notes

Telegram is the current broadest chat bridge: text input/output, approval
buttons, command handling, and tail-style command output exist. Reconnect and
audit should move behind the shared contract.

Matrix supports room text, approval reactions, chunked room-message tail output,
audit rows, polling cursor state, Client-Server E2EE key endpoint shapes, SAS
verification to-device message shapes/state tracking, SAS HKDF decimal/emoji
comparison helpers, `hkdf-hmac-sha256.v2` MAC helpers, room encrypted/key
request content shapes, local Megolm encrypt/decrypt state wrappers, local
Megolm room-event payload encrypt/decrypt/send helpers, local Olm account/pre-key
and stored type-1 device-session encrypt/decrypt wrappers, local outbound
`m.room_key` to-device sharing helpers, and encrypted local crypto/session-state
persistence. Encrypted rooms are not full Olm/Megolm E2EE yet; room-level
privacy remains E2.

Slack supports Socket Mode, message input, approval buttons/edit modals,
chunked tail output, reconnect backoff, and audit rows. Live workspace
verification remains required.

Zulip supports stream/topic messages, topic-mapped text input, slash-command
approval replies, per-session topic tail output, event queue reconnect, and
audit rows. Reaction decision callbacks remain future work.

IRC supports registered-nick SASL PLAIN login, owner-DM text input, `!onibi`
approval replies, DM tail chunking, reconnect backoff, send pacing, audit rows,
and a local `chatout.Provider` adapter. It has no Onibi-supported E2EE path.

Signal supports local `signal-cli` JSON-RPC send/events, approval reactions,
owner filtering, text input, tail chunking, reconnect backoff, audit rows, and
a local `chatout.Provider` adapter. Live linked-number verification remains
required.

Discord supports Gateway text, slash-command fallback, components approval
buttons/edit modals, per-session tail threads, reconnect backoff, and audit
rows. Live guild verification remains required.

Pushover is notify-only. It can send emergency-priority approval alerts, poll
receipts, and map acknowledged receipts to approve, but it does not provide
terminal input, deny callbacks, or native edit decisions.

ntfy validates topic secrecy, publishes optional signed Approve/Deny action
buttons, can tail the JSON topic stream with reconnect/replay, and has a local
notify-only `chatout.Provider` adapter. Password E2E is not implemented because
the 2026-07-10 official ntfy publish/subscribe docs do not verify
`X-Message-Encryption` support; upstream E2E is tracked in
<https://github.com/binwiederhier/ntfy/issues/69>.

Gotify sends REST messages, adds optional signed approval deep-links through
`client::notification.click.url`, and can receive `/stream` WebSocket messages
with reconnect backoff. It also has a local notify-only `chatout.Provider`
adapter. Native buttons are not available in Gotify; the signed approval page is
the callback workaround.

APNs is notify-only. It sends direct Apple alert pushes through a user-provided
APNs auth key and native app device token. PWA-only Onibi cannot mint a native
device token, so web push remains the fallback when APNs config is absent.

SMS and email are notify-only failover transports. They send signed approval
URLs through Twilio SMS or a user-provided SMTP relay and require an externally
reachable Onibi action base URL for tap-through decisions.

## Conformance Expectations

Provider implementations must pass shared conformance tests for:

- name and capability reporting
- approval request send
- idempotent approve, deny, edit, and expire decisions
- inbound text normalization
- rate-limit bucket reporting
- retry-after handling
- reconnect backoff cap and reset
- audit row action/detail shape

Provider-specific live tests remain opt-in because they require external
secrets. Shared unit tests must use fake clients and must not call remote APIs.
