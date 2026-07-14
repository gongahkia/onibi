# Protocol Compatibility Policy

Version 1 core protocol schemas use exact, definite-length CBOR arrays.

- Unknown or extra fields, indefinite arrays, trailing bytes, unknown enum values, and unsupported schema versions are rejected.
- Receivers do not skip unknown core fields or reinterpret them with defaults.
- New optional protocol data must use the versioned extension-frame schema and explicit version negotiation.
- A sender must not depend on a peer accepting an extension unless that support is explicitly negotiated.
