# Agent Implementation Prompts

These prompts capture the remaining implementation ideas that were identified after the repository review. Each file is intended to be copied into a separate Codex agent with enough context to work independently.

Recommended implementation order:

1. `01-strengthen-report-evidence-semantics.md`
2. `02-helper-wrapper-sink-promotion.md`
3. `03-framework-aware-receiver-disambiguation.md`
4. `04-nodegoat-known-miss-regressions.md`
5. `05-query-quality-metrics.md`
6. `06-explanation-confidence-metadata.md`
7. `07-verification-exploit-templates.md`
8. `08-verification-preconditions.md`
9. `09-safe-proof-mode.md`
10. `10-target-launch-profiles.md`
11. `11-rich-verification-evidence.md`
12. `12-suppression-lifecycle.md`
13. `13-pr-friendly-baselines.md`
14. `14-lsp-watch-incremental-dx.md`
15. `15-compliance-framework-metadata.md`
16. `16-compliance-evidence-bundles.md`
17. `17-control-owner-metadata.md`
18. `18-composite-risk-scoring.md`
19. `19-plugin-api-stability.md`
20. `20-example-rule-packs.md`
21. `21-rule-authoring-ux.md`
22. `22-advisory-db-cli-workflow.md`
23. `23-first-class-docker-artifact.md`
24. `24-ci-provider-templates.md`
25. `25-first-run-golden-path-e2e.md`
26. `26-token-budget-enforcement.md`
27. `27-llm-privacy-prompt-injection-hardening.md`
28. `28-known-limitations-registry.md`

Suggested parallel tracks:

- Detection: prompts 2, 3, 4, 5
- Verification: prompts 7, 8, 9, 10, 11
- Reporting and risk: prompts 1, 6, 18, 28
- Compliance: prompts 15, 16, 17
- Developer experience: prompts 12, 13, 14, 21, 25
- Ecosystem and distribution: prompts 19, 20, 22, 23, 24
- LLM safety and operations: prompts 26, 27

General agent instruction:

- Work in small, coherent commits when requested by the coordinating user.
- Do not revert unrelated user or agent changes.
- Prefer deterministic behavior when an LLM key is unavailable.
- Add or update tests for each behavior change.
- If the local environment lacks optional dependencies, run lightweight checks such as `python3 -m compileall`, `python3 -m ruff check`, and targeted dependency-free smoke tests where possible.
