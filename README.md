![](https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg)

# Piranesi

Piranesi is an alpha, local-first VM and Linux host posture assessment CLI. It turns
host evidence into a focused vulnerability and exposure report for VM sandbox,
homelab, lab-infra, and security review workflows.

The current center of gravity is a local VM snapshot workflow: run `piranesi collect`
on a Linux VM or host, run `piranesi assess`, and review JSON/Markdown reports. The
first supported raw evidence bundle format is osquery plus Trivy JSON. Deterministic
analysis works without LLM credentials; optional LLM analysis can add evidence-bound
posture reasoning when a LiteLLM-compatible API key is configured.

## Status

`v0.2.0` is being pivoted from the earlier JavaScript/TypeScript SAST prototype into
the VM vulnerability sandbox proposal. The legacy source-code pipeline still exists
internally for now, but it is no longer the primary public use case.

Phase 1 targets Debian/Ubuntu-style Linux host evidence and produces a snapshot
report. It does not yet ship fleet dashboards, ticket sync, PDF export, Windows
support, or cloud inventory ingestion.

## What It Does

- Collects local VM/host evidence with osquery and optional Trivy.
- Loads a canonical `host_snapshot.json` or a raw evidence bundle directory.
- Normalizes osquery host facts: OS, kernel, packages, listening ports, users,
  services, network interfaces, process inventory, sudo evidence, selected SSH
  configuration, firewall/update command evidence, and selected kernel sysctl values.
- Reads Trivy JSON output for package vulnerability evidence.
- Flags exposed high-risk services, public SSH exposure, SSH hardening gaps,
  privileged local accounts, package CVEs, pending security updates, missing
  unattended security update automation, weak sysctl values, and missing evidence
  coverage.
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

uv run piranesi doctor .

uv run piranesi collect --output piranesi-evidence

uv run piranesi assess piranesi-evidence \
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

Use the bundled development fixture without collecting local host evidence:

```bash
uv run piranesi assess tests/fixtures/host/debian-vulnerable \
  --output piranesi-output
```

Use optional evidence-bound LLM reasoning:

```bash
OPENAI_API_KEY=... uv run piranesi assess path/to/evidence-bundle \
  --analysis both \
  --output piranesi-output
```

## Raw Bundle Layout

`piranesi collect` writes a collector layout:

```text
piranesi-evidence/
  host_snapshot.json
  collection-manifest.json
  raw/
    osquery/
      system_info.json
      os_version.json
      kernel_info.json
      interface_addresses.json
      deb_packages.json
      listening_ports.json
      processes.json
      users.json
      systemd_units.json
      sshd_config.json
      sudoers.json
    trivy/
      results.json
    commands/
      apt_upgradable.json
      ufw_status.json
      iptables_rules.json
      nft_ruleset.json
      sshd_effective_config.json
      group_sudo.json
      group_admin.json
      group_wheel.json
      sysctl_net_ipv4_ip_forward.json
      sysctl_net_ipv6_conf_all_forwarding.json
      sysctl_kernel_unprivileged_bpf_disabled.json
      sysctl_kernel_kptr_restrict.json
```

Piranesi also accepts a hand-built bundle containing `osquery/*.json` and/or
`trivy/*.json` at the bundle root. Command evidence may also be provided at
`commands/*.json`:

```text
evidence-bundle/
  osquery/
    system_info.json
    os_version.json
    kernel_info.json
    interface_addresses.json
    deb_packages.json
    listening_ports.json
    processes.json
    users.json
    sshd_config.json
    sudoers.json
  trivy/
    results.json
  commands/
    apt_upgradable.json
    ufw_status.json
    sysctl_kernel_kptr_restrict.json
```

If `host_snapshot.json` exists at the bundle root, Piranesi treats it as the canonical
input and skips raw bundle normalization.

JSON reports include `host_metadata` and `top_actions` in addition to the canonical
snapshot and finding list. Markdown reports render matching `Host Metadata` and
`Top Actions` sections for operator review.

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
uv run piranesi collect --output piranesi-evidence --no-trivy
uv run piranesi assess piranesi-evidence
scripts/host_smoke_check.sh
```

The older source-code analysis modules are still in the tree during the pivot. Treat
new work as belonging to the host posture surface unless a change explicitly supports
migration or backward compatibility.

## License

Apache 2.0

<div align="center">
    <img src="./asset/logo/imaginary-prisons.jpg" width="50%">
</div>
