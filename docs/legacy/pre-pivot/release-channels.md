# Release Channels

Piranesi release distribution is split across Python packages, container images,
checksums, provenance attestations, and recurring smoke tests.

## PyPI And pipx

`.github/workflows/publish-pypi.yml` builds the wheel and sdist for release
tags, checks release metadata, verifies the tag matches `pyproject.toml`, writes
public schemas into `dist/`, generates `SHA256SUMS`, attaches build provenance,
and publishes through PyPI trusted publishing.

Install path for users:

```bash
pipx install piranesi
piranesi quickstart
piranesi demo --output piranesi-demo-output
```

## Container Images

`.github/workflows/publish-container.yml` publishes versioned GHCR images for
release tags and `latest` for published releases:

```bash
docker run --rm \
  -v "$PWD:/workspace" \
  ghcr.io/gongahkia/piranesi:<version> \
  demo --output /workspace/piranesi-demo-output
```

The image is built from `Dockerfile`, uses a version-pinned Python slim base, and
runs as a non-root user.

## Checksums And Provenance

Release artifacts include `SHA256SUMS` generated from `dist/*`. GitHub Actions
build provenance attestations are emitted for Python artifacts and container
images. Verification flow:

```bash
shasum -a 256 -c dist/SHA256SUMS
```

Use GitHub's artifact attestation tooling to verify provenance for artifacts
downloaded from GitHub releases or GHCR.

## Recurring Smoke Tests

`.github/workflows/release-smoke.yml` runs weekly and on demand. It builds
artifacts, verifies checksums, installs the built wheel with `pipx`, runs
`piranesi quickstart`, runs the bundled demo, and exports the host report schema
from the installed package.

This smoke lane is intentionally small and deterministic; it does not install
system tools such as osquery, Trivy, Joern, Docker, or Kubernetes clients.

