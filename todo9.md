# TODO 9: Add Host Evaluation Metrics And Benchmark Harness

## Goal

Create a repeatable host-posture evaluation harness that can measure coverage, triage speed proxies, and false-positive behavior against fixtures and baseline tools.

The proposal claims:

- 15-25% more surface-area vulnerabilities identified versus baseline tools.
- 30-40% faster analyst time-to-decision.
- Fewer false positives through confidence-scored recommendations with evidence backing.

Current host mode does not have benchmark artifacts to support those claims.

## Current State

Relevant files and areas:

- `eval/`
- `tests/test_host_posture.py`
- `tests/fixtures/host/`
- `src/piranesi/host/analyze.py`
- `src/piranesi/host/report.py`
- `README.md`
- `docs/host-posture.md`

The repo has evaluation infrastructure for older SAST/corpus workflows, but not a host-specific benchmark.

## Desired Outputs

Add an evaluation command or script:

```bash
uv run python eval/host_benchmark.py --fixtures tests/fixtures/host --output eval/reports/host-benchmark
```

Output files:

```text
eval/reports/host-benchmark/
  host_benchmark.json
  host_benchmark.md
  findings_matrix.csv
```

Metrics:

- Fixture count.
- Expected issue count.
- Detected issue count.
- True positives.
- False positives.
- False negatives.
- Precision.
- Recall.
- F1.
- Evidence coverage score.
- Mean findings per host.
- Mean top-action count.
- Time-to-triage proxy.

## Ground Truth Format

Add fixture ground truth files:

```text
tests/fixtures/host/debian-vulnerable/ground_truth.json
tests/fixtures/host/debian-clean/ground_truth.json
```

Example:

```json
{
  "schema_version": 1,
  "expected_findings": [
    {
      "rule_id": "host.listener.high_risk_service",
      "instance_key": "tcp:redis-server",
      "severity": "high",
      "title_contains": "Redis"
    }
  ],
  "expected_absent": [
    {
      "rule_id": "host.ssh.permit_empty_passwords"
    }
  ]
}
```

Use rule IDs and instance keys rather than brittle title-only matching.

## Baselines

Support baseline comparisons where evidence exists:

- Trivy-only findings.
- Lynis-only findings if todo2 lands.
- OpenSCAP-only findings if todo2 lands.
- Piranesi deterministic.
- Piranesi deterministic plus LLM, if credentials are configured.

If tools are unavailable, benchmark should skip those baselines and record skip reasons.

## Triage Speed Proxy

Actual analyst time requires a user study, so implement proxies:

- Number of findings after deduplication.
- Number of top actions.
- Evidence completeness.
- Findings with remediation present.
- Findings with risk score present if todo6 lands.
- Report sections needed to decide top three actions.

Document clearly that these are proxies, not measured human time.

## False Positive Proxy

For clean fixtures:

- Any non-coverage finding is a false positive unless listed in ground truth.

For vulnerable fixtures:

- Findings not matching `expected_findings` are potential false positives unless listed under `allowed_extra`.

Add `allowed_extra` to ground truth for legitimate opportunistic findings.

## Implementation Structure

Suggested files:

```text
eval/host_benchmark.py
src/piranesi/host/eval.py
tests/test_host_benchmark.py
```

Keep reusable logic in `src/piranesi/host/eval.py`; keep CLI/script wrapper thin.

## Tests

Add tests for:

- Ground truth parser.
- Matching by rule id and instance key.
- Clean fixture precision handling.
- Markdown report rendering.
- JSON schema stability.
- Skips when optional baseline tool output is missing.

## Documentation

Update:

- `README.md`
- `docs/host-posture.md`

Add an "Evaluation" section:

- How to run benchmark.
- What metrics mean.
- What claims are currently supported.
- What remains unproven.

## Acceptance Criteria

- Host benchmark runs locally without external services.
- Benchmark outputs JSON and Markdown.
- At least current `debian-clean` and `debian-vulnerable` fixtures have ground truth.
- Metrics expose precision/recall or explain why unavailable.
- Docs no longer leave proposal metrics unsupported.

## Out Of Scope

- Formal user study.
- Live production fleet measurement.
- Guaranteed 15-25% coverage improvement claim.
- LLM benchmark requiring paid credentials by default.

## Validation Commands

```bash
uv run pytest tests/test_host_benchmark.py tests/test_host_posture.py
uv run python eval/host_benchmark.py --fixtures tests/fixtures/host --output /tmp/piranesi-host-benchmark
```

