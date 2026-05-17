> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Example: Hand-Crafted Vulnerable Express App

This example documents the release-validation run against the bundled sample app in `examples/vuln-express`.

## Target

- Location: `examples/vuln-express`
- Intentional vulnerabilities: 5
- Planted classes: SQLi, reflected XSS, path traversal, command injection, SSRF

## Setup

From the repository root:

```bash
cd examples/vuln-express
npm install
cd ../..
```

## Full CLI Invocation

The release pass used the real CLI entry point:

```bash
export OPENAI_API_KEY="<your_key>"

uv run piranesi run examples/vuln-express \
  --authorized \
  --yes \
  --output /tmp/piranesi-vuln-detect-out \
  --no-execute \
  --quiet
```

`--no-execute` keeps the verify stage from launching Docker. If no LLM credential is configured, triage runs in deterministic pass-through mode and patch generation is skipped.

For a compact terminal summary, the same target was also run through the helper script:

```bash
uv run python docs/examples/run_detect_summary.py examples/vuln-express
```

## Representative Output

```text
Piranesi Detect Summary
Target: /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express
Transpile failures tolerated: 0
Candidate findings: 4
By CWE:
  CWE-22: 1
  CWE-78: 1
  CWE-79: 1
  CWE-918: 1
Findings:
  - CWE-78 | source=cmd | sink=execSync | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:49
  - CWE-79 | source=q | sink=res.send | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:31
  - CWE-22 | source=file | sink=fs.readFileSync | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:43
  - CWE-918 | source=url | sink=fetch | /Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express/app.js:55
```

The generated report summary for the same `piranesi run` execution was:

```text
# Piranesi Security Analysis Report

**Target:** `/Users/gongahkia/Desktop/coding/projects/piranesi/examples/vuln-express`
**Version:** piranesi v0.2.0

## Executive Summary

- **Findings detected:** 4
- **Findings confirmed:** 0
- **Severity breakdown:** none
- **Top regulatory concerns:** none
- **Total LLM cost:** $0.00
- **Duration:** 19.5s
```

## What Was Found

Piranesi correctly identified four of the five planted issues:

- `CWE-79` reflected XSS in `searchHandler`
- `CWE-22` path traversal in `filesHandler`
- `CWE-78` command injection in `shellHandler`
- `CWE-918` SSRF in `proxyHandler`

These matched the intended vulnerable routes:

- `/search`
- `/files`
- `/shell`
- `/proxy`

## What Was Missed

The current release build missed the planted SQL injection in `/users`.

Why it was missed:

- The vulnerable handler routes user input into a local helper named `query(...)`.
- Piranesi's built-in SQL sink model looks for database-style sink names such as `query`, `$queryRaw`, `$executeRaw`, and `raw`, but the current flow extraction still did not promote this helper-based pattern into a candidate finding in this sample.
- This is a real detector gap, not a documentation issue.

## False Positives

None in the current detect-only run. The four emitted candidate findings all corresponded to planted vulnerabilities.

## Verification, Triage, and Legal Stages

- Triage executed through the configured LLM provider.
- `triage.json` recorded model-backed verdicts for each candidate.
- Verify produced no confirmed findings because the run used `--no-execute`.
- Legal and patch outputs were therefore empty.

## Timing

The actual `piranesi run` artifacts recorded:

- Scan: 9.9s
- Detect: 9.7s
- Triage: 0.0s
- Verify: 0.0s
- Legal: 0.0s
- Patch: 0.0s
- Report: 0.0s
- Total: 19.5s

## Takeaway

This is the cleanest current release example for the real CLI:

- The run is reproducible on a fresh machine.
- The signal is good: 4 true positives and 0 false positives.
- The remaining SQLi miss is an honest release note and a useful regression target for the next release.
