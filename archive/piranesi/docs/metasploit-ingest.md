# Metasploit Ingest

`piranesi ingest metasploit` imports an operator-supplied Metasploit JSON evidence export into a
local workspace. Piranesi does not operate Metasploit, manage sessions, run payloads, or interact
with targets.

```bash
uv run piranesi ingest metasploit \
  --input metasploit-evidence.json \
  --workspace ./workspace
```

The original export is copied under `raw/metasploit/`, and normalized findings are written to
`normalized/findings.json`.

Supported JSON arrays:

- `vulns[]`: vulnerability evidence, mapped with service context, references, and severity.
- `loot[]`: loot metadata and redacted loot content.
- `sessions[]`: informational session observations from the exported database state.

Loot content is marked redacted by default. Session records are preserved as evidence observations
only; they do not imply live session access or C2 operation in Piranesi.
