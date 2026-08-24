# Signed Component distribution

Zenbu can package and install a signed `wasm-component` plugin on Linux. This
is a local distribution format, not a registry client. It deliberately does
not fetch from a URL, resolve dependencies, select versions across packages,
or distribute `lua-trusted` code. A signature establishes the provenance of a
specific package archive; it does not expand that Component's declared
capabilities or turn Lua into a sandboxed runtime.

The current implementation is Linux-only. It requires Bash, GNU `tar`, GNU
coreutils `date`/`sha256sum`, GNU `mv -T`, and OpenSSL with Ed25519 support.
The package format and its command-line API must be separately validated and
adapted before macOS support is claimed.

## Bundle v1

A `.zcp` archive is a gzip-compressed tarball with exactly four regular-file
members, in this order:

```text
bundle.toml
bundle.sig
payload/zenbu-plugin.toml
payload/plugin.wasm
```

`payload/zenbu-plugin.toml` must be a normal Extension API v1 manifest with
`runtime = "wasm-component"`, `api = 1`, and `entrypoint = "plugin.wasm"`.
The source package is fully staged through `plugin-check` before `bundle`
creates an archive. The installer also stages the extracted payload with the
whole active package root before it makes the package active.

`bundle.toml` is canonical byte-for-byte: precisely the following ten lines,
with no optional or unknown fields. The `sha256:` values are lowercase
hexadecimal SHA-256 digests.

```toml
format = 1
plugin_id = "com.example.plugin"
plugin_version = "1.2.3"
runtime = "wasm-component"
api = 1
signer = "sha256:<sha256-of-public-key-spki-der>"
not_before = "2026-01-01"
expires = "2027-01-01"
manifest_sha256 = "sha256:<sha256-of-payload-manifest>"
component_sha256 = "sha256:<sha256-of-payload-component>"
```

`bundle.sig` is a 64-byte Ed25519 signature over the exact bytes of
`bundle.toml`. The two signed payload hashes bind the manifest and Component
binary. The archive creator fixes member order, ownership, and timestamps, so
identical package bytes, signing key, and validity dates produce identical
bundle bytes. The verifier rejects archives larger than 64 MiB, Components
larger than 32 MiB, manifests larger than 64 KiB, noncanonical metadata,
extra/archive-link members, digest mismatches, and invalid signatures before
the package is staged.

The signer identifier is the SHA-256 hash of the Ed25519 public key's DER
SubjectPublicKeyInfo representation. It identifies a key, not a person or an
online account; the human provenance decision remains the explicit local
trust action below.

## Trust, expiry, and revocation

Generate a signing key, record its key ID, and add the public key to a local
store with an explicit validity range:

```sh
store="${XDG_STATE_HOME:-$HOME/.local/state}/zenbu/component-packages"
not_before=$(date -u +%F)
expires=$(date -u -d '+365 days' +%F)
./scripts/zenbu-component-distribution.sh keygen signing.pem signing.pub
./scripts/zenbu-component-distribution.sh key-id signing.pub
./scripts/zenbu-component-distribution.sh trust-add "$store" signing.pub "$not_before" "$expires"
```

`trust-add` accepts only Ed25519 public keys. Trust is local and
out-of-band—operators are responsible for obtaining a key and deciding whose
packages it represents. There is no embedded key-discovery service, trust-on-
first-use behavior, transparency-log lookup, or key delegation.

Verification applies both the key and bundle UTC date windows. A revoked key,
unknown key, not-yet-valid key/bundle, or expired key/bundle cannot verify,
install, install from cache, or roll back a package:

```sh
./scripts/zenbu-component-distribution.sh revoke "$store" \
  'sha256:0123...cdef' 'maintainer key compromised'
```

Revocation does not asynchronously unload a package already active in an
editor process or silently remove its installed files. That would be a new
host lifecycle policy with availability consequences. Instead it blocks every
future verification/installation operation; use `verify` to audit an archive
and `list` to inventory active package provenance, then explicitly
disable/remove a package under an operator's normal local-change policy. An
existing Zenbu session retains its immutable Component
generation until its usual plugin reload path.

## Build, verify, install, and roll back

Build a guest with the first-party SDK first, then package it:

```sh
./scripts/zenbu-component-package.sh build /path/to/my-component
./scripts/zenbu-component-distribution.sh bundle \
  /path/to/my-component signing.pem my-component-1.2.3.zcp "$not_before" "$expires"
./scripts/zenbu-component-distribution.sh verify my-component-1.2.3.zcp "$store"
./scripts/zenbu-component-distribution.sh install my-component-1.2.3.zcp "$store"
./scripts/zenbu-component-distribution.sh list "$store"
```

The store layout is intentionally inspectable:

```text
STORE/
  trust/<key-hash>.pem, trust/<key-hash>.toml
  revoked/<key-hash>
  cache/<archive-sha256>.zcp
  packages/<plugin-id>/<version>/
  active/<plugin-id> -> /absolute/path/to/STORE/packages/<plugin-id>/<version>
```

An install takes an exclusive local directory lock, verifies into a temporary
directory on the store filesystem, caches the verified archive by its full
SHA-256, and stages a candidate root made from every active package plus the
candidate. Duplicate IDs, registry/binding collisions, manifest errors,
invalid Component ABI/imports, and capability/register failures therefore
fail before the `active/<plugin-id>` symlink changes. The final active-link
replacement uses GNU `mv -T` in the same filesystem. Previous version
directories are retained rather than overwritten.

The lock is deliberately not guessed away after a crash: `install-busy` means
an operator must first determine whether another installer is still running,
then inspect and remove that exact store's `.install.lock` only when it is
stale. This avoids treating an unknown concurrent write as safely abandoned.

Automatic install requires a strictly newer `MAJOR.MINOR.PATCH` version than
the active version; it cannot silently downgrade or replace the same active
version. A previously verified stored version may be activated only after the
same root staging via the explicit rollback command:

```sh
./scripts/zenbu-component-distribution.sh rollback "$store" com.example.plugin 1.2.2
```

Rollback re-verifies the original cached archive against present trust,
revocation, expiry, and payload checks. It therefore intentionally fails if
the cache was removed, was corrupted, or its signer is now revoked/expired.
`install-cached ARCHIVE-SHA256 STORE` is the offline equivalent of installing
an earlier cached archive; it repeats all current verification and normal
version policy. No command makes a network request, so offline operation has
no fallback or freshness ambiguity.

The installer prints a stable failure category with an explanation, for example
`bundle-invalid`, `integrity-mismatch`, `signer-unknown`,
`signer-revoked`, `signer-expired`, `bundle-expired`, `signature-invalid`,
`package-stage-failed`, `rollback-required`, or `cache-corrupt`. Host staging
then preserves the structured extension diagnostic from the normal plugin
loader.

`plugin-root-check ROOT` is the host-side command used for this all-package
staging check. Unlike `plugin-check PACKAGE`, it fails if any discovered
package in `ROOT` fails, including a duplicate ID or a cross-package binding
collision. `plugin-metadata PACKAGE` is a narrow machine-readable manifest
inspection command used by the tooling; it does not replace host staging.

## Boundaries and follow-up

The format has one signed package at a time. It has no dependency declarations,
resolver, package index, marketplace ranking, publishing protocol, remote
download, update polling, permissions UI, key rotation/delegation, or
automatic removal policy. Those are separate decisions: a resolver chooses a
coherent graph, while a marketplace supplies discovery and publication. Neither
is implicit in a signed archive, and neither may bypass capability staging.

The archive's signature is a provenance/integrity check. Component isolation
remains exactly the Wasmtime/host boundary described in
[Component authoring](WASM_COMPONENTS.md) and [the isolation policy](ISOLATION.md);
it is not a claim that every dependency, operating system, signing key, or
native process is safe.

`make component-distribution-test` runs Linux integration coverage for
deterministic bundles, payload tampering, expired bundles and keys, unknown
keys, duplicate IDs, invalid signed upgrades retaining a healthy active
generation, normal upgrade/rollback, offline cache installation, and
revocation. `make check` runs it on Linux and explicitly skips it on non-Linux
hosts until the platform port is verified.
