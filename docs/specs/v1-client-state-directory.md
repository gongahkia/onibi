# V1 client state directory

`ClientStateDirectory` is the single path authority for daemon-owned client state. A root must be an absolute, non-root path with no parent traversal. The caller selects the root; the project never derives it from untrusted network input. Key material remains in the OS keystore and never appears in this directory.

```
<state-root>/
  yeokcham-daemon.lock
  yeokcham-outbox.sqlite
  yeokcham-inbox.sqlite
  yeokcham-contacts.sqlite
  yeokcham-ratchets.sqlite
  attachment-uploads/
    <canonical-lowercase-attachment-id>/
      manifest.cbor
      journal.cbor
      chunk-<index>.cbor
```

The layout version is `1`. Each SQLite database and CBOR document validates its own schema version; a layout version does not override those validation boundaries. SQLite WAL and shared-memory sidecars are part of their parent database and must remain alongside it.

Attachment submissions are staged as `<canonical-lowercase-attachment-id>.pending` under `attachment-uploads/` and renamed atomically only after every file has been written. Consumers must reject non-regular state database paths and noncanonical or mismatched attachment content.
