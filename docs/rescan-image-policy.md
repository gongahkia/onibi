# Rescan Image Policy

Rescan replay images must be immutable. Piranesi accepts only image references that
include an explicit SHA-256 digest:

```text
ghcr.io/org/scanner:v1@sha256:<64-hex-digest>
ghcr.io/org/scanner@sha256:<64-hex-digest>
```

Rejected:

- `scanner:latest`
- `scanner:v1`
- `scanner:latest@sha256:<digest>`
- non-`sha256` or malformed digests

The accepted digest is recorded separately from the human-facing image reference so
future replay provenance can prove exactly which image was used. Piranesi does not
auto-select images or silently trust mutable registry tags.
