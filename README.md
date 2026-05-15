<p align="center">
  <a href="https://github.com/gongahkia/piranesi">
    <img alt="Piranesi" src="asset/logo/imaginary-prisons.jpg" width="180">
  </a>
</p>

<h1 align="center">Piranesi</h1>

<p align="center">
  <strong>Local-first evidence workbench for host, application, and infrastructure security review.</strong>
</p>

<p align="center">
  <a href="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/gongahkia/piranesi/actions/workflows/ci.yml/badge.svg" /></a>
  <a href="https://github.com/gongahkia/piranesi/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" /></a>
  <a href="https://github.com/gongahkia/piranesi"><img alt="Status" src="https://img.shields.io/badge/status-alpha-orange?style=flat-square" /></a>
</p>

---

Piranesi turns local security evidence into focused, evidence-bound posture
reports. It collects and normalizes signals from tools like osquery, Trivy,
Lynis, OpenSCAP, and the compatibility source-code scanner, then explains what
matters: exposed services, SSH hardening gaps, package CVEs, firewall/update
posture, privileged accounts, risky application findings, missing evidence, and
operator-ready next actions.

The product direction is deliberately not "another black-box scanner." Piranesi is
a local-first evidence workbench: bring evidence in, keep it inspectable, rank
and explain risk, and produce reports that an engineer or analyst can act on
without cloud upload.

> `v0.2.0` is an alpha pivot from the earlier source-code security prototype into
> a broader local evidence workbench. Linux host posture is the stable-alpha
> primary workflow; source-code, container, Kubernetes, and fleet reports remain
> inspectable in the same local review model.

## Table Of Contents

- [Why Piranesi](#why-piranesi)
- [Current Scope](#current-scope)
- [Quick Start](#quick-start)
- [Comparison And Positioning](#comparison-and-positioning)
- [Sample Gallery](#sample-gallery)
- [Host Evidence](#host-evidence)
- [Reports](#reports)
- [Privacy And Data Handling](#privacy-and-data-handling)
- [Control Mapping](#control-mapping)
- [Evaluation](#evaluation)
- [LLM Analysis](#llm-analysis)
- [Release Channels](#release-channels)
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

The long-term wedge is one local posture workbench that composes osquery, Trivy,
Lynis, OpenSCAP, policy-as-code, adaptive probing, source-code review, container
and Kubernetes evidence, fleet summaries, and benchmarked evidence quality into
one practical workflow.

## Current Scope

Piranesi currently supports a single-host and small-fleet Linux host workflow:

```bash
piranesi collect  # run on a Linux VM or host
piranesi assess   # turn evidence into JSON/Markdown reports
```

Implemented today:

- Local host evidence collection with osquery.
- Optional Trivy filesystem vulnerability evidence.
- Agentless SSH collection for existing Linux hosts.
- Canonical `host_snapshot.json` loading.
- Raw bundle ingestion from `osquery/`, `trivy/`, `commands/`, `lynis/`, and `openscap/` evidence.
- Debian/Ubuntu, RPM-family, Amazon Linux, and Alpine package/update normalization into the same report model.
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
- Fleet assessment across multiple local host evidence bundles.
- Host benchmark harness with fixture ground truth, precision/recall/F1, and CSV matrix output.
- Structured host control mappings for CIS Ubuntu Linux, NIST CSF 2.0, and NIST SP 800-53 families.
- Optional LLM analysis constrained to supplied evidence.
- `host-report.json`, `host-report.md`, `host-report.pdf`, and static dashboard output.

Not implemented yet:

- fleet dashboard
- ticket sync
- Windows support
- cloud inventory ingestion
- long-running agent

See [docs/capabilities.md](docs/capabilities.md) for the detailed capability matrix.

## Comparison And Positioning

Piranesi composes with scanners and security platforms rather than replacing
them. For a conservative tool-by-tool comparison against Lynis, OpenSCAP, Trivy,
Wazuh, Semgrep, Snyk, and osquery, see
[docs/comparison.md](docs/comparison.md).

## Quick Start

### Try Piranesi in 10 minutes

Packaged install path:

```bash
pipx install piranesi
piranesi quickstart
piranesi demo --output piranesi-demo-output
piranesi doctor --host
```

`piranesi demo` uses bundled host evidence fixtures and deterministic assessment.
It writes `host-report.json` and `host-report.md` without cloning the repository,
installing osquery/Trivy, or configuring LLM credentials.

Open the same output in the local review workbench:

```bash
piranesi ui piranesi-demo-output --open
```

## Sample Gallery

Reusable local demos are documented in
[docs/sample-gallery.md](docs/sample-gallery.md). The workbench also exposes a
downloadable vulnerable Express ZIP demo when started with `piranesi ui
--workbench --open`.

Containerized evaluation:

```bash
docker run --rm -v "$PWD:/workspace" ghcr.io/gongahkia/piranesi:latest demo
```

From a source checkout:

```bash
uv sync
uv run piranesi --version
uv run piranesi doctor .
```

Assess the bundled vulnerable host fixture:

```bash
uv run piranesi demo --output piranesi-demo-output
```

This writes:

```text
piranesi-demo-output/
  host-report.json
  host-report.md
```

Review that output locally:

```bash
uv run piranesi ui piranesi-demo-output --open
```

Release, PyPI/pipx, GHCR container, checksum, provenance, and smoke-test details
are documented in [docs/release-channels.md](docs/release-channels.md).

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
- risk-based finding order and per-finding risk scores
- severity summary
- host metadata
- top actions
- evidence inventory
- collection health
- structured control summary by framework
- LLM redaction metadata when LLM host analysis is requested
- findings with severity, confidence, evidence, remediation, legacy control references, and structured controls
- deterministic risk rationale for severity, exploitability, blast radius, remediation urgency, and evidence quality
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

## Privacy And Data Handling

Piranesi's deterministic paths are local by default: evidence is read from local
files or local collection commands, reports are written to local output
directories, and the review UI binds to `127.0.0.1`. External calls happen only
when explicitly configured, such as advisory database updates, LLM-backed
analysis, or outbound exports. See
[docs/privacy-data-handling.md](docs/privacy-data-handling.md) for storage paths,
ZIP workbench retention, LLM redaction, exporter defaults, and redaction limits.

## Control Mapping

Host findings keep the legacy `control_refs` strings and now also include
`structured_control_refs` with framework, version, control ID or family, title,
mapping confidence, and rationale. Reports include a `control_summary` grouped by
framework.

Current deterministic host mappings cover broad CIS Ubuntu Linux families, NIST
CSF 2.0 categories, and NIST SP 800-53 Rev. 5 families. Exact control IDs are not
invented: when Piranesi does not have local support for an exact benchmark item,
the mapping is deliberately broad and carries lower confidence. Lynis and OpenSCAP
findings preserve their own local check or XCCDF identifiers as structured
controls, and OpenSCAP-supplied CCE/CIS references remain in legacy
`control_refs` while also being rendered as structured references.

These mappings are supporting evidence for triage and audit preparation. They are
not a compliance attestation or a complete CIS/NIST profile assessment.

## Evaluation

Run the host benchmark harness against the checked-in fixtures:

```bash
uv run python eval/host_benchmark.py \
  --fixtures tests/fixtures/host \
  --output eval/reports/host-benchmark
```

The harness reads `ground_truth.json` from each host fixture and writes:

```text
eval/reports/host-benchmark/
  host_benchmark.json
  host_benchmark.md
  findings_matrix.csv
```

Metrics include fixture count, expected and detected issue counts, true positives,
false positives, false negatives, precision, recall, F1, evidence coverage score,
mean findings per host, mean top-action count, and a time-to-triage proxy.
The triage-speed values are explicitly proxies, not measured analyst time.

Current benchmark outputs support local fixture-level regression tracking and
baseline comparisons where evidence is present. They do not prove the broader
15-25% coverage-improvement or 30-40% analyst-time claims; those still require a
larger benchmark corpus and a measured user study.

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

## Release Channels

PyPI/pipx publishing, GHCR container publishing, checksum generation, provenance
attestation, and recurring release smoke tests are documented in
[docs/release-channels.md](docs/release-channels.md).

## Roadmap

The host-depth, adoption, and scale roadmap has landed. The active roadmap is now
focused on production hardening, distribution, and credibility work.

Near-term product hardening:

- larger host benchmark corpus and measured analyst study
  ([protocol](docs/analyst-study.md))
- signed release artifacts and recurring release smoke tests
- expanded integration fixtures for GitHub, Jira, Slack/webhooks, Docker, and Kubernetes
- more remote collection compatibility testing across common Linux distributions
  ([matrix](docs/ssh-compatibility.md))

Distribution and community:

- publish PyPI/pipx and container release channels
- document support guarantees for public APIs, schemas, and rule-pack formats
- grow community fixture, policy, mapping, and benchmark contributions

## CLI Reference

```bash
piranesi --version
piranesi quickstart
piranesi demo --output piranesi-demo-output
piranesi doctor --host
piranesi doctor .
piranesi collect --output piranesi-evidence [--trivy | --no-trivy] [--lynis] [--openscap] [--auth-evidence]
piranesi remote collect --host vm-001 --output fleet-evidence/vm-001 --no-trivy
piranesi remote collect --hosts hosts.txt --output fleet-evidence --jobs 4 --no-trivy
piranesi remote doctor --hosts hosts.txt --no-trivy
piranesi host rule test-all rules/community/host
piranesi host fixture validate tests/fixtures/host/debian-vulnerable
piranesi host benchmark submit --fixture tests/fixtures/host/debian-vulnerable
piranesi container assess --image tests/fixtures/container/trivy-image.json --output piranesi-container-output
piranesi container assess --docker-host local --output piranesi-containers
piranesi k8s assess tests/fixtures/k8s --output piranesi-k8s-output
piranesi assess <host_snapshot.json|evidence-bundle> \
  --output piranesi-output \
  --analysis deterministic|llm|both \
  --format json|markdown|both|pdf|dashboard|all
piranesi ui piranesi-output [--watch] [--open]
piranesi hypothesize <host_snapshot.json|evidence-bundle> --output piranesi-output
piranesi fleet assess <fleet-evidence> --output fleet-output
piranesi ui fleet-output
piranesi fleet summarize <fleet-output>
piranesi probe <evidence-bundle> --output probe-plan.json
piranesi collect-followup <probe-plan.json> --output piranesi-evidence-followup
python eval/host_benchmark.py --fixtures tests/fixtures/host --output eval/reports/host-benchmark
```

Fleet assessment expects one child directory per host bundle:

```bash
piranesi remote collect --hosts hosts.txt --output fleet-evidence --jobs 4 --no-trivy
scp -r vm-001:/tmp/piranesi-evidence fleet-evidence/vm-001
scp -r vm-002:/tmp/piranesi-evidence fleet-evidence/vm-002
uv run piranesi fleet assess fleet-evidence --output fleet-output
uv run piranesi fleet summarize fleet-output
```

Fleet output contains per-host reports under `hosts/<name>/` plus
`fleet-report.json` and `fleet-report.md`. Individual host failures are recorded
and remote collection writes `remote-collection-summary.json` plus Markdown when
collecting multiple hosts.
and assessment continues unless `--fail-fast` is set.

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

Use `piranesi ui <piranesi-output|fleet-output>` to inspect existing JSON reports
in a local-only review workbench. It binds to `127.0.0.1` by default, supports
`--watch`, and does not serve arbitrary files from the report directory. See
[docs/local-ui.md](docs/local-ui.md).

Use `piranesi host rule scaffold`, `piranesi host rule test-all`, and
`piranesi host fixture validate` to contribute constrained community host rules
and benchmark fixtures. See
[docs/contributing-host-rules.md](docs/contributing-host-rules.md) and
[docs/community-benchmarks.md](docs/community-benchmarks.md).

Use `piranesi container assess` and `piranesi k8s assess` to normalize adjacent
container image, local Docker, and Kubernetes manifest evidence without diluting
the host posture model. See
[docs/container-kubernetes.md](docs/container-kubernetes.md).

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
