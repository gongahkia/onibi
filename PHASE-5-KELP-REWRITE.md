# PHASE 5: Code-Gen Nodes And Reusable Custom Skills

## Goal

Support workflow steps that no existing skill can cover by generating custom code once, persisting it as a reviewed artifact, and executing it inside NanoClaw containers.

Code-gen nodes must be auditable, replayable, and promotable into reusable skills.

## Code-Gen Policy

OpenClaw creates a code-gen node only after skill registry lookup fails to find a suitable skill.

Generated nodes must include:

- Original node prompt.
- Planner rationale for why no skill fits.
- Generated source code artifact reference.
- Runtime image or build template.
- Dependency manifest.
- Input and output schemas.
- Sandbox policy.
- Review status.
- Replay metadata.

Generated code must not be regenerated during an approved run. Regeneration creates a new draft revision.

## Anthropic Agent SDK Usage

NanoClaw uses the Anthropic Agent SDK for in-node LLM calls and code generation where required.

Allowed SDK roles:

- Generate code for a draft code-gen node.
- Repair generated code during draft validation.
- Execute LLM-backed node behavior when that behavior is explicitly declared.

Disallowed SDK roles:

- Replanning the whole workflow during execution.
- Modifying an approved DAG mid-run.
- Adding undeclared tools, secrets, or network access.
- Silently changing generated code during replay.

## Planner Backend Upgrade

Phase 3 uses a deterministic mocked planner so OpenClaw can ship the visual planning workflow before live planner infrastructure exists.

Phase 5 replaces that mock planner path with a real draft-time planner backend. The planner must:

- Use skill registry metadata, metaprompts, adapter capabilities, and code-gen fallback policy.
- Prefer existing deterministic skills before creating code-gen nodes.
- Emit workflow-spec compliant draft JSON through the shared API contracts.
- Include planner rationale for skill reuse, code-gen fallback, and required review.
- Persist generated code artifacts and dependency manifests before approval.
- Never mutate approved revisions or replan during NanoClaw execution.

## Sandbox Execution

Code-gen nodes run in Docker with:

- No host filesystem access beyond declared mounts.
- No network access unless explicitly approved.
- Pinned dependencies.
- Resource limits.
- Structured inputs and outputs.
- Captured logs and artifacts.

Generated source and dependency manifests are stored with content hashes. NanoClaw verifies hashes before execution.

## Promotion To Skill

A code-gen node can become a reusable skill when:

- It has stable input and output schemas.
- It has passing fixture tests.
- It has no workflow-specific secrets or hardcoded identifiers.
- It has documented adapter dependencies.
- It has deterministic replay behavior.

Promotion adds a new skill registry record and replaces future matching code-gen needs with the promoted skill.

## Implementation Checkpoints

1. Define code-gen node schema in `workflow-spec`.
2. Implement skill lookup fallback that emits code-gen node requirements.
3. Implement generated artifact storage with content hashes.
4. Implement draft-time code generation and repair loop.
5. Implement Docker execution for generated code.
6. Implement generated dependency installation policy.
7. Implement code review status and approval blocking.
8. Implement promotion flow from code-gen node to reusable skill.
9. Replace the mocked `POST /api/workflows/plan` implementation with the live draft-time planner backend.

## Tests

- Unit tests for code-gen node schema validation.
- Fixture tests for generated scraper, regex parser, and ad-hoc API call nodes.
- Hash verification tests that fail when generated code changes after approval.
- Docker sandbox tests for network and filesystem restrictions.
- Replay tests proving approved generated code is reused exactly.
- Promotion tests showing a reusable skill replaces future matching code-gen nodes.
- Planner tests proving the live planner prefers skills, falls back to code-gen only when needed, and emits valid workflow-spec drafts.

## Acceptance Criteria

- Code-gen nodes are explicit, auditable workflow nodes.
- Approved runs execute persisted generated code, not fresh generated code.
- Generated code runs in a container with declared limits.
- Code-gen nodes can be promoted to skills through a tested path.
- NanoClaw never uses code generation to mutate an approved workflow mid-run.
- OpenClaw planning no longer depends on the Phase 3 deterministic mock planner.
