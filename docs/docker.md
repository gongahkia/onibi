# Docker Usage

Piranesi ships a first-class Docker image flow so you can scan without installing
Python/Node/Joern on the host.

## Build

```bash
docker build -t piranesi:local .
```

## Scan A Local Repository

Run from the repository you want to scan:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$PWD":/workspace \
  -w /workspace \
  piranesi:local \
  run . --authorized --yes --no-execute --output /workspace/piranesi-output
```

This command keeps scans deterministic/no-LLM by default (`--no-execute` + no API key).

## Optional LLM Keys

Pass keys at runtime only (never bake them into the image):

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e OPENAI_API_KEY \
  -v "$PWD":/workspace \
  -w /workspace \
  piranesi:local \
  run . --authorized --yes --output /workspace/piranesi-output
```

## Config, Output, And Mounts

- Working directory in container: `/workspace`
- Default config path: `./piranesi.toml` (relative to `/workspace` when `-w /workspace`)
- Explicit config path example: `--config /workspace/piranesi.toml`
- Output directory should be mounted path: `--output /workspace/piranesi-output`

## Permission Pitfalls

- Use `--user "$(id -u):$(id -g)"` to avoid root-owned output files on Linux/macOS.
- If output files are already root-owned from older runs, fix once with `sudo chown -R "$USER" piranesi-output`.

## Verify Stage Note

`verify` requires Docker-in-Docker style runtime capabilities if you execute it from inside
this container. For host-only deterministic scans, keep `--no-execute`.

## Smoke Check Script

```bash
scripts/docker_smoke_check.sh
```

This script builds the image and checks `piranesi --version` / `piranesi --help`.
