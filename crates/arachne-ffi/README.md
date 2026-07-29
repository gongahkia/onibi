# C ABI v1

## ABI negotiation

- Call `arachne_abi_negotiate` before every other ABI function.
- The requested token must equal `ARACHNE_ABI_VERSION`; negotiation returns that exact token when accepted and `ARACHNE_ABI_NEGOTIATION_REJECTED` when rejected.
- The ABI is pre-release: consumers must not infer compatibility from either major or minor components and must renegotiate after every ABI update.

## Thread safety

- Every public C ABI function may be called concurrently.
- Operations on one client, configuration builder, buffer, or event subscription are linearized. A concurrent duplicate lifecycle transition returns `ARACHNE_STATUS_STATE`; use-after-release returns `ARACHNE_STATUS_INVALID_INPUT`.
- A client remains opaque. Exactly one `arachne_client_release` call accepts an active client; concurrent or later releases return `ARACHNE_STATUS_INVALID_INPUT`.
- `arachne_client_complete_async` validates a client only while it is submitted. A caller may release that client after a successful submission.
- A successful asynchronous submission queues one callback for four library-created background workers. The caller retains its callback context and must keep it valid until the callback runs.
- Completion callbacks run without an internal C ABI client-registry lock and may synchronously call any C ABI function, including `arachne_client_release` for the submitted client. Callback-context synchronization remains the caller's responsibility.
- `arachne_secret_buffer_zeroize` accepts only caller-owned writable bytes. Its buffer remains the caller's responsibility before and after the call.
- `arachne_client_identity_export_recovery` requires a handle-scoped identity and a nonempty passphrase no larger than `ARACHNE_MAX_RECOVERY_PASSPHRASE_BYTES`; it returns an opaque encrypted archive buffer that the caller releases.
- `arachne_client_identity_import_recovery` accepts exactly `ARACHNE_RECOVERY_ARCHIVE_BYTES` and writes the recovered identity public key only after successful authentication.

## Callback backpressure

- At most 1,024 callbacks may be queued or executing. `arachne_client_complete_async` never waits for capacity and returns `ARACHNE_STATUS_RESOURCE_LIMIT` when the bound is full.
- The queue dispatches work to the fixed worker pool; callback execution may overlap and has no ordering guarantee.

## Engine status mapping

- SDK configuration failures map to `ARACHNE_STATUS_INVALID_INPUT`; exhausted SDK event sequence space maps to `ARACHNE_STATUS_RESOURCE_LIMIT`.
- SDK mode, lifecycle, state, engine, and async-task failures map to `ARACHNE_STATUS_STATE` without exposing raw engine details.
- `arachne_client_copy_last_error_detail` returns a bounded canonical ASCII token for the last SDK error on an active client. The caller owns its buffer; query the required length with a null buffer and zero capacity, then provide at least that capacity. The payload contains no engine strings, paths, or secrets.

## Delivery profiles

- `arachne_delivery_profile_select` accepts a caller-owned `arachne_delivery_profile_policy_t`; each allow flag is exactly zero or one, and the local-mesh transport count is bounded by `ARACHNE_MAX_LOCAL_MESH_TRANSPORTS`.
- Direct selection requires `ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED`; Tor-maildrop requires zero acknowledgement and zero local-mesh transport; local-mesh requires zero acknowledgement and an allowed `ARACHNE_LOCAL_MESH_TRANSPORT_*` value.
- The function clears `arachne_delivery_profile_t` before every failure. Invalid policy or selection input returns `ARACHNE_STATUS_INVALID_INPUT`; a policy-disallowed selection returns `ARACHNE_STATUS_STATE`.

## Messages

- `arachne_client_message_send` accepts a canonical encoded encrypted envelope no larger than `ARACHNE_MAX_MESSAGE_ENVELOPE_BYTES`, a recipient public key, and a valid creation time and TTL.
- It writes a 16-byte message identifier only after queueing succeeds and clears that output before every failure. Invalid message input returns `ARACHNE_STATUS_INVALID_INPUT`; a missing identity, stopped client, or unavailable outbox returns `ARACHNE_STATUS_STATE`; a full outbox returns `ARACHNE_STATUS_RESOURCE_LIMIT`.
- Client-scoped failures expose only bounded redacted `sdk_message_*` error-detail tokens.

## Attachments

- `arachne_attachment_transfer_create` accepts one encoded manifest and at most 1,600 encoded chunks through caller-owned `arachne_byte_slice_t` descriptors; each manifest and chunk has an explicit ABI maximum.
- `arachne_attachment_transfer_run_cycle` uploads no more than the configured 1–64 chunks through a synchronous callback. `ARACHNE_STATUS_OK` acknowledges an upload; any other callback status records a retry outcome without losing transfer state.
- A transfer is unavailable to concurrent cycles or release while its upload callback is running. The cycle output is cleared before failure; release accepts exactly one idle active handle.

## Cancellation

- `arachne_cancellation_create` creates one bounded opaque cancellation handle. Cancellation is idempotent; release invalidates the handle but does not revoke an already-started wait.
- `arachne_event_subscription_wait` blocks for at most `ARACHNE_MAX_CANCELLATION_DEADLINE_MILLISECONDS`, one event, or cancellation. Cancellation, deadline, and closure return `ARACHNE_STATUS_STATE`; lag returns `ARACHNE_STATUS_RESOURCE_LIMIT`.

## Library-owned buffers

- `arachne_client_take_last_error_detail` transfers the redacted detail into an opaque library-owned buffer and writes a null output on failure. Read its bytes and length with `arachne_buffer_data` and `arachne_buffer_length`, then call `arachne_buffer_release` exactly once.
- At most 1,024 library-owned buffers may be active. A buffer data pointer remains valid only until release. The caller must synchronize pointer use with release; invalid or released buffers return a null data pointer, zero length, or `ARACHNE_STATUS_INVALID_INPUT` as applicable.

## Event subscriptions

- `arachne_client_subscribe_events` creates an opaque subscription only for a running client. At most 1,024 subscriptions may be active; release each subscription exactly once.
- `arachne_event_subscription_poll` never blocks. It requires distinct caller-owned writable `arachne_event_t` and `uint8_t` outputs; once both are valid, it clears both before an empty or failing poll. `ARACHNE_STATUS_OK` with `has_event == 0` means no event is available; `has_event == 1` returns one event.
- Every returned event has `version == ARACHNE_EVENT_VERSION`, a nonzero client event sequence, and one `ARACHNE_EVENT_*` kind. Lifecycle events have an all-zero `message_identifier`; message events carry its 16 bytes.
- A lagged subscription returns `ARACHNE_STATUS_RESOURCE_LIMIT`; retrying may read the first retained event. A closed subscription returns `ARACHNE_STATUS_STATE`. No skipped-count, engine detail, or other internal event data crosses the ABI.
