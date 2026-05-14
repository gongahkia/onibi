# Local Review Workbench

Piranesi can inspect host, fleet, and source-code reports in a local-only web UI:

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

## ZIP Workbench

Use the local web workbench to upload a ZIP of a web app, run the deterministic
source-code scan, and review the generated report:

```bash
piranesi ui --workbench --open
```

The first workbench version accepts ZIP uploads only. It extracts into a local
job directory, rejects unsafe archive paths and symlinks, runs `piranesi run`
with `--no-execute --no-fail --format both`, and then shows the resulting
`report.json`/`report.md` in the same review interface. URL and GitHub import
are tracked as follow-up work.

## Views

The first version of the workbench includes:

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

Binding to `0.0.0.0` prints an explicit warning because the workbench becomes
reachable from the network:

```bash
piranesi ui piranesi-output --host 0.0.0.0
```

Keep the default unless you are deliberately testing in an isolated environment.

## Redaction

The `/api/report` summary redacts host identifiers, IP addresses, usernames, MAC
addresses, secrets, and tokens by default. Finding evidence is intended for local
review and redacts obvious secret-bearing values. Raw snapshots are not served as
standalone files.
