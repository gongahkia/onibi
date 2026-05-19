import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { hashWorkflowDag as hashNanoClawWorkflowDag } from "@kelpclaw/nanoclaw";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import {
  createWorkflowSpecDiff,
  stableJsonStringify,
  stableWorkflowStringify,
  validateWorkflowSpec,
  workflowSchemaVersion
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowArtifactManifestRecord,
  WorkflowApprovalRecord,
  WorkflowApprovedRevision,
  WorkflowAuditRecord,
  WorkflowDraftRevision,
  WorkflowDraftRevisionSource,
  WorkflowRunRecord,
  WorkflowRunEvent,
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
    options?: { readonly force?: boolean; readonly preserveRevision?: boolean }
  ): WorkflowDraftRevision;
  getWorkflow(id: string): StoredWorkflow | undefined;
  approveWorkflow(
    workflowId: string,
    approvedBy: string,
    workflowOverride?: WorkflowSpec
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
  requireWorkflow(id: string): StoredWorkflow;
}

export class InMemoryWorkflowStore implements WorkflowStore {
  protected readonly workflows = new Map<string, WorkflowAggregate>();
  protected readonly draftRevisions = new Map<string, WorkflowDraftRevision>();
  protected readonly approvedRevisions = new Map<string, WorkflowApprovedRevision>();
  protected readonly executions = new Map<string, StoredExecution>();
  protected readonly runs = new Map<string, WorkflowRunRecord>();
  protected readonly audits = new Map<string, WorkflowAuditRecord>();
  protected readonly artifactManifests = new Map<string, WorkflowArtifactManifestRecord>();

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
    options: { readonly force?: boolean; readonly preserveRevision?: boolean } = {}
  ): WorkflowDraftRevision {
    const draft = super.saveDraftRevision(workflow, validation, source, options);
    this.persistDraftRevision(draft);
    this.persistWorkflowAggregate(draft.workflowId);
    return draft;
  }

  public override approveWorkflow(
    workflowId: string,
    approvedBy: string,
    workflowOverride?: WorkflowSpec
  ): WorkflowApprovedRevision {
    const approved = super.approveWorkflow(workflowId, approvedBy, workflowOverride);
    this.persistApprovedRevision(approved);
    this.persistWorkflowAggregate(workflowId);
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

  private persistAllWorkflowState(workflowId: string): void {
    const stored = this.requireWorkflow(workflowId);
    for (const draft of stored.draftRevisions) {
      this.persistDraftRevision(draft);
    }
    for (const approved of stored.approvedRevisions) {
      this.persistApprovedRevision(approved);
    }
    this.persistWorkflowAggregate(workflowId);
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
        `VALUES (${sqlValue(draft.id)}, ${sqlValue(draft.workflowId)}, ${draft.revision}, ${sqlValue(stableStringify(draft.workflow))}, ${sqlValue(stableStringify(draft.validation))}, ${sqlValue(draft.source)}, ${sqlValue(draft.createdAt)});`
      ].join(" ")
    );
  }

  private persistApprovedRevision(approved: WorkflowApprovedRevision): void {
    this.runSql(
      [
        "INSERT OR IGNORE INTO approved_revisions (id, workflow_id, revision, approved_by, created_at, workflow_json, draft_spec_json, frozen_spec_json, diff_json)",
        `VALUES (${sqlValue(approved.id)}, ${sqlValue(approved.workflowId)}, ${approved.revision}, ${sqlValue(approved.approvedBy)}, ${sqlValue(approved.createdAt)}, ${sqlValue(stableStringify(approved.workflow))}, ${sqlValue(approved.draftSpecJson)}, ${sqlValue(approved.frozenSpecJson)}, ${sqlValue(stableStringify(approved.diff))});`
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
