# Prompt: Add Query Quality Metrics for Source and Sink Specs

You are working in Piranesi, a security scanning CLI that uses source/sink specifications to detect tainted flows. The project needs better visibility into detection quality: how many specs are loaded, which specs match, which specs never match, and which specs generate too many candidates.

Goal: add query quality metrics for source/sink specifications so maintainers can evaluate coverage and noisy rules.

Implementation requirements:

- Inspect spec loading and detection code in `src/piranesi/scan/` and `src/piranesi/detect/`.
- Add metrics that can be emitted in scan/report artifacts, such as loaded source specs, loaded sink specs, matched specs, unmatched specs, candidate counts by spec, and noisy/high-cardinality specs.
- Keep the default user experience concise. Detailed metrics can live in JSON or an optional diagnostics section.
- Include enough metadata to identify a spec by ID/name/category/file.
- Add tests for metrics generation using small fixtures.
- Update docs to explain how maintainers should use these metrics to improve rule quality.

Acceptance criteria:

- Running a scan can produce machine-readable source/sink quality metrics.
- Reports or diagnostics identify specs that are unused or unusually noisy.
- Tests cover metrics for matched and unmatched specs.
- No LLM dependency is introduced.
