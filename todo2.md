# TODO 2: Add Lynis And OpenSCAP Evidence Support

## Goal

Add host evidence ingestion and deterministic analysis support for Lynis and OpenSCAP so Piranesi can move beyond osquery plus Trivy and start covering compliance and hardening baselines described in the VM vulnerability proposal.

## Current State

Current collection is centered on:

- osquery JSON
- optional Trivy filesystem vulnerability JSON
- selected local command helpers for firewall, apt updates, SSH, groups, and sysctl

Relevant files:

- `src/piranesi/host/collect.py`
- `src/piranesi/host/ingest.py`
- `src/piranesi/host/analyze.py`
- `src/piranesi/host/models.py`
- `src/piranesi/host/report.py`
- `tests/test_host_posture.py`
- `docs/host-posture.md`
- `README.md`

The proposal explicitly names:

- Lynis for lightweight Linux hardening/compliance checks.
- OpenSCAP for NIST-standardized compliance scanning through XCCDF and OVAL.

## Desired Behavior

Add optional collection flags:

```bash
uv run piranesi collect --output piranesi-evidence --lynis
uv run piranesi collect --output piranesi-evidence --openscap
uv run piranesi collect --output piranesi-evidence --lynis --openscap
```

Default behavior should remain conservative. If these tools are expensive or intrusive, decide whether they should be opt-in. Document the choice.

Raw bundle layout should support:

```text
piranesi-evidence/
  raw/
    lynis/
      report.dat
      report.json
    openscap/
      results.xml
      report.html
      oval-results.xml
```

Hand-built bundles should also be accepted:

```text
evidence-bundle/
  lynis/
    report.dat
  openscap/
    results.xml
```

## Data Model

Do not force all Lynis/OpenSCAP details into loosely typed `config` only. Add enough structure to make findings and reports robust.

Suggested additions:

```python
class BaselineCheck(BaseModel):
    source: Literal["lynis", "openscap"]
    check_id: str
    title: str
    result: Literal["pass", "fail", "warn", "not_applicable", "unknown"]
    severity: Severity | None = None
    control_refs: list[str] = Field(default_factory=list)
    evidence: list[EvidenceItem] = Field(default_factory=list)
    remediation: str | None = None
```

Add to `HostSnapshot`:

```python
baseline_checks: list[BaselineCheck] = Field(default_factory=list)
```

If changing the canonical schema is too broad, store parsed results under `config["baseline_checks"]` as an interim step, but prefer a typed model.

## Collection

Implement optional executable discovery for:

- `lynis`
- `oscap`

Record all commands in `collection-manifest.json`.

Suggested commands:

- Lynis: `lynis audit system --no-colors --quiet` plus export/copy of `report.dat`.
- OpenSCAP: support at least ingesting pre-existing XCCDF/OVAL XML first. Live OpenSCAP profile execution may require distribution-specific content packages and should be carefully documented.

Important:

- Do not mutate host state.
- Record missing tools as optional health warnings, not findings.
- Time out long-running commands.
- Store stderr compactly in the manifest.

## Ingestion

Implement parsers:

- Lynis `report.dat` parser for warnings, suggestions, hardening index, tests performed.
- Optional Lynis JSON parser if available.
- OpenSCAP XML parser for rule result, idref, severity, title, description, remediation/fix text if present.

Avoid ad hoc string parsing when XML APIs are practical. Use `xml.etree.ElementTree` or an existing dependency already in the project.

## Deterministic Findings

Add host findings for failed/warn baseline checks:

- Category: `baseline` or `compliance`
- Source tool: `lynis` or `openscap`
- Include `control_refs` when available.
- Confidence should depend on parser/source quality. OpenSCAP rule results can be high confidence. Lynis suggestions may be medium-high.
- Deduplicate findings by source, check id, and host.

Add top action grouping for baseline/compliance issues.

## Collection Health

Extend capability health in `src/piranesi/host/analyze.py`:

- `lynis`
- `openscap`

These should be optional unless the user explicitly requested them. If requested and failed, surface clear health warnings.

## Tests

Add fixtures under:

```text
tests/fixtures/host/baseline/
  lynis/report.dat
  openscap/results.xml
```

Tests should cover:

- Loading raw Lynis-only evidence.
- Loading raw OpenSCAP-only evidence.
- Combined osquery + Lynis + OpenSCAP bundle.
- Failed checks become findings.
- Passed checks do not become findings.
- Control references are preserved.
- Missing optional tool manifests create health warnings, not false findings.

## Documentation

Update:

- `README.md`
- `docs/host-posture.md`

Document:

- Required external tools.
- Example bundle layout.
- Supported parser formats.
- Limitations of profile execution.
- How missing Lynis/OpenSCAP evidence affects confidence.

## Acceptance Criteria

- Piranesi can ingest Lynis and OpenSCAP evidence from a raw bundle.
- Piranesi can optionally collect Lynis/OpenSCAP evidence when tools are installed.
- Deterministic host findings include failed baseline checks with evidence and remediation.
- Existing osquery/Trivy-only workflows continue to pass.
- Tests cover parser and report behavior.

## Out Of Scope

- Full CIS profile coverage guarantees.
- Enterprise policy management.
- Remote OpenSCAP remediation execution.
- Auto-remediation.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py
uv run piranesi assess tests/fixtures/host/baseline --output /tmp/piranesi-baseline-out --format both
```

