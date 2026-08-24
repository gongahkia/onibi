# ADR 0033: signed local Component bundles remain separate from resolution

## Status

Accepted for Linux.

## Context

M8/M9 can safely discover and stage local packages but previously leave package
provenance, portable transfer, trust, cache, upgrades, and rollback entirely to
the operator. A distribution mechanism must not turn a signature into ambient
authority, bypass complete-root staging, or quietly introduce a registry and
dependency solver.

## Decision

Zenbu defines a Linux-only `.zcp` Component bundle: a canonical metadata file,
an Ed25519 signature over that metadata, and exactly the manifest and
`plugin.wasm` payload files. Signed metadata binds a plugin ID, SemVer-core
version, Extension API/runtime, signer key identifier, validity dates, and
SHA-256 payload hashes. Key identifiers are the SHA-256 hash of DER public-key
SPKI bytes.

An operator adds public keys to a local trust store with explicit date bounds
and may irreversibly revoke a key. Verification has no network behavior and
requires current key/bundle validity, non-revocation, a trusted signer,
canonical metadata, matching hashes, and a valid signature. A cache stores
verified archive bytes by SHA-256; cache installation repeats verification.

Installation stages a temporary candidate Component with every active package
using the ordinary Extension host. Only after that succeeds does it publish an
immutable version directory and atomically replace the active package symlink.
Automatic update must be strictly newer than the active SemVer-core version;
explicit rollback re-verifies the retained original archive and stages the
selected version before repointing that same symlink.

## Consequences

An invalid, malformed, expired, untrusted, revoked, incompatible, or colliding
bundle cannot replace a healthy active package generation. Existing version
directories and cached archives make intentional rollback possible, subject to
the current trust policy. Revocation blocks later package operations but does
not asynchronously kill a running session or mutate active files; that requires
a separate lifecycle policy.

This decision requires GNU/Linux command behavior and OpenSSL's Ed25519 command
interface. It is not macOS support. A macOS port must validate archive,
time, filesystem-rename, key-format, and CI behavior before claiming parity.

The decision intentionally leaves dependency graphs, remote transport,
registry protocol, key discovery/rotation, marketplace policy, untrusted Lua
distribution, and broader sandbox claims for explicit future work.
