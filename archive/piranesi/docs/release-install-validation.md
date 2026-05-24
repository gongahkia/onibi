# Release Install Validation

Date: 2026-05-20

Status: required before publishing release artifacts.

This checklist validates Piranesi from an external-user installation path. It is
not a publishing workflow and does not upload artifacts to PyPI, GHCR, or any other registry.

## Source Checkout Smoke

Run from a clean checkout:

```bash
uv sync
uv run piranesi --version
uv run piranesi ingest init --workspace /tmp/piranesi-release-source
uv run piranesi report --workspace /tmp/piranesi-release-source --format json
uv run piranesi sign --workspace /tmp/piranesi-release-source
```

## Wheel Smoke

Build and install the wheel into a disposable environment:

```bash
uv build
python -m venv /tmp/piranesi-wheel-venv
/tmp/piranesi-wheel-venv/bin/python -m pip install dist/*.whl
/tmp/piranesi-wheel-venv/bin/piranesi --version
/tmp/piranesi-wheel-venv/bin/piranesi ingest init --workspace /tmp/piranesi-release-wheel
/tmp/piranesi-wheel-venv/bin/piranesi report --workspace /tmp/piranesi-release-wheel --format json
```

## pipx Smoke

Validate the CLI shape through `pipx` before publishing instructions that depend
on it:

```bash
uv build
python -m pipx install --force dist/*.whl
piranesi --version
piranesi ingest init --workspace /tmp/piranesi-release-pipx
piranesi report --workspace /tmp/piranesi-release-pipx --format json
python -m pipx uninstall piranesi
```

## Artifact Checks

Before tagging a release, verify:

- `uv run pytest -q -m "not integration and not joern and not docker and not e2e and not slow"` passes;
- `uv run ruff check src/ tests/` passes;
- `uv run ruff format --check src/ tests/` passes;
- `uv run mypy src/piranesi/` passes;
- built wheels include `piranesi` console script metadata;
- README install instructions match the artifact being published;
- generated reports, signatures, and handoff artifacts stay local to the
  workspace.

## Out of Scope

- Publishing to PyPI.
- Publishing a container image.
- Signing release artifacts or producing provenance attestations.
- Testing hosted services or remote scanners.
