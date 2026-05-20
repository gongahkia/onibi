# Rescan Replay Extractors

Replay extractors recover a replay spec from baseline workspace evidence. They do
not execute tools.

Current extractors:

- `nmap`: reads raw nmap XML, recovers the original `args`, host scope, version, and
  input digest.
- `nuclei`: reads raw nuclei JSONL, recovers a single target, template paths or IDs,
  record count, and input digest.

Extractor output uses `piranesi.replay-spec.v1`:

```json
{
  "schema_version": "piranesi.replay-spec.v1",
  "tool": "nmap",
  "recovered_command": ["nmap", "-sV", "127.0.0.1"],
  "target_scope": ["127.0.0.1"],
  "input_evidence": [{"path": "raw/nmap/example.xml", "sha256": "..."}],
  "confidence": "high",
  "metadata": {}
}
```

Fail-closed behavior:

- unsupported tools are ignored by bulk extraction;
- malformed supported evidence is reported as an extraction warning;
- nmap XML without original `args` is rejected;
- nuclei JSONL without target or template data is rejected;
- nuclei JSONL with multiple targets and no original list file is rejected as
  ambiguous.

These specs are consumed by `piranesi rescan --from-baseline`; use `--dry-run --json`
to inspect them before execution.
