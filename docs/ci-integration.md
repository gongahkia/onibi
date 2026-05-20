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
piranesi sign --workspace workspace
piranesi sign --workspace workspace --verify
```

The templates do not run active scans against external targets.
The deterministic replay harness uses fixture-backed replay outputs in the default
test lane; real Docker replay tests should stay behind the `docker` marker.
