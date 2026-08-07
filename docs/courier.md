# Onion courier

The active courier path is `arachne courier`. It keeps a separately started background process from `arachne daemon serve`, which remains the shared-IP mesh daemon.

Each courier user needs a verified Arachne contact and a recipient-bound relay invitation. A relay administrator provisions the invitation against the relay database:

```text
arachne-relay provision-invite \
  --config /absolute/path/relay.conf \
  --identity /absolute/path/relay-identity \
  --recipient-public-key <verified-contact-ed25519-public-key-hex> \
  --onion-service-public-key <onion-v3-ed25519-public-key-hex> \
  --virtual-port 443
```

The relay must serve TLS for courier clients. Run it with a pinned PEM certificate and key:

```text
arachne-relay run --config /absolute/path/relay.conf --identity /absolute/path/relay-identity \
  --tls-certificate /absolute/path/relay.pem --tls-private-key /absolute/path/relay.key
```

Create and exchange a signed courier bundle only after normal contact verification:

```text
arachne courier bundle create --state-directory /absolute/path/alice \
  --relay-invitation <relay-invitation-hex> --relay-tls-pin <sha256-of-relay-certificate-hex>
arachne courier bundle export --state-directory /absolute/path/alice
arachne courier bundle import --state-directory /absolute/path/bob --bundle <alice-bundle-hex>
```

The receiving side must first be a `verified` contact. Bundle import validates the publisher signature, X3DH prekey material, recipient-bound invitation, relay TLS pin, and invitation validity window.

Create `/absolute/path/alice/courier.conf`:

```text
config_version = 1
state_directory = "/absolute/path/alice"
socks_proxy = "127.0.0.1:9050"
poll_interval_seconds = 5
```

Start one daemon for each client:

```text
arachne courier daemon --config /absolute/path/alice/courier.conf
```

Each state directory is a distinct local client profile. Its opaque profile
identifier scopes the macOS Keychain entries for the signing identity and
courier X25519 material, so `/absolute/path/alice` and
`/absolute/path/alice-work` can run independently under the same macOS user.
Do not copy a state directory without its Keychain entries; that creates an
unrecoverable profile rather than a second client.

Create or inspect the identity that belongs to a profile with the same state
directory. Contact invitations use that profile identity as well:

```text
arachne identity create --state-directory /absolute/path/alice
arachne contact invitation create --state-directory /absolute/path/alice
```

Queue text or a file while the daemon is running:

```text
arachne courier send --config /absolute/path/alice/courier.conf \
  --recipient-public-key <bob-public-key-hex> --text "hello" --ttl-seconds 86400

arachne courier attachment send --config /absolute/path/alice/courier.conf \
  --recipient-public-key <bob-public-key-hex> --path /absolute/path/report.pdf
```

Text is X3DH-bootstrapped then Double-Ratchet encrypted. A relay-hosted,
requester-authenticated directory leases one published one-time prekey during
the first bootstrap; the receiver replenishes its inventory below the local
threshold and publishes a higher-generation signed bundle. An advertised OTK
is never offered by a later bundle, while its private half remains locally to
accept delayed bootstrap frames.

Files are padded into fixed 64 KiB chunks, encrypted independently, and first
staged under `attachment-uploads` with an encrypted courier job. The daemon,
not the CLI command, resumes chunk transfer from the durable upload journal,
then queues the ratchet-encrypted attachment descriptor. Staged data is kept
until recipient ACK or local expiry. The recipient reads the text inbox and
materializes a received attachment explicitly:

```text
arachne courier inbox --state-directory /absolute/path/bob
arachne courier attachment receive --config /absolute/path/bob/courier.conf \
  --message-identifier <message-identifier-hex> --output /absolute/path/download.pdf
```

A relay storage receipt is not a delivery acknowledgement. The sender’s outbox
records a successful relay upload separately and does not intentionally upload
the same envelope again on later polls. It remains pending until the recipient
daemon has authenticated the message and returned an Ed25519-signed ACK frame.
Courier event logs deliberately record only aggregate counts and error classes.

[`deploy/courier-acceptance`](../deploy/courier-acceptance) supplies the three-service relay/onion/SOCKS Compose topology for the end-to-end acceptance run. Real public-Tor acceptance has not been run in this checkout. The local test suite exercises the X3DH/ratchet path, SOCKS connector, pinned TLS transport, relay mailbox behavior, and attachment cryptography independently; it does not prove that an onion service has published or that two independent OS keystores have completed the command sequence.
