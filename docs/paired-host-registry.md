# Paired Host Registry

The PWA `Hosts` control stores host ID, display name, public identity, endpoint,
and local state in encrypted IndexedDB records. Its schema has no pair URL,
owner-cookie, enrollment-secret, private-key, or SSH-password field; mesh and
relay endpoints are HTTPS origins only.

`Revoke` marks the local record as revoked and prevents this browser from
reactivating it. It does not revoke the host at the fleet hub; use the hub
revocation control for that authority action.

Export uses a versioned, password-encrypted and authenticated envelope:

- PBKDF2-SHA-256 with a random 16-byte salt and 600000 iterations.
- AES-256-GCM with a random 12-byte nonce and fixed versioned associated data.
- A transfer passphrase must contain at least 12 UTF-8 bytes.

Imports reject malformed or incompatible envelopes, failed authentication,
duplicate/conflicting IDs, and stale or revoked source records. Only pending
and active metadata is transferable.
