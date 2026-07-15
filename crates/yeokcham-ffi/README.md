# C ABI v1

## Thread safety

- `yeokcham_handle_create`, `yeokcham_handle_release`, `yeokcham_handle_complete_async`, and `yeokcham_abi_negotiate` may be called concurrently.
- A handle remains opaque. Exactly one `yeokcham_handle_release` call accepts an active handle; concurrent or later releases return `YEOKCHAM_STATUS_INVALID_INPUT`.
- `yeokcham_handle_complete_async` validates a handle only while it is submitted. A caller may release that handle after a successful submission.
- A successful asynchronous submission schedules one callback on a library-created background thread. The caller retains its callback context and must keep it valid until the callback runs.
- Completion callbacks may call the C ABI. Callback-context synchronization remains the caller's responsibility.
