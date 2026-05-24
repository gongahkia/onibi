# Nuclei JSONL Ingest

`piranesi ingest nuclei` imports JSONL output produced by nuclei:

```bash
piranesi ingest nuclei --input results.jsonl --workspace ./workspace
```

Piranesi does not run nuclei. The command copies the JSONL export into `raw/nuclei/`,
normalizes findings, and appends an `ingest nuclei` audit event with the input digest,
record counts, finding counts, and parser warnings.

## Mapping

- `template-id` becomes the deterministic finding identity input and a `nuclei` tag.
- `info.name`, `info.description`, `info.remediation`, `info.reference`, and `info.tags`
  are preserved in the finding and report model.
- `info.severity` maps directly for `critical`, `high`, `medium`, and `low`; unknown or
  missing severities map to `info`.
- all imported nuclei records use `tool-observed` confidence because nuclei has observed a
  template match, but Piranesi has not independently confirmed the issue.
- `info.classification.cwe-id` and `cve-id` become `weakness_ids`.
- `host`, `scheme`, `port`, `url`, and `matched-at` become asset, service, and affected
  instance context.
- `request`, `response`, and `curl-command` are retained as redacted evidence snippets so
  default reports hide them, while `--include-sensitive-evidence` can show them during
  local review.

Malformed JSONL lines are reported as warnings and skipped when at least one valid record
exists. Empty or fully invalid files fail with a clear parser error.
