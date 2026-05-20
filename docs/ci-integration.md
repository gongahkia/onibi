# CI Integration

CI examples are intentionally import-only. They validate fixture provenance, ingest
checked-in sample exports, render a report, and verify the workspace contract.

Templates:

- `docs/ci-templates/github-actions-piranesi.yml`
- `docs/ci-templates/gitlab-ci-piranesi.yml`

Typical CI steps:

```bash
piranesi ingest init --workspace workspace --client CI --project fixture-smoke
piranesi ingest nmap --input tests/fixtures/pentest/nmap/localhost-http.xml --workspace workspace
piranesi ingest nuclei --input tests/fixtures/pentest/nuclei/localhost-http.jsonl --workspace workspace
piranesi report --workspace workspace --format json
piranesi pff export --workspace workspace
piranesi ci validate-pff --input workspace/reports/findings.pff.json
piranesi ci validate-report-bundle --path workspace/reports
piranesi sign --workspace workspace
piranesi sign --workspace workspace --verify
piranesi ci validate-delivery --workspace workspace
```

The templates do not run active scans against external targets.
The deterministic replay harness uses fixture-backed replay outputs in the default
test lane; real Docker replay tests should stay behind the `docker` marker.

The `piranesi ci` commands exit non-zero when a PFF artifact, report JSON file,
report directory, or red-team handoff archive has an unsupported schema version,
malformed structure, unsafe archive entry path, or missing archive manifest entry.
`validate-delivery` adds a local pre-delivery QA gate over findings, evidence
references, report redaction, handoff manifests, and chain-of-custody coverage.
