# V2 client state directory

`ClientStateDirectory` is the single path authority for daemon-owned client state. A root must be an absolute, non-root path with no parent traversal. The caller selects the root; the project never derives it from untrusted network input. Key material remains in the OS keystore and never appears in this directory.

```
<state-root>/
  arachne-daemon.lock
  arachne-outbox.sqlite
  arachne-inbox.sqlite
  arachne-contacts.sqlite
  arachne-ratchets.sqlite
  arachne-one-time-prekeys.sqlite
  attachment-uploads/
    <canonical-lowercase-attachment-id>/
      manifest.cbor
      journal.cbor
      chunk-<index>.cbor
```

The layout version is `2`; version 1 roots remain valid and gain `arachne-one-time-prekeys.sqlite` on first one-time-prekey replenishment. Each SQLite database and CBOR document validates its own schema version; a layout version does not override those validation boundaries. SQLite WAL and shared-memory sidecars are part of their parent database and must remain alongside it.

`DaemonRuntime` is the exclusive owner of an active client-state root. It holds an exclusive lock on `arachne-daemon.lock` from successful startup until `shutdown` or drop. A concurrent startup for the same root fails with `AlreadyRunning`; a new owner may start only after the prior owner releases the lock.

Attachment submissions are staged as `<canonical-lowercase-attachment-id>.pending` under `attachment-uploads/` and renamed atomically only after every file has been written. Consumers must reject non-regular state database paths and noncanonical or mismatched attachment content.
