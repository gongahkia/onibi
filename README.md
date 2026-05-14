<p align="center">
  <a href="https://github.com/gongahkia/piranesi">
    <img alt="Piranesi" src="asset/logo/imaginary-prisons.jpg" width="180">
  </a>
</p>

<h1 align="center">Piranesi</h1>

<p align="center">
  <strong>Local-first host posture workbench for Linux VMs, labs, and security review.</strong>
</p>

<p align="center">
  <a href="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg" /></a>
  <a href="https://github.com/gongahkia/piranesi/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" /></a>
  <a href="https://github.com/gongahkia/piranesi"><img alt="Status" src="https://img.shields.io/badge/status-alpha-orange?style=flat-square" /></a>
</p>

---

Piranesi turns local host evidence into focused, evidence-bound posture reports.
It collects and normalizes signals from tools like osquery and Trivy, then explains
what matters: exposed services, SSH hardening gaps, package CVEs, firewall/update
posture, privileged accounts, kernel hardening issues, missing evidence, and
operator-ready next actions.

The product direction is deliberately not "another black-box scanner." Piranesi is
a local-first evidence workbench: bring host evidence in, keep it inspectable, rank
and explain risk, and produce reports that an engineer or analyst can act on.

> `v0.2.0` is an alpha pivot from the earlier source-code security prototype into
> VM and Linux host posture assessment. The legacy source-code pipeline remains in
> the tree for compatibility, but the primary product surface is now host posture.

## Table Of Contents

- [Why Piranesi](#why-piranesi)
- [Current Scope](#current-scope)
- [Quick Start](#quick-start)
- [Host Evidence](#host-evidence)
- [Reports](#reports)
- [LLM Analysis](#llm-analysis)
- [Roadmap](#roadmap)
- [CLI Reference](#cli-reference)
- [Development](#development)
- [License](#license)

---

## Why Piranesi

Security teams already have plenty of scanners. Piranesi is aimed at the gap before
and around those platforms: VM sandboxes, homelabs, lab infrastructure, offline
reviews, airgapped evidence bundles, and teams that want a transparent local report
before adopting heavier fleet tooling.

Piranesi's positioning:

- **Local-first:** deterministic assessment works without API keys or cloud upload.
- **Evidence-bound:** findings cite concrete snapshot, osquery, Trivy, or command evidence.
- **Composable:** Piranesi normalizes existing tools instead of trying to replace them.
- **Analyst-ready:** reports include metadata, evidence inventory, collection health,
  top actions, remediation, and known limitations.
- **Developer-friendly:** everything is CLI-native, JSON-first, fixture-testable, and
  suitable for CI or local review.

The long-term wedge is a local posture workbench that composes osquery, Trivy,
Lynis, OpenSCAP, policy-as-code, adaptive probing, fleet summaries, and benchmarked
evidence quality into one practical workflow.

## Current Scope

Piranesi currently supports a single-host Debian/Ubuntu-oriented workflow:

```bash
piranesi collect  # run on a Linux VM or host
piranesi assess   # turn evidence into JSON/Markdown reports
```

Implemented today:

- Local host evidence collection with osquery.
- Optional Trivy filesystem vulnerability evidence.
- Canonical `host_snapshot.json` loading.
- Raw bundle ingestion from `osquery/`, `trivy/`, `commands/`, `lynis/`, and `openscap/` evidence.
- Deterministic findings for:
  - high-risk public listeners
  - public SSH exposure
  - SSH hardening gaps
  - privileged local accounts
  - Trivy package CVEs
  - pending security updates
  - missing unattended security updates
  - selected weak sysctl values
  - missing core evidence coverage
  - Lynis baseline warnings and suggestions
  - OpenSCAP failed XCCDF rule results with control references
- Adaptive probing: deterministic follow-up probe plans from initial findings.
- Safe allowlisted probe executor - no arbitrary command execution.
- Separate evidence-bound host hypothesis reports that never count as findings.
- Optional LLM analysis constrained to supplied evidence.
- `host-report.json`, `host-report.md`, `host-report.pdf`, and static dashboard output.

Not implemented yet:

- fleet dashboard
- ticket sync
- Windows support
- cloud inventory ingestion
- long-running agent

See [docs/capabilities.md](docs/capabilities.md) for the detailed capability matrix.

## Quick Start

From a source checkout:

```bash
uv sync
uv run piranesi --version
uv run piranesi doctor .
```

Assess the bundled vulnerable host fixture:

```bash
uv run piranesi assess tests/fixtures/host/debian-vulnerable \
  --output piranesi-output \
  --analysis deterministic \
  --format both
```

This writes:

```text
piranesi-output/
  host-report.json
  host-report.md
  host-report.pdf
  host-dashboard/
    index.html
    host-report.json
    assets/
```

Collect evidence on a Linux VM or host:

```bash
uv run piranesi collect --output piranesi-evidence
```

Assess that evidence:

```bash
uv run piranesi assess piranesi-evidence \
  --output piranesi-output \
  --analysis deterministic \
  --format both
```

Generate a separate hypothesis report for possible evidence gaps and follow-up:

```bash
uv run piranesi hypothesize piranesi-evidence --output piranesi-output
```

Collect without Trivy when it is unavailable or too expensive:

```bash
uv run piranesi collect --output piranesi-evidence --no-trivy
```

Collect with optional Lynis hardening baseline:

```bash
uv run piranesi collect --output piranesi-evidence --lynis
```

Collect bounded, redacted authentication/session evidence only when explicitly needed:

```bash
uv run piranesi collect --output piranesi-evidence --auth-evidence
```

## Host Evidence

`piranesi collect` writes a local evidence bundle:

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
    lynis/
      report.dat
    openscap/
      results.xml
```

Piranesi also accepts hand-built bundles:

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

If `host_snapshot.json` exists at the bundle root, Piranesi treats it as the
canonical input and skips raw bundle normalization.

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

See [docs/host-posture.md](docs/host-posture.md) for collection details and
permission notes.

## Reports

Host reports include:

- target and generated timestamp
- analysis modes
- posture score
- severity summary
- host metadata
- top actions
- evidence inventory
- collection health
- LLM redaction metadata when LLM host analysis is requested
- findings with severity, confidence, evidence, remediation, and control references
- known limitations
- embedded canonical snapshot

Write JSON only:

```bash
uv run piranesi assess piranesi-evidence --format json --output piranesi-output
```

Write Markdown only:

```bash
uv run piranesi assess piranesi-evidence --format markdown --output piranesi-output
```

Write both:

```bash
uv run piranesi assess piranesi-evidence --format both --output piranesi-output
```

Write PDF, dashboard, or every host output:

```bash
uv run piranesi assess piranesi-evidence --format pdf --output piranesi-output
uv run piranesi assess piranesi-evidence --format dashboard --output piranesi-output
uv run piranesi assess piranesi-evidence --format all --output piranesi-output
```

Hypothesis reports are written separately as `host-hypotheses.json` and
`host-hypotheses.md`:

```bash
uv run piranesi hypothesize piranesi-evidence --output piranesi-output
```

Hypotheses are not confirmed findings. They do not affect `findings_total`,
`--fail-severity`, or posture score.

## LLM Analysis

Deterministic assessment does not require LLM credentials.

Optional LLM analysis can add evidence-bound posture reasoning when a
LiteLLM-compatible API key is configured:

```bash
OPENAI_API_KEY=... uv run piranesi assess piranesi-evidence \
  --analysis both \
  --output piranesi-output
```

Supported environment variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENROUTER_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `GOOGLE_API_KEY`
- `LITELLM_API_KEY`

LLM output is advisory and must remain tied to explicit snapshot evidence.
Before host evidence is sent to an LLM, Piranesi builds a structured host payload
and applies strict host redaction. Hostnames, usernames, IPs, MAC addresses, home
paths, command lines, and likely secrets are replaced with stable placeholders
such as `[HOSTNAME_1]`, `[USER_1]`, `[PRIVATE_IP_1]`, and `[SECRET]`. Package and
service names are preserved so vulnerability context remains useful. LLM prompt
traces contain the redacted prompt only.

`piranesi hypothesize` may also use the configured LLM provider. Its prompt requires
available evidence citations, explicit missing evidence, concise reasoning summaries,
and safe follow-up probes or analyst questions rather than exploit payloads.

## Roadmap

The roadmap is organized as implementation specs in `todo1.md` through `todo20.md`.

Near-term product depth:

- exploitability, blast-radius, and urgency scoring
- fleet assessment
- host benchmark harness
- structured CIS/NIST control mapping

Adoption and scale:

- frictionless install, demo, and onboarding
- agentless SSH collection
- stable report API and embeddable library mode
- workflow exporters for SARIF, tickets, CSV, and webhooks
- policy-as-code gates
- remediation planning and before/after tracking
- multi-platform host support
- container and Kubernetes evidence normalization
- local web review workbench
- community host rules, fixtures, and benchmarks

## CLI Reference

```bash
piranesi --version
piranesi doctor .
piranesi collect --output piranesi-evidence [--trivy | --no-trivy] [--lynis] [--openscap] [--auth-evidence]
piranesi assess <host_snapshot.json|evidence-bundle> \
  --output piranesi-output \
  --analysis deterministic|llm|both \
  --format json|markdown|both|pdf|dashboard|all
piranesi hypothesize <host_snapshot.json|evidence-bundle> --output piranesi-output
piranesi probe <evidence-bundle> --output probe-plan.json
piranesi collect-followup <probe-plan.json> --output piranesi-evidence-followup
```

Adaptive probing workflow:

```bash
# 1. Generate probe plan from initial findings
uv run piranesi probe piranesi-evidence --output probe-plan.json
# 2. Review the plan
cat probe-plan.json | python3 -m json.tool
# 3. Execute allowed follow-up probes
uv run piranesi collect-followup probe-plan.json --output piranesi-evidence-followup
# 4. Re-assess with follow-up evidence
uv run piranesi assess piranesi-evidence-followup --output piranesi-output --format both
```

Useful assessment options:

| Option | Description |
| --- | --- |
| `--analysis deterministic` | Run deterministic host analysis only. |
| `--analysis llm` | Run LLM-only host analysis when credentials are configured. |
| `--analysis both` | Combine deterministic and LLM analysis. |
| `--format json` | Write `host-report.json`. |
| `--format markdown` | Write `host-report.md`. |
| `--format both` | Write JSON and Markdown reports. |
| `--format pdf` | Write `host-report.pdf`. |
| `--format dashboard` | Write a static local dashboard under `host-dashboard/`. |
| `--format all` | Write JSON, Markdown, PDF, and dashboard outputs. |
| `--fail-severity high` | Exit non-zero when unsuppressed findings meet the threshold. |
| `--no-fail` | Write reports without failing the command on findings. |
| `--treat-private-as-public` | Treat private-interface listeners as exposed for lab hardening. |

## Development

```bash
uv sync
uv run pytest tests/test_host_posture.py
uv run piranesi collect --output piranesi-evidence --no-trivy
uv run piranesi assess piranesi-evidence
scripts/host_smoke_check.sh
```

The older source-code analysis modules are still present during the pivot. Treat
new work as belonging to the host posture surface unless a change explicitly
supports migration or backward compatibility.

## License

Apache-2.0. See [LICENSE](LICENSE).
