# AppSec Harness

The pivot target is Pi-local AppSec triage through `kelp-pi chat`. The old laptop `kelp-claw appsec audit --agent-command ...` path remains in `packages/cli` as legacy reference until Zig parity.

## Pi Contract

Input:

- Chat turns.
- Scope declarations.
- Passive scanner imports.
- Active scanner requests.
- Operator approvals.

Output:

- Signed transcript target.
- Normalized findings.
- Retrieval citations.
- Policy decisions.
- Raw evidence.
- Static audit bundle.

## Current Zig Commands

```console
$ kelp-pi policy check --tool Bash --command 'nuclei -u http://target'
$ kelp-pi scope set --host http://fixture.local --until 2026-12-31T00:00:00Z
$ kelp-pi approval-request --scope-id default --command 'nuclei http://fixture.local'
$ kelp-pi scan nuclei --target http://fixture.local --approval-token <token> --dry-run
$ kelp-pi index ingest --input findings.json --path evidence/findings.json
$ kelp-pi ask "default" --emit-finding --finding-title "Default admin marker"
$ kelp-pi bundle assemble --run-id local --workspace . --output audit-bundle
$ kelp-pi verify-bundle audit-bundle
```

## Triage JSON Target

```json
{
  "summary": "short triage summary",
  "triageFindings": [
    {
      "id": "finding-1",
      "title": "finding title",
      "severity": "high",
      "confidence": "medium",
      "evidenceIds": ["scanner-finding-id"],
      "citations": [{ "path": "evidence/findings.json", "chunkId": "..." }],
      "rationale": "why this matters",
      "recommendedAction": "next safe action"
    }
  ],
  "recommendedNextSteps": ["safe follow-up"],
  "limitations": ["what could not be verified"]
}
```

Invalid JSON, missing citations, or disallowed tool requests fail closed.

## Safety

`appsec-agent-baseline` denies destructive shell, credential exfiltration, exploit execution, persistence, and lateral movement. It requires approval for active scanners and container runtime actions beyond build.
