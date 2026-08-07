# Courier acceptance topology

This Compose topology starts a TLS relay, a Tor v3 onion service for it, and an independent Tor SOCKS client exposed only on host loopback. It is deliberately an acceptance harness, not a production deployment: its relay identity and one-day self-signed TLS certificate are recreated with the Docker volumes.

```text
docker compose -f deploy/courier-acceptance/compose.yaml up --build
```

Wait for both Tor processes to bootstrap, then obtain the generated hostname and relay certificate from the running containers:

```text
docker compose -f deploy/courier-acceptance/compose.yaml exec tor-onion cat /var/lib/tor/hidden_service/hostname
docker compose -f deploy/courier-acceptance/compose.yaml cp relay:/runtime/relay.pem /tmp/arachne-relay.pem
openssl x509 -in /tmp/arachne-relay.pem -outform der | shasum -a 256
```

Derive the 32-byte onion-service public key required by `arachne-relay provision-invite` from the hostname:

```text
python3 - <<'PY'
import base64
hostname = input().strip().removesuffix('.onion')
print(base64.b32decode(hostname.upper()).hex()[:64])
PY
```

Use the public key, certificate pin, and recipient identities to provision both relay invitations, then follow [`docs/courier.md`](../../docs/courier.md). Run the two clients under independent operating-system keyring profiles; a single macOS login has one Arachne client-identity entry and is not an independent client root.

No public-Tor acceptance run has been performed from this checkout. In particular, Docker and the host need outbound Tor connectivity and the hidden service needs time to publish before this topology can prove the end-to-end path.
