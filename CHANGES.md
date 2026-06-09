# FindEvil Hackathon Audit Changes

## Task 1: tool execution provenance
- Added optional, backward-compatible `toolUseId` and `toolName` fields to FindEvil claim `evidenceRefs`.
- Threaded provenance from normalized Sentinel agent events into linker-produced evidence refs when an artifact path matches the tool call path.
- Preserved undefined provenance when no producing tool event is available.
- Changed files: `packages/findevil/src/types/claim.ts`, `packages/findevil/src/extractor/prompts.ts`, `packages/findevil/src/linker/index.ts`, `packages/findevil/src/sentinel/index.ts`, `packages/findevil/test/linker.test.ts`.
- New test: linker evidence refs carry the producing tool call metadata.

## Task 2: hallucination metric
- Added `confirmedClaims`, `hallucinationCount`, and `hallucinationRate` to benchmark scoring and DFIR-Metric output.
- Definition used in code and output: a hallucinated finding is a confirmed claim with no ground-truth support; inferred claims are excluded.
- Kept existing TP/FP/FN/precision/recall/F1 behavior unchanged.
- Changed files: `packages/findevil/src/benchmark/types.ts`, `packages/findevil/src/benchmark/scorer.ts`, `packages/findevil/src/benchmark/benchmark.ts`, `packages/findevil/src/benchmark/dfir-metric.ts`, `packages/cli/src/findevil/benchmark.ts`, `packages/findevil/test/benchmark.test.ts`.
- New test: confirmed unsupported claims count as hallucinations; inferred unsupported claims do not.

## Task 3: MCP symlink containment
- Hardened MCP evidence path resolution with lexical containment plus realpath containment against the real evidence root.
- Added no-follow file opens for MCP file hashing and text reads where the runtime exposes `O_NOFOLLOW`.
- Left `checkReadOnlyMount()` unwired by design; the live MCP boundary remains path containment plus typed read-only command allowlisting.
- Changed files: `packages/findevil/src/mcp/server.ts`, `packages/findevil/test/mcp-server.test.ts`.
- New test: a symlink inside `evidenceRoot` pointing outside is rejected; normal contained files still hash.

## Residual gaps
- Firewall handling in the current Sentinel flow remains a detective/post-exec normalization control unless the external hook integration enforces it before tool execution.
- `checkReadOnlyMount()` remains available in `spoliation/mount.ts` but is not called by Sentinel or MCP in this change.
- `toolUseId`/`toolName` provenance is attached only when a normalized tool event contains a path-like artifact value that resolves inside `evidenceRoot`.
