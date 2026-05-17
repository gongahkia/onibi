> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Host Posture Workflow

Piranesi's current stable-alpha workflow assesses a single Debian/Ubuntu VM or
Linux host from local, read-only evidence. Collection runs on the target host and
writes a local evidence bundle. Assessment can run on the same machine or against
the copied bundle elsewhere.

## Install On Debian/Ubuntu

For packaged use:

```bash
python3 -m pip install --user pipx
python3 -m pipx ensurepath
pipx install piranesi
piranesi quickstart
piranesi demo --output piranesi-demo-output
piranesi doctor --host
```

The demo uses bundled fixtures and deterministic analysis, so it does not require
osquery, Trivy, a Linux VM, or LLM credentials. Use it first to inspect the report
shape, then run `piranesi doctor --host` on the machine where collection will run.

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

## Platform Coverage

| Platform family | Tier | Package inventory | Update evidence | Firewall evidence | Notes |
| --- | --- | --- | --- | --- | --- |
| Debian/Ubuntu | Stable Alpha | `deb_packages` | `apt list --upgradable` | `ufw`, `iptables`, `nft` | Includes unattended-upgrades and selected sysctl checks. |
| RHEL/CentOS/Rocky/Alma/Fedora | Alpha | `rpm_packages` | `dnf updateinfo list security`, `yum updateinfo list security` | `firewall-cmd --state`, `iptables`, `nft` | SELinux state is parsed from `getenforce`. Debian-only checks are health warnings, not findings. |
| Amazon Linux | Alpha | `rpm_packages` | `yum`/`dnf` security update output | `firewall-cmd --state`, `iptables`, `nft` | Treated as an RPM-family platform with Amazon Linux metadata. |
| Alpine | Experimental | `apk_packages` | `apk version -l '<'` | Supplied raw evidence only | No unattended-upgrades finding; unsupported Debian/systemd checks are health warnings. |
| macOS | Experimental read-only posture | Snapshot/raw evidence only | Not implemented | Not implemented | No Windows support is implemented. |

## Commands

Check readiness:

```bash
uv run piranesi doctor --host
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

Apply deterministic policy-as-code gates:

```bash
uv run piranesi policy validate examples/policies/production-linux.toml
uv run piranesi assess piranesi-evidence \
  --output piranesi-output \
  --policy examples/policies/production-linux.toml
```

Policy results are written into `policy_profile`, `policy_summary`,
`policy_gate_results`, and `required_evidence_status` fields in JSON reports.
See [docs/policy-as-code.md](policy-as-code.md) for the policy language and
starter profiles.

Generate a review-only remediation plan from a completed report:

```bash
uv run piranesi remediate plan piranesi-output/host-report.json \
  --output piranesi-output/remediation-plan.md
uv run piranesi remediate checklist piranesi-output/host-report.json --format markdown
```

After applying fixes through your normal change process, collect and assess again,
then compare before/after reports:

```bash
uv run piranesi host diff \
  piranesi-output-before/host-report.json \
  piranesi-output-after/host-report.json
uv run piranesi remediate verify \
  piranesi-output-before/host-report.json \
  piranesi-output-after/host-report.json
```

The diff classifies findings as `new`, `fixed`, `changed`, `unchanged`, and
`suppressed`. See [docs/remediation-workflow.md](remediation-workflow.md) for the
full closure workflow.

Host assessment also supports PDF and static dashboard outputs:

```bash
uv run piranesi assess piranesi-evidence --output piranesi-output --format pdf
uv run piranesi assess piranesi-evidence --output piranesi-output --format dashboard
uv run piranesi assess piranesi-evidence --output piranesi-output --format all
```

Inspect existing host or fleet reports in the local-only review workbench:

```bash
uv run piranesi ui piranesi-output
uv run piranesi ui fleet-output
uv run piranesi ui --watch piranesi-output
```

The UI binds to `127.0.0.1` by default, serves only embedded local assets, and
uses redacted summary API responses. See [docs/local-ui.md](local-ui.md) for
security and review workflow details.

Validate community host rules, fixtures, and benchmark submissions locally:

```bash
uv run piranesi host rule scaffold "Disable risky service"
uv run piranesi host rule test-all rules/community/host
uv run piranesi host fixture validate tests/fixtures/host/debian-vulnerable
uv run piranesi host benchmark submit --fixture tests/fixtures/host/debian-vulnerable
```

Community host rules are constrained TOML data and cannot execute shell commands
or import Python. See [docs/contributing-host-rules.md](contributing-host-rules.md)
and [docs/community-benchmarks.md](community-benchmarks.md).

Assess adjacent local infrastructure evidence separately from host posture:

```bash
uv run piranesi container assess --image tests/fixtures/container/trivy-image.json --output piranesi-container-output
uv run piranesi container assess --docker-host local --output piranesi-containers
uv run piranesi k8s assess tests/fixtures/k8s --output piranesi-k8s-output
```

Container and Kubernetes reports use compatible evidence inventory, risk, and top
action concepts without folding infrastructure resources into `HostSnapshot`.
See [docs/container-kubernetes.md](container-kubernetes.md).

Export public JSON schemas for integrations:

```bash
uv run piranesi schema host-report --output host-report.schema.json
uv run piranesi schema host-snapshot --output host-snapshot.schema.json
uv run piranesi schema fleet-report --output fleet-report.schema.json
```

The same contracts are available through the Typer-free Python API:

```python
from piranesi.host.api import assess_host_bundle

report = assess_host_bundle("piranesi-evidence")
payload = assess_host_bundle("piranesi-evidence", format="dict")
```

See [docs/api.md](api.md) for schema compatibility rules and public/internal
module boundaries.

Export findings into common security workflows:

```bash
uv run piranesi export sarif piranesi-output/host-report.json --output host.sarif.json
uv run piranesi export csv fleet-output/fleet-report.json --output fleet-findings.csv
uv run piranesi export github-issues piranesi-output/host-report.json --dry-run
uv run piranesi export jira piranesi-output/host-report.json --project SEC --dry-run
```

See [docs/integrations.md](integrations.md) for redaction defaults, webhook
delivery, and ticket creation requirements.

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

Assess a local fleet evidence directory:

```bash
uv run piranesi fleet assess fleet-evidence --output fleet-output
uv run piranesi fleet summarize fleet-output
```

The fleet input directory should contain one child evidence bundle per host, for
example `fleet-evidence/vm-001/host_snapshot.json` or
`fleet-evidence/vm-002/raw/osquery/...`. The output includes per-host reports under
`fleet-output/hosts/<name>/` plus `fleet-report.json` and `fleet-report.md`.
Individual host failures are recorded and assessment continues unless `--fail-fast`
is set. Fleet assessment supports `--fail-severity` and
`--treat-private-as-public`.

One manual collection pattern is:

```bash
scp -r vm-001:/tmp/piranesi-evidence fleet-evidence/vm-001
scp -r vm-002:/tmp/piranesi-evidence fleet-evidence/vm-002
uv run piranesi fleet assess fleet-evidence --output fleet-output
```

Piranesi can also collect the same reassessable bundle layout over SSH without
installing an agent:

```bash
uv run piranesi remote collect --host vm-001 --output fleet-evidence/vm-001 --no-trivy
uv run piranesi remote collect --hosts hosts.txt --output fleet-evidence --jobs 4 --no-trivy
uv run piranesi remote doctor --hosts hosts.txt --no-trivy
uv run piranesi fleet assess fleet-evidence --output fleet-output
```

Remote collection runs read-only commands through SSH and records every command
array in `collection-manifest.json`. `--dry-run` prints the planned command arrays
without writing evidence. Sudo-dependent evidence is skipped unless `--sudo-mode`
is set to `prompt` or `passwordless`; use `--no-trivy` when you want only
non-sudo host posture signals.

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
- Legacy control references (CCE, CIS) preserved from OpenSCAP evidence.
- Structured CIS/NIST/Lynis/OpenSCAP control references attached to host findings.

## Risk Ranking

Every host finding includes a deterministic `risk` object in JSON and a risk
summary in Markdown. The score combines severity, confidence, exploitability,
blast radius, remediation urgency, and evidence quality into a `total` from 0 to
100. Ranking and top actions use this risk total first, then severity and
confidence as tie-breakers.

Risk scoring uses only collected evidence and optional local intel embedded in the
bundle. It does not make live network calls during `assess`. Coverage findings are
capped so missing evidence can still guide follow-up without outranking direct
exposure, vulnerability, identity, or baseline findings.

## Control Mapping

Each `HostFinding` keeps the compatibility `control_refs` string list and adds
`structured_control_refs`. A structured control reference includes:

- framework
- version when known
- control ID or broad family/category
- title
- mapping confidence from 0.0 to 1.0
- rationale

`HostPostureReport` also includes `control_summary`, which groups mapped findings
by framework and records mapped control count, highest severity, and average
mapping confidence.

Supported host-control mapping families:

- CIS Ubuntu Linux broad hardening families for SSH, firewall, patching, kernel,
  privileged accounts, and vulnerability management.
- NIST CSF 2.0 categories such as `PR.AA`, `PR.PS`, `DE.CM`, `ID.AM`, and
  `ID.RA`.
- NIST SP 800-53 Rev. 5 families such as `AC family`, `IA family`, `SC family`,
  `CM family`, `SI family`, `RA family`, and `AU family`.
- Lynis check IDs and OpenSCAP XCCDF rule IDs when those tools provide local
  baseline evidence.

Piranesi does not invent precise CIS or NIST control IDs where it lacks local
supporting evidence. Broad family/category mappings use lower confidence and state
that exact profile control selection depends on the environment. OpenSCAP-supplied
CCE/CIS references remain in `control_refs` and are also rendered as structured
references sourced from the OpenSCAP evidence.

Control mappings support triage and audit preparation. They are not a compliance
attestation, a full CIS benchmark result, or a complete NIST control assessment.

## Evaluation

Host fixtures can carry explicit `ground_truth.json` files with expected findings,
expected-absent checks, and allowed opportunistic findings. The host benchmark
harness uses rule IDs and instance keys for matching so tests are not brittle
title-only comparisons.

Run the local host benchmark:

```bash
uv run python eval/host_benchmark.py \
  --fixtures tests/fixtures/host \
  --output /tmp/piranesi-host-benchmark
```

The output directory contains:

```text
host_benchmark.json
host_benchmark.md
findings_matrix.csv
```

The JSON and Markdown reports include fixture count, expected issue count,
detected issue count, true positives, false positives, false negatives, precision,
recall, F1, evidence coverage score, mean findings per host, mean top-action
count, and a time-to-triage proxy. The triage-speed metrics are explicitly
proxies: they use deduplicated finding count, top-action count, evidence
completeness, remediation presence, risk-score presence, and report sections
needed for top-three action decisions. They are not measured human analyst time.

Baseline comparisons are local-only. Trivy, Lynis, and OpenSCAP baselines are
scored only for fixtures that contain those evidence types; missing baseline
evidence is recorded as a skip with a reason. The deterministic+LLM baseline is
skipped by the standalone script unless a caller injects an LLM provider.

Current fixture metrics are useful for regression tracking and false-positive
accounting. They do not prove broad coverage-improvement or analyst-time claims;
those need a larger corpus and a measured user study.

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
findings with evidence, remediation, legacy control references, and structured
tool-supplied control references.

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
