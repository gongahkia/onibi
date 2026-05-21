import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { hashWorkflowDag as hashNanoClawWorkflowDag } from "@kelpclaw/nanoclaw";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import type { CodegenAgentArtifactRecord, CodegenAgentRunRecord } from "@kelpclaw/codegen";
import {
  createWorkflowSpecDiff,
  stableJsonStringify,
  stableWorkflowStringify,
  validateWorkflowSpec,
  workflowSchemaVersion
} from "@kelpclaw/workflow-spec";
import type {
  GeneratedNodeEvalReport,
  GeneratedNodeTestReport,
  WorkflowDeploymentRecord,
  WorkflowArtifactManifestRecord,
  WorkflowApprovalRecord,
  WorkflowApprovedRevision,
  WorkflowAuditRecord,
  WorkflowAgentTimelineEvent,
  WorkflowBudgetLedger,
  WorkflowBudgetPolicy,
  WorkflowBranch,
  WorkflowBranchMergeRecord,
  WorkflowDraftEvaluation,
  WorkflowDraftRevision,
  WorkflowDraftRevisionSource,
  WorkflowGraphDiff,
  WorkflowGeneratedModuleReuseDecision,
  WorkflowJob,
  WorkflowJobEvent,
  WorkflowNodeDecisionTrace,
  WorkflowPlannerFeedback,
  WorkflowPromptTurn,
  WorkflowRunRecord,
  WorkflowRunEvent,
  WorkflowSpec,
  WorkflowValidationResult,
  WorkflowWorkspace
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

export interface WorkflowRevisionLookup {
  readonly draftRevision?: WorkflowDraftRevision | undefined;
  readonly approvedRevision?: WorkflowApprovedRevision | undefined;
}

export interface WorkflowStore {
  saveWorkflow(workflow: WorkflowSpec, validation: WorkflowValidationResult): StoredWorkflow;
  saveDraftRevision(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult,
    source: WorkflowDraftRevisionSource,
    options?: SaveDraftRevisionOptions
  ): WorkflowDraftRevision;
  saveBranch(record: WorkflowBranch): WorkflowBranch;
  getBranch(id: string): WorkflowBranch | undefined;
  getDefaultBranch(workflowId: string): WorkflowBranch;
  listBranches(workflowId: string): readonly WorkflowBranch[];
  savePromptTurn(record: WorkflowPromptTurn): WorkflowPromptTurn;
  listPromptTurns(workflowId: string, branchId?: string | undefined): readonly WorkflowPromptTurn[];
  saveBranchMerge(record: WorkflowBranchMergeRecord): WorkflowBranchMergeRecord;
  listBranchMerges(workflowId: string): readonly WorkflowBranchMergeRecord[];
  saveGeneratedModuleReuseDecision(
    record: WorkflowGeneratedModuleReuseDecision
  ): WorkflowGeneratedModuleReuseDecision;
  listGeneratedModuleReuseDecisions(
    workflowId: string,
    branchId?: string | undefined
  ): readonly WorkflowGeneratedModuleReuseDecision[];
  getWorkflow(id: string): StoredWorkflow | undefined;
  approveWorkflow(
    workflowId: string,
    approvedBy: string,
    workflowOverride?: WorkflowSpec,
    branchId?: string | undefined
  ): WorkflowApprovedRevision;
  createRevision(workflowId: string, input?: RevisionInput): StoredWorkflow;
  getDraftRevision(id: string): WorkflowDraftRevision | undefined;
  getLatestDraftRevision(workflowId: string): WorkflowDraftRevision | undefined;
  getApprovedRevision(id: string): WorkflowApprovedRevision | undefined;
  getLatestApprovedRevision(workflowId: string): WorkflowApprovedRevision | undefined;
  getWorkflowRevision(id: string): WorkflowRevisionLookup | undefined;
  saveExecution(execution: StoredExecution): StoredExecution;
  getExecution(id: string): StoredExecution | undefined;
  saveRun(run: WorkflowRunRecord): WorkflowRunRecord;
  getRun(id: string): WorkflowRunRecord | undefined;
  listRunEvents(runId: string): readonly WorkflowRunEvent[];
  saveAuditRecord(record: WorkflowAuditRecord): WorkflowAuditRecord;
  listAuditRecords(workflowId: string): readonly WorkflowAuditRecord[];
  saveArtifactManifest(record: WorkflowArtifactManifestRecord): WorkflowArtifactManifestRecord;
  getArtifactManifest(id: string): WorkflowArtifactManifestRecord | undefined;
  saveGraphDiff(record: WorkflowGraphDiff): WorkflowGraphDiff;
  getGraphDiff(id: string): WorkflowGraphDiff | undefined;
  listGraphDiffs(workflowId: string): readonly WorkflowGraphDiff[];
  savePlannerFeedback(record: WorkflowPlannerFeedback): WorkflowPlannerFeedback;
  getPlannerFeedback(id: string): WorkflowPlannerFeedback | undefined;
  listPlannerFeedback(workflowId: string): readonly WorkflowPlannerFeedback[];
  saveJob(record: WorkflowJob): WorkflowJob;
  getJob(id: string): WorkflowJob | undefined;
  listJobs(workflowId?: string | undefined): readonly WorkflowJob[];
  claimNextQueuedJob(
    workerId: string,
    types?: readonly WorkflowJob["type"][] | undefined,
    claimedAt?: string | undefined
  ): WorkflowJob | undefined;
  appendJobEvent(jobId: string, event: WorkflowJobEvent): WorkflowJob;
  saveDraftEvaluation(record: WorkflowDraftEvaluation): WorkflowDraftEvaluation;
  getDraftEvaluation(id: string): WorkflowDraftEvaluation | undefined;
  getLatestDraftEvaluation(
    workflowId: string,
    branchId?: string | undefined
  ): WorkflowDraftEvaluation | undefined;
  listDraftEvaluations(
    workflowId: string,
    branchId?: string | undefined
  ): readonly WorkflowDraftEvaluation[];
  saveWorkspace(record: WorkflowWorkspace): WorkflowWorkspace;
  getWorkspace(id: string): WorkflowWorkspace | undefined;
  listWorkspaces(workflowId: string): readonly WorkflowWorkspace[];
  saveAgentRun(record: CodegenAgentRunRecord): CodegenAgentRunRecord;
  listAgentRuns(workflowId: string, nodeId?: string | undefined): readonly CodegenAgentRunRecord[];
  saveAgentArtifact(record: CodegenAgentArtifactRecord): CodegenAgentArtifactRecord;
  listAgentArtifacts(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly CodegenAgentArtifactRecord[];
  saveGeneratedNodeTestReport(record: GeneratedNodeTestReport): GeneratedNodeTestReport;
  listGeneratedNodeTestReports(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly GeneratedNodeTestReport[];
  saveGeneratedNodeEvalReport(record: GeneratedNodeEvalReport): GeneratedNodeEvalReport;
  listGeneratedNodeEvalReports(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly GeneratedNodeEvalReport[];
  saveDeployment(record: WorkflowDeploymentRecord): WorkflowDeploymentRecord;
  listDeployments(workflowId: string): readonly WorkflowDeploymentRecord[];
  saveBudgetPolicy(record: WorkflowBudgetPolicy): WorkflowBudgetPolicy;
  getBudgetPolicy(
    workflowId: string,
    branchId?: string | undefined
  ): WorkflowBudgetPolicy | undefined;
  saveBudgetLedger(record: WorkflowBudgetLedger): WorkflowBudgetLedger;
  listBudgetLedgers(workflowId: string): readonly WorkflowBudgetLedger[];
  saveAgentTimelineEvent(record: WorkflowAgentTimelineEvent): WorkflowAgentTimelineEvent;
  listAgentTimelineEvents(workflowId: string): readonly WorkflowAgentTimelineEvent[];
  saveNodeDecisionTrace(record: WorkflowNodeDecisionTrace): WorkflowNodeDecisionTrace;
  listNodeDecisionTraces(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly WorkflowNodeDecisionTrace[];
  requireWorkflow(id: string): StoredWorkflow;
}

export interface SaveDraftRevisionOptions {
  readonly force?: boolean;
  readonly preserveRevision?: boolean;
  readonly branchId?: string | undefined;
  readonly parentDraftRevisionId?: string | undefined;
  readonly updateBranchHead?: boolean | undefined;
}

export class InMemoryWorkflowStore implements WorkflowStore {
  protected readonly workflows = new Map<string, WorkflowAggregate>();
  protected readonly draftRevisions = new Map<string, WorkflowDraftRevision>();
  protected readonly approvedRevisions = new Map<string, WorkflowApprovedRevision>();
  protected readonly branches = new Map<string, WorkflowBranch>();
  protected readonly promptTurns = new Map<string, WorkflowPromptTurn>();
  protected readonly branchMerges = new Map<string, WorkflowBranchMergeRecord>();
  protected readonly generatedModuleReuseDecisions = new Map<
    string,
    WorkflowGeneratedModuleReuseDecision
  >();
  protected readonly executions = new Map<string, StoredExecution>();
  protected readonly runs = new Map<string, WorkflowRunRecord>();
  protected readonly audits = new Map<string, WorkflowAuditRecord>();
  protected readonly artifactManifests = new Map<string, WorkflowArtifactManifestRecord>();
  protected readonly graphDiffs = new Map<string, WorkflowGraphDiff>();
  protected readonly plannerFeedback = new Map<string, WorkflowPlannerFeedback>();
  protected readonly jobs = new Map<string, WorkflowJob>();
  protected readonly draftEvaluations = new Map<string, WorkflowDraftEvaluation>();
  protected readonly workspaces = new Map<string, WorkflowWorkspace>();
  protected readonly agentRuns = new Map<string, CodegenAgentRunRecord>();
  protected readonly agentArtifacts = new Map<string, CodegenAgentArtifactRecord>();
  protected readonly generatedNodeTestReports = new Map<string, GeneratedNodeTestReport>();
  protected readonly generatedNodeEvalReports = new Map<string, GeneratedNodeEvalReport>();
  protected readonly deployments = new Map<string, WorkflowDeploymentRecord>();
  protected readonly budgetPolicies = new Map<string, WorkflowBudgetPolicy>();
  protected readonly budgetLedgers = new Map<string, WorkflowBudgetLedger>();
  protected readonly agentTimelineEvents = new Map<string, WorkflowAgentTimelineEvent>();
  protected readonly nodeDecisionTraces = new Map<string, WorkflowNodeDecisionTrace>();

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
    options: SaveDraftRevisionOptions = {}
  ): WorkflowDraftRevision {
    if (!validation.ok) {
      throw new Error("Cannot save an invalid workflow draft revision.");
    }

    const existing = this.workflows.get(workflow.id);
    const branchId = options.branchId ?? defaultBranchId(workflow.id);
    const existingBranch = this.branches.get(branchId);
    const latestDraftId = existingBranch?.headDraftRevisionId ?? existing?.latestDraftRevisionId;
    const latestDraft = latestDraftId ? this.draftRevisions.get(latestDraftId) : undefined;
    if (
      !options.force &&
      latestDraft &&
      draftFingerprint(latestDraft.workflow) === draftFingerprint(workflow)
    ) {
      if (existingBranch && options.updateBranchHead !== false) {
        this.branches.set(existingBranch.id, {
          ...existingBranch,
          headDraftRevisionId: latestDraft.id,
          updatedAt: latestDraft.createdAt
        });
      }
      return latestDraft;
    }

    const now = new Date().toISOString();
    const revision =
      options.preserveRevision || !existing
        ? workflow.revision
        : (latestDraft?.workflow.revision ?? existing.workflow.revision) + 1;
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
      id: draftRevisionId(draftWorkflow.id, branchId, draftWorkflow.revision, existing),
      workflowId: draftWorkflow.id,
      branchId,
      parentDraftRevisionId: options.parentDraftRevisionId ?? existingBranch?.headDraftRevisionId,
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
    const updatesMainBranch = branchId === defaultBranchId(draftWorkflow.id);
    if (!existing || updatesMainBranch) {
      aggregate.workflow = draftWorkflow;
      aggregate.validation = draftValidation;
      aggregate.latestDraftRevisionId = draftRevision.id;
    }
    aggregate.draftRevisionIds = [...aggregate.draftRevisionIds, draftRevision.id];
    this.workflows.set(draftWorkflow.id, aggregate);
    const branch =
      existingBranch ??
      (branchId === defaultBranchId(draftWorkflow.id)
        ? this.createDefaultBranch(draftWorkflow, draftRevision, "system")
        : undefined);
    if (branch && options.updateBranchHead !== false) {
      this.branches.set(branch.id, {
        ...branch,
        headDraftRevisionId: draftRevision.id,
        updatedAt: now
      });
    }

    return draftRevision;
  }

  public saveBranch(record: WorkflowBranch): WorkflowBranch {
    this.branches.set(record.id, record);
    return record;
  }

  public getBranch(id: string): WorkflowBranch | undefined {
    return this.branches.get(id);
  }

  public getDefaultBranch(workflowId: string): WorkflowBranch {
    const branchId = defaultBranchId(workflowId);
    const existing = this.branches.get(branchId);
    if (existing) {
      return existing;
    }
    const latestDraft = this.getLatestDraftRevision(workflowId);
    if (!latestDraft) {
      throw new Error(`Workflow '${workflowId}' does not have a draft revision.`);
    }
    const branch = this.createDefaultBranch(latestDraft.workflow, latestDraft, "system");
    this.branches.set(branch.id, branch);
    return branch;
  }

  public listBranches(workflowId: string): readonly WorkflowBranch[] {
    if (this.workflows.has(workflowId) && !this.branches.has(defaultBranchId(workflowId))) {
      this.getDefaultBranch(workflowId);
    }
    return [...this.branches.values()]
      .filter((branch) => branch.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public savePromptTurn(record: WorkflowPromptTurn): WorkflowPromptTurn {
    this.promptTurns.set(record.id, record);
    return record;
  }

  public listPromptTurns(
    workflowId: string,
    branchId?: string | undefined
  ): readonly WorkflowPromptTurn[] {
    return [...this.promptTurns.values()]
      .filter(
        (turn) =>
          turn.workflowId === workflowId && (branchId === undefined || turn.branchId === branchId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveBranchMerge(record: WorkflowBranchMergeRecord): WorkflowBranchMergeRecord {
    this.branchMerges.set(record.id, record);
    return record;
  }

  public listBranchMerges(workflowId: string): readonly WorkflowBranchMergeRecord[] {
    return [...this.branchMerges.values()]
      .filter((merge) => merge.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveGeneratedModuleReuseDecision(
    record: WorkflowGeneratedModuleReuseDecision
  ): WorkflowGeneratedModuleReuseDecision {
    this.generatedModuleReuseDecisions.set(record.id, record);
    return record;
  }

  public listGeneratedModuleReuseDecisions(
    workflowId: string,
    branchId?: string | undefined
  ): readonly WorkflowGeneratedModuleReuseDecision[] {
    return [...this.generatedModuleReuseDecisions.values()]
      .filter(
        (decision) =>
          decision.workflowId === workflowId &&
          (branchId === undefined || decision.branchId === branchId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
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
    workflowOverride?: WorkflowSpec,
    branchId?: string | undefined
  ): WorkflowApprovedRevision {
    const stored = this.requireWorkflow(workflowId);
    const targetBranchId = branchId ?? defaultBranchId(workflowId);
    const branch = this.branches.get(targetBranchId);
    const branchHead = branch ? this.draftRevisions.get(branch.headDraftRevisionId) : undefined;
    const workflow = workflowOverride ?? branchHead?.workflow ?? stored.workflow;
    const validation = validateWorkflowSpec(workflow);
    if (!validation.ok) {
      throw new Error(validation.errors.map((error) => error.code).join(", "));
    }

    const latestDraftRevision = this.saveDraftRevision(
      validation.workflow,
      validation,
      "validate",
      {
        branchId: targetBranchId,
        parentDraftRevisionId: branchHead?.id
      }
    );
    const latestDraft = latestDraftRevision.workflow;
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

    const approvedBranchId =
      targetBranchId === defaultBranchId(approvedWorkflow.id) ? undefined : targetBranchId;
    const approvedRevision: WorkflowApprovedRevision = {
      id: approvedRevisionId(approvedWorkflow.id, targetBranchId, approvedWorkflow.revision),
      workflowId: approvedWorkflow.id,
      ...(approvedBranchId ? { branchId: approvedBranchId } : {}),
      revision: approvedWorkflow.revision,
      approvedBy,
      createdAt: approval.approvedAt,
      workflow: approvedWorkflow,
      draftSpecJson: stableWorkflowStringify(latestDraft),
      frozenSpecJson: stableWorkflowStringify(approvedWorkflow),
      diff: createWorkflowSpecDiff(latestDraft, approvedWorkflow)
    };

    const existingApproved = this.approvedRevisions.get(approvedRevision.id);
    if (existingApproved) {
      assertImmutableRecordUnchanged(
        "approved revision",
        approvedRevision.id,
        existingApproved,
        approvedRevision
      );
      return existingApproved;
    }

    this.approvedRevisions.set(approvedRevision.id, approvedRevision);
    const aggregate = this.workflows.get(approvedWorkflow.id);
    if (!aggregate) {
      throw new Error(`Unknown workflow '${workflowId}'.`);
    }
    if (targetBranchId === defaultBranchId(approvedWorkflow.id)) {
      aggregate.workflow = approvedWorkflow;
      aggregate.validation = approvedValidation;
      aggregate.latestApprovedRevisionId = approvedRevision.id;
    }
    if (!aggregate.approvedRevisionIds.includes(approvedRevision.id)) {
      aggregate.approvedRevisionIds = [...aggregate.approvedRevisionIds, approvedRevision.id];
    }
    this.workflows.set(approvedWorkflow.id, aggregate);
    const latestBranch = this.branches.get(targetBranchId);
    if (latestBranch) {
      this.branches.set(targetBranchId, {
        ...latestBranch,
        latestApprovedRevisionId: approvedRevision.id,
        updatedAt: approval.approvedAt
      });
    }

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

  public getWorkflowRevision(id: string): WorkflowRevisionLookup | undefined {
    const draftRevision = this.getDraftRevision(id);
    if (draftRevision) {
      return { draftRevision };
    }

    const approvedRevision = this.getApprovedRevision(id);
    return approvedRevision ? { approvedRevision } : undefined;
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

  public listRunEvents(runId: string): readonly WorkflowRunEvent[] {
    return this.runs.get(runId)?.events ?? [];
  }

  public saveAuditRecord(record: WorkflowAuditRecord): WorkflowAuditRecord {
    const existing = this.audits.get(record.id);
    if (existing) {
      assertImmutableRecordUnchanged("audit record", record.id, existing, record);
      return existing;
    }

    this.audits.set(record.id, record);
    return record;
  }

  public listAuditRecords(workflowId: string): readonly WorkflowAuditRecord[] {
    return [...this.audits.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id)
      );
  }

  public saveArtifactManifest(
    record: WorkflowArtifactManifestRecord
  ): WorkflowArtifactManifestRecord {
    const existing = this.artifactManifests.get(record.id);
    if (existing) {
      assertImmutableRecordUnchanged("artifact manifest", record.id, existing, record);
      return existing;
    }

    this.artifactManifests.set(record.id, record);
    return record;
  }

  public getArtifactManifest(id: string): WorkflowArtifactManifestRecord | undefined {
    return this.artifactManifests.get(id);
  }

  public saveGraphDiff(record: WorkflowGraphDiff): WorkflowGraphDiff {
    const existing = this.graphDiffs.get(record.id);
    if (existing) {
      assertImmutableRecordUnchanged("graph diff", record.id, existing, record);
      return existing;
    }

    this.graphDiffs.set(record.id, record);
    return record;
  }

  public getGraphDiff(id: string): WorkflowGraphDiff | undefined {
    return this.graphDiffs.get(id);
  }

  public listGraphDiffs(workflowId: string): readonly WorkflowGraphDiff[] {
    return [...this.graphDiffs.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public savePlannerFeedback(record: WorkflowPlannerFeedback): WorkflowPlannerFeedback {
    this.plannerFeedback.set(record.id, record);
    return record;
  }

  public getPlannerFeedback(id: string): WorkflowPlannerFeedback | undefined {
    return this.plannerFeedback.get(id);
  }

  public listPlannerFeedback(workflowId: string): readonly WorkflowPlannerFeedback[] {
    return [...this.plannerFeedback.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveJob(record: WorkflowJob): WorkflowJob {
    this.jobs.set(record.id, record);
    return record;
  }

  public getJob(id: string): WorkflowJob | undefined {
    return this.jobs.get(id);
  }

  public listJobs(workflowId?: string | undefined): readonly WorkflowJob[] {
    return [...this.jobs.values()]
      .filter((record) => workflowId === undefined || record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public claimNextQueuedJob(
    workerId: string,
    types?: readonly WorkflowJob["type"][] | undefined,
    claimedAt = new Date().toISOString()
  ): WorkflowJob | undefined {
    const allowedTypes = types ? new Set(types) : undefined;
    const job = this.listJobs().find(
      (record) => record.status === "queued" && (!allowedTypes || allowedTypes.has(record.type))
    );
    if (!job) {
      return undefined;
    }

    return this.saveJob({
      ...job,
      status: "running",
      startedAt: job.startedAt ?? claimedAt,
      claimedAt,
      workerId,
      updatedAt: claimedAt,
      retry: {
        ...job.retry,
        attempt: job.retry.attempt + 1
      }
    });
  }

  public appendJobEvent(jobId: string, event: WorkflowJobEvent): WorkflowJob {
    const job = this.jobs.get(jobId);
    if (!job) {
      throw new Error(`Unknown job '${jobId}'.`);
    }

    const updated: WorkflowJob = {
      ...job,
      updatedAt: event.timestamp,
      events: [...job.events, event]
    };
    this.jobs.set(jobId, updated);
    return updated;
  }

  public saveDraftEvaluation(record: WorkflowDraftEvaluation): WorkflowDraftEvaluation {
    this.draftEvaluations.set(record.id, record);
    return record;
  }

  public getDraftEvaluation(id: string): WorkflowDraftEvaluation | undefined {
    return this.draftEvaluations.get(id);
  }

  public getLatestDraftEvaluation(
    workflowId: string,
    branchId?: string | undefined
  ): WorkflowDraftEvaluation | undefined {
    return this.listDraftEvaluations(workflowId, branchId).at(-1);
  }

  public listDraftEvaluations(
    workflowId: string,
    branchId?: string | undefined
  ): readonly WorkflowDraftEvaluation[] {
    return [...this.draftEvaluations.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId &&
          (branchId === undefined || record.branchId === branchId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveWorkspace(record: WorkflowWorkspace): WorkflowWorkspace {
    this.workspaces.set(record.id, record);
    return record;
  }

  public getWorkspace(id: string): WorkflowWorkspace | undefined {
    return this.workspaces.get(id);
  }

  public listWorkspaces(workflowId: string): readonly WorkflowWorkspace[] {
    return [...this.workspaces.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveAgentRun(record: CodegenAgentRunRecord): CodegenAgentRunRecord {
    this.agentRuns.set(record.id, record);
    return record;
  }

  public listAgentRuns(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly CodegenAgentRunRecord[] {
    return [...this.agentRuns.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId)
      )
      .sort(
        (left, right) =>
          left.startedAt.localeCompare(right.startedAt) || left.id.localeCompare(right.id)
      );
  }

  public saveAgentArtifact(record: CodegenAgentArtifactRecord): CodegenAgentArtifactRecord {
    this.agentArtifacts.set(record.id, record);
    return record;
  }

  public listAgentArtifacts(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly CodegenAgentArtifactRecord[] {
    return [...this.agentArtifacts.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveGeneratedNodeTestReport(record: GeneratedNodeTestReport): GeneratedNodeTestReport {
    this.generatedNodeTestReports.set(record.id, record);
    return record;
  }

  public listGeneratedNodeTestReports(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly GeneratedNodeTestReport[] {
    return [...this.generatedNodeTestReports.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveGeneratedNodeEvalReport(record: GeneratedNodeEvalReport): GeneratedNodeEvalReport {
    this.generatedNodeEvalReports.set(record.id, record);
    return record;
  }

  public listGeneratedNodeEvalReports(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly GeneratedNodeEvalReport[] {
    return [...this.generatedNodeEvalReports.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveDeployment(record: WorkflowDeploymentRecord): WorkflowDeploymentRecord {
    this.deployments.set(record.id, record);
    return record;
  }

  public listDeployments(workflowId: string): readonly WorkflowDeploymentRecord[] {
    return [...this.deployments.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveBudgetPolicy(record: WorkflowBudgetPolicy): WorkflowBudgetPolicy {
    this.budgetPolicies.set(budgetPolicyKey(record.workflowId, record.branchId), record);
    return record;
  }

  public getBudgetPolicy(
    workflowId: string,
    branchId?: string | undefined
  ): WorkflowBudgetPolicy | undefined {
    return (
      this.budgetPolicies.get(budgetPolicyKey(workflowId, branchId)) ??
      this.budgetPolicies.get(budgetPolicyKey(workflowId, undefined))
    );
  }

  public saveBudgetLedger(record: WorkflowBudgetLedger): WorkflowBudgetLedger {
    this.budgetLedgers.set(record.id, record);
    return record;
  }

  public listBudgetLedgers(workflowId: string): readonly WorkflowBudgetLedger[] {
    return [...this.budgetLedgers.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public saveAgentTimelineEvent(record: WorkflowAgentTimelineEvent): WorkflowAgentTimelineEvent {
    this.agentTimelineEvents.set(record.id, record);
    return record;
  }

  public listAgentTimelineEvents(workflowId: string): readonly WorkflowAgentTimelineEvent[] {
    return [...this.agentTimelineEvents.values()]
      .filter((record) => record.workflowId === workflowId)
      .sort(
        (left, right) =>
          left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id)
      );
  }

  public saveNodeDecisionTrace(record: WorkflowNodeDecisionTrace): WorkflowNodeDecisionTrace {
    const existing = this.nodeDecisionTraces.get(record.id);
    if (existing) {
      assertImmutableRecordUnchanged("node decision trace", record.id, existing, record);
      return existing;
    }

    this.nodeDecisionTraces.set(record.id, record);
    return record;
  }

  public listNodeDecisionTraces(
    workflowId: string,
    nodeId?: string | undefined
  ): readonly WorkflowNodeDecisionTrace[] {
    return [...this.nodeDecisionTraces.values()]
      .filter(
        (record) =>
          record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId)
      )
      .sort(
        (left, right) =>
          left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id)
      );
  }

  public requireWorkflow(id: string): StoredWorkflow {
    const aggregate = this.workflows.get(id);
    if (!aggregate) {
      throw new Error(`Unknown workflow '${id}'.`);
    }

    return this.toStoredWorkflow(aggregate);
  }

  protected createDefaultBranch(
    workflow: WorkflowSpec,
    draftRevision: WorkflowDraftRevision,
    createdBy: string
  ): WorkflowBranch {
    const now = draftRevision.createdAt;
    return {
      id: defaultBranchId(workflow.id),
      workflowId: workflow.id,
      name: "main",
      status: "active",
      createdAt: now,
      updatedAt: now,
      createdBy,
      baseDraftRevisionId: draftRevision.id,
      headDraftRevisionId: draftRevision.id,
      metadata: {
        default: true
      }
    };
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

export interface SqliteWorkflowStoreOptions {
  readonly databasePath: string;
  readonly sqliteBin?: string | undefined;
}

export class SqliteWorkflowStore extends InMemoryWorkflowStore {
  private readonly databasePath: string;
  private readonly sqliteBin: string;

  public constructor(options: SqliteWorkflowStoreOptions) {
    super();
    this.databasePath = options.databasePath;
    this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
    mkdirSync(dirname(this.databasePath), { recursive: true });
    this.runSql(sqliteMigrations.join("\n"));
    this.hydrate();
  }

  public override saveWorkflow(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult
  ): StoredWorkflow {
    const stored = super.saveWorkflow(workflow, validation);
    this.persistAllWorkflowState(stored.workflow.id);
    return stored;
  }

  public override saveDraftRevision(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult,
    source: WorkflowDraftRevisionSource,
    options: SaveDraftRevisionOptions = {}
  ): WorkflowDraftRevision {
    const draft = super.saveDraftRevision(workflow, validation, source, options);
    const branch = draft.branchId ? this.branches.get(draft.branchId) : undefined;
    if (branch) {
      this.persistBranch(branch);
    }
    this.persistDraftRevision(draft);
    this.persistWorkflowAggregate(draft.workflowId);
    return draft;
  }

  public override saveBranch(record: WorkflowBranch): WorkflowBranch {
    const saved = super.saveBranch(record);
    this.persistBranch(saved);
    return saved;
  }

  public override getDefaultBranch(workflowId: string): WorkflowBranch {
    const branch = super.getDefaultBranch(workflowId);
    this.persistBranch(branch);
    return branch;
  }

  public override savePromptTurn(record: WorkflowPromptTurn): WorkflowPromptTurn {
    const saved = super.savePromptTurn(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO workflow_prompt_turns (id, workflow_id, branch_id, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveBranchMerge(record: WorkflowBranchMergeRecord): WorkflowBranchMergeRecord {
    const saved = super.saveBranchMerge(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO workflow_branch_merges (id, workflow_id, source_branch_id, target_branch_id, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.sourceBranchId)}, ${sqlValue(saved.targetBranchId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveGeneratedModuleReuseDecision(
    record: WorkflowGeneratedModuleReuseDecision
  ): WorkflowGeneratedModuleReuseDecision {
    const saved = super.saveGeneratedModuleReuseDecision(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO generated_module_reuse_decisions (id, workflow_id, branch_id, node_id, status, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override approveWorkflow(
    workflowId: string,
    approvedBy: string,
    workflowOverride?: WorkflowSpec,
    branchId?: string | undefined
  ): WorkflowApprovedRevision {
    const approved = super.approveWorkflow(workflowId, approvedBy, workflowOverride, branchId);
    this.persistApprovedRevision(approved);
    this.persistWorkflowAggregate(workflowId);
    for (const branch of this.listBranches(workflowId)) {
      this.persistBranch(branch);
    }
    return approved;
  }

  public override createRevision(workflowId: string, input: RevisionInput = {}): StoredWorkflow {
    const stored = super.createRevision(workflowId, input);
    this.persistAllWorkflowState(workflowId);
    return stored;
  }

  public override saveExecution(execution: StoredExecution): StoredExecution {
    const saved = super.saveExecution(execution);
    this.runSql(
      [
        "INSERT OR REPLACE INTO executions (id, workflow_id, revision, created_at, result_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${saved.revision}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved.result))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveRun(run: WorkflowRunRecord): WorkflowRunRecord {
    const saved = super.saveRun(run);
    this.runSql(
      [
        "INSERT OR REPLACE INTO runs (id, workflow_id, approved_revision_id, revision, status, created_at, started_at, finished_at, events_json, result_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.approvedRevisionId)}, ${saved.revision}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.startedAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved.events))}, ${sqlValue(saved.result ? stableStringify(saved.result) : null)});`,
        ...saved.events.map(
          (event, index) =>
            `INSERT OR REPLACE INTO run_events (id, run_id, workflow_id, timestamp, event_json) VALUES (${sqlValue(`${saved.id}.${event.id}.${index}`)}, ${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`
        )
      ].join("\n")
    );
    return saved;
  }

  public override saveAuditRecord(record: WorkflowAuditRecord): WorkflowAuditRecord {
    const saved = super.saveAuditRecord(record);
    this.runSql(
      [
        "INSERT OR IGNORE INTO audit_records (id, workflow_id, revision_id, run_id, node_id, action, actor, timestamp, correlation_id, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.revisionId)}, ${sqlValue(saved.runId ?? null)}, ${sqlValue(saved.nodeId ?? null)}, ${sqlValue(saved.action)}, ${sqlValue(saved.actor)}, ${sqlValue(saved.timestamp)}, ${sqlValue(saved.correlationId)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveArtifactManifest(
    record: WorkflowArtifactManifestRecord
  ): WorkflowArtifactManifestRecord {
    const saved = super.saveArtifactManifest(record);
    this.runSql(
      [
        "INSERT OR IGNORE INTO artifact_manifests (id, workflow_id, revision_id, created_at, artifacts_json, manifest_checksum, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.revisionId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved.artifacts))}, ${sqlValue(saved.manifestChecksum)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveGraphDiff(record: WorkflowGraphDiff): WorkflowGraphDiff {
    const saved = super.saveGraphDiff(record);
    this.runSql(
      [
        "INSERT OR IGNORE INTO graph_diffs (id, workflow_id, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override savePlannerFeedback(record: WorkflowPlannerFeedback): WorkflowPlannerFeedback {
    const saved = super.savePlannerFeedback(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO planner_feedback (id, workflow_id, graph_diff_id, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.graphDiffId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveJob(record: WorkflowJob): WorkflowJob {
    const saved = super.saveJob(record);
    this.persistJob(saved);
    return saved;
  }

  public override appendJobEvent(jobId: string, event: WorkflowJobEvent): WorkflowJob {
    const saved = super.appendJobEvent(jobId, event);
    this.persistJob(saved);
    this.runSql(
      [
        "INSERT OR REPLACE INTO job_events (id, job_id, timestamp, event_json)",
        `VALUES (${sqlValue(event.id)}, ${sqlValue(jobId)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveDraftEvaluation(record: WorkflowDraftEvaluation): WorkflowDraftEvaluation {
    const saved = super.saveDraftEvaluation(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO draft_evaluations (id, workflow_id, draft_revision_id, status, created_at, finished_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.draftRevisionId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveWorkspace(record: WorkflowWorkspace): WorkflowWorkspace {
    const saved = super.saveWorkspace(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO workspaces (id, job_id, workflow_id, created_at, updated_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveAgentRun(record: CodegenAgentRunRecord): CodegenAgentRunRecord {
    const saved = super.saveAgentRun(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO agent_runs (id, job_id, workflow_id, node_id, role, status, started_at, finished_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.role)}, ${sqlValue(saved.status)}, ${sqlValue(saved.startedAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveAgentArtifact(
    record: CodegenAgentArtifactRecord
  ): CodegenAgentArtifactRecord {
    const saved = super.saveAgentArtifact(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO agent_artifacts (id, job_id, workflow_id, node_id, agent_run_id, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.agentRunId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveGeneratedNodeTestReport(
    record: GeneratedNodeTestReport
  ): GeneratedNodeTestReport {
    const saved = super.saveGeneratedNodeTestReport(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO generated_node_tests (id, job_id, workflow_id, node_id, status, created_at, finished_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveGeneratedNodeEvalReport(
    record: GeneratedNodeEvalReport
  ): GeneratedNodeEvalReport {
    const saved = super.saveGeneratedNodeEvalReport(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO generated_node_eval_reports (id, job_id, workflow_id, node_id, status, created_at, finished_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveDeployment(record: WorkflowDeploymentRecord): WorkflowDeploymentRecord {
    const saved = super.saveDeployment(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO deployments (id, workflow_id, approved_revision_id, kind, status, created_at, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.approvedRevisionId)}, ${sqlValue(saved.kind)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveBudgetPolicy(record: WorkflowBudgetPolicy): WorkflowBudgetPolicy {
    const saved = super.saveBudgetPolicy(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO budget_policies (id, workflow_id, branch_id, updated_at, record_json)",
        `VALUES (${sqlValue(budgetPolicyKey(saved.workflowId, saved.branchId))}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId ?? null)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveBudgetLedger(record: WorkflowBudgetLedger): WorkflowBudgetLedger {
    const saved = super.saveBudgetLedger(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO budget_ledgers (id, workflow_id, created_at, updated_at, status, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(saved.status)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveAgentTimelineEvent(
    record: WorkflowAgentTimelineEvent
  ): WorkflowAgentTimelineEvent {
    const saved = super.saveAgentTimelineEvent(record);
    this.runSql(
      [
        "INSERT OR REPLACE INTO agent_timeline_events (id, workflow_id, timestamp, role, status, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.timestamp)}, ${sqlValue(saved.role)}, ${sqlValue(saved.status)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  public override saveNodeDecisionTrace(
    record: WorkflowNodeDecisionTrace
  ): WorkflowNodeDecisionTrace {
    const saved = super.saveNodeDecisionTrace(record);
    this.runSql(
      [
        "INSERT OR IGNORE INTO node_decision_traces (id, workflow_id, node_id, created_at, kind, source, record_json)",
        `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.kind)}, ${sqlValue(saved.source)}, ${sqlValue(stableStringify(saved))});`
      ].join(" ")
    );
    return saved;
  }

  private persistAllWorkflowState(workflowId: string): void {
    const stored = this.requireWorkflow(workflowId);
    for (const draft of stored.draftRevisions) {
      this.persistDraftRevision(draft);
    }
    for (const approved of stored.approvedRevisions) {
      this.persistApprovedRevision(approved);
    }
    for (const branch of this.listBranches(workflowId)) {
      this.persistBranch(branch);
    }
    this.persistWorkflowAggregate(workflowId);
  }

  private persistBranch(branch: WorkflowBranch): void {
    this.runSql(
      [
        "INSERT OR REPLACE INTO workflow_branches (id, workflow_id, name, status, created_at, updated_at, record_json)",
        `VALUES (${sqlValue(branch.id)}, ${sqlValue(branch.workflowId)}, ${sqlValue(branch.name)}, ${sqlValue(branch.status)}, ${sqlValue(branch.createdAt)}, ${sqlValue(branch.updatedAt)}, ${sqlValue(stableStringify(branch))});`
      ].join(" ")
    );
  }

  private persistWorkflowAggregate(workflowId: string): void {
    const aggregate = this.workflows.get(workflowId);
    if (!aggregate) {
      return;
    }

    this.runSql(
      [
        "INSERT OR REPLACE INTO workflow_aggregates (workflow_id, workflow_json, validation_json, created_at, latest_draft_revision_id, latest_approved_revision_id, draft_revision_ids_json, approved_revision_ids_json)",
        `VALUES (${sqlValue(workflowId)}, ${sqlValue(stableStringify(aggregate.workflow))}, ${sqlValue(stableStringify(aggregate.validation))}, ${sqlValue(aggregate.createdAt)}, ${sqlValue(aggregate.latestDraftRevisionId)}, ${sqlValue(aggregate.latestApprovedRevisionId)}, ${sqlValue(stableStringify(aggregate.draftRevisionIds))}, ${sqlValue(stableStringify(aggregate.approvedRevisionIds))});`
      ].join(" ")
    );
  }

  private persistDraftRevision(draft: WorkflowDraftRevision): void {
    this.runSql(
      [
        "INSERT OR REPLACE INTO draft_revisions (id, workflow_id, revision, workflow_json, validation_json, source, created_at)",
        `VALUES (${sqlValue(draft.id)}, ${sqlValue(draft.workflowId)}, ${draft.revision}, ${sqlValue(stableStringify(draft.workflow))}, ${sqlValue(stableStringify(draft.validation))}, ${sqlValue(draft.source)}, ${sqlValue(draft.createdAt)});`,
        "INSERT OR REPLACE INTO draft_revision_records (id, workflow_id, created_at, record_json)",
        `VALUES (${sqlValue(draft.id)}, ${sqlValue(draft.workflowId)}, ${sqlValue(draft.createdAt)}, ${sqlValue(stableStringify(draft))});`
      ].join(" ")
    );
  }

  private persistApprovedRevision(approved: WorkflowApprovedRevision): void {
    this.runSql(
      [
        "INSERT OR IGNORE INTO approved_revisions (id, workflow_id, revision, approved_by, created_at, workflow_json, draft_spec_json, frozen_spec_json, diff_json)",
        `VALUES (${sqlValue(approved.id)}, ${sqlValue(approved.workflowId)}, ${approved.revision}, ${sqlValue(approved.approvedBy)}, ${sqlValue(approved.createdAt)}, ${sqlValue(stableStringify(approved.workflow))}, ${sqlValue(approved.draftSpecJson)}, ${sqlValue(approved.frozenSpecJson)}, ${sqlValue(stableStringify(approved.diff))});`,
        "INSERT OR IGNORE INTO approved_revision_records (id, workflow_id, created_at, record_json)",
        `VALUES (${sqlValue(approved.id)}, ${sqlValue(approved.workflowId)}, ${sqlValue(approved.createdAt)}, ${sqlValue(stableStringify(approved))});`
      ].join(" ")
    );
  }

  private hydrate(): void {
    for (const row of this.queryRows<DraftRevisionRow>(
      "SELECT * FROM draft_revisions ORDER BY created_at, id;"
    )) {
      this.draftRevisions.set(row.id, {
        id: row.id,
        workflowId: row.workflow_id,
        revision: row.revision,
        workflow: parseJson(row.workflow_json),
        validation: parseJson(row.validation_json),
        source: row.source,
        createdAt: row.created_at
      });
    }
    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM draft_revision_records ORDER BY created_at, id;"
    )) {
      this.draftRevisions.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<ApprovedRevisionRow>(
      "SELECT * FROM approved_revisions ORDER BY created_at, id;"
    )) {
      this.approvedRevisions.set(row.id, {
        id: row.id,
        workflowId: row.workflow_id,
        revision: row.revision,
        approvedBy: row.approved_by,
        createdAt: row.created_at,
        workflow: parseJson(row.workflow_json),
        draftSpecJson: row.draft_spec_json,
        frozenSpecJson: row.frozen_spec_json,
        diff: parseJson(row.diff_json)
      });
    }
    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM approved_revision_records ORDER BY created_at, id;"
    )) {
      this.approvedRevisions.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<WorkflowAggregateRow>(
      "SELECT * FROM workflow_aggregates ORDER BY created_at, workflow_id;"
    )) {
      this.workflows.set(row.workflow_id, {
        workflow: parseJson(row.workflow_json),
        validation: parseJson(row.validation_json),
        createdAt: row.created_at,
        latestDraftRevisionId: row.latest_draft_revision_id,
        latestApprovedRevisionId: row.latest_approved_revision_id,
        draftRevisionIds: parseJson(row.draft_revision_ids_json),
        approvedRevisionIds: parseJson(row.approved_revision_ids_json)
      });
    }

    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM workflow_branches ORDER BY created_at, id;"
    )) {
      this.branches.set(row.id, parseJson(row.record_json));
    }
    for (const workflowId of this.workflows.keys()) {
      if (!this.branches.has(defaultBranchId(workflowId))) {
        const latestDraft = this.getLatestDraftRevision(workflowId);
        if (latestDraft) {
          this.branches.set(
            defaultBranchId(workflowId),
            this.createDefaultBranch(latestDraft.workflow, latestDraft, "system")
          );
        }
      }
    }
    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM workflow_prompt_turns ORDER BY created_at, id;"
    )) {
      this.promptTurns.set(row.id, parseJson(row.record_json));
    }
    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM workflow_branch_merges ORDER BY created_at, id;"
    )) {
      this.branchMerges.set(row.id, parseJson(row.record_json));
    }
    for (const row of this.queryRows<RecordJsonRow>(
      "SELECT * FROM generated_module_reuse_decisions ORDER BY created_at, id;"
    )) {
      this.generatedModuleReuseDecisions.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<ExecutionRow>(
      "SELECT * FROM executions ORDER BY created_at, id;"
    )) {
      this.executions.set(row.id, {
        id: row.id,
        workflowId: row.workflow_id,
        revision: row.revision,
        createdAt: row.created_at,
        result: parseJson(row.result_json)
      });
    }

    for (const row of this.queryRows<RunRow>("SELECT * FROM runs ORDER BY created_at, id;")) {
      this.runs.set(row.id, {
        id: row.id,
        workflowId: row.workflow_id,
        approvedRevisionId: row.approved_revision_id,
        revision: row.revision,
        status: row.status,
        createdAt: row.created_at,
        startedAt: row.started_at,
        finishedAt: row.finished_at,
        events: parseJson(row.events_json),
        result: row.result_json ? parseJson(row.result_json) : null
      });
    }

    for (const row of this.queryRows<AuditRow>(
      "SELECT * FROM audit_records ORDER BY timestamp, id;"
    )) {
      this.audits.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<ArtifactManifestRow>(
      "SELECT * FROM artifact_manifests ORDER BY created_at, id;"
    )) {
      this.artifactManifests.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<GraphDiffRow>(
      "SELECT * FROM graph_diffs ORDER BY created_at, id;"
    )) {
      this.graphDiffs.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<PlannerFeedbackRow>(
      "SELECT * FROM planner_feedback ORDER BY created_at, id;"
    )) {
      this.plannerFeedback.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<JobRow>("SELECT * FROM jobs ORDER BY created_at, id;")) {
      this.jobs.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<DraftEvaluationRow>(
      "SELECT * FROM draft_evaluations ORDER BY created_at, id;"
    )) {
      this.draftEvaluations.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<WorkspaceRow>(
      "SELECT * FROM workspaces ORDER BY created_at, id;"
    )) {
      this.workspaces.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<AgentRunRow>(
      "SELECT * FROM agent_runs ORDER BY started_at, id;"
    )) {
      this.agentRuns.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<AgentArtifactRow>(
      "SELECT * FROM agent_artifacts ORDER BY created_at, id;"
    )) {
      this.agentArtifacts.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<GeneratedNodeTestReportRow>(
      "SELECT * FROM generated_node_tests ORDER BY created_at, id;"
    )) {
      this.generatedNodeTestReports.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<GeneratedNodeEvalReportRow>(
      "SELECT * FROM generated_node_eval_reports ORDER BY created_at, id;"
    )) {
      this.generatedNodeEvalReports.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<DeploymentRow>(
      "SELECT * FROM deployments ORDER BY created_at, id;"
    )) {
      this.deployments.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<BudgetPolicyRow>(
      "SELECT * FROM budget_policies ORDER BY updated_at, id;"
    )) {
      const policy = parseJson<WorkflowBudgetPolicy>(row.record_json);
      this.budgetPolicies.set(budgetPolicyKey(policy.workflowId, policy.branchId), policy);
    }

    for (const row of this.queryRows<BudgetLedgerRow>(
      "SELECT * FROM budget_ledgers ORDER BY created_at, id;"
    )) {
      this.budgetLedgers.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<AgentTimelineEventRow>(
      "SELECT * FROM agent_timeline_events ORDER BY timestamp, id;"
    )) {
      this.agentTimelineEvents.set(row.id, parseJson(row.record_json));
    }

    for (const row of this.queryRows<NodeDecisionTraceRow>(
      "SELECT * FROM node_decision_traces ORDER BY created_at, id;"
    )) {
      this.nodeDecisionTraces.set(row.id, parseJson(row.record_json));
    }
  }

  private persistJob(job: WorkflowJob): void {
    this.runSql(
      [
        "INSERT OR REPLACE INTO jobs (id, type, status, workflow_id, created_at, updated_at, correlation_id, record_json)",
        `VALUES (${sqlValue(job.id)}, ${sqlValue(job.type)}, ${sqlValue(job.status)}, ${sqlValue(job.workflowId)}, ${sqlValue(job.createdAt)}, ${sqlValue(job.updatedAt)}, ${sqlValue(job.correlationId)}, ${sqlValue(stableStringify(job))});`,
        ...job.events.map(
          (event) =>
            `INSERT OR REPLACE INTO job_events (id, job_id, timestamp, event_json) VALUES (${sqlValue(event.id)}, ${sqlValue(job.id)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`
        )
      ].join("\n")
    );
  }

  private runSql(sql: string): void {
    execFileSync(this.sqliteBin, [this.databasePath], {
      input: `PRAGMA foreign_keys = ON;\n${sql}\n`,
      encoding: "utf8"
    });
  }

  private queryRows<T>(sql: string): T[] {
    const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
      encoding: "utf8"
    });
    return output.trim().length === 0 ? [] : (JSON.parse(output) as T[]);
  }
}

export function hashWorkflowDag(workflow: WorkflowSpec): string {
  return hashNanoClawWorkflowDag(workflow);
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

export function defaultBranchId(workflowId: string): string {
  return `branch.${workflowId}.main`;
}

function budgetPolicyKey(workflowId: string, branchId: string | undefined): string {
  return `${workflowId}:${branchId ?? "workflow"}`;
}

function draftRevisionId(
  workflowId: string,
  branchId: string,
  revision: number,
  existing: WorkflowAggregate | undefined
): string {
  const index = existing?.draftRevisionIds.length ?? 0;
  if (branchId === defaultBranchId(workflowId)) {
    return `draft.${workflowId}.r${revision}.${index}`;
  }
  return `draft.${workflowId}.${sanitizeRecordIdPart(branchId)}.r${revision}.${index}`;
}

function approvedRevisionId(workflowId: string, branchId: string, revision: number): string {
  if (branchId === defaultBranchId(workflowId)) {
    return `approved.${workflowId}.r${revision}`;
  }
  return `approved.${workflowId}.${sanitizeRecordIdPart(branchId)}.r${revision}`;
}

function sanitizeRecordIdPart(value: string): string {
  return value.replace(/[^a-zA-Z0-9._-]+/gu, "_");
}

function assertImmutableRecordUnchanged(
  kind: string,
  id: string,
  existing: unknown,
  next: unknown
): void {
  if (stableStringify(existing) !== stableStringify(next)) {
    throw new Error(`Immutable ${kind} '${id}' cannot be changed.`);
  }
}

function stableStringify(value: unknown): string {
  return stableJsonStringify(value as never);
}

function parseJson<T>(value: string): T {
  return JSON.parse(value) as T;
}

function sqlValue(value: string | null | undefined): string {
  if (value === null || value === undefined) {
    return "NULL";
  }

  return `'${value.replace(/'/gu, "''")}'`;
}

const sqliteMigrations = [
  `CREATE TABLE IF NOT EXISTS schema_migrations (
    id TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
  );`,
  `CREATE TABLE IF NOT EXISTS workflow_aggregates (
    workflow_id TEXT PRIMARY KEY,
    workflow_json TEXT NOT NULL,
    validation_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    latest_draft_revision_id TEXT,
    latest_approved_revision_id TEXT,
    draft_revision_ids_json TEXT NOT NULL,
    approved_revision_ids_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS draft_revisions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    workflow_json TEXT NOT NULL,
    validation_json TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS draft_revision_records (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS approved_revisions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    approved_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    workflow_json TEXT NOT NULL,
    draft_spec_json TEXT NOT NULL,
    frozen_spec_json TEXT NOT NULL,
    diff_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS approved_revision_records (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS workflow_branches (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS workflow_prompt_turns (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    branch_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS workflow_branch_merges (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    source_branch_id TEXT NOT NULL,
    target_branch_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS generated_module_reuse_decisions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    branch_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS executions (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    result_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    approved_revision_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    events_json TEXT NOT NULL,
    result_json TEXT
  );`,
  `CREATE TABLE IF NOT EXISTS run_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    event_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS audit_records (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    run_id TEXT,
    node_id TEXT,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS artifact_manifests (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    artifacts_json TEXT NOT NULL,
    manifest_checksum TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS graph_diffs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS planner_feedback (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    graph_diff_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    workflow_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS job_events (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    event_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS draft_evaluations (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    draft_revision_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS agent_artifacts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    agent_run_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS generated_node_tests (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS generated_node_eval_reports (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    approved_revision_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS budget_policies (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    branch_id TEXT,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS budget_ledgers (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    status TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS agent_timeline_events (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  `CREATE TABLE IF NOT EXISTS node_decision_traces (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    kind TEXT NOT NULL,
    source TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
  "INSERT OR IGNORE INTO schema_migrations (id) VALUES ('0001_phase7_enterprise_store');"
] as const;

interface WorkflowAggregateRow {
  readonly workflow_id: string;
  readonly workflow_json: string;
  readonly validation_json: string;
  readonly created_at: string;
  readonly latest_draft_revision_id: string | null;
  readonly latest_approved_revision_id: string | null;
  readonly draft_revision_ids_json: string;
  readonly approved_revision_ids_json: string;
}

interface DraftRevisionRow {
  readonly id: string;
  readonly workflow_id: string;
  readonly revision: number;
  readonly workflow_json: string;
  readonly validation_json: string;
  readonly source: WorkflowDraftRevisionSource;
  readonly created_at: string;
}

interface RecordJsonRow {
  readonly id: string;
  readonly created_at: string;
  readonly record_json: string;
}

interface ApprovedRevisionRow {
  readonly id: string;
  readonly workflow_id: string;
  readonly revision: number;
  readonly approved_by: string;
  readonly created_at: string;
  readonly workflow_json: string;
  readonly draft_spec_json: string;
  readonly frozen_spec_json: string;
  readonly diff_json: string;
}

interface ExecutionRow {
  readonly id: string;
  readonly workflow_id: string;
  readonly revision: number;
  readonly created_at: string;
  readonly result_json: string;
}

interface RunRow {
  readonly id: string;
  readonly workflow_id: string;
  readonly approved_revision_id: string;
  readonly revision: number;
  readonly status: WorkflowRunRecord["status"];
  readonly created_at: string;
  readonly started_at: string;
  readonly finished_at: string;
  readonly events_json: string;
  readonly result_json: string | null;
}

interface AuditRow {
  readonly id: string;
  readonly record_json: string;
}

interface ArtifactManifestRow {
  readonly id: string;
  readonly record_json: string;
}

interface GraphDiffRow {
  readonly id: string;
  readonly record_json: string;
}

interface PlannerFeedbackRow {
  readonly id: string;
  readonly record_json: string;
}

interface JobRow {
  readonly id: string;
  readonly record_json: string;
}

interface DraftEvaluationRow {
  readonly id: string;
  readonly record_json: string;
}

interface WorkspaceRow {
  readonly id: string;
  readonly record_json: string;
}

interface AgentRunRow {
  readonly id: string;
  readonly record_json: string;
}

interface AgentArtifactRow {
  readonly id: string;
  readonly record_json: string;
}

interface GeneratedNodeTestReportRow {
  readonly id: string;
  readonly record_json: string;
}

interface GeneratedNodeEvalReportRow {
  readonly id: string;
  readonly record_json: string;
}

interface DeploymentRow {
  readonly id: string;
  readonly record_json: string;
}

interface BudgetPolicyRow {
  readonly id: string;
  readonly record_json: string;
}

interface BudgetLedgerRow {
  readonly id: string;
  readonly record_json: string;
}

interface AgentTimelineEventRow {
  readonly id: string;
  readonly record_json: string;
}

interface NodeDecisionTraceRow {
  readonly id: string;
  readonly record_json: string;
}
