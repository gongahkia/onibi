# sqlmap Ingest

`piranesi ingest sqlmap` imports an operator-supplied sqlmap JSON summary or text output artifact
into a local workspace. Piranesi does not run sqlmap or interact with targets.

```bash
uv run piranesi ingest sqlmap \
  --input sqlmap-summary.json \
  --workspace ./workspace
```

The original artifact is copied under `raw/sqlmap/`, and normalized findings are written to
`normalized/findings.json`. Payload snippets are marked redacted because sqlmap evidence can include
request parameters, injected probes, and sensitive application data.

Supported inputs:

- JSON objects with a top-level `vulnerabilities[]` list.
- A single JSON object with `target`, `parameter`, and `payload` fields.
- Basic sqlmap text logs containing `Parameter:`, `Type:`, and `Payload:` lines.

Findings preserve the source digest, raw path, target URL, parameter location, DBMS metadata when
present, payload evidence, CWE-89 mapping, and references supplied by the artifact.
