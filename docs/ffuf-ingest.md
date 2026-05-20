# ffuf Ingest

`piranesi ingest ffuf` imports an operator-supplied ffuf JSON output file into a local workspace.
Piranesi does not run ffuf or interact with targets.

```bash
uv run piranesi ingest ffuf \
  --input ffuf-results.json \
  --workspace ./workspace
```

The original export is copied under `raw/ffuf/`, and normalized discovery findings are written to
`normalized/findings.json`. ffuf results are mapped as `info` severity because a matched path or
response is discovery evidence, not a confirmed vulnerability by itself.

Each finding preserves the source digest, raw path, result locator, URL, HTTP status, redirect
target, response length, word count, line count, and source command line when present.
