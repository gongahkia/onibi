# Public API compatibility policy

## Public interfaces

The publishable Rust API is `arachne-sdk`; its documented re-exports are the supported Rust surface. `arachne-core`, `arachne-protocol`, `arachne-daemon`, `arachne-daemon-api`, `arachne-relay`, `arachne-ffi`, and `arachne-cli` are internal Rust crates and set `publish = false`.

The C ABI is an external interface, but not a publishable Rust API. Its only compatibility identifier is `ARACHNE_ABI_VERSION`; callers must negotiate the exact value before invoking versioned ABI operations. C ABI ownership, threading, and callback rules are defined in `crates/arachne-ffi/README.md`.

Wire compatibility is independent of package compatibility. `ProtocolVersion` negotiation and the fail-closed schema rules in `PROTOCOL_COMPATIBILITY.md` define the protocol contract. Daemon protobufs, local state formats, release manifests, and CLI output are not general Rust API contracts; each must carry and validate its own explicit format version before becoming externally consumable.

## Versioning rules

`arachne-sdk` is currently `0.1.0`. Its public `SDK_API_VERSION` is the package major/minor pair and must be updated in the same release as the package version.

Before `1.0.0`, `SdkApiVersion::supports` accepts only an exact major/minor version. A breaking SDK change must increment the minor component (`0.1` to `0.2`); a compatible change increments the patch component. This is deliberately stricter than treating the initial line as stable, and remains compatible with Cargo's left-most-nonzero version convention.

At `1.0.0` and later, a provider supports a required SDK version only when both versions have the same major component and the provider minor is at least the required minor. Remove, rename, or semantically weaken a public SDK item only in the next major release. Additions require a minor release; fixes preserving the documented contract require a patch release. Deprecated items remain available through the current major line.

Changing a Cargo feature's availability, a public type's layout or exhaustiveness, platform support, security behavior, bounds, or error semantics is treated as a breaking change unless the existing public contract explicitly permits it.

## Compatibility checks

Every release that changes `arachne-sdk` must:

1. Update the package version and `SDK_API_VERSION` together.
2. Run `cargo test -p arachne-sdk --test api_version_contract --locked` to exercise the published Rust boundary.
3. Run `cargo test -p arachne-core --test workspace_policy --locked` to verify scope and policy enforcement.
4. Run `cargo fmt --check`, `cargo clippy --workspace --all-targets --locked -- -D warnings`, and the affected tests.

The ABI and protocol version numbers do not follow the SDK package version. Their compatibility checks remain exact negotiation and protocol-schema validation respectively.
