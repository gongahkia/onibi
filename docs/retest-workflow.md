# Retest Workflow

`piranesi retest` compares two workspaces and writes a lifecycle diff:

```bash
piranesi retest --baseline ./old-workspace --current ./new-workspace --output retest.json
piranesi retest --baseline ./old-workspace --current ./new-workspace --output retest.md
```

The command classifies findings as:

- `new`: present only in the current workspace;
- `open`: present in both workspaces with the same evidence signature;
- `closed`: present only in the baseline workspace;
- `changed`: present in both workspaces but evidence, severity, confidence, asset, or service changed;
- `regressed`: a baseline finding marked closed has reappeared in the current workspace;
- `ambiguous`: fallback matching found more than one plausible baseline candidate.

Stable finding IDs are used first. When IDs do not match, Piranesi tries a conservative
fallback match on asset, title, service protocol/port, and weakness identifiers. Ambiguous
fallback matches are reported for human review instead of silently classified.

The command annotates current workspace findings with retest status for report rendering and
appends a retest event to the current workspace audit log. Raw evidence under `raw/` is not
modified.
