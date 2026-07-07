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
  --fail-on-qa error \
  --out .kelpclaw/appsec/local
```

Verify:

```console
$ kelp-claw verify-audit-bundle .kelpclaw/appsec/local/audit-bundle
```

AppSec QA writes `appsec-qa.json` into the output directory and audit bundle.
`--fail-on-qa error` fails only on QA errors; `--fail-on-qa warning` also fails
on warnings. Without `--fail-on-qa`, QA issues are recorded but do not fail the
audit.

Compare two AppSec runs:

```console
$ kelp-claw appsec diff \
  --baseline .kelpclaw/appsec/baseline \
  --current .kelpclaw/appsec/current
$ kelp-claw appsec diff \
  --baseline .kelpclaw/appsec/baseline/evidence-workspace \
  --current .kelpclaw/appsec/current/evidence-workspace \
  --format markdown \
  --out .kelpclaw/appsec/diff.md \
  --fail-on regressed
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
  --zap-json zap.json \
  --nmap-xml nmap.xml \
  --burp-xml burp.xml \
  --nessus-xml nessus.xml
```

Imported AppSec findings use deterministic `evidence:<sha256-prefix>` IDs derived
from the scanner tool, upstream rule/template/plugin ID, rule/name, asset,
location, weakness IDs, and normalized title. Upstream IDs are preserved in
`sourceReferences[].metadata` and `provenance`.

IDs are stable for equivalent normalized scanner findings across reruns and
input file paths. They can change when scanners rename rules, report different
asset/location values, or alter weakness mappings.

Normalized findings may include `mappings` with `cwe`, `owaspAsvs`,
`owaspTop10`, and `owaspLlmTop10` arrays. Scanner-supplied mappings are
preserved when present, agent triage may add the same optional object, and
unmapped findings remain valid.

## PR Workflow

```yaml
name: KelpClaw AppSec
on: [pull_request]
jobs:
  appsec:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
        with:
          mode: appsec
          context: .
          dockerfile: Dockerfile
          agent-command: ./appsec-agent.sh
          policy: appsec-agent-baseline
          skip-docker-build: "false"
          fail-on-qa: "true"
          lab-mode: "false"
          sarif: findings.sarif
          nuclei-jsonl: nuclei.jsonl
          zap-json: zap.json
          nmap-xml: nmap.xml
          burp-xml: burp.xml
          nessus-xml: nessus.xml
          upload-artifact: "true"
          upload-sarif: "true"
```

## Supporting Surfaces

KelpClaw still supports agent skill audit, repository inventory, governed web evidence, release manifests, and signed evidence workspaces. Those flows support the AppSec harness instead of replacing it.
