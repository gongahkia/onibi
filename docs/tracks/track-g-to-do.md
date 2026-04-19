# Track G TODO: Graph Intelligence, Cross-Tooling, And Agent Harness

## Goal
Integrate web/asset intelligence signals, normalize external security tool findings, and add a policy-bound agent harness to improve prioritization quality without compromising deterministic core behavior.

## Priority
P2/P3. Execute after Tracks A-F hardening and reliability milestones.

## Work Items
1. Add an `intel` ingestion layer with snapshot-based inputs (no live network dependency in core scan path).
2. Define canonical enrichment schemas for external advisories, asset metadata, and graph edges.
3. Build adapters for external tool outputs (first wave: SARIF, Semgrep JSON, CodeQL SARIF, Trivy JSON, ZAP JSON).
4. Implement a unified external-finding normalization pipeline with provenance and confidence attribution.
5. Introduce a local graph model for entities and relationships:
6. Nodes: asset, domain, repo, package, dependency, endpoint, finding, advisory.
7. Edges: depends_on, hosts, calls, affected_by, mentions, reachable_from.
8. Add staleness and trust scoring for each enrichment source.
9. Add enrichment-aware prioritization features that can influence ranking but not overwrite base evidence.
10. Add agent-tool contracts (`run`, `parse`, `normalize`, `score`, `explain`) with typed I/O boundaries.
11. Add policy gates for agent actions (read-only vs mutating operations, redaction and evidence restrictions).
12. Add deterministic/offline mode guarantees where enrichment and agent layers are optional.
13. Add report extensions for enrichment provenance, graph-derived context, and trust/staleness summaries.
14. Add benchmark fixtures and regression tests for external adapter parsing, graph correctness, and ranking stability.

## Deliverables
1. New modules under `src/piranesi/intel/`, `src/piranesi/adapters/`, and `src/piranesi/graph/`.
2. Canonical schemas for normalized external findings and graph entities/edges.
3. Adapter implementations with parser tests and fixture corpora.
4. Agent harness contracts and policy enforcement layer.
5. CLI/report updates to surface enrichment signals and provenance.
6. Operational documentation for source trust, update cadence, and failure handling.

## Acceptance Criteria
1. Core deterministic lanes remain green with enrichment disabled.
2. Enrichment lanes execute independently and fail with actionable parser/provenance diagnostics.
3. External findings normalize into a stable internal schema with lossless critical fields.
4. Graph relationships are reproducible from snapshots and validated by regression tests.
5. Agent harness cannot bypass policy constraints or redaction boundaries.
6. Prioritization improves measurable signal quality without false-positive inflation beyond agreed threshold.

## Metrics
1. Adapter parse success rate by tool/source.
2. Normalization schema drift incidents per release.
3. Graph entity/edge validation pass rate.
4. Enrichment source freshness and trust score coverage.
5. Ranking delta quality against baseline precision/recall and triage effort.
6. Offline deterministic run success rate with enrichment disabled.

## Suggested Sequence
1. Phase 1: external-finding normalization and SARIF-first adapter.
2. Phase 2: graph storage/model and deterministic relationship builder.
3. Phase 3: enrichment scoring and report/CLI integration.
4. Phase 4: policy-bound agent harness with controlled tool orchestration.

## Status
- [ ] Planned
- [ ] In progress
- [ ] Completed
