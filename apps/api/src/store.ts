import { createHash } from "node:crypto";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import {
  stableWorkflowStringify,
  validateWorkflowSpec,
  workflowSchemaVersion
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowApprovalRecord,
  WorkflowSpec,
  WorkflowValidationResult
} from "@kelpclaw/workflow-spec";

export interface StoredWorkflow {
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
  readonly createdAt: string;
}

export interface StoredExecution {
  readonly id: string;
  readonly workflowId: string;
  readonly revision: number;
  readonly createdAt: string;
  readonly result: DagExecutionResult;
}

export interface RevisionInput {
  readonly name?: string | undefined;
  readonly prompt?: string | undefined;
  readonly workflow?: WorkflowSpec | undefined;
}

export class InMemoryWorkflowStore {
  private readonly workflows = new Map<string, StoredWorkflow>();
  private readonly executions = new Map<string, StoredExecution>();

  public saveWorkflow(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult
  ): StoredWorkflow {
    const stored = {
      workflow,
      validation,
      createdAt: new Date().toISOString()
    };

    this.workflows.set(workflow.id, stored);
    return stored;
  }

  public getWorkflow(id: string): StoredWorkflow | undefined {
    return this.workflows.get(id);
  }

  public approveWorkflow(workflowId: string, approvedBy: string): StoredWorkflow {
    const stored = this.requireWorkflow(workflowId);
    const workflow = stored.workflow;
    const approval: WorkflowApprovalRecord = {
      status: "approved",
      approvedBy,
      approvedAt: new Date().toISOString(),
      frozenRevision: workflow.revision,
      frozenDagHash: hashWorkflowDag(workflow),
      nodeOrder: calculateNodeOrder(workflow)
    };
    const approvedWorkflow = {
      ...workflow,
      approval,
      updatedAt: approval.approvedAt
    };
    const validation = validateWorkflowSpec(approvedWorkflow);
    const updated = {
      ...stored,
      workflow: approvedWorkflow,
      validation
    };

    this.workflows.set(workflowId, updated);
    return updated;
  }

  public createRevision(workflowId: string, input: RevisionInput = {}): StoredWorkflow {
    const stored = this.requireWorkflow(workflowId);
    const source = input.workflow ?? stored.workflow;
    const now = new Date().toISOString();
    const revision: WorkflowSpec = {
      ...source,
      id: workflowId,
      schemaVersion: workflowSchemaVersion,
      name: input.name ?? source.name,
      prompt: input.prompt ?? source.prompt,
      revision: stored.workflow.revision + 1,
      approval: null,
      createdAt: stored.workflow.createdAt,
      updatedAt: now
    };
    const validation = validateWorkflowSpec(revision);
    if (!validation.ok) {
      throw new Error(validation.errors.map((error) => error.code).join(", "));
    }

    return this.saveWorkflow(revision, validation);
  }

  public saveExecution(execution: StoredExecution): StoredExecution {
    this.executions.set(execution.id, execution);
    return execution;
  }

  public getExecution(id: string): StoredExecution | undefined {
    return this.executions.get(id);
  }

  public requireWorkflow(id: string): StoredWorkflow {
    const stored = this.workflows.get(id);
    if (!stored) {
      throw new Error(`Unknown workflow '${id}'.`);
    }

    return stored;
  }
}

export function hashWorkflowDag(workflow: WorkflowSpec): string {
  return `sha256:${createHash("sha256")
    .update(stableWorkflowStringify({ ...workflow, approval: null }), "utf8")
    .digest("hex")}`;
}

export function calculateNodeOrder(workflow: WorkflowSpec): readonly string[] {
  const validation = validateWorkflowSpec(workflow);
  if (!validation.ok) {
    throw new Error(validation.errors.map((error) => error.code).join(", "));
  }

  const indegrees = new Map(workflow.nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(workflow.nodes.map((node) => [node.id, [] as string[]]));
  for (const edge of workflow.edges) {
    outgoing.get(edge.source.nodeId)?.push(edge.target.nodeId);
    indegrees.set(edge.target.nodeId, (indegrees.get(edge.target.nodeId) ?? 0) + 1);
  }

  const ready = [...indegrees.entries()]
    .filter(([, indegree]) => indegree === 0)
    .map(([nodeId]) => nodeId)
    .sort();
  const order: string[] = [];

  while (ready.length > 0) {
    const nodeId = ready.shift();
    if (nodeId === undefined) {
      break;
    }

    order.push(nodeId);
    for (const target of outgoing.get(nodeId) ?? []) {
      const nextIndegree = (indegrees.get(target) ?? 0) - 1;
      indegrees.set(target, nextIndegree);
      if (nextIndegree === 0) {
        ready.push(target);
        ready.sort();
      }
    }
  }

  return order;
}
