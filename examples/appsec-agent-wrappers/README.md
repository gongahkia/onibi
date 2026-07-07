# AppSec Agent Wrapper Templates

These wrappers adapt common agent CLIs to `kelp-claw appsec audit`. Every wrapper reads `KELPCLAW_APPSEC_INPUT`, scopes the prompt to passive triage, and writes valid JSON to `KELPCLAW_APPSEC_OUTPUT`.

Use Node as the `--agent-command`, then pass the wrapper and its native CLI flags as repeated `--agent-arg` values.

## Deterministic Fixture

```console
kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --skip-docker-build \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-agent-wrappers/deterministic-fixture.mjs" \
  --sarif findings.sarif
```

## Codex CLI

```console
kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-agent-wrappers/codex-cli-wrapper.mjs" \
  --agent-arg --model \
  --agent-arg gpt-5 \
  --sarif findings.sarif
```

The Codex wrapper runs `codex exec --output-last-message <temp> -`. Set `KELPCLAW_CODEX_COMMAND` to override the binary.

## Claude Code

```console
kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-agent-wrappers/claude-code-wrapper.mjs" \
  --agent-arg --model \
  --agent-arg sonnet \
  --sarif findings.sarif
```

The Claude wrapper runs `claude -p --output-format json --tools ""`. Set `KELPCLAW_CLAUDE_COMMAND` to override the binary.

## Goose

```console
kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-agent-wrappers/goose-wrapper.mjs" \
  --agent-arg --provider \
  --agent-arg openai \
  --agent-arg --model \
  --agent-arg gpt-5 \
  --sarif findings.sarif
```

The Goose wrapper runs `goose run --instructions - --no-session --quiet --output-format json`. Set `KELPCLAW_GOOSE_COMMAND` to override the binary.

## Custom Command

```console
kelp-claw appsec audit \
  --context . \
  --dockerfile Dockerfile \
  --agent-command node \
  --agent-arg "$PWD/examples/appsec-agent-wrappers/custom-command-wrapper.mjs" \
  --agent-arg "$PWD/path/to/company-agent" \
  --agent-arg --json \
  --sarif findings.sarif
```

The custom wrapper sends the scoped prompt to stdin and expects triage JSON on stdout.

## Output Contract

Wrappers must write this JSON object:

```json
{
  "summary": "string",
  "triageFindings": [
    {
      "id": "string",
      "title": "string",
      "severity": "critical|high|medium|low|info",
      "confidence": "high|medium|low",
      "evidenceIds": ["input.findings[].id"],
      "mappings": {
        "cwe": ["CWE-200"],
        "owaspAsvs": ["V14.2.1"],
        "owaspTop10": ["A05:2021"],
        "owaspLlmTop10": []
      },
      "rationale": "string",
      "recommendedAction": "string"
    }
  ],
  "recommendedNextSteps": ["string"],
  "limitations": ["string"]
}
```

If the wrapped CLI exits non-zero, omits the output file, or writes invalid JSON, the audit status is `failed`. KelpClaw records stdout/stderr and the triage parse error in the output directory and bundle.
