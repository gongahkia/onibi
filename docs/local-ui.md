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
`report.json`/`report.md` in the same review interface. URL and GitHub import
are tracked as follow-up work.

Uploaded ZIPs, extracted source, generated reports, and `scan.log` remain in the
job directory until you delete that directory.

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

## Redaction

The `/api/report` summary redacts host identifiers, IP addresses, usernames, MAC
addresses, secrets, and tokens by default. Finding evidence is intended for local
review and redacts obvious secret-bearing values. Raw snapshots are not served as
standalone files.
