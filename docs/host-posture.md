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

# Lynis is optional. Use --lynis to collect hardening baseline evidence.
# Install on Debian/Ubuntu: sudo apt-get install lynis
lynis --version

# OpenSCAP is optional. Use --openscap to discover oscap for XCCDF ingestion.
# Install on Debian/Ubuntu: sudo apt-get install libopenscap8 ssg-debian
oscap --version
```

## Permissions

Collection is designed to be read-only. It runs osquery queries and optional local
commands for firewall, update, SSH, group, and sysctl evidence. Run it as a user
that can read the host facts you want to assess. Some commands may return partial
data or fail without elevated permissions; those failures are recorded in
`collection-manifest.json` instead of mutating the host.

No LLM credentials are required for deterministic assessment.
When host LLM analysis is enabled, Piranesi sends a redacted structured host
payload to the provider. Hostnames, usernames, IP addresses, MAC addresses, home
paths, command lines, and likely secrets are replaced with stable placeholders.
Package names, service names, ports, severities, and evidence key relationships are
preserved. Reports include `llm_redaction` metadata, and prompt traces contain only
the redacted prompt.

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

Collect with optional Lynis hardening audit:

```bash
uv run piranesi collect --output piranesi-evidence --lynis
```

Collect with optional OpenSCAP discovery:

```bash
uv run piranesi collect --output piranesi-evidence --openscap
```

Collect bounded, redacted authentication/session evidence:

```bash
uv run piranesi collect --output piranesi-evidence --auth-evidence
```

Authentication evidence collection is opt-in. Piranesi does not collect raw auth
logs by default; opt-in command output is bounded and redacted before it is stored.

Assess deterministically and write JSON plus Markdown:

```bash
uv run piranesi assess piranesi-evidence \
  --output piranesi-output \
  --analysis deterministic \
  --format both
```

Host assessment also supports PDF and static dashboard outputs:

```bash
uv run piranesi assess piranesi-evidence --output piranesi-output --format pdf
uv run piranesi assess piranesi-evidence --output piranesi-output --format dashboard
uv run piranesi assess piranesi-evidence --output piranesi-output --format all
```

Generate evidence-bound hypotheses separately from confirmed findings:

```bash
uv run piranesi hypothesize piranesi-evidence --output piranesi-output
```

This writes `host-hypotheses.json` and `host-hypotheses.md`. Hypotheses are not
confirmed findings and do not change `findings_total`, fail-severity exit behavior,
or posture score. Deterministic templates cover compound SSH exposure with missing
auth evidence, public database listeners with missing firewall/config evidence,
package CVE/service linkage ambiguity, and weak kernel hardening with missing patch
evidence. When LLM credentials are configured, hypothesis generation uses the same
host redaction layer as LLM assessment and requires cited evidence, missing
evidence, concise reasoning, and safe follow-up probes or analyst questions.

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
- Lynis warnings and suggestions when `lynis/report.dat` is present in the bundle.
- OpenSCAP failed XCCDF rule results when `openscap/results.xml` is present.
- Control references (CCE, CIS) preserved from OpenSCAP evidence.

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
- `lynis` missing: pass `--lynis` to opt in. Missing Lynis is a health warning, not
  a false finding. You can also place `lynis/report.dat` in a raw bundle.
- `oscap` missing: pass `--openscap` to opt in. Live profile execution requires
  distribution-specific SCAP content packages. Place `openscap/results.xml` in the
  bundle for ingestion.

## Baseline Evidence Structure

When Lynis or OpenSCAP evidence is present in a bundle, Piranesi parses it into
typed `BaselineCheck` models and converts failed/warn checks into deterministic
findings with evidence, remediation, and control references.

```text
evidence-bundle/
  lynis/
    report.dat
  openscap/
    results.xml
```

Baseline checks are conservative: only checks with `fail` or `warn` results become
findings. Passed and not-applicable checks are recorded in the snapshot but do not
generate findings.

## Adaptive Probing

Piranesi supports a staged adaptive probing workflow where initial findings drive
follow-up evidence collection. This is an explicit two-step process for auditability.

### Generate a Probe Plan

```bash
uv run piranesi probe piranesi-evidence --output probe-plan.json
```

The probe plan is a JSON file listing follow-up data collection probes suggested by
initial findings. Review it before execution:

```bash
cat probe-plan.json | python3 -m json.tool
```

### Execute Follow-up Probes

```bash
uv run piranesi collect-followup probe-plan.json --output piranesi-evidence-followup
```

Probe plans generated by `piranesi probe` record the original evidence bundle path.
`collect-followup` merges that base evidence into the follow-up output, writes
canonical follow-up command output under `raw/commands/` or `raw/osquery/`, and
keeps audit artifacts in `raw/followup/`.

### Safety Guarantees

- **Allowlist only**: Only statically defined command and osquery templates can execute.
- **No shell invocation**: All commands use argument arrays via `subprocess.run`.
- **Tamper detection**: Modified commands (different from the allowlisted template) are
  rejected at execution time.
- **Unknown probes rejected**: Probe IDs not in the allowlist are rejected and logged.
- **Plans are reviewable**: Probe plans are JSON files meant to be inspected before
  execution.

### Probe Examples

| Finding | Probes Generated |
|---------|-----------------|
| Public SSH + password auth | `last`, `lastb`, `sshd -T` |
| Public Redis | Process detail, systemd unit |
| Missing firewall evidence | `ufw status`, `iptables -S`, `nft list ruleset` |
| Privileged user | Sudoers entries, group membership |

### Follow-up Bundle Structure

```text
piranesi-evidence-followup/
  host_snapshot.json
  collection-manifest.json
  raw/
    osquery/
    commands/
    followup/
      probe-plan.json
      probe-results.json
      last_logins.json
      redis_process_detail.json
```

Re-assess after follow-up collection:

```bash
uv run piranesi assess piranesi-evidence-followup --output piranesi-output --format both
```
