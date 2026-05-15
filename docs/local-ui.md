# Local Evidence Workbench

Piranesi can inspect host, fleet, and source-code reports in a local-only web UI.
The workbench is the review surface for reports produced by `piranesi demo`,
`piranesi assess`, `piranesi fleet assess`, and the compatibility source-code
pipeline:

```bash
piranesi ui piranesi-output
piranesi ui fleet-output
piranesi ui source-output
piranesi ui --watch piranesi-output
```

By default the server binds to `127.0.0.1` and does not open a browser. Pass
`--open` to launch the default browser:

```bash
piranesi ui piranesi-output --open
```

The report path must be either a directory containing `host-report.json`,
`fleet-report.json`, or source-code `report.json`, or one of those report files
directly. The UI serves only embedded static assets and redacted report API
responses; it does not expose the report directory as a general file server.

The first-run flow matches the CLI quickstart:

```bash
piranesi demo --output piranesi-demo-output
piranesi ui piranesi-demo-output --open
piranesi doctor --host
```

Privacy defaults: report review stays on loopback, ZIP workbench jobs are stored
under `~/.piranesi/ui-jobs/` unless `--jobs-dir` is supplied, and workbench scans
strip API-key-like environment variables before launching the local scan process.
See [privacy and data handling](privacy-data-handling.md) for retention,
deletion, LLM, and outbound export details.

## ZIP Workbench

Use the local workbench without a report path to upload a ZIP of a web app, run
the deterministic compatibility source-code scan, and review the generated
report:

```bash
piranesi ui --workbench --open
```

This mode accepts ZIP uploads only. It extracts into a local job directory,
rejects unsafe archive paths and symlinks, runs `piranesi run` with
`--no-execute --no-fail --format both`, and then shows the resulting
`report.json`/`report.md` in the same review interface.

The first-run state includes:

- bundled ZIP demo launch
- ZIP upload
- public GitHub repository import
- recent local scans
- disabled or CLI-directed modes for existing reports, host evidence, container
  artifacts, and Kubernetes manifests
- readiness diagnostics and privacy defaults

Uploaded ZIPs, imported repositories, extracted source, generated reports, and
`scan.log` remain in the job directory until you delete the local job from the
workbench or remove the directory.

## URL And GitHub Import

The workbench accepts public GitHub repository URLs:

```text
https://github.com/owner/repo
```

Generic URLs are rejected. GitHub imports run locally by cloning with
`--depth 1 --single-branch --no-tags` into an isolated job directory, removing
`.git`, enforcing the same file-count and extracted-size limits as ZIP uploads,
and reusing the same job status, report, findings, artifact, and history APIs.

Private repositories, credentials embedded in URLs, general website crawling,
and live browser testing are intentionally unsupported in this path.

## Local Report Library

Workbench jobs are indexed in `jobs-index.json` under the configured jobs
directory. On restart, the UI reloads completed and failed local jobs so users
can reopen previous reports or delete records and associated local artifacts.
Jobs that were queued or running during a restart are marked failed with restart
context because the scan process is not resumed.

No database is introduced; the index is a local JSON file beside the job
directories.

## Artifacts And Handoff

Loaded reports and workbench jobs expose local artifact downloads from the UI:

- JSON report
- Markdown report when generated
- SARIF
- CSV
- host PDF where available or renderable

GitHub, Jira, Slack-compatible webhook, and generic webhook actions are previewed
as dry-runs only. The UI displays redacted payload previews and refuses external
sends without explicit confirmation. Real externally visible creation or send
actions remain CLI operations using `piranesi export ... --create/--send --yes`.

## Views

The current workbench includes:

- host overview
- findings table with severity, category, and suppression filters
- finding detail with evidence and remediation
- evidence inventory
- collection health
- top actions
- suppression review
- fleet summary for `fleet-report.json`
- source-code application review for `report.json`
- ZIP upload workbench with local scan progress
- local scan history with reopen/delete
- sample gallery and guided first-run mode cards
- dry-run handoff previews and artifact downloads

## Watch Mode

`--watch` reloads the report from disk for each browser request. Use it while
rerunning assessment into the same output directory:

```bash
piranesi ui --watch piranesi-output
```

## Network Binding

The default bind address is local loopback:

```bash
piranesi ui piranesi-output --host 127.0.0.1 --port 8765
```

Binding to `0.0.0.0` prints an explicit warning because the workbench is
unauthenticated and becomes reachable from the network:

```bash
piranesi ui piranesi-output --host 0.0.0.0
```

Keep the default unless you are deliberately testing in an isolated environment
or you have placed the UI behind a trusted access-control layer such as an SSH
tunnel, VPN-only listener, or authenticated reverse proxy.

## Workbench Preflight

The workbench exposes `GET /api/preflight` and renders a readiness panel before
scan execution. It reports local tool presence, version probes where available,
required versus optional status for the current mode, and install guidance for
missing tools. Diagnostics stay local and do not install or download anything.

## Finding Detail Pages

Finding details include status, affected location or host component, evidence
snippets, risk rationale, confidence notes, remediation, related controls, and a
copy-friendly analyst handoff block when the report schema provides those
fields. Missing optional fields render as empty/none rather than failing the UI.

## Redaction

The `/api/report` summary redacts host identifiers, IP addresses, usernames, MAC
addresses, secrets, and tokens by default. Finding evidence is intended for local
review and redacts obvious secret-bearing values. Raw snapshots are not served as
standalone files.
