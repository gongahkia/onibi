# Support Bundles

Support bundles are portable ZIP archives intended for bug reports and community
help. They collect runtime context without requiring users to share source code
or raw host evidence by default.

## Generate A Bundle

```bash
piranesi support-bundle --output piranesi-support.zip
```

Useful options:

```bash
piranesi support-bundle \
  --project-root . \
  --config piranesi.toml \
  --report piranesi-output \
  --log piranesi-output/scan.log \
  --output piranesi-support.zip
```

Report artifacts are excluded by default. Include redacted report JSON and
Markdown only when the maintainer explicitly asks for them:

```bash
piranesi support-bundle \
  --report piranesi-output \
  --include-report-artifacts \
  --output piranesi-support-with-report.zip
```

## Contents

Every bundle includes:

- `manifest.json`: bundle manifest, entry list, and redaction statement
- `preflight.json`: local dependency readiness data
- `environment.json`: Piranesi, Python, and platform metadata
- `README.txt`: sharing guidance

Optional entries include redacted config, selected logs, report metadata, and
explicitly requested report artifacts.

## Redaction Boundary

Redaction is best effort. Piranesi redacts obvious secret keys, bearer tokens,
API-key-like values, local usernames, hostnames, project-root paths, and IP
addresses. Review the ZIP before posting it publicly or attaching it to an
issue.

Maintainers should ask for the smallest bundle that answers the debugging
question. Start with the default bundle, then request `--report` or
`--include-report-artifacts` only when needed.
