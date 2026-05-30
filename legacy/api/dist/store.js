import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { hashWorkflowDag as hashNanoClawWorkflowDag } from "@kelpclaw/nanoclaw";
import { createWorkflowSpecDiff, stableJsonStringify, stableWorkflowStringify, validateWorkflowSpec, workflowSchemaVersion } from "@kelpclaw/workflow-spec";
export class InMemoryWorkflowStore {
    workflows = new Map();
    draftRevisions = new Map();
    approvedRevisions = new Map();
    branches = new Map();
    promptTurns = new Map();
    branchMerges = new Map();
    generatedModuleReuseDecisions = new Map();
    executions = new Map();
    runs = new Map();
    audits = new Map();
    artifactManifests = new Map();
    graphDiffs = new Map();
    plannerFeedback = new Map();
    jobs = new Map();
    draftEvaluations = new Map();
    workspaces = new Map();
    agentRuns = new Map();
    agentArtifacts = new Map();
    generatedNodeTestReports = new Map();
    generatedNodeEvalReports = new Map();
    deployments = new Map();
    connectors = new Map();
    schedules = new Map();
    runCheckpoints = new Map();
    alertPolicies = new Map();
    retentionPolicies = new Map();
    budgetPolicies = new Map();
    budgetLedgers = new Map();
    agentTimelineEvents = new Map();
    nodeDecisionTraces = new Map();
    agentMemories = new Map();
    saveWorkflow(workflow, validation) {
        const draft = this.saveDraftRevision(workflow, validation, "revision", {
            force: true,
            preserveRevision: true
        });
        return this.requireWorkflow(draft.workflowId);
    }
    saveDraftRevision(workflow, validation, source, options = {}) {
        if (!validation.ok) {
            throw new Error("Cannot save an invalid workflow draft revision.");
        }
        const existing = this.workflows.get(workflow.id);
        const branchId = options.branchId ?? defaultBranchId(workflow.id);
        const existingBranch = this.branches.get(branchId);
        const latestDraftId = existingBranch?.headDraftRevisionId ?? existing?.latestDraftRevisionId;
        const latestDraft = latestDraftId ? this.draftRevisions.get(latestDraftId) : undefined;
        if (!options.force &&
            latestDraft &&
            draftFingerprint(latestDraft.workflow) === draftFingerprint(workflow)) {
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
        const revision = options.preserveRevision || !existing
            ? workflow.revision
            : (latestDraft?.workflow.revision ?? existing.workflow.revision) + 1;
        const draftWorkflow = {
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
        const draftRevision = {
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
        const aggregate = existing ?? {
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
        const branch = existingBranch ??
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
    saveBranch(record) {
        this.branches.set(record.id, record);
        return record;
    }
    getBranch(id) {
        return this.branches.get(id);
    }
    getDefaultBranch(workflowId) {
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
    listBranches(workflowId) {
        if (this.workflows.has(workflowId) && !this.branches.has(defaultBranchId(workflowId))) {
            this.getDefaultBranch(workflowId);
        }
        return [...this.branches.values()]
            .filter((branch) => branch.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    savePromptTurn(record) {
        this.promptTurns.set(record.id, record);
        return record;
    }
    listPromptTurns(workflowId, branchId) {
        return [...this.promptTurns.values()]
            .filter((turn) => turn.workflowId === workflowId && (branchId === undefined || turn.branchId === branchId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveBranchMerge(record) {
        this.branchMerges.set(record.id, record);
        return record;
    }
    listBranchMerges(workflowId) {
        return [...this.branchMerges.values()]
            .filter((merge) => merge.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveGeneratedModuleReuseDecision(record) {
        this.generatedModuleReuseDecisions.set(record.id, record);
        return record;
    }
    listGeneratedModuleReuseDecisions(workflowId, branchId) {
        return [...this.generatedModuleReuseDecisions.values()]
            .filter((decision) => decision.workflowId === workflowId &&
            (branchId === undefined || decision.branchId === branchId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    getWorkflow(id) {
        const aggregate = this.workflows.get(id);
        if (!aggregate) {
            return undefined;
        }
        return this.toStoredWorkflow(aggregate);
    }
    approveWorkflow(workflowId, approvedBy, workflowOverride, branchId) {
        const stored = this.requireWorkflow(workflowId);
        const targetBranchId = branchId ?? defaultBranchId(workflowId);
        const branch = this.branches.get(targetBranchId);
        const branchHead = branch ? this.draftRevisions.get(branch.headDraftRevisionId) : undefined;
        const workflow = workflowOverride ?? branchHead?.workflow ?? stored.workflow;
        const validation = validateWorkflowSpec(workflow);
        if (!validation.ok) {
            throw new Error(validation.errors.map((error) => error.code).join(", "));
        }
        const latestDraftRevision = this.saveDraftRevision(validation.workflow, validation, "validate", {
            branchId: targetBranchId,
            parentDraftRevisionId: branchHead?.id
        });
        const latestDraft = latestDraftRevision.workflow;
        const approval = {
            status: "approved",
            approvedBy,
            approvedAt: new Date().toISOString(),
            frozenRevision: latestDraft.revision,
            frozenDagHash: hashWorkflowDag(latestDraft),
            nodeOrder: calculateNodeOrder(latestDraft)
        };
        const approvedWorkflow = {
            ...latestDraft,
            approval,
            updatedAt: approval.approvedAt
        };
        const approvedValidation = validateWorkflowSpec(approvedWorkflow);
        if (!approvedValidation.ok) {
            throw new Error(approvedValidation.errors.map((error) => error.code).join(", "));
        }
        const approvedBranchId = targetBranchId === defaultBranchId(approvedWorkflow.id) ? undefined : targetBranchId;
        const approvedRevision = {
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
            assertImmutableRecordUnchanged("approved revision", approvedRevision.id, existingApproved, approvedRevision);
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
    createRevision(workflowId, input = {}) {
        const stored = this.requireWorkflow(workflowId);
        const source = input.workflow ?? stored.workflow;
        const now = new Date().toISOString();
        const revision = {
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
    getDraftRevision(id) {
        return this.draftRevisions.get(id);
    }
    getLatestDraftRevision(workflowId) {
        const aggregate = this.workflows.get(workflowId);
        return aggregate?.latestDraftRevisionId
            ? this.draftRevisions.get(aggregate.latestDraftRevisionId)
            : undefined;
    }
    getApprovedRevision(id) {
        return this.approvedRevisions.get(id);
    }
    getLatestApprovedRevision(workflowId) {
        const aggregate = this.workflows.get(workflowId);
        return aggregate?.latestApprovedRevisionId
            ? this.approvedRevisions.get(aggregate.latestApprovedRevisionId)
            : undefined;
    }
    getWorkflowRevision(id) {
        const draftRevision = this.getDraftRevision(id);
        if (draftRevision) {
            return { draftRevision };
        }
        const approvedRevision = this.getApprovedRevision(id);
        return approvedRevision ? { approvedRevision } : undefined;
    }
    saveExecution(execution) {
        this.executions.set(execution.id, execution);
        return execution;
    }
    getExecution(id) {
        return this.executions.get(id);
    }
    saveRun(run) {
        this.runs.set(run.id, run);
        return run;
    }
    getRun(id) {
        return this.runs.get(id);
    }
    listRuns(workflowId) {
        return [...this.runs.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    listRunEvents(runId) {
        return this.runs.get(runId)?.events ?? [];
    }
    saveRunCheckpoint(record) {
        this.runCheckpoints.set(record.id, record);
        return record;
    }
    getRunCheckpoint(runId, nodeId, inputHash) {
        return [...this.runCheckpoints.values()]
            .filter((record) => record.runId === runId && record.nodeId === nodeId && record.inputHash === inputHash)
            .sort((left, right) => right.attempt - left.attempt ||
            right.startedAt.localeCompare(left.startedAt) ||
            right.id.localeCompare(left.id))[0];
    }
    listRunCheckpoints(runId) {
        return [...this.runCheckpoints.values()]
            .filter((record) => record.runId === runId)
            .sort((left, right) => left.startedAt.localeCompare(right.startedAt) || left.id.localeCompare(right.id));
    }
    saveAuditRecord(record) {
        const existing = this.audits.get(record.id);
        if (existing) {
            assertImmutableRecordUnchanged("audit record", record.id, existing, record);
            return existing;
        }
        this.audits.set(record.id, record);
        return record;
    }
    listAuditRecords(workflowId) {
        return [...this.audits.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id));
    }
    saveArtifactManifest(record) {
        const existing = this.artifactManifests.get(record.id);
        if (existing) {
            assertImmutableRecordUnchanged("artifact manifest", record.id, existing, record);
            return existing;
        }
        this.artifactManifests.set(record.id, record);
        return record;
    }
    getArtifactManifest(id) {
        return this.artifactManifests.get(id);
    }
    saveGraphDiff(record) {
        const existing = this.graphDiffs.get(record.id);
        if (existing) {
            assertImmutableRecordUnchanged("graph diff", record.id, existing, record);
            return existing;
        }
        this.graphDiffs.set(record.id, record);
        return record;
    }
    getGraphDiff(id) {
        return this.graphDiffs.get(id);
    }
    listGraphDiffs(workflowId) {
        return [...this.graphDiffs.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    savePlannerFeedback(record) {
        this.plannerFeedback.set(record.id, record);
        return record;
    }
    getPlannerFeedback(id) {
        return this.plannerFeedback.get(id);
    }
    listPlannerFeedback(workflowId) {
        return [...this.plannerFeedback.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveJob(record) {
        this.jobs.set(record.id, record);
        return record;
    }
    getJob(id) {
        return this.jobs.get(id);
    }
    listJobs(workflowId) {
        return [...this.jobs.values()]
            .filter((record) => workflowId === undefined || record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    claimNextQueuedJob(workerId, types, claimedAt = new Date().toISOString()) {
        const allowedTypes = types ? new Set(types) : undefined;
        const nowMs = Date.parse(claimedAt);
        const job = this.listJobs().find((record) => record.status === "queued" &&
            (!allowedTypes || allowedTypes.has(record.type)) &&
            (!record.retry.nextRunAt || Date.parse(record.retry.nextRunAt) <= nowMs) &&
            (record.type !== "run.workflow" || typeof record.payload?.runId === "string"));
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
    appendJobEvent(jobId, event) {
        const job = this.jobs.get(jobId);
        if (!job) {
            throw new Error(`Unknown job '${jobId}'.`);
        }
        const updated = {
            ...job,
            updatedAt: event.timestamp,
            events: [...job.events, event]
        };
        this.jobs.set(jobId, updated);
        return updated;
    }
    saveDraftEvaluation(record) {
        this.draftEvaluations.set(record.id, record);
        return record;
    }
    getDraftEvaluation(id) {
        return this.draftEvaluations.get(id);
    }
    getLatestDraftEvaluation(workflowId, branchId) {
        return this.listDraftEvaluations(workflowId, branchId).at(-1);
    }
    listDraftEvaluations(workflowId, branchId) {
        return [...this.draftEvaluations.values()]
            .filter((record) => record.workflowId === workflowId &&
            (branchId === undefined || record.branchId === branchId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveWorkspace(record) {
        this.workspaces.set(record.id, record);
        return record;
    }
    getWorkspace(id) {
        return this.workspaces.get(id);
    }
    listWorkspaces(workflowId) {
        return [...this.workspaces.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveAgentRun(record) {
        this.agentRuns.set(record.id, record);
        return record;
    }
    listAgentRuns(workflowId, nodeId) {
        return [...this.agentRuns.values()]
            .filter((record) => record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId))
            .sort((left, right) => left.startedAt.localeCompare(right.startedAt) || left.id.localeCompare(right.id));
    }
    saveAgentArtifact(record) {
        this.agentArtifacts.set(record.id, record);
        return record;
    }
    listAgentArtifacts(workflowId, nodeId) {
        return [...this.agentArtifacts.values()]
            .filter((record) => record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveGeneratedNodeTestReport(record) {
        this.generatedNodeTestReports.set(record.id, record);
        return record;
    }
    listGeneratedNodeTestReports(workflowId, nodeId) {
        return [...this.generatedNodeTestReports.values()]
            .filter((record) => record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveGeneratedNodeEvalReport(record) {
        this.generatedNodeEvalReports.set(record.id, record);
        return record;
    }
    listGeneratedNodeEvalReports(workflowId, nodeId) {
        return [...this.generatedNodeEvalReports.values()]
            .filter((record) => record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveDeployment(record) {
        this.deployments.set(record.id, record);
        return record;
    }
    listDeployments(workflowId) {
        return [...this.deployments.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveConnector(record) {
        this.connectors.set(record.id, record);
        return record;
    }
    getConnector(id) {
        return this.connectors.get(id);
    }
    listConnectors() {
        return [...this.connectors.values()].sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    deleteConnector(id) {
        return this.connectors.delete(id);
    }
    saveSchedule(record) {
        this.schedules.set(record.id, record);
        return record;
    }
    getSchedule(id) {
        return this.schedules.get(id);
    }
    listSchedules(workflowId) {
        return [...this.schedules.values()]
            .filter((record) => workflowId === undefined || record.workflowId === workflowId)
            .sort((left, right) => left.nextFireAt.localeCompare(right.nextFireAt) || left.id.localeCompare(right.id));
    }
    saveAlertPolicy(record) {
        this.alertPolicies.set(policyKey(record.workflowId, record.branchId), record);
        return record;
    }
    getAlertPolicy(workflowId, branchId) {
        return (this.alertPolicies.get(policyKey(workflowId, branchId)) ??
            this.alertPolicies.get(policyKey(workflowId, undefined)));
    }
    saveRetentionPolicy(record) {
        this.retentionPolicies.set(policyKey(record.workflowId, record.branchId), record);
        return record;
    }
    getRetentionPolicy(workflowId, branchId) {
        return (this.retentionPolicies.get(policyKey(workflowId, branchId)) ??
            this.retentionPolicies.get(policyKey(workflowId, undefined)));
    }
    saveBudgetPolicy(record) {
        this.budgetPolicies.set(budgetPolicyKey(record.workflowId, record.branchId), record);
        return record;
    }
    getBudgetPolicy(workflowId, branchId) {
        return (this.budgetPolicies.get(budgetPolicyKey(workflowId, branchId)) ??
            this.budgetPolicies.get(budgetPolicyKey(workflowId, undefined)));
    }
    saveBudgetLedger(record) {
        this.budgetLedgers.set(record.id, record);
        return record;
    }
    listBudgetLedgers(workflowId) {
        return [...this.budgetLedgers.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveAgentTimelineEvent(record) {
        this.agentTimelineEvents.set(record.id, record);
        return record;
    }
    listAgentTimelineEvents(workflowId) {
        return [...this.agentTimelineEvents.values()]
            .filter((record) => record.workflowId === workflowId)
            .sort((left, right) => left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id));
    }
    saveNodeDecisionTrace(record) {
        const existing = this.nodeDecisionTraces.get(record.id);
        if (existing) {
            assertImmutableRecordUnchanged("node decision trace", record.id, existing, record);
            return existing;
        }
        this.nodeDecisionTraces.set(record.id, record);
        return record;
    }
    listNodeDecisionTraces(workflowId, nodeId) {
        return [...this.nodeDecisionTraces.values()]
            .filter((record) => record.workflowId === workflowId && (nodeId === undefined || record.nodeId === nodeId))
            .sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id.localeCompare(right.id));
    }
    saveAgentMemory(record) {
        this.agentMemories.set(record.id, record);
        return record;
    }
    listAgentMemory(workflowId, options = {}) {
        const nowMs = Date.parse(options.now ?? new Date().toISOString());
        return [...this.agentMemories.values()]
            .filter((record) => workflowId === undefined || record.workflowId === workflowId)
            .filter((record) => options.namespace === undefined || record.namespace === options.namespace)
            .filter((record) => options.includeExpired === true ||
            record.expiresAt === undefined ||
            Date.parse(record.expiresAt) > nowMs)
            .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt) || left.id.localeCompare(right.id));
    }
    deleteAgentMemory(id) {
        return this.agentMemories.delete(id);
    }
    expireAgentMemory(now = new Date().toISOString()) {
        const nowMs = Date.parse(now);
        const expired = [...this.agentMemories.values()].filter((record) => record.expiresAt !== undefined && Date.parse(record.expiresAt) <= nowMs);
        for (const record of expired) {
            this.agentMemories.delete(record.id);
        }
        return expired;
    }
    requireWorkflow(id) {
        const aggregate = this.workflows.get(id);
        if (!aggregate) {
            throw new Error(`Unknown workflow '${id}'.`);
        }
        return this.toStoredWorkflow(aggregate);
    }
    createDefaultBranch(workflow, draftRevision, createdBy) {
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
    toStoredWorkflow(aggregate) {
        return {
            workflow: aggregate.workflow,
            validation: aggregate.validation,
            createdAt: aggregate.createdAt,
            draftRevisions: aggregate.draftRevisionIds
                .map((revisionId) => this.draftRevisions.get(revisionId))
                .filter((revision) => revision !== undefined),
            approvedRevisions: aggregate.approvedRevisionIds
                .map((revisionId) => this.approvedRevisions.get(revisionId))
                .filter((revision) => revision !== undefined),
            latestApprovedRevisionId: aggregate.latestApprovedRevisionId
        };
    }
}
export class SqliteWorkflowStore extends InMemoryWorkflowStore {
    databasePath;
    sqliteBin;
    constructor(options) {
        super();
        this.databasePath = options.databasePath;
        this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
        mkdirSync(dirname(this.databasePath), { recursive: true });
        this.runSql(sqliteMigrations.join("\n"));
        this.hydrate();
    }
    saveWorkflow(workflow, validation) {
        const stored = super.saveWorkflow(workflow, validation);
        this.persistAllWorkflowState(stored.workflow.id);
        return stored;
    }
    saveDraftRevision(workflow, validation, source, options = {}) {
        const draft = super.saveDraftRevision(workflow, validation, source, options);
        const branch = draft.branchId ? this.branches.get(draft.branchId) : undefined;
        if (branch) {
            this.persistBranch(branch);
        }
        this.persistDraftRevision(draft);
        this.persistWorkflowAggregate(draft.workflowId);
        return draft;
    }
    saveBranch(record) {
        const saved = super.saveBranch(record);
        this.persistBranch(saved);
        return saved;
    }
    getDefaultBranch(workflowId) {
        const branch = super.getDefaultBranch(workflowId);
        this.persistBranch(branch);
        return branch;
    }
    savePromptTurn(record) {
        const saved = super.savePromptTurn(record);
        this.runSql([
            "INSERT OR REPLACE INTO workflow_prompt_turns (id, workflow_id, branch_id, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveBranchMerge(record) {
        const saved = super.saveBranchMerge(record);
        this.runSql([
            "INSERT OR REPLACE INTO workflow_branch_merges (id, workflow_id, source_branch_id, target_branch_id, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.sourceBranchId)}, ${sqlValue(saved.targetBranchId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveGeneratedModuleReuseDecision(record) {
        const saved = super.saveGeneratedModuleReuseDecision(record);
        this.runSql([
            "INSERT OR REPLACE INTO generated_module_reuse_decisions (id, workflow_id, branch_id, node_id, status, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    approveWorkflow(workflowId, approvedBy, workflowOverride, branchId) {
        const approved = super.approveWorkflow(workflowId, approvedBy, workflowOverride, branchId);
        this.persistApprovedRevision(approved);
        this.persistWorkflowAggregate(workflowId);
        for (const branch of this.listBranches(workflowId)) {
            this.persistBranch(branch);
        }
        return approved;
    }
    createRevision(workflowId, input = {}) {
        const stored = super.createRevision(workflowId, input);
        this.persistAllWorkflowState(workflowId);
        return stored;
    }
    saveExecution(execution) {
        const saved = super.saveExecution(execution);
        this.runSql([
            "INSERT OR REPLACE INTO executions (id, workflow_id, revision, created_at, result_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${saved.revision}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved.result))});`
        ].join(" "));
        return saved;
    }
    saveRun(run) {
        const saved = super.saveRun(run);
        this.runSql([
            "INSERT OR REPLACE INTO runs (id, workflow_id, approved_revision_id, revision, status, created_at, started_at, finished_at, events_json, result_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.approvedRevisionId)}, ${saved.revision}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.startedAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved.events))}, ${sqlValue(saved.result ? stableStringify(saved.result) : null)});`,
            ...saved.events.map((event, index) => `INSERT OR REPLACE INTO run_events (id, run_id, workflow_id, timestamp, event_json) VALUES (${sqlValue(`${saved.id}.${event.id}.${index}`)}, ${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`)
        ].join("\n"));
        return saved;
    }
    saveRunCheckpoint(record) {
        const saved = super.saveRunCheckpoint(record);
        this.runSql([
            "INSERT OR REPLACE INTO run_checkpoints (id, run_id, workflow_id, approved_revision_id, node_id, attempt, status, input_hash, started_at, finished_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.runId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.approvedRevisionId)}, ${sqlValue(saved.nodeId)}, ${saved.attempt}, ${sqlValue(saved.status)}, ${sqlValue(saved.inputHash)}, ${sqlValue(saved.startedAt)}, ${sqlValue(saved.finishedAt ?? null)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAuditRecord(record) {
        const saved = super.saveAuditRecord(record);
        this.runSql([
            "INSERT OR IGNORE INTO audit_records (id, workflow_id, revision_id, run_id, node_id, action, actor, timestamp, correlation_id, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.revisionId)}, ${sqlValue(saved.runId ?? null)}, ${sqlValue(saved.nodeId ?? null)}, ${sqlValue(saved.action)}, ${sqlValue(saved.actor)}, ${sqlValue(saved.timestamp)}, ${sqlValue(saved.correlationId)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveArtifactManifest(record) {
        const saved = super.saveArtifactManifest(record);
        this.runSql([
            "INSERT OR IGNORE INTO artifact_manifests (id, workflow_id, revision_id, created_at, artifacts_json, manifest_checksum, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.revisionId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved.artifacts))}, ${sqlValue(saved.manifestChecksum)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveGraphDiff(record) {
        const saved = super.saveGraphDiff(record);
        this.runSql([
            "INSERT OR IGNORE INTO graph_diffs (id, workflow_id, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    savePlannerFeedback(record) {
        const saved = super.savePlannerFeedback(record);
        this.runSql([
            "INSERT OR REPLACE INTO planner_feedback (id, workflow_id, graph_diff_id, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.graphDiffId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveJob(record) {
        const saved = super.saveJob(record);
        this.persistJob(saved);
        return saved;
    }
    appendJobEvent(jobId, event) {
        const saved = super.appendJobEvent(jobId, event);
        this.persistJob(saved);
        this.runSql([
            "INSERT OR REPLACE INTO job_events (id, job_id, timestamp, event_json)",
            `VALUES (${sqlValue(event.id)}, ${sqlValue(jobId)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`
        ].join(" "));
        return saved;
    }
    saveDraftEvaluation(record) {
        const saved = super.saveDraftEvaluation(record);
        this.runSql([
            "INSERT OR REPLACE INTO draft_evaluations (id, workflow_id, draft_revision_id, status, created_at, finished_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.draftRevisionId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveWorkspace(record) {
        const saved = super.saveWorkspace(record);
        this.runSql([
            "INSERT OR REPLACE INTO workspaces (id, job_id, workflow_id, created_at, updated_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAgentRun(record) {
        const saved = super.saveAgentRun(record);
        this.runSql([
            "INSERT OR REPLACE INTO agent_runs (id, job_id, workflow_id, node_id, role, status, started_at, finished_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.role)}, ${sqlValue(saved.status)}, ${sqlValue(saved.startedAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAgentArtifact(record) {
        const saved = super.saveAgentArtifact(record);
        this.runSql([
            "INSERT OR REPLACE INTO agent_artifacts (id, job_id, workflow_id, node_id, agent_run_id, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.agentRunId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveGeneratedNodeTestReport(record) {
        const saved = super.saveGeneratedNodeTestReport(record);
        this.runSql([
            "INSERT OR REPLACE INTO generated_node_tests (id, job_id, workflow_id, node_id, status, created_at, finished_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveGeneratedNodeEvalReport(record) {
        const saved = super.saveGeneratedNodeEvalReport(record);
        this.runSql([
            "INSERT OR REPLACE INTO generated_node_eval_reports (id, job_id, workflow_id, node_id, status, created_at, finished_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.jobId)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.finishedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveDeployment(record) {
        const saved = super.saveDeployment(record);
        this.runSql([
            "INSERT OR REPLACE INTO deployments (id, workflow_id, approved_revision_id, kind, status, created_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.approvedRevisionId)}, ${sqlValue(saved.kind)}, ${sqlValue(saved.status)}, ${sqlValue(saved.createdAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveConnector(record) {
        const saved = super.saveConnector(record);
        this.runSql([
            "INSERT OR REPLACE INTO connectors (id, kind, adapter_id, created_at, updated_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.kind)}, ${sqlValue(saved.adapterId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    deleteConnector(id) {
        const deleted = super.deleteConnector(id);
        if (deleted) {
            this.runSql(`DELETE FROM connectors WHERE id = ${sqlValue(id)};`);
        }
        return deleted;
    }
    saveSchedule(record) {
        const saved = super.saveSchedule(record);
        this.runSql([
            "INSERT OR REPLACE INTO workflow_schedules (id, workflow_id, deployment_id, approved_revision_id, node_id, status, next_fire_at, updated_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.deploymentId)}, ${sqlValue(saved.approvedRevisionId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.status)}, ${sqlValue(saved.nextFireAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAlertPolicy(record) {
        const saved = super.saveAlertPolicy(record);
        this.runSql([
            "INSERT OR REPLACE INTO alert_policies (id, workflow_id, branch_id, updated_at, record_json)",
            `VALUES (${sqlValue(policyKey(saved.workflowId, saved.branchId))}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId ?? null)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveRetentionPolicy(record) {
        const saved = super.saveRetentionPolicy(record);
        this.runSql([
            "INSERT OR REPLACE INTO retention_policies (id, workflow_id, branch_id, updated_at, record_json)",
            `VALUES (${sqlValue(policyKey(saved.workflowId, saved.branchId))}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId ?? null)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveBudgetPolicy(record) {
        const saved = super.saveBudgetPolicy(record);
        this.runSql([
            "INSERT OR REPLACE INTO budget_policies (id, workflow_id, branch_id, updated_at, record_json)",
            `VALUES (${sqlValue(budgetPolicyKey(saved.workflowId, saved.branchId))}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.branchId ?? null)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveBudgetLedger(record) {
        const saved = super.saveBudgetLedger(record);
        this.runSql([
            "INSERT OR REPLACE INTO budget_ledgers (id, workflow_id, created_at, updated_at, status, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(saved.status)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAgentTimelineEvent(record) {
        const saved = super.saveAgentTimelineEvent(record);
        this.runSql([
            "INSERT OR REPLACE INTO agent_timeline_events (id, workflow_id, timestamp, role, status, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.timestamp)}, ${sqlValue(saved.role)}, ${sqlValue(saved.status)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveNodeDecisionTrace(record) {
        const saved = super.saveNodeDecisionTrace(record);
        this.runSql([
            "INSERT OR IGNORE INTO node_decision_traces (id, workflow_id, node_id, created_at, kind, source, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.nodeId)}, ${sqlValue(saved.createdAt)}, ${sqlValue(saved.kind)}, ${sqlValue(saved.source)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    saveAgentMemory(record) {
        const saved = super.saveAgentMemory(record);
        this.runSql([
            "INSERT OR REPLACE INTO agent_memories (id, workflow_id, namespace, scope, updated_at, expires_at, record_json)",
            `VALUES (${sqlValue(saved.id)}, ${sqlValue(saved.workflowId)}, ${sqlValue(saved.namespace)}, ${sqlValue(saved.scope)}, ${sqlValue(saved.updatedAt)}, ${sqlValue(saved.expiresAt ?? null)}, ${sqlValue(stableStringify(saved))});`
        ].join(" "));
        return saved;
    }
    deleteAgentMemory(id) {
        const deleted = super.deleteAgentMemory(id);
        if (deleted) {
            this.runSql(`DELETE FROM agent_memories WHERE id = ${sqlValue(id)};`);
        }
        return deleted;
    }
    expireAgentMemory(now) {
        const expired = super.expireAgentMemory(now);
        if (expired.length > 0) {
            this.runSql(expired
                .map((record) => `DELETE FROM agent_memories WHERE id = ${sqlValue(record.id)};`)
                .join("\n"));
        }
        return expired;
    }
    persistAllWorkflowState(workflowId) {
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
    persistBranch(branch) {
        this.runSql([
            "INSERT OR REPLACE INTO workflow_branches (id, workflow_id, name, status, created_at, updated_at, record_json)",
            `VALUES (${sqlValue(branch.id)}, ${sqlValue(branch.workflowId)}, ${sqlValue(branch.name)}, ${sqlValue(branch.status)}, ${sqlValue(branch.createdAt)}, ${sqlValue(branch.updatedAt)}, ${sqlValue(stableStringify(branch))});`
        ].join(" "));
    }
    persistWorkflowAggregate(workflowId) {
        const aggregate = this.workflows.get(workflowId);
        if (!aggregate) {
            return;
        }
        this.runSql([
            "INSERT OR REPLACE INTO workflow_aggregates (workflow_id, workflow_json, validation_json, created_at, latest_draft_revision_id, latest_approved_revision_id, draft_revision_ids_json, approved_revision_ids_json)",
            `VALUES (${sqlValue(workflowId)}, ${sqlValue(stableStringify(aggregate.workflow))}, ${sqlValue(stableStringify(aggregate.validation))}, ${sqlValue(aggregate.createdAt)}, ${sqlValue(aggregate.latestDraftRevisionId)}, ${sqlValue(aggregate.latestApprovedRevisionId)}, ${sqlValue(stableStringify(aggregate.draftRevisionIds))}, ${sqlValue(stableStringify(aggregate.approvedRevisionIds))});`
        ].join(" "));
    }
    persistDraftRevision(draft) {
        this.runSql([
            "INSERT OR REPLACE INTO draft_revisions (id, workflow_id, revision, workflow_json, validation_json, source, created_at)",
            `VALUES (${sqlValue(draft.id)}, ${sqlValue(draft.workflowId)}, ${draft.revision}, ${sqlValue(stableStringify(draft.workflow))}, ${sqlValue(stableStringify(draft.validation))}, ${sqlValue(draft.source)}, ${sqlValue(draft.createdAt)});`,
            "INSERT OR REPLACE INTO draft_revision_records (id, workflow_id, created_at, record_json)",
            `VALUES (${sqlValue(draft.id)}, ${sqlValue(draft.workflowId)}, ${sqlValue(draft.createdAt)}, ${sqlValue(stableStringify(draft))});`
        ].join(" "));
    }
    persistApprovedRevision(approved) {
        this.runSql([
            "INSERT OR IGNORE INTO approved_revisions (id, workflow_id, revision, approved_by, created_at, workflow_json, draft_spec_json, frozen_spec_json, diff_json)",
            `VALUES (${sqlValue(approved.id)}, ${sqlValue(approved.workflowId)}, ${approved.revision}, ${sqlValue(approved.approvedBy)}, ${sqlValue(approved.createdAt)}, ${sqlValue(stableStringify(approved.workflow))}, ${sqlValue(approved.draftSpecJson)}, ${sqlValue(approved.frozenSpecJson)}, ${sqlValue(stableStringify(approved.diff))});`,
            "INSERT OR IGNORE INTO approved_revision_records (id, workflow_id, created_at, record_json)",
            `VALUES (${sqlValue(approved.id)}, ${sqlValue(approved.workflowId)}, ${sqlValue(approved.createdAt)}, ${sqlValue(stableStringify(approved))});`
        ].join(" "));
    }
    hydrate() {
        for (const row of this.queryRows("SELECT * FROM draft_revisions ORDER BY created_at, id;")) {
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
        for (const row of this.queryRows("SELECT * FROM draft_revision_records ORDER BY created_at, id;")) {
            this.draftRevisions.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM approved_revisions ORDER BY created_at, id;")) {
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
        for (const row of this.queryRows("SELECT * FROM approved_revision_records ORDER BY created_at, id;")) {
            this.approvedRevisions.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM workflow_aggregates ORDER BY created_at, workflow_id;")) {
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
        for (const row of this.queryRows("SELECT * FROM workflow_branches ORDER BY created_at, id;")) {
            this.branches.set(row.id, parseJson(row.record_json));
        }
        for (const workflowId of this.workflows.keys()) {
            if (!this.branches.has(defaultBranchId(workflowId))) {
                const latestDraft = this.getLatestDraftRevision(workflowId);
                if (latestDraft) {
                    this.branches.set(defaultBranchId(workflowId), this.createDefaultBranch(latestDraft.workflow, latestDraft, "system"));
                }
            }
        }
        for (const row of this.queryRows("SELECT * FROM workflow_prompt_turns ORDER BY created_at, id;")) {
            this.promptTurns.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM workflow_branch_merges ORDER BY created_at, id;")) {
            this.branchMerges.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM generated_module_reuse_decisions ORDER BY created_at, id;")) {
            this.generatedModuleReuseDecisions.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM executions ORDER BY created_at, id;")) {
            this.executions.set(row.id, {
                id: row.id,
                workflowId: row.workflow_id,
                revision: row.revision,
                createdAt: row.created_at,
                result: parseJson(row.result_json)
            });
        }
        for (const row of this.queryRows("SELECT * FROM runs ORDER BY created_at, id;")) {
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
        for (const row of this.queryRows("SELECT * FROM run_checkpoints ORDER BY started_at, id;")) {
            this.runCheckpoints.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM audit_records ORDER BY timestamp, id;")) {
            this.audits.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM artifact_manifests ORDER BY created_at, id;")) {
            this.artifactManifests.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM graph_diffs ORDER BY created_at, id;")) {
            this.graphDiffs.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM planner_feedback ORDER BY created_at, id;")) {
            this.plannerFeedback.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM jobs ORDER BY created_at, id;")) {
            this.jobs.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM draft_evaluations ORDER BY created_at, id;")) {
            this.draftEvaluations.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM workspaces ORDER BY created_at, id;")) {
            this.workspaces.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM agent_runs ORDER BY started_at, id;")) {
            this.agentRuns.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM agent_artifacts ORDER BY created_at, id;")) {
            this.agentArtifacts.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM generated_node_tests ORDER BY created_at, id;")) {
            this.generatedNodeTestReports.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM generated_node_eval_reports ORDER BY created_at, id;")) {
            this.generatedNodeEvalReports.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM deployments ORDER BY created_at, id;")) {
            this.deployments.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM connectors ORDER BY created_at, id;")) {
            this.connectors.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM workflow_schedules ORDER BY next_fire_at, id;")) {
            this.schedules.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM alert_policies ORDER BY updated_at, id;")) {
            const policy = parseJson(row.record_json);
            this.alertPolicies.set(policyKey(policy.workflowId, policy.branchId), policy);
        }
        for (const row of this.queryRows("SELECT * FROM retention_policies ORDER BY updated_at, id;")) {
            const policy = parseJson(row.record_json);
            this.retentionPolicies.set(policyKey(policy.workflowId, policy.branchId), policy);
        }
        for (const row of this.queryRows("SELECT * FROM budget_policies ORDER BY updated_at, id;")) {
            const policy = parseJson(row.record_json);
            this.budgetPolicies.set(budgetPolicyKey(policy.workflowId, policy.branchId), policy);
        }
        for (const row of this.queryRows("SELECT * FROM budget_ledgers ORDER BY created_at, id;")) {
            this.budgetLedgers.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM agent_timeline_events ORDER BY timestamp, id;")) {
            this.agentTimelineEvents.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM node_decision_traces ORDER BY created_at, id;")) {
            this.nodeDecisionTraces.set(row.id, parseJson(row.record_json));
        }
        for (const row of this.queryRows("SELECT * FROM agent_memories ORDER BY updated_at, id;")) {
            this.agentMemories.set(row.id, parseJson(row.record_json));
        }
    }
    persistJob(job) {
        this.runSql([
            "INSERT OR REPLACE INTO jobs (id, type, status, workflow_id, created_at, updated_at, correlation_id, record_json)",
            `VALUES (${sqlValue(job.id)}, ${sqlValue(job.type)}, ${sqlValue(job.status)}, ${sqlValue(job.workflowId)}, ${sqlValue(job.createdAt)}, ${sqlValue(job.updatedAt)}, ${sqlValue(job.correlationId)}, ${sqlValue(stableStringify(job))});`,
            ...job.events.map((event) => `INSERT OR REPLACE INTO job_events (id, job_id, timestamp, event_json) VALUES (${sqlValue(event.id)}, ${sqlValue(job.id)}, ${sqlValue(event.timestamp)}, ${sqlValue(stableStringify(event))});`)
        ].join("\n"));
    }
    runSql(sql) {
        execFileSync(this.sqliteBin, [this.databasePath], {
            input: `PRAGMA foreign_keys = ON;\n${sql}\n`,
            encoding: "utf8"
        });
    }
    queryRows(sql) {
        const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
            encoding: "utf8"
        });
        return output.trim().length === 0 ? [] : JSON.parse(output);
    }
}
export function hashWorkflowDag(workflow) {
    return hashNanoClawWorkflowDag(workflow);
}
export function calculateNodeOrder(workflow) {
    const validation = validateWorkflowSpec(workflow);
    if (!validation.ok) {
        throw new Error(validation.errors.map((error) => error.code).join(", "));
    }
    const indegrees = new Map(workflow.nodes.map((node) => [node.id, 0]));
    const outgoing = new Map(workflow.nodes.map((node) => [node.id, []]));
    for (const edge of workflow.edges) {
        outgoing.get(edge.source.nodeId)?.push(edge.target.nodeId);
        indegrees.set(edge.target.nodeId, (indegrees.get(edge.target.nodeId) ?? 0) + 1);
    }
    const ready = [...indegrees.entries()]
        .filter(([, indegree]) => indegree === 0)
        .map(([nodeId]) => nodeId)
        .sort();
    const order = [];
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
function draftFingerprint(workflow) {
    return stableWorkflowStringify({
        ...workflow,
        revision: 1,
        approval: null,
        updatedAt: workflow.createdAt
    });
}
export function defaultBranchId(workflowId) {
    return `branch.${workflowId}.main`;
}
function budgetPolicyKey(workflowId, branchId) {
    return `${workflowId}:${branchId ?? "workflow"}`;
}
function policyKey(workflowId, branchId) {
    return `${workflowId}:${branchId ?? "workflow"}`;
}
function draftRevisionId(workflowId, branchId, revision, existing) {
    const index = existing?.draftRevisionIds.length ?? 0;
    if (branchId === defaultBranchId(workflowId)) {
        return `draft.${workflowId}.r${revision}.${index}`;
    }
    return `draft.${workflowId}.${sanitizeRecordIdPart(branchId)}.r${revision}.${index}`;
}
function approvedRevisionId(workflowId, branchId, revision) {
    if (branchId === defaultBranchId(workflowId)) {
        return `approved.${workflowId}.r${revision}`;
    }
    return `approved.${workflowId}.${sanitizeRecordIdPart(branchId)}.r${revision}`;
}
function sanitizeRecordIdPart(value) {
    return value.replace(/[^a-zA-Z0-9._-]+/gu, "_");
}
function assertImmutableRecordUnchanged(kind, id, existing, next) {
    if (stableStringify(existing) !== stableStringify(next)) {
        throw new Error(`Immutable ${kind} '${id}' cannot be changed.`);
    }
}
function stableStringify(value) {
    return stableJsonStringify(value);
}
function parseJson(value) {
    return JSON.parse(value);
}
function sqlValue(value) {
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
    `CREATE TABLE IF NOT EXISTS run_checkpoints (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    approved_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    status TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    record_json TEXT NOT NULL
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
    `CREATE TABLE IF NOT EXISTS connectors (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    adapter_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
    `CREATE TABLE IF NOT EXISTS workflow_schedules (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    deployment_id TEXT NOT NULL,
    approved_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    status TEXT NOT NULL,
    next_fire_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
    `CREATE TABLE IF NOT EXISTS alert_policies (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    branch_id TEXT,
    updated_at TEXT NOT NULL,
    record_json TEXT NOT NULL
  );`,
    `CREATE TABLE IF NOT EXISTS retention_policies (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    branch_id TEXT,
    updated_at TEXT NOT NULL,
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
    `CREATE TABLE IF NOT EXISTS agent_memories (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    namespace TEXT NOT NULL,
    scope TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT,
    record_json TEXT NOT NULL
  );`,
    "INSERT OR IGNORE INTO schema_migrations (id) VALUES ('0001_phase7_enterprise_store');"
];
//# sourceMappingURL=store.js.map