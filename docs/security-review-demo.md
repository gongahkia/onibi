# Security Review Demo

This demo shows how to hand AppSec triage evidence to a reviewer without asking them to run KelpClaw locally.

```console
$ kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command ./appsec-agent.sh \
  --sarif findings.sarif \
  --out .kelpclaw/appsec/review

$ kelp-claw verify-audit-bundle .kelpclaw/appsec/review/audit-bundle
```

Expected bundle files include `appsec-run.json`, `appsec-input.json`, `appsec-triage.json`, `policy-decisions.json`, `findings.sarif`, `manifest.json`, `manifest.sig`, `attestation.json`, and `attestation.sig`.

The reviewer opens `audit-bundle/index.html`, checks SARIF findings, reads the triage limitations, and verifies the manifest signature before trusting the artifact.
