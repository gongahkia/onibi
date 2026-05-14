# TODO 11: Add Frictionless Packaging, Install, And First-Run Onboarding

## Goal

Make Piranesi easy to install, evaluate, and trust in the first 10 minutes. Wide
adoption depends on a low-friction path from "I found the project" to "I have a
useful host posture report."

## Current State

Current source-checkout usage works for developers:

```bash
uv sync
uv run piranesi doctor .
uv run piranesi collect --output piranesi-evidence
uv run piranesi assess piranesi-evidence --output piranesi-output
```

This is workable for contributors, but it is too much ceremony for broad users.
The project needs packaged install paths, better first-run diagnostics, and
sample-data demos that do not require a live Linux VM.

## Desired Behavior

Support clear install/evaluation paths:

```bash
pipx install piranesi
piranesi quickstart
piranesi demo --output piranesi-demo-output
piranesi doctor --host
```

Also support containerized evaluation:

```bash
docker run --rm -v "$PWD:/workspace" ghcr.io/gongahkia/piranesi:latest demo
```

The user should be able to generate a meaningful demo report without external
tools, then get precise next steps for real host collection.

## Implementation Notes

Add:

- `piranesi quickstart`: explains the shortest safe path for the current platform.
- `piranesi demo`: assesses bundled host fixtures and writes a complete demo report.
- Stronger `doctor --host` output focused only on host-posture dependencies.
- Published package metadata suitable for PyPI/pipx.
- Release artifacts for GitHub Releases, container images, and checksums.

Keep source-checkout development unchanged.

## Documentation

Update:

- `README.md`
- `docs/getting-started.md`
- `docs/host-posture.md`
- `docs/docker.md`

Add a "Try Piranesi in 10 minutes" section with exact commands.

## Tests

Add tests for:

- `quickstart` exits 0 and prints platform-appropriate next steps.
- `demo` writes JSON and Markdown reports from bundled fixtures.
- `doctor --host` avoids legacy source-code dependency noise.
- Package console script resolves correctly.

## Acceptance Criteria

- A new user can install and run a demo without cloning the repo.
- The first-run path does not require LLM credentials.
- Missing host tools produce actionable next steps.
- Release artifacts are reproducible and checksumed.

## Out Of Scope

- Hosted SaaS signup.
- GUI installer.
- Auto-installing system dependencies.

## Validation Commands

```bash
uv run pytest tests/test_cli.py tests/test_host_posture.py
uv run piranesi demo --output /tmp/piranesi-demo
uv run piranesi quickstart
```

