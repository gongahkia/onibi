# V1 root architecture

## Durable state

`arachne-daemon` owns client durable state. SQLite provides atomic durability only; every
logical record is contained in one canonical CBOR document encrypted with
XChaCha20-Poly1305. A random 32-byte database key exists only in the OS keystore. The
database stores a random database ID, a key ID, a format version, nonce, and ciphertext.
The database ID and format version are AEAD associated data. Missing or invalid keystore
material fails closed. Schema upgrades decrypt, validate, transform, and atomically replace
the document.

## Contacts and sessions

Imported contacts are pending. QR or safety-number confirmation creates verified state.
Verified contacts bind an Ed25519 identity to an independently generated X25519 identity.
Identity rotation requires a canonical statement signed by both old and new Ed25519 keys plus
explicit local confirmation. Revoked identities cannot establish sessions or direct transport.

X3DH uses X25519 identity, ephemeral, signed-prekey, and optional one-time-prekey roles.
The X25519 identity binding is signed by the Ed25519 identity. The KDF input is 32 `0xff`
bytes followed by the concatenated contributory DH outputs; HKDF-SHA256 uses the protocol
domain as info. Ratchet state is sealed durable state. Each session retains at most 10,000
skipped keys and 10,000 consumed-key tombstones.

## Relay and direct transport

A relay signing identity issues canonical mailbox grants. Each grant binds the relay key,
recipient identity, Tor endpoint, mailbox capability, grant ID, issued-at Unix seconds, and a
TTL of at most 30 days.

Direct transport uses QUIC with mutual TLS 1.3. Its self-signed certificates use the existing
Ed25519 identity key. Peers verify certificate self-signature and an exact expected identity;
there is no CA, DNS, or unauthenticated mode. Direct-profile CBOR v1 remains endpoint-only;
verified-contact state supplies the expected peer identity.
