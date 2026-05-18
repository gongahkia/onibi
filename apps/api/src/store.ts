import { createHash } from "node:crypto";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import {
  createWorkflowSpecDiff,
  stableWorkflowStringify,
  validateWorkflowSpec,
  workflowSchemaVersion
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowApprovalRecord,
  WorkflowApprovedRevision,
  WorkflowDraftRevision,
  WorkflowDraftRevisionSource,
  WorkflowRunRecord,
  WorkflowSpec,
  WorkflowValidationResult
} from "@kelpclaw/workflow-spec";

export interface StoredWorkflow {
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
  readonly createdAt: string;
  readonly draftRevisions: readonly WorkflowDraftRevision[];
  readonly approvedRevisions: readonly WorkflowApprovedRevision[];
  readonly latestApprovedRevisionId: string | null;
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

interface WorkflowAggregate {
  workflow: WorkflowSpec;
  validation: WorkflowValidationResult;
  createdAt: string;
  latestDraftRevisionId: string | null;
  latestApprovedRevisionId: string | null;
  draftRevisionIds: string[];
  approvedRevisionIds: string[];
}

export class InMemoryWorkflowStore {
  private readonly workflows = new Map<string, WorkflowAggregate>();
  private readonly draftRevisions = new Map<string, WorkflowDraftRevision>();
  private readonly approvedRevisions = new Map<string, WorkflowApprovedRevision>();
  private readonly executions = new Map<string, StoredExecution>();
  private readonly runs = new Map<string, WorkflowRunRecord>();

  public saveWorkflow(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult
  ): StoredWorkflow {
    const draft = this.saveDraftRevision(workflow, validation, "revision", {
      force: true,
      preserveRevision: true
    });

    return this.requireWorkflow(draft.workflowId);
  }

  public saveDraftRevision(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult,
    source: WorkflowDraftRevisionSource,
    options: { readonly force?: boolean; readonly preserveRevision?: boolean } = {}
  ): WorkflowDraftRevision {
    if (!validation.ok) {
      throw new Error("Cannot save an invalid workflow draft revision.");
    }

    const existing = this.workflows.get(workflow.id);
    const latestDraft = existing?.latestDraftRevisionId
      ? this.draftRevisions.get(existing.latestDraftRevisionId)
      : undefined;
    if (
      !options.force &&
      latestDraft &&
      draftFingerprint(latestDraft.workflow) === draftFingerprint(workflow)
    ) {
      return latestDraft;
    }

    const now = new Date().toISOString();
    const revision =
      options.preserveRevision || !existing ? workflow.revision : existing.workflow.revision + 1;
    const draftWorkflow: WorkflowSpec = {
      ...workflow,
      id: workflow.id,
      schemaVersion: workflowSchemaVersion,
      revision,
      approval: null,
      createdAt: existing?.workflow.createdAt ?? workflow.createdAt,
      updatedAt: now
    };
    const draftValidation = validateWorkflowSpec(draftWorkflow);
    if (!draftValidation.ok) {
      throw new Error(draftValidation.errors.map((error) => error.code).join(", "));
    }

    const draftRevision: WorkflowDraftRevision = {
      id: `draft.${draftWorkflow.id}.r${draftWorkflow.revision}.${existing?.draftRevisionIds.length ?? 0}`,
      workflowId: draftWorkflow.id,
      revision: draftWorkflow.revision,
      workflow: draftWorkflow,
      validation: draftValidation,
      source,
      createdAt: now
    };

    this.draftRevisions.set(draftRevision.id, draftRevision);

    const aggregate: WorkflowAggregate = existing ?? {
      workflow: draftWorkflow,
      validation: draftValidation,
      createdAt: now,
      latestDraftRevisionId: null,
      latestApprovedRevisionId: null,
      draftRevisionIds: [],
      approvedRevisionIds: []
    };
    aggregate.workflow = draftWorkflow;
    aggregate.validation = draftValidation;
    aggregate.latestDraftRevisionId = draftRevision.id;
    aggregate.draftRevisionIds = [...aggregate.draftRevisionIds, draftRevision.id];
    this.workflows.set(draftWorkflow.id, aggregate);

    return draftRevision;
  }

  public getWorkflow(id: string): StoredWorkflow | undefined {
    const aggregate = this.workflows.get(id);
    if (!aggregate) {
      return undefined;
    }

    return this.toStoredWorkflow(aggregate);
  }

  public approveWorkflow(
    workflowId: string,
    approvedBy: string,
    workflowOverride?: WorkflowSpec
  ): WorkflowApprovedRevision {
    const stored = this.requireWorkflow(workflowId);
    const workflow = workflowOverride ?? stored.workflow;
    const validation = validateWorkflowSpec(workflow);
    if (!validation.ok) {
      throw new Error(validation.errors.map((error) => error.code).join(", "));
    }

    const latestDraft = this.saveDraftRevision(
      validation.workflow,
      validation,
      "validate"
    ).workflow;
    const approval: WorkflowApprovalRecord = {
      status: "approved",
      approvedBy,
      approvedAt: new Date().toISOString(),
      frozenRevision: latestDraft.revision,
      frozenDagHash: hashWorkflowDag(latestDraft),
      nodeOrder: calculateNodeOrder(latestDraft)
    };
    const approvedWorkflow: WorkflowSpec = {
      ...latestDraft,
      approval,
      updatedAt: approval.approvedAt
    };
    const approvedValidation = validateWorkflowSpec(approvedWorkflow);
    if (!approvedValidation.ok) {
      throw new Error(approvedValidation.errors.map((error) => error.code).join(", "));
    }

    const approvedRevision: WorkflowApprovedRevision = {
      id: `approved.${approvedWorkflow.id}.r${approvedWorkflow.revision}`,
      workflowId: approvedWorkflow.id,
      revision: approvedWorkflow.revision,
      approvedBy,
      createdAt: approval.approvedAt,
      workflow: approvedWorkflow,
      draftSpecJson: stableWorkflowStringify(latestDraft),
      frozenSpecJson: stableWorkflowStringify(approvedWorkflow),
      diff: createWorkflowSpecDiff(latestDraft, approvedWorkflow)
    };

    this.approvedRevisions.set(approvedRevision.id, approvedRevision);
    const aggregate = this.workflows.get(approvedWorkflow.id);
    if (!aggregate) {
      throw new Error(`Unknown workflow '${workflowId}'.`);
    }
    aggregate.workflow = approvedWorkflow;
    aggregate.validation = approvedValidation;
    aggregate.latestApprovedRevisionId = approvedRevision.id;
    if (!aggregate.approvedRevisionIds.includes(approvedRevision.id)) {
      aggregate.approvedRevisionIds = [...aggregate.approvedRevisionIds, approvedRevision.id];
    }
    this.workflows.set(approvedWorkflow.id, aggregate);

    return approvedRevision;
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

    this.saveDraftRevision(revision, validation, "revision", {
      force: true,
      preserveRevision: true
    });
    return this.requireWorkflow(workflowId);
  }

  public getDraftRevision(id: string): WorkflowDraftRevision | undefined {
    return this.draftRevisions.get(id);
  }

  public getLatestDraftRevision(workflowId: string): WorkflowDraftRevision | undefined {
    const aggregate = this.workflows.get(workflowId);
    return aggregate?.latestDraftRevisionId
      ? this.draftRevisions.get(aggregate.latestDraftRevisionId)
      : undefined;
  }

  public getApprovedRevision(id: string): WorkflowApprovedRevision | undefined {
    return this.approvedRevisions.get(id);
  }

  public getLatestApprovedRevision(workflowId: string): WorkflowApprovedRevision | undefined {
    const aggregate = this.workflows.get(workflowId);
    return aggregate?.latestApprovedRevisionId
      ? this.approvedRevisions.get(aggregate.latestApprovedRevisionId)
      : undefined;
  }

  public saveExecution(execution: StoredExecution): StoredExecution {
    this.executions.set(execution.id, execution);
    return execution;
  }

  public getExecution(id: string): StoredExecution | undefined {
    return this.executions.get(id);
  }

  public saveRun(run: WorkflowRunRecord): WorkflowRunRecord {
    this.runs.set(run.id, run);
    return run;
  }

  public getRun(id: string): WorkflowRunRecord | undefined {
    return this.runs.get(id);
  }

  public requireWorkflow(id: string): StoredWorkflow {
    const aggregate = this.workflows.get(id);
    if (!aggregate) {
      throw new Error(`Unknown workflow '${id}'.`);
    }

    return this.toStoredWorkflow(aggregate);
  }

  private toStoredWorkflow(aggregate: WorkflowAggregate): StoredWorkflow {
    return {
      workflow: aggregate.workflow,
      validation: aggregate.validation,
      createdAt: aggregate.createdAt,
      draftRevisions: aggregate.draftRevisionIds
        .map((revisionId) => this.draftRevisions.get(revisionId))
        .filter((revision): revision is WorkflowDraftRevision => revision !== undefined),
      approvedRevisions: aggregate.approvedRevisionIds
        .map((revisionId) => this.approvedRevisions.get(revisionId))
        .filter((revision): revision is WorkflowApprovedRevision => revision !== undefined),
      latestApprovedRevisionId: aggregate.latestApprovedRevisionId
    };
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

function draftFingerprint(workflow: WorkflowSpec): string {
  return stableWorkflowStringify({
    ...workflow,
    revision: 1,
    approval: null,
    updatedAt: workflow.createdAt
  });
}
