# C ABI v1

## ABI negotiation

- Call `yeokcham_abi_negotiate` before every other ABI function.
- The requested token must equal `YEOKCHAM_ABI_VERSION`; negotiation returns that exact token when accepted and `YEOKCHAM_ABI_NEGOTIATION_REJECTED` when rejected.
- The ABI is pre-release: consumers must not infer compatibility from either major or minor components and must renegotiate after every ABI update.

## Thread safety

- Every public C ABI function may be called concurrently.
- Operations on one client or configuration builder are linearized. A concurrent duplicate lifecycle transition returns `YEOKCHAM_STATUS_STATE`; use-after-release returns `YEOKCHAM_STATUS_INVALID_INPUT`.
- A client remains opaque. Exactly one `yeokcham_client_release` call accepts an active client; concurrent or later releases return `YEOKCHAM_STATUS_INVALID_INPUT`.
- `yeokcham_client_complete_async` validates a client only while it is submitted. A caller may release that client after a successful submission.
- A successful asynchronous submission queues one callback for four library-created background workers. The caller retains its callback context and must keep it valid until the callback runs.
- Completion callbacks run without an internal C ABI client-registry lock and may synchronously call any C ABI function, including `yeokcham_client_release` for the submitted client. Callback-context synchronization remains the caller's responsibility.
- `yeokcham_secret_buffer_zeroize` accepts only caller-owned writable bytes. Its buffer remains the caller's responsibility before and after the call.

## Callback backpressure

- At most 1,024 callbacks may be queued or executing. `yeokcham_client_complete_async` never waits for capacity and returns `YEOKCHAM_STATUS_RESOURCE_LIMIT` when the bound is full.
- The queue dispatches work to the fixed worker pool; callback execution may overlap and has no ordering guarantee.

## Engine status mapping

- SDK configuration failures map to `YEOKCHAM_STATUS_INVALID_INPUT`; exhausted SDK event sequence space maps to `YEOKCHAM_STATUS_RESOURCE_LIMIT`.
- SDK mode, lifecycle, state, engine, and async-task failures map to `YEOKCHAM_STATUS_STATE` without exposing raw engine details.
- `yeokcham_client_copy_last_error_detail` returns a bounded canonical ASCII token for the last SDK error on an active client. The caller owns its buffer; query the required length with a null buffer and zero capacity, then provide at least that capacity. The payload contains no engine strings, paths, or secrets.
