# Local observability

Arachne records operational events locally. Logs and diagnostics intentionally exclude message plaintext, ciphertext, attachment bytes, private keys, capabilities, authentication tokens, and peer network addresses.

## Client and daemon

Commands that queue an encrypted message or attachment write JSON Lines events to:

```text
<state-directory>/logs/arachne-client.jsonl
```

`arachne daemon serve` writes lifecycle events to:

```text
<state-directory>/logs/arachne-daemon.jsonl
```

`arachne courier daemon` records separate courier lifecycle and cycle events
in the same state directory. Each completed cycle includes only aggregate
counts for uploaded envelopes, uploaded attachment chunks, received frames,
delivery ACKs, expired work, and failures. It deliberately excludes payloads,
identifiers, capabilities, and peer addresses.

Inspect the managed files and their aggregate size:

```text
arachne logs list --state-directory /absolute/client-state
```

The diagnostic snapshot checks the client-state layout and reports aggregate log counts without opening databases or printing their contents:

```text
arachne diagnose --state-directory /absolute/client-state
```

Retention is manual. This command deletes only regular Arachne JSON Lines files older than the requested whole number of days; it does not follow symlinks or remove unrelated files:

```text
arachne logs prune --state-directory /absolute/client-state --older-than-days 30
```

Set `RUST_LOG` before starting a process to change its local log level, for example `RUST_LOG=debug`. Avoid broad debugging filters in routine use because dependency logs can be noisy.

## Relay

The relay appends JSON Lines lifecycle events next to its configured database:

```text
<database-parent>/logs/arachne-relay.jsonl
```

Its health listener defaults to `127.0.0.1:8080`; the metrics listener defaults to `127.0.0.1:8081`. Both refuse non-loopback binding. The metrics endpoint exposes only these aggregate gauges:

- registered mailboxes
- stored envelopes
- stored attachment chunks
- total stored encrypted bytes

For a local operator check:

```text
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8081/metrics
```

Do not publish the metrics listener directly. If metrics collection is needed, use a local collector or a separately reviewed authenticated proxy.
