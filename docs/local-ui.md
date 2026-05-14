# Local Review Workbench

Piranesi can inspect host and fleet reports in a local-only web UI:

```bash
piranesi ui piranesi-output
piranesi ui fleet-output
piranesi ui --watch piranesi-output
```

By default the server binds to `127.0.0.1` and does not open a browser. Pass
`--open` to launch the default browser:

```bash
piranesi ui piranesi-output --open
```

The report path must be either a directory containing `host-report.json` or
`fleet-report.json`, or one of those report files directly. The UI serves only
embedded static assets and redacted report API responses; it does not expose the
report directory as a general file server.

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
