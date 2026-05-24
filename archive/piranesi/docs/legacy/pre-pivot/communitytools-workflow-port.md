> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Communitytools Workflow Port

This document identifies the parts of the bundled `communitytools/` tree that fit
Piranesi's purpose and should be ported into the product workflow.

Piranesi's center of gravity is a local-first, CLI-native AppSec analysis
pipeline. The architecture is intentionally stage based: `scan`, `detect`,
`triage`, `verify`, `legal`, `patch`, and `report` exchange inspectable JSON
artifacts. That shape should remain the stable core. The useful material from
`communitytools` is not its interactive slash-command UX; it is the operating
discipline around evidence workspaces, role-separated validation, compact
technique references, and append-only CLI logs.

## Fit Summary

| Communitytools pattern | Piranesi fit | Port target |
| --- | --- | --- |
| Engagement output tree with `recon/`, `findings/`, `logs/`, `tools/`, `artifacts/validated`, and `artifacts/false-positives` | Strong fit. Piranesi already writes stage artifacts; this adds a human and agent-friendly evidence workspace around them. | New optional `--engagement` output layout, backed by `report/evidence` helpers. |
| NDJSON coordinator/executor logs plus a tool invocation archive | Strong fit. Agents debug best from commands, stdout, stderr, exit codes, and stable files. | Extend `observability.run_subprocess` to optionally archive significant command invocations. |
| Append-only `experiments.md` registry | Strong fit for `verify`, exploit reproduction, and manual investigation. | Add `piranesi debug experiment add/update/list` or generate during `verify`. |
| Blind validator role with evidence-only context | Strong fit conceptually, but should be deterministic first. | Add `piranesi verify validate-evidence` before any LLM validator mode. |
| Five validation checks: severity/CVSS, evidence exists, PoC syntax/target, claims vs raw output, log corroboration | Strong fit after translation from pentest findings to Piranesi artifacts. | Add validation models and tests under `verify/` and `report/`. |
| Compact skill references with line limits and progressive disclosure | Good fit for rule authoring and agent guidance, not runtime scanning. | Add `docs/agent-playbooks/` or `rules/community/` references with strict size checks. |
| Failure-driven benchmark loop: run, find misses, generalize, add reference, rerun | Strong fit. Piranesi already has `eval/validate_all.py`, coverage gaps, and report comparison. | Make this the default evaluator workflow for new detectors and rules. |
| Tech stack OSINT workflow | Partial fit. Active/passive internet recon is outside core local-first scanning, but normalized snapshots fit `intel/`. | Keep offline-first: ingest external recon JSON through `piranesi intel normalize`. |
| Kali/Playwright interactive pentest container | Partial fit. Useful for optional verification, but too broad for core. | Provide optional debug recipes, not a default dependency. |

## Port Priorities

### 1. Agent-Friendly CLI Debug Bundle

Add a first-class debug bundle that makes one run easy to inspect without a UI:

```text
piranesi-output/
|-- scan.json
|-- detect.json
|-- triage.json
|-- verify.json
|-- legal.json
|-- patch.json
|-- report.json
|-- report.md
|-- debug/
|   |-- commands.ndjson
|   |-- stage-timings.json
|   |-- experiments.md
|   `-- tools/
|       |-- 001_joern-import.md
|       |-- 002_tsc.md
|       `-- 003_docker-verify.md
`-- evidence/
    `-- finding-<stable-id>/
        |-- finding.json
        |-- raw-source.txt
        |-- reproducer.py
        |-- reproducer-output.txt
        `-- validation/
```

CLI contract:

```bash
piranesi run . --authorized --yes --json-logs --debug-bundle
cat piranesi-output/debug/commands.ndjson
ls piranesi-output/debug/tools/
```

Implementation notes:

- Keep stdout/stderr machine readable when `--json-logs` is set.
- Archive exact command, cwd, duration, exit code, stdout, and stderr previews.
- Prefer deterministic command replay over screenshots or interactive UI.
- Never require network access for the core debug bundle.

### 2. Evidence Validation Gate

Port the `communitytools` blind-validation discipline into a deterministic
Piranesi stage before adding any model-based validator.

Translated checks:

| Check | Piranesi version |
| --- | --- |
| CVSS consistency | Severity, composite risk band, and advisory CVSS must not contradict each other. |
| Evidence exists | Every reported finding must link to `scan/detect/verify` artifacts and at least one source location. |
| PoC validation | Generated reproducer parses, references the target profile or source location, and records execute/skipped status. |
| Claims vs raw evidence | Report claims must map back to raw artifact fields, source snippets, advisory snapshots, or command logs. |
| Log corroboration | Verification claims require corresponding `verify_attempt` and command-log events with distinct timestamps. |

CLI contract:

```bash
piranesi validate-evidence piranesi-output --format json
piranesi validate-evidence piranesi-output --strict
```

The output should preserve rejected material under
`evidence/false-positives/<finding-id>.json` instead of silently dropping it.

### 3. Technique References As Detector Fuel

Port selected `communitytools/skills/*/reference` content into compact,
test-backed rule or detector improvements. Do not import the prose wholesale.

High-value candidates:

- Injection bypass ladders: SQL keyword reconstruction, NoSQL filter merges,
  command injection separators, SSTI escalation.
- Server-side patterns: SSRF loopback/container pivots, path traversal encoding,
  file upload extension checks, deserialization entry points.
- Client-side patterns: DOM XSS sinks, prototype pollution sources/sinks, CORS
  misconfigurations, CSRF evidence expectations.
- Authentication patterns: JWT algorithm and key-confusion checks, OAuth redirect
  URI issues, mass assignment.
- Source-code scanning language patterns for Python, Java, Go, PHP, and Ruby,
  matching Piranesi's current experimental/pattern-only language coverage.

Porting rule:

1. Add or update a detector/spec.
2. Add a minimal vulnerable fixture and a safe fixture.
3. Run `eval/validate_all.py` or focused tests.
4. Update known limitations if coverage is intentionally partial.

### 4. Failure-Driven Skill And Rule Updates

Adopt the `communitytools` improvement loop for Piranesi's evaluation harness:

```text
run benchmark -> inspect miss/noise -> diagnose missing general pattern ->
update detector/rule/reference -> add fixture -> rerun and compare
```

This fits the existing evaluator commands:

```bash
python3 eval/validate_all.py --gt-dir eval/ground_truth --output /tmp/current.json
python3 eval/coverage_gap_report.py --gt-dir eval/ground_truth --dimension cwe+language
python3 eval/compare_reports.py --baseline-report /tmp/baseline.json --current-report /tmp/current.json
```

The guardrail from `communitytools` should be kept: only generalizable patterns
belong in Piranesi. Do not add target-specific payloads, hostnames, flags, or
challenge-only assumptions.

### 5. Offline Recon And Tech Stack Ingestion

Do not make Piranesi an OSINT crawler by default. Instead, let agents or external
tools produce snapshots, then ingest them through the existing `intel` workflow:

```bash
piranesi intel normalize techstack-report.json \
  --tool generic \
  --source-name external-recon \
  --trust-level trusted \
  --output piranesi-output/intel/normalized.json

piranesi intel graph \
  --normalized piranesi-output/intel/normalized.json \
  --output piranesi-output/intel/graph.json
```

Needed adapter work:

- Add `techstack` parser support for the JSON schema in
  `communitytools/formats/techstack-json-report.md`.
- Map discovered packages, frameworks, domains, and endpoints into the existing
  `IntelligenceGraph` node types.
- Keep enrichment bounded so external recon cannot override Piranesi's local
  evidence.

### 6. Local Launch Enumeration

Piranesi should help agents discover how to run a local target before dynamic
verification. The first bounded workflow is:

```bash
piranesi dev launch-plan ./app
piranesi dev launch-plan ./app --json
piranesi dev launch-plan ./app --write-profile auto --probe
```

This infers local launch candidates from `package.json`, common Python app entry
points, and basic Docker hints, then prints or writes a
`[verify.target_profiles.<name>]` snippet. With `--probe`, Piranesi starts the
first inferred candidate locally, polls readiness, writes launch logs to
`piranesi-output/debug/`, and tears the process down. The verify stage can then
use `--target-profile <name>` to start the app locally, run readiness checks,
replay safe proof requests, and tear the process down.

Keep this local and explicit. Do not turn launch enumeration into broad crawling
or remote exploitation; external recon should enter through `intel normalize`.

## What Not To Port

- The slash-command UX. Piranesi should stay Typer CLI first.
- The broad Kali container as a required path. It expands the trust boundary and
  dependency surface too much for a local SAST pipeline.
- Social-engineering workflows. They are outside Piranesi's stated AppSec source
  analysis intent.
- Report branding assets. Piranesi already has Markdown, SARIF, JUnit, CSV, TUI,
  and compliance outputs; branded PDF generation is secondary.
- Unbounded payload catalogues. Use compact detector specs and fixtures instead.

## Recommended Implementation Order

1. Add `--debug-bundle` and command archive plumbing.
2. Add evidence directory generation for verified findings.
3. Add deterministic evidence validation CLI.
4. Port two or three high-value technique families into detectors with fixtures.
5. Add a `techstack` intel adapter for offline recon snapshots.
6. Expand `dev launch-plan` into profile writing and optional readiness probing.
7. Add optional model-assisted blind validation after deterministic validation is
   stable.

This order prioritizes CLI-based debugging and agent replayability before adding
more attack knowledge. That is the right bias for Piranesi because better logs,
stable artifacts, and deterministic replay make every later detector easier for
agents and humans to debug.
