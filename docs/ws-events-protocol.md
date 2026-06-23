# WebSocket events protocol

`/ws/events` is the owner-only JSON event stream for the web cockpit. The
client must present the `onibi_owner` cookie and the matching `token` query
parameter used by `/ws/pty`.

All application messages are JSON envelopes:

```json
{"type":"approval.requested","ts":"2026-06-23T12:00:00Z","payload":{}}
```

`type` is a stable event name, `ts` is RFC3339Nano UTC time, and `payload` is an
event-specific object. The server may send `server.hello` first after a
successful upgrade.

## Event types

`approval.requested`

Payload:

- `id`: approval id.
- `session_id`: session that emitted the request.
- `agent`: adapter name.
- `tool`: tool name.
- `scrubbed_input`: approval input after `approval.Scrub`.
- `risk_level`: `low`, `medium`, or `high` from `approval.ClassifyRisk`.
- `risk_reasons`: risk classifier reasons.
- `expires_at`: RFC3339Nano UTC expiry.

`approval.decided`

Emitted for approve, deny, edit, and cancel terminal states.

Payload:

- `id`
- `session_id`
- `verdict`: `approve`, `deny`, `edited`, or `cancelled`.
- `reason`: optional denial/cancel reason.
- `decided_at`: Unix seconds.

`approval.expired`

Emitted when the queue transitions a pending approval to expired.

Payload:

- `id`
- `session_id`
- `verdict`: `expired`.
- `reason`
- `expires_at`

`session.started`

Payload contains at least `session_id`. The current implementation emits this
for active PTY hosts when a client attaches.

`session.ended`

Reserved for registry-backed session end events.

`render.event`

Reserved for selected non-PTY render notifications.

## Approval decisions

`POST /approval/{id}` requires owner auth and accepts:

```json
{"verdict":"approve"}
```

Valid verdicts are `approve`, `deny`, and `edit`. `deny` may include `reason`.
`edit` must include `edited_input`, which is validated against the original tool
input schema before `approval.Queue.Decide` is called.
