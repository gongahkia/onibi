# Host Posture Workflow

Piranesi's current stable-alpha workflow assesses a single Debian/Ubuntu VM or
Linux host from local, read-only evidence. Collection runs on the target host and
writes a local evidence bundle. Assessment can run on the same machine or against
the copied bundle elsewhere.

## Install On Debian/Ubuntu

Install development prerequisites from a source checkout:

```bash
sudo apt-get update
sudo apt-get install -y curl git python3 python3-venv
curl -LsSf https://astral.sh/uv/install.sh | sh
git clone https://github.com/gongahkia/piranesi.git
cd piranesi
uv sync
```

Install collection tools on the VM when you want real host evidence:

```bash
# osquery is required for `piranesi collect`.
# Follow the osquery Debian/Ubuntu package instructions for your distribution.
osqueryi --version

# Trivy is optional. Use --no-trivy when it is unavailable or too expensive.
trivy --version
```

## Permissions

Collection is designed to be read-only. It runs osquery queries and optional local
commands for firewall, update, SSH, group, and sysctl evidence. Run it as a user
that can read the host facts you want to assess. Some commands may return partial
data or fail without elevated permissions; those failures are recorded in
`collection-manifest.json` instead of mutating the host.

No LLM credentials are required for deterministic assessment.

## Commands

Check readiness:

```bash
uv run piranesi doctor .
```

Collect evidence without Trivy:

```bash
uv run piranesi collect --output piranesi-evidence --no-trivy
```

Collect with Trivy filesystem vulnerability evidence:

```bash
uv run piranesi collect --output piranesi-evidence --trivy
```

Assess deterministically and write JSON plus Markdown:

```bash
uv run piranesi assess piranesi-evidence \
  --output piranesi-output \
  --analysis deterministic \
  --format both
```

Run the local smoke harness inside a Debian/Ubuntu VM:

```bash
scripts/host_smoke_check.sh
scripts/host_smoke_check.sh --output /tmp/piranesi-smoke
```

The smoke harness runs `doctor`, `collect --no-trivy`, and deterministic `assess`,
then checks that `collection-manifest.json`, `host_metadata`, `top_actions`, and
`snapshot.identity.hostname` are present. Optional tool gaps may appear as coverage
findings or manifest entries.

## Evidence Limitations

Piranesi does not infer insecure state from missing optional command evidence. For
example, missing `ufw`, `iptables`, `nft`, `apt`, `sshd`, `getent`, or `sysctl`
output may reduce confidence or coverage, but it does not create a firewall,
patching, SSH, identity, or kernel finding by itself.

Current deterministic host findings cover:

- Trivy package vulnerabilities when Trivy JSON is present.
- Public high-risk listeners and SSH exposure from osquery listening ports.
- Selected SSH hardening settings from osquery Augeas and `sshd -T`.
- Firewall inactivity when firewall evidence exists and public listeners exist.
- Pending security updates from `apt list --upgradable`.
- Missing `unattended-upgrades` when apt update evidence and package inventory are present.
- Privileged local accounts from osquery users and groups.
- Selected weak sysctl values when command evidence exists.
- Coverage gaps for core evidence classes.

## Troubleshooting

- `osqueryi` missing: install osquery on the Debian/Ubuntu VM. `piranesi collect`
  requires osquery and exits when it cannot produce usable osquery JSON.
- `trivy` missing: rerun with `--no-trivy` or install Trivy. Vulnerability evidence
  is optional.
- `ufw`, `iptables`, or `nft` missing or denied: firewall evidence will be partial.
  Review `raw/commands/*.json` and `collection-manifest.json`.
- `apt` missing: pending update and unattended-upgrades posture cannot be assessed
  from command evidence.
- `sshd` missing or denied: Piranesi falls back to osquery SSH config evidence when
  available.
- `getent` missing: sudo/admin/wheel group membership command evidence is omitted.
- `sysctl` missing: kernel/sysctl findings are skipped unless explicit sysctl values
  are collected.
