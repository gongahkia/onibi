# Cargo Feature Policy

All workspace crates declare `default = []`; every capability is explicit opt-in.

- Features must be additive and safe in every combination.
- Features must not weaken validation, encryption, authentication, privacy, delivery-profile constraints, or protocol compatibility.
- A feature must not change a wire format or a security policy; versioned protocol negotiation or explicit runtime configuration owns those changes.
- Private optional dependencies use `dep:` feature references; public flags describe the capability, not a dependency name.
- Each new feature documents its enabled dependencies, compatible combinations, and protocol or security effect here.
- CI tests the workspace with `--no-default-features` and `--all-features`.

## `arachne-core/test-fixtures`

- Enables deterministic bounded byte fixtures and synthetic `Secret<Vec<u8>>` test inputs; no dependencies.
- Compatible with every workspace feature combination.
- Does not alter protocol behavior or production security policy; callers provide explicit deterministic seeds and fixture lengths.
