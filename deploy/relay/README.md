# Relay OCI image

Build from the repository root:

```text
docker build --file deploy/relay/Dockerfile --tag arachne-relay:local .
```

The image runs as UID/GID `65532`, requires a writable volume at `/var/lib/arachne`, and is compatible with a read-only root filesystem. Mount `relay.conf.example` as `/etc/arachne/relay.conf` after setting its public listen address and persistent database path.

Mount a read-only, exact `RelaySigningKeypair::serialize()` binary at `/run/secrets/arachne-relay-identity`. The relay rejects absent, writable, non-regular, malformed, or wrong-length identity material. Generate it once on a secure admin host with `arachne-relay generate-identity --output /absolute/path/identity`, then mount it into the container. The image never generates an identity automatically.

The built-in health check requests `127.0.0.1:8080/readyz`. The process handles SIGTERM and waits at most 30 seconds for graceful request draining by default.
