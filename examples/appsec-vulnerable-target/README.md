# KelpClaw AppSec Harness Example

Tiny intentionally vulnerable Docker fixture plus deterministic scanner imports for a local AppSec audit demo.

The default audit below does not run Docker or live scanners. It imports static SARIF and Nuclei fixtures, runs the sample triage agent, signs evidence, and verifies the audit bundle.

```console
pnpm kelp-claw appsec audit \
  --context "$PWD/examples/appsec-vulnerable-target" \
  --dockerfile Dockerfile \
  --skip-docker-build \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-vulnerable-target/sample-triage-agent.mjs" \
  --sarif "$PWD/examples/appsec-vulnerable-target/fixtures/sample.sarif" \
  --nuclei-jsonl "$PWD/examples/appsec-vulnerable-target/fixtures/nuclei.jsonl" \
  --run-id appsec-example.local \
  --out .kelpclaw/appsec-example

pnpm kelp-claw verify-audit-bundle .kelpclaw/appsec-example/audit-bundle
```

Optional local app run:

```console
docker build -t kelpclaw-appsec-example examples/appsec-vulnerable-target
docker run --rm -p 127.0.0.1:8080:8080 kelpclaw-appsec-example
```

The app is for localhost lab use only. Fixtures are deterministic and do not require external network access.
