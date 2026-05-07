![](https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg)

# Piranesi

Piranesi is an alpha, local-first VM and Linux host posture assessment CLI. It turns
host evidence into a focused vulnerability and exposure report for VM sandbox,
homelab, lab-infra, and security review workflows.

The current center of gravity is a snapshot workflow: collect evidence from a VM or
host, run `piranesi assess`, and review JSON/Markdown reports. The first supported
raw evidence bundle format is osquery plus Trivy JSON. Deterministic analysis works
without LLM credentials; optional LLM analysis can add evidence-bound posture
reasoning when a LiteLLM-compatible API key is configured.

## Status

`v0.2.0` is being pivoted from the earlier JavaScript/TypeScript SAST prototype into
the VM vulnerability sandbox proposal. The legacy source-code pipeline still exists
internally for now, but it is no longer the primary public use case.

Phase 1 targets Debian/Ubuntu-style Linux host evidence and produces a snapshot
report. It does not yet ship a local collector, fleet dashboard, ticket sync, PDF
export, Windows support, or cloud inventory ingestion.

## What It Does

- Loads a canonical `host_snapshot.json` or a raw evidence bundle directory.
- Normalizes osquery host facts: OS, kernel, packages, listening ports, users,
  services, and selected SSH configuration.
- Reads Trivy JSON output for package vulnerability evidence.
- Flags exposed high-risk services, public SSH exposure, SSH hardening gaps,
  privileged local accounts, package CVEs, and missing evidence coverage.
- Writes `host-report.json` and/or `host-report.md`.
- Supports deterministic, LLM-only, or combined analysis modes.

## Requirements

- Python 3.12+
- `uv` for source-checkout development
- Optional: osquery on the assessed VM/host
- Optional: Trivy for package vulnerability evidence
- Optional: one LiteLLM-compatible API key for `--analysis llm` or `--analysis both`

Supported LLM environment variables are `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`,
`OPENROUTER_API_KEY`, `AZURE_OPENAI_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, and
`LITELLM_API_KEY`.

## Quick Start

```bash
uv sync
uv run piranesi --version

uv run piranesi assess tests/fixtures/host/debian-vulnerable \
  --output piranesi-output \
  --analysis deterministic \
  --format both
```

This writes:

- `piranesi-output/host-report.json`
- `piranesi-output/host-report.md`

Use a canonical snapshot directly:

```bash
uv run piranesi assess path/to/host_snapshot.json --output piranesi-output
```

Use optional evidence-bound LLM reasoning:

```bash
OPENAI_API_KEY=... uv run piranesi assess path/to/evidence-bundle \
  --analysis both \
  --output piranesi-output
```

## Raw Bundle Layout

Piranesi accepts a directory containing `osquery/*.json` and/or `trivy/*.json`:

```text
evidence-bundle/
  osquery/
    system_info.json
    os_version.json
    kernel_info.json
    deb_packages.json
    listening_ports.json
    users.json
    sshd_config.json
  trivy/
    results.json
```

If `host_snapshot.json` exists at the bundle root, Piranesi treats it as the canonical
input and skips raw bundle normalization.

## Canonical Snapshot Shape

The stable internal interchange format is `HostSnapshot`:

```json
{
  "schema_version": 1,
  "identity": { "hostname": "debian-vm-01" },
  "os": { "name": "Ubuntu", "version_id": "22.04" },
  "kernel": "5.15.0-101-generic",
  "packages": [],
  "listening_ports": [],
  "processes": [],
  "services": [],
  "users": [],
  "config": {},
  "tool_provenance": {},
  "raw_evidence": {}
}
```

See `tests/fixtures/host/debian-clean/host_snapshot.json` for a complete example.

## Development

```bash
uv sync
uv run pytest tests/test_host_posture.py
uv run piranesi assess tests/fixtures/host/debian-vulnerable
```

The older source-code analysis modules are still in the tree during the pivot. Treat
new work as belonging to the host posture surface unless a change explicitly supports
migration or backward compatibility.

## License

Apache 2.0

<div align="center">
    <img src="./asset/logo/imaginary-prisons.jpg" width="50%">
</div>
