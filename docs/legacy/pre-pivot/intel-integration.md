# Intel Integration (Track G)

## Scope
Track G adds optional, offline-first intelligence workflows that ingest external tool snapshots, normalize findings into a stable schema, build reproducible relationship graphs, and enforce policy-bound agent operations.

The core `scan/detect/triage/verify/report` deterministic path is unchanged.

## Modules
- `src/piranesi/adapters/`
  - Snapshot parsers for `sarif`, `codeql_sarif`, `semgrep`, `trivy`, and `zap`.
- `src/piranesi/intel/`
  - Canonical provenance and normalized-finding schemas.
  - Trust/staleness scoring.
  - Enrichment ranking signal.
  - Agent harness policy contracts.
- `src/piranesi/graph/`
  - Local graph node/edge model and deterministic builder.

## Canonical Schemas
- `IntelSourceProvenance`
  - Captures source name, tool, snapshot path/hash, trust level, staleness horizon, ingest timestamp.
- `NormalizedExternalFinding`
  - Stable `finding_id`, core security fields, location/package/endpoint context, provenance, trust/staleness scores.
- `NormalizationBundle`
  - Serializable snapshot with findings and parser diagnostics.
- `IntelligenceGraph`
  - Nodes: `asset`, `domain`, `repo`, `package`, `dependency`, `endpoint`, `finding`, `advisory`.
  - Edges: `depends_on`, `hosts`, `calls`, `affected_by`, `mentions`, `reachable_from`.

## CLI Workflows

```bash
# Normalize external snapshot
piranesi intel normalize external.sarif.json \
  --tool sarif \
  --source-name codeql-ci \
  --trust-level verified \
  --output piranesi-output/intel/normalized.json

# Build local relationship graph
piranesi intel graph \
  --normalized piranesi-output/intel/normalized.json \
  --output piranesi-output/intel/graph.json

# Produce enrichment summary report extension
piranesi intel summary \
  --normalized piranesi-output/intel/normalized.json \
  --output piranesi-output/intel/summary.json
```

These commands use only local files and do not require network access.

## Trust And Staleness Model
- Trust levels: `verified`, `trusted`, `untrusted`.
- Staleness score decays with `stale_after_hours` and optional `collected_at` timestamp.
- Source quality is derived from trust and staleness; it influences enrichment confidence but never replaces base evidence.

## Agent Harness Policy Contracts
`AgentActionRequest` operations: `run`, `parse`, `normalize`, `score`, `explain`.

`AgentPolicy` gates:
- operation allowlist,
- mutating operation permission,
- sensitive evidence permission.

Requests violating policy return deterministic denial reasons.

## Ranking Behavior
`enrichment_priority_signal` introduces a bounded adjustment (`[-10, +10]`) to ranking inputs. This preserves the base evidence score as authoritative and limits enrichment-induced volatility.

## Failure Handling
- Parser failures emit diagnostics and never mutate core artifacts.
- Graph build failures return explicit missing-node edge errors.
- Policy denials are explicit and non-bypassable in contract checks.

## Update Cadence
- Adapter fixture review: weekly.
- Schema drift review: per release candidate.
- Trust/staleness threshold review: monthly.
- Agent policy review: quarterly or after any incident involving evidence handling.

## Test Coverage
- Adapter parsing and normalization: `tests/test_intel_adapters.py`
- Graph correctness and bounded ranking behavior: `tests/test_intel_graph.py`
- Agent harness policy enforcement: `tests/test_intel_agent_harness.py`
- CLI integration workflow: `tests/test_intel_cli.py`
