# KelpClaw Agent Runtime Demo

Use this script for the v0.1 governance demo: Claude Code records a tool call, KelpClaw gates one risky step, a reviewer approves, the trajectory promotes into a skill, and the same audit evidence survives restart.

## Three-Minute Script

| Time | Action                                                                                                                    | Proof To Show                                                                                                                 |
| ---- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| 0:00 | Start OpenClaw in trajectory mode and run Claude Code with the KelpClaw hook installed.                                   | A live `claude-code` run appears with `PreToolUse` and `PostToolUse` agent-step cards.                                        |
| 0:30 | Trigger a safe `Bash` command.                                                                                            | The card shows tool name, args/result, `chainIndex`, `contentHash`, and `prevEventHash`.                                      |
| 1:00 | Trigger `Bash` with `rm -rf /tmp/demo` under the deny policy.                                                             | API returns `POLICY_DENIED`; OpenClaw shows a denied event and `policy.denied` audit record.                                  |
| 1:30 | Trigger an email-send policy with `require-approval`.                                                                     | The event is pending; operator promotion returns `POLICY_APPROVAL_REQUIRED`.                                                  |
| 2:00 | Switch to a reviewer token and approve the pending step.                                                                  | OpenClaw shows `policy.approved`; promotion becomes available.                                                                |
| 2:30 | Promote the trajectory.                                                                                                   | Response includes promoted skill JSON, draft workflow, `bom.json`, artifact checksums, and OTLP export status.                |
| 2:50 | Restart the API against the same SQLite DB and run `kelp-claw audit-verify <runId>` plus `kelp-claw tbom-export <runId>`. | Verification returns `{ "valid": true }`; TBOM still includes tools, domains, secrets, classifications, and audit chain head. |

Say this exactly in the demo: KelpClaw is evidence-ready for governance review. Its hash chain is tamper-evident, not tamper-proof. It helps assemble audit evidence; it does not certify compliance.

## Acceptance Matrix

| #   | Criterion                                                     | Local Evidence                                                                                                                      |
| --- | ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Claude Code hook records agent steps with hash links.         | `packages/agent-hooks/test/agent-hooks.test.ts`; real local install must be re-run on each demo machine.                            |
| 2   | OpenClaw streams trajectory cards.                            | `apps/openclaw/test/openclaw.test.tsx` plus local `http://127.0.0.1:5173` smoke.                                                    |
| 3   | Deny policy blocks `rm -rf` and records audit.                | `apps/api/test/api.test.ts` policy denial test.                                                                                     |
| 4   | Reviewer token can promote; operator cannot.                  | `apps/api/test/api.test.ts` promotion RBAC test.                                                                                    |
| 5   | Promotion creates skill JSON, workflow JSON, and `bom.json`.  | `apps/api/test/api.test.ts` promotion artifact assertions.                                                                          |
| 6   | Promoted skill is selectable by registry lookup.              | `apps/api/test/api.test.ts` and `packages/skill-registry/test/skill-registry.test.ts`.                                              |
| 7   | MCP sidecar exposes `kelp.*` tools.                           | `apps/mcp-server/src/index.ts`; run a local stdio smoke before recording.                                                           |
| 8   | CLI audit verify returns valid or broken index.               | `packages/cli/src/index.ts`; API test covers the backing endpoint.                                                                  |
| 9   | OTLP export emits one trace and one span per tool call.       | `packages/adapters/test/adapters.test.ts` and `apps/api/test/api.test.ts`; Datadog requires `DD_API_KEY` and an OTLP test endpoint. |
| 10  | Cross-agent replay has same shape with different source tags. | `packages/codegen/test/codegen.test.ts` cross-agent fixture test.                                                                   |
| 11  | Full verification is green.                                   | `pnpm verify`.                                                                                                                      |

## SQLite Restart Smoke

The API test `keeps agent-run audit verification and TBOM export working after SQLite restart` records an agent run with `SqliteAgentRunStore`, promotes it into content-addressed artifacts, closes the API, reopens against the same database, then verifies:

- the run and `trajectory.promoted` audit event rehydrate;
- `/api/agent-runs/:id/audit/verify` returns `{ "valid": true }`;
- `/api/agent-runs/:id/tbom` still returns tools, external domains, consumed secret refs, classifications, and source agent;
- the stored `bom.json` artifact checksum still verifies.
