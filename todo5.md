# TODO 5: Add Evidence-Bound Zero-Day Hypothesis Workflow

## Goal

Add an explicit, safe workflow for zero-day or novel vulnerability hypothesis generation from host evidence while preserving Piranesi's current evidence-bound default behavior.

The proposal describes zero-day discovery through contextual analysis and dependency reasoning. Current host LLM analysis is intentionally conservative: it only reports issues supported by listed evidence keys and rejects findings with no evidence. That remains the right default, but the proposal needs a separate hypothesis mode.

## Current State

Relevant files:

- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `src/piranesi/cli.py`
- `src/piranesi/llm/provider.py`
- `src/piranesi/llm/router.py`
- `tests/test_host_posture.py`
- `docs/host-posture.md`

Current LLM system prompt says:

- Analyze Linux VM security posture evidence.
- Only report issues supported by provided evidence.
- Do not invent missing host facts.

This is good for findings, but it does not support separate, lower-confidence hypotheses.

## Desired Behavior

Add a separate CLI workflow:

```bash
uv run piranesi hypothesize piranesi-evidence --output piranesi-output
```

or an assess option:

```bash
uv run piranesi assess piranesi-evidence --analysis both --hypotheses --output piranesi-output
```

Prefer a separate `hypothesize` command if feasible. Hypotheses should not be mixed into confirmed findings unless clearly marked.

Expected output:

```text
piranesi-output/
  host-hypotheses.json
  host-hypotheses.md
```

## Data Model

Add models:

```python
class HostHypothesis(BaseModel):
    id: str
    title: str
    hypothesis_type: Literal[
        "compound_misconfiguration",
        "novel_attack_path",
        "dependency_risk",
        "configuration_ambiguity",
    ]
    confidence: float = Field(ge=0.0, le=1.0)
    severity_if_true: Severity
    supporting_evidence: list[EvidenceItem] = Field(default_factory=list)
    missing_evidence: list[str] = Field(default_factory=list)
    reasoning_summary: str
    suggested_followup_probes: list[str] = Field(default_factory=list)
    analyst_questions: list[str] = Field(default_factory=list)
    must_not_treat_as_finding: bool = True

class HostHypothesisReport(BaseModel):
    schema_version: int = 1
    target: str
    generated_at: str
    hypotheses: list[HostHypothesis]
```

Hypotheses should be clearly separate from `HostFinding`.

## LLM Prompt Requirements

The prompt must:

- Require JSON output.
- Require every hypothesis to cite available evidence.
- Require missing evidence to be listed explicitly.
- Prohibit claims of confirmed vulnerability unless a deterministic/LLM finding already confirms it.
- Ask for follow-up probes, not exploit payloads.
- Avoid chain-of-thought disclosure. Ask for concise reasoning summaries.

Use a schema validation model similar to `_LlmHostAnalysis`.

## Deterministic Hypotheses

Implement deterministic hypothesis templates first where possible:

- Public SSH + password auth + privileged users, but no auth log evidence.
- Public database service + missing firewall evidence + unknown service config.
- Package CVE with no Trivy fixed version + service running that package may expose it.
- Kernel hardening weak + public services + no patch evidence.

These can be generated without an LLM and are useful when credentials are absent.

## Integration With Adaptive Probing

If todo3 has landed, hypotheses should emit probe IDs or probe suggestions compatible with adaptive probing.

If todo3 has not landed, record plain `suggested_followup_probes` strings only.

## Reporting

Markdown should clearly show:

- "Hypotheses are not confirmed findings."
- Supporting evidence.
- Missing evidence.
- Suggested follow-up.
- Analyst questions.

Do not include hypotheses in:

- `findings_total`
- fail-severity exit behavior
- posture score penalties

Unless product direction changes, hypotheses should not fail CI.

## Tests

Add tests for:

- Deterministic hypotheses generated from `debian-vulnerable`.
- Clean fixture produces no or low-priority hypotheses.
- LLM hypothesis response validation rejects missing supporting evidence.
- Hypotheses are not counted as findings.
- Markdown and JSON hypothesis reports are written.

## Acceptance Criteria

- Piranesi can produce a separate hypothesis report.
- Hypotheses are evidence-bound but may discuss missing evidence and possible attack paths.
- No hypothesis is treated as a confirmed finding.
- CLI and docs make the distinction clear.
- Tests cover deterministic and schema validation behavior.

## Out Of Scope

- Exploit generation.
- Unsafe active verification.
- Automatically escalating hypotheses to findings.
- Claiming true zero-day discovery without analyst validation.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_cli.py
uv run piranesi hypothesize tests/fixtures/host/debian-vulnerable --output /tmp/piranesi-hypotheses
```

