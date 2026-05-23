import type { AgentStepSourceAgent } from "@kelpclaw/workflow-spec";
import type { TrajectoryRun, TrajectoryStep } from "./trajectory-synth.js";

export const crossAgentReplaySkillMdFixture = `---
name: kelpclaw-replay-smoke
description: Replays a deterministic two-step coding-agent tool sequence.
---

# KelpClaw Replay Smoke

Run the captured shell command, read the captured output file, and return the same result across agents.
`;

export function createCrossAgentReplayRuns(): readonly TrajectoryRun[] {
  return (["claude-code", "codex-cli", "goose"] as const).map((sourceAgent) =>
    createCrossAgentReplayRun(sourceAgent)
  );
}

export function createCrossAgentReplayRun(sourceAgent: AgentStepSourceAgent): TrajectoryRun {
  return {
    id: `agent-run.cross-agent.${sourceAgent}`,
    sourceAgent,
    sessionId: `session.cross-agent.${sourceAgent}`,
    title: "KelpClaw replay smoke",
    events: crossAgentReplayEvents(sourceAgent)
  };
}

export function trajectoryReplayShape(run: TrajectoryRun) {
  return {
    eventCount: run.events.length,
    tools: run.events.map((event) => event.toolName),
    statuses: run.events.map((event) => event.status),
    outputs: run.events.map((event) => event.result ?? null),
    hashChainShape: run.events.map((event) => ({
      chainIndex: event.chainIndex,
      hasContentHash: event.contentHash.startsWith("sha256:"),
      hasPrevEventHash: event.prevEventHash.startsWith("sha256:")
    }))
  };
}

function crossAgentReplayEvents(sourceAgent: AgentStepSourceAgent): readonly TrajectoryStep[] {
  return [
    {
      sourceAgent,
      sessionId: `session.cross-agent.${sourceAgent}`,
      hookEvent: "PostToolUse",
      toolName: "Bash",
      toolUseId: `toolu.${sourceAgent}.write`,
      args: { command: 'printf "kelpclaw\\n" > .kelpclaw-replay-smoke.txt' },
      result: { stdout: "", stderr: "", exitCode: 0 },
      status: "succeeded",
      contentHash: `sha256:${"1".repeat(64)}`,
      prevEventHash: `sha256:${"0".repeat(64)}`,
      chainIndex: 0,
      startedAt: "2026-05-23T00:00:00.000Z",
      finishedAt: "2026-05-23T00:00:00.100Z"
    },
    {
      sourceAgent,
      sessionId: `session.cross-agent.${sourceAgent}`,
      hookEvent: "PostToolUse",
      toolName: "Read",
      toolUseId: `toolu.${sourceAgent}.read`,
      args: { filePath: ".kelpclaw-replay-smoke.txt" },
      result: { content: "kelpclaw\n" },
      status: "succeeded",
      contentHash: `sha256:${"2".repeat(64)}`,
      prevEventHash: `sha256:${"1".repeat(64)}`,
      chainIndex: 1,
      startedAt: "2026-05-23T00:00:01.000Z",
      finishedAt: "2026-05-23T00:00:01.100Z"
    }
  ];
}
