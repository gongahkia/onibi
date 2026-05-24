> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Comparison And Positioning

Piranesi is a local-first evidence workbench. It does not replace mature
scanners, compliance engines, fleet agents, or commercial AppSec platforms. Use
those tools for their core detection and management strengths, then use Piranesi
when you need inspectable local evidence, normalized reports, risk explanation,
and analyst-ready handoff artifacts.

This page is intentionally conservative. Capability notes link to primary
project or vendor documentation and avoid feature-parity claims.

## Short Version

Use Piranesi when the task is:

- turning local host, application, container, Kubernetes, or tool-output evidence
  into one inspectable report;
- reviewing an offline bundle, lab VM, airgapped host, or local ZIP without cloud
  upload;
- explaining why a finding matters, which evidence supports it, what is missing,
  and what the next action is;
- producing local JSON, Markdown, PDF, SARIF, CSV, or support-bundle artifacts
  for human review and downstream workflows.

Do not use Piranesi as the sole control for:

- enterprise endpoint telemetry, alerting, or response;
- formal certification or compliance attestation;
- production vulnerability management at fleet scale;
- managed SAST/SCA/secrets governance;
- cloud CNAPP, asset inventory, or policy enforcement.

## Comparison Matrix

| Tool | Primary Strength | Piranesi Does Not Replace It | Where Piranesi Composes |
| --- | --- | --- | --- |
| Lynis | Unix/Linux/macOS security auditing, hardening checks, warnings, suggestions, report files, and detailed logging. See [Lynis features](https://cisofy.com/documentation/lynis/features/). | Piranesi does not replace Lynis hardening audits or its built-in test library. | Piranesi can ingest Lynis evidence and present selected warnings alongside osquery, package, SSH, firewall, update, and control-mapping context. |
| OpenSCAP / SCAP Security Guide | Machine-readable security baselines, XCCDF/OVAL/SCAP content, automated local scans, ARF result output, and HTML reports. See [SCAP Security Guide](https://www.open-scap.org/security-policies/scap-security-guide/). | Piranesi does not replace SCAP content, `oscap`, SCAP Workbench, assessor review, or formal compliance validation. | Piranesi can normalize OpenSCAP failed rules into posture findings and report controls, caveats, and remediation context with other host evidence. |
| Trivy | Vulnerability, misconfiguration, secret, license, SBOM, container image, filesystem, repository, Kubernetes, and IaC scanning coverage. See the [Trivy user guide](https://trivy.dev/docs/latest/guide/). | Piranesi does not replace Trivy's vulnerability databases, scanners, policy checks, VEX/SBOM workflows, or Kubernetes/container scanning breadth. | Piranesi can ingest Trivy JSON, prioritize package CVEs with host exposure and remediation context, and include Trivy availability in preflight/support bundles. |
| Wazuh | Agent/server-backed endpoint monitoring, vulnerability detection, security configuration assessment, log analysis, incident response, inventory, and SIEM/XDR-style workflows. See [Wazuh vulnerability detection](https://documentation.wazuh.com/current/user-manual/capabilities/vulnerability-detection/index.html). | Piranesi does not replace Wazuh agents, central management, alerting, dashboards, retention, response actions, or vulnerability-management operations. | Piranesi is useful before or around fleet tooling: local evidence review, offline triage, one-off lab assessments, and compact reports that can be exported into ticketing or webhook flows. |
| Semgrep | Developer-oriented SAST, SCA, secrets scanning, custom rules, supported-language coverage, and managed scan workflows. See [Semgrep docs](https://semgrep.dev/docs/). | Piranesi does not replace Semgrep Code, Semgrep Supply Chain, Semgrep Secrets, managed scans, or Semgrep's rule ecosystem. | Piranesi's legacy source-code path can produce local reports and SARIF for small reviews; it can sit beside Semgrep output when a team wants a broader local evidence report. |
| Snyk | Platform scanning across code, open-source dependencies, containers, IaC, APIs/web apps, PR checks, prioritization, and managed governance. See [Snyk scanning overview](https://docs.snyk.io/scan-with-snyk/overview). | Piranesi does not replace Snyk's hosted platform, SCM integrations, organization governance, PR checks, vulnerability intelligence, or remediation workflows. | Piranesi can provide local-first review and normalization where cloud upload is not desired, then export concise artifacts for teams that also use Snyk. |
| osquery | SQL-like operating-system visibility through `osqueryi` and `osqueryd`, including standalone local queries and JSON output. See [using osqueryi](https://osquery.readthedocs.io/en/stable/introduction/using-osqueryi/). | Piranesi does not replace osquery tables, deployments, schedules, packs, or fleet log aggregation. | Piranesi uses osquery-style evidence as a core host input and turns selected raw rows into findings, coverage status, and remediation-oriented host reports. |

## Concrete Workflows

### Host Posture

Run osquery-based local collection, optionally include Trivy, Lynis, or OpenSCAP,
then assess the bundle with Piranesi:

```bash
piranesi collect --output piranesi-evidence --lynis --openscap
piranesi assess piranesi-evidence --output piranesi-output --format both
```

Use this when you need a compact report for one host, a lab VM, a support case,
or an offline evidence review. Use Lynis/OpenSCAP/Wazuh for deeper hardening,
formal baseline, or continuous endpoint coverage.

### Container Review

Use Trivy or existing container artifacts for primary container vulnerability and
misconfiguration coverage. Use Piranesi to normalize available results into a
local review artifact when the surrounding evidence also matters:

```bash
piranesi container assess --image trivy-image.json --output piranesi-container
```

Piranesi is not a CNAPP or registry scanner. It is a local report layer for
small, inspectable review workflows.

### CI Triage

Run the scanners already trusted by the project, then use Piranesi where its
contracts help with local diffing, report generation, or export:

```bash
piranesi export sarif piranesi-output/host-report.json \
  --output host-report.sarif.json
piranesi export csv piranesi-output/host-report.json \
  --output host-findings.csv
```

Keep vendor-native CI gates in place when they enforce policy or block merges.

### Local App ZIP Review

Use the workbench for a local ZIP review when the target is a small source
bundle and cloud upload is not appropriate:

```bash
piranesi ui --workbench --open
```

For managed SAST, SCA, or secrets governance across repositories, keep Semgrep,
Snyk, or similar platforms in the primary role.

### Fleet Brief

Assess multiple local host evidence bundles and produce a fleet report:

```bash
piranesi fleet assess hosts/ --output fleet-output
```

This is useful for small, local briefs. It is not a replacement for Wazuh-style
endpoint management, SIEM retention, or real-time response.

## Decision Guide

Choose the specialized tool first when you need authoritative detection or
ongoing operations:

- choose Lynis for Unix/Linux/macOS hardening audits;
- choose OpenSCAP/SCAP Security Guide for SCAP baselines and XCCDF/OVAL content;
- choose Trivy for container, filesystem, repository, Kubernetes, IaC, SBOM,
  vulnerability, secret, and license scanning;
- choose Wazuh for managed endpoint telemetry, inventory, alerting, and response;
- choose Semgrep for developer-facing SAST/SCA/secrets rules and managed scans;
- choose Snyk for hosted code, dependency, container, IaC, API/web, PR, and
  governance workflows;
- choose osquery for raw endpoint state queries and deployable endpoint
  visibility.

Choose Piranesi when the result needs to be a local, inspectable, evidence-bound
report that combines several sources and explains risk in a way a human can act
on quickly.

