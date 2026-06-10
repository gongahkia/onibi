# AppSec Harness

KelpClaw AppSec audit turns a Dockerized target plus scanner evidence into a reproducible triage bundle.

## Command

```console
$ kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command ./appsec-agent.sh \
  --sarif findings.sarif \
  --nuclei-jsonl nuclei.jsonl \
  --zap-json zap.json \
  --out .kelpclaw/appsec/local
```

V1 builds the Dockerfile and imports scanner output. It does not run exploits or active scanners by default.

## Agent Contract

The agent command receives:

- `KELPCLAW_APPSEC_INPUT`: JSON input with target metadata, Docker build result, scanner evidence summary, and policy decisions.
- `KELPCLAW_APPSEC_OUTPUT`: path where the agent must write triage JSON.
- `KELPCLAW_EVIDENCE_WORKSPACE`: normalized evidence workspace.

Output schema:

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
      "rationale": "why this matters",
      "recommendedAction": "next safe action"
    }
  ],
  "recommendedNextSteps": ["safe follow-up"],
  "limitations": ["what could not be verified"]
}
```

Invalid JSON fails closed, while preserving logs and imported evidence.

## Scanner Evidence

Supported passive imports:

- `--sarif`
- `--nuclei-jsonl`
- `--zap-json`
- `--nmap-xml`
- `--burp-xml`
- `--nessus-xml`

Scanner execution remains external in v1. KelpClaw records and signs the evidence it receives.

## Outputs

Default output layout:

```text
.kelpclaw/appsec/<run-id>/
  appsec-run.json
  appsec-input.json
  appsec-triage.json
  findings.sarif
  policy-decisions.json
  docker-build.stdout.log
  docker-build.stderr.log
  agent.stdout.log
  agent.stderr.log
  evidence-workspace/
  audit-bundle/
```

The audit bundle contains a static `index.html`, manifest, signature, attestation, SARIF, run metadata, policy decisions, and logs.

## Safety

`appsec-agent-baseline` blocks destructive shell, credential exfiltration, exploit execution, persistence, and lateral movement. It requires approval for active scanners and container runtime actions beyond Docker build.
