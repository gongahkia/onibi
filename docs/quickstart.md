# KelpClaw Quickstart

Use the AppSec harness first.

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm --filter @kelpclaw/cli build
$ pnpm --filter @kelpclaw/cli exec kelp-claw doctor
```

Create an agent command that writes valid triage JSON:

```sh
#!/usr/bin/env sh
cat > "$KELPCLAW_APPSEC_OUTPUT" <<'JSON'
{
  "summary": "No live triage agent configured; sample output only.",
  "triageFindings": [],
  "recommendedNextSteps": ["Import scanner output and rerun."],
  "limitations": ["Sample agent did not inspect the target."]
}
JSON
```

Run an audit:

```console
$ kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command ./appsec-agent.sh \
  --out .kelpclaw/appsec/local
```

Verify:

```console
$ kelp-claw verify-audit-bundle .kelpclaw/appsec/local/audit-bundle
```

## Scanner Evidence

Add passive scanner outputs when available:

```console
$ kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command ./appsec-agent.sh \
  --sarif findings.sarif \
  --nuclei-jsonl nuclei.jsonl \
  --zap-json zap.json
```

## Supporting Surfaces

KelpClaw still supports agent skill audit, repository inventory, governed web evidence, release manifests, and signed evidence workspaces. Those flows support the AppSec harness instead of replacing it.
