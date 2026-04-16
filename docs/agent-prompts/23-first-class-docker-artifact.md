# Prompt: Make Docker Image a First-Class Supported Artifact

You are working in Piranesi, a Python AppSec CLI. Users should be able to run it consistently through Docker without installing the Python toolchain locally.

Goal: add first-class Docker support for running scans and CI workflows.

Implementation requirements:

- Inspect existing Docker files, pyproject metadata, and docs.
- Add or improve a Dockerfile that installs Piranesi with deterministic dependencies.
- Add a `.dockerignore` if missing.
- Document volume mounts, working directory, configuration path, output directory, and examples for scanning a local repo.
- Include non-root execution where practical.
- Add a lightweight build or smoke-check script if CI can support it.
- Avoid embedding secrets or API keys into the image.

Acceptance criteria:

- A user can build and run Piranesi via Docker with one documented command.
- The image supports deterministic/no-LLM mode by default and optional LLM keys through environment variables.
- Docker docs cover common permission/output pitfalls.
