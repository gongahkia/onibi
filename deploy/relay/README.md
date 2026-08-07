# Relay OCI image

Build from the repository root:

```text
docker build --file deploy/relay/Dockerfile --tag arachne-relay:local .
```

The image runs as UID/GID `65532`, requires a writable volume at `/var/lib/arachne`, and is compatible with a read-only root filesystem. Mount `relay.conf.example` as `/etc/arachne/relay.conf` after setting its public listen address and persistent database path.

Mount a read-only, exact `RelaySigningKeypair::serialize()` binary at `/run/secrets/arachne-relay-identity`. The relay rejects absent, writable, non-regular, malformed, or wrong-length identity material. Generate it once on a secure admin host with `arachne-relay generate-identity --output /absolute/path/identity`, then mount it into the container. The image never generates an identity automatically.

The built-in health check requests `127.0.0.1:8080/readyz`. The process handles SIGTERM and waits at most 30 seconds for graceful request draining by default.

For the onion courier, the relay must run TLS because client transport pins the exact leaf certificate. Mount PEM material read-only and add `--tls-certificate /run/secrets/relay.pem --tls-private-key /run/secrets/relay.key` to the `arachne-relay run` command. The relay’s TLS certificate pin is the lowercase SHA-256 digest of the certificate DER bytes, carried in each signed courier bundle. Relay administrators can create a registered, recipient-bound relay invitation with `arachne-relay provision-invite`; see [`docs/courier.md`](../../docs/courier.md) for the client exchange.

The relay appends redacted JSON Lines lifecycle logs to `logs/arachne-relay.jsonl` beside its configured database. Its loopback-only metrics listener defaults to `127.0.0.1:8081` and exposes aggregate mailbox, envelope, attachment-chunk, and encrypted-byte gauges. See [`docs/operations.md`](../../docs/operations.md) for local inspection and retention commands.
