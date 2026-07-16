# C ABI v1

## ABI negotiation

- Call `yeokcham_abi_negotiate` before every other ABI function.
- The requested token must equal `YEOKCHAM_ABI_VERSION`; negotiation returns that exact token when accepted and `YEOKCHAM_ABI_NEGOTIATION_REJECTED` when rejected.
- The ABI is pre-release: consumers must not infer compatibility from either major or minor components and must renegotiate after every ABI update.

## Thread safety

- `yeokcham_client_create`, `yeokcham_client_release`, `yeokcham_client_complete_async`, and `yeokcham_abi_negotiate` may be called concurrently.
- A client remains opaque. Exactly one `yeokcham_client_release` call accepts an active client; concurrent or later releases return `YEOKCHAM_STATUS_INVALID_INPUT`.
- `yeokcham_client_complete_async` validates a client only while it is submitted. A caller may release that client after a successful submission.
- A successful asynchronous submission schedules one callback on a library-created background thread. The caller retains its callback context and must keep it valid until the callback runs.
- Completion callbacks may call the C ABI. Callback-context synchronization remains the caller's responsibility.
- `yeokcham_secret_buffer_zeroize` accepts only caller-owned writable bytes. Its buffer remains the caller's responsibility before and after the call.
