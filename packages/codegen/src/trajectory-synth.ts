import {
  createWorkflowEdge,
  createWorkflowNode,
  createWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type {
  AgentStepClassification,
  AgentStepSourceAgent,
  AgentStepStatus,
  JsonRecord,
  JsonValue,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";

export interface TrajectoryStep {
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId: string;
  readonly parentToolUseId?: string | undefined;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: AgentStepStatus;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
  readonly classification?: AgentStepClassification | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
}

export interface TrajectoryRun {
  readonly id: string;
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly title?: string | undefined;
  readonly events: readonly TrajectoryStep[];
}

export interface TrajectorySynthesisOptions {
  readonly workflowId?: string | undefined;
  readonly name?: string | undefined;
  readonly prompt?: string | undefined;
  readonly createdAt?: string | undefined;
}

export function synthesizeWorkflowFromTrajectory(
  run: TrajectoryRun,
  options: TrajectorySynthesisOptions = {}
): WorkflowSpec {
  const createdAt = options.createdAt ?? new Date().toISOString();
  const groups = collapseConsecutiveSameToolCalls(run.events);
  const trigger = createWorkflowNode({
    id: "trajectory-trigger",
    kind: "trigger",
    label: "Trajectory Trigger",
    description: `Starts replay for recorded run '${run.id}'.`,
    outputs: { request: { type: "object", additionalProperties: true } },
    config: {
      trigger: "trajectory",
      sourceAgent: run.sourceAgent,
      sessionId: run.sessionId,
      runId: run.id
    }
  });
  const agentStepNodes = groups.map((group, index) => {
    const representative = group.at(-1)!;
    return createWorkflowNode({
      id: `agent-step-${String(index + 1).padStart(3, "0")}`,
      kind: "agent-step",
      label: representative.toolName,
      description:
        group.length === 1
          ? `Captured ${representative.hookEvent} tool call.`
          : `Captured ${group.length} consecutive ${representative.toolName} calls.`,
      inputs: { previous: { type: "object", additionalProperties: true } },
      outputs: { result: { type: "object", additionalProperties: true } },
      config: {
        toolName: representative.toolName,
        callCount: group.length,
        toolUseIds: group.map((step) => step.toolUseId)
      },
      agentStep: {
        sourceAgent: representative.sourceAgent,
        sessionId: representative.sessionId,
        hookEvent: representative.hookEvent,
        toolName: representative.toolName,
        toolUseId: representative.toolUseId,
        ...(representative.parentToolUseId
          ? { parentToolUseId: representative.parentToolUseId }
          : {}),
        args:
          group.length === 1
            ? representative.args
            : {
                calls: group.map((step) => ({
                  toolUseId: step.toolUseId,
                  args: step.args
                }))
              },
        ...(representative.result !== undefined
          ? {
              result:
                group.length === 1
                  ? representative.result
                  : {
                      calls: group.map((step) => ({
                        toolUseId: step.toolUseId,
                        result: step.result ?? null
                      }))
                    }
            }
          : {}),
        status: representative.status,
        contentHash: representative.contentHash,
        prevEventHash: representative.prevEventHash,
        chainIndex: representative.chainIndex,
        ...(representative.classification ? { classification: representative.classification } : {}),
        startedAt: group[0]?.startedAt ?? representative.startedAt,
        ...(representative.finishedAt ? { finishedAt: representative.finishedAt } : {})
      }
    });
  });
  const delivery = createWorkflowNode({
    id: "trajectory-delivery",
    kind: "delivery",
    label: "Trajectory Output",
    description: "Returns the final captured trajectory result.",
    inputs: { rows: { type: "object", additionalProperties: true } },
    outputs: { delivery: { type: "object", additionalProperties: true } },
    config: {
      channel: "trajectory",
      channels: ["trajectory"]
    }
  });
  const nodes = [trigger, ...agentStepNodes, delivery];

  return createWorkflowSpec({
    id: options.workflowId ?? `workflow.trajectory.${sanitizeId(run.id)}`,
    name: options.name ?? run.title ?? `Trajectory ${run.id}`,
    prompt: options.prompt ?? `Replay recorded agent run ${run.id}.`,
    createdAt,
    updatedAt: createdAt,
    nodes,
    edges: nodes.slice(0, -1).map((node, index) => {
      const next = nodes[index + 1]!;
      return createWorkflowEdge({
        sourceNodeId: node.id,
        sourcePort: index === 0 ? "request" : "result",
        targetNodeId: next.id,
        targetPort: next.kind === "delivery" ? "rows" : "previous"
      });
    })
  });
}

function collapseConsecutiveSameToolCalls(
  events: readonly TrajectoryStep[]
): readonly (readonly TrajectoryStep[])[] {
  const groups: TrajectoryStep[][] = [];
  for (const event of [...events].sort((left, right) => left.chainIndex - right.chainIndex)) {
    const previous = groups.at(-1);
    if (previous?.at(-1)?.toolName === event.toolName) {
      previous.push(event);
    } else {
      groups.push([event]);
    }
  }
  return groups;
}

function sanitizeId(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/gu, "-")
    .replace(/^-+|-+$/gu, "")
    .slice(0, 64);
}
