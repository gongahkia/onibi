import { randomUUID } from "node:crypto";
import { join } from "node:path";
import Fastify from "fastify";
import {
  LocalCodegenArtifactStore,
  checksumArtifactContent,
  createGeneratedArtifact
} from "@kelpclaw/codegen";
import { registerPromotedSkill } from "@kelpclaw/skill-registry";
import {
  AdapterBackedNodeRunner,
  DockerNodeRunner,
  MockNodeRunner,
  compileWorkflowDag,
  executeCompiledDag
} from "@kelpclaw/nanoclaw";
import {
  WorkflowValidationError,
  gmailReceiptsToSheetsWorkflowFixture,
  redactSecretString,
  stableJsonStringify,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type { FastifyInstance, FastifyRequest } from "fastify";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type { SkillMetadata } from "@kelpclaw/skill-registry";
import type {
  WorkflowArtifactManifestRecord,
  WorkflowAuditAction,
  WorkflowAuditAdapterCallRecord,
  WorkflowAuditContainerRecord,
  WorkflowAuditDeliveryRecord,
  WorkflowAuditRecord,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowEventSeverity,
  WorkflowNode,
  WorkflowObservabilityEventKind,
  WorkflowPlanRequest,
  WorkflowPlanResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowRunEvent,
  WorkflowSpec,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowValidationIssue,
  WorkflowValidateRequest,
  WorkflowValidateResponse
} from "@kelpclaw/workflow-spec";
import {
  createPlannerBackendFromEnv,
  planMockWorkflowDraft,
  planWorkflowDraft,
  repromptWorkflow
} from "./planner.js";
import { InMemoryWorkflowStore, SqliteWorkflowStore } from "./store.js";
import type { RevisionInput, WorkflowStore } from "./store.js";
import type { WorkflowPlannerBackend } from "./planner.js";

interface RouteParamsWithId {
  readonly id: string;
}

interface CodegenRouteParams extends RouteParamsWithId {
  readonly nodeId: string;
}

interface RunRouteParams {
  readonly id: string;
  readonly runId: string;
}

interface ApprovalRequestBody {
  readonly approvedBy: string;
}

interface CodegenReviewRequestBody {
  readonly status: "approved" | "rejected";
  readonly reviewedBy: string;
  readonly notes?: string | undefined;
}

interface MockPlanRequestBody {
  readonly name?: string;
}

export interface ApiAppOptions {
  readonly store?: WorkflowStore | undefined;
  readonly planner?: WorkflowPlannerBackend | undefined;
  readonly artifactStore?: CodegenArtifactStore | undefined;
}

export function createConfiguredWorkflowStore(): WorkflowStore {
  if (process.env.KELPCLAW_WORKFLOW_STORE === "memory") {
    return new InMemoryWorkflowStore();
  }

  return new SqliteWorkflowStore({
    databasePath:
      process.env.KELPCLAW_WORKFLOW_DB ?? join(process.cwd(), ".kelpclaw", "workflow.sqlite")
  });
}

export function buildApiApp(options: ApiAppOptions = {}): FastifyInstance {
  const app = Fastify({
    logger: false
  });
  const store = options.store ?? new InMemoryWorkflowStore();
  const artifactStore = options.artifactStore ?? new LocalCodegenArtifactStore();
  const planner = options.planner ?? createPlannerBackendFromEnv({ artifactStore });

  app.get("/health", async () => ({
    status: "ok",
    service: "kelpclaw-api"
  }));

  app.post<{ Body: MockPlanRequestBody }>("/api/plans/mock", async (request) => {
    const prompt = request.body?.name ?? gmailReceiptsToSheetsWorkflowFixture.prompt;
    const workflow = planMockWorkflowDraft({ prompt });

    return {
      workflow
    };
  });

  app.post<{ Body: WorkflowPlanRequest; Reply: WorkflowPlanResponse }>(
    "/api/workflows/plan",
    async (request, reply) => {
      let workflow: WorkflowSpec;
      try {
        workflow = await planWorkflowDraft(request.body, planner);
      } catch (error) {
        return reply.code(503).send({
          ok: false,
          error: "PLANNER_BACKEND_UNAVAILABLE",
          message:
            error instanceof Error
              ? redactSecretString(error.message)
              : "Planner backend is unavailable."
        } as never);
      }
      const validation = validateWorkflowSpec(workflow);
      if (!validation.ok) {
        return reply.code(500).send({
          ok: false,
          error: "PLANNER_GENERATED_INVALID_WORKFLOW",
          message: validation.errors.map((error) => error.code).join(", "),
          validation
        } as never);
      }

      const draftRevision = store.saveDraftRevision(validation.workflow, validation, "plan", {
        force: true,
        preserveRevision: true
      });
      persistCodegenArtifactManifests(
        store,
        draftRevision.workflow,
        draftRevision.id,
        draftRevision.createdAt
      );
      recordAudit(store, {
        action: "workflow.created",
        actor: "planner",
        workflowId: draftRevision.workflowId,
        revisionId: draftRevision.id,
        correlationId: correlationIdForRequest(request),
        summary: "Planned workflow draft revision.",
        secretRefs: collectSecretRefs(draftRevision.workflow)
      });

      return {
        ok: true,
        workflow: draftRevision.workflow,
        draftRevision,
        validation: draftRevision.validation
      };
    }
  );

  app.post("/api/workflows/validate", async (request) => {
    const input = isValidateRequest(request.body) ? request.body.workflow : request.body;
    return validateWorkflowSpec(input);
  });

  app.post("/api/workflows", async (request, reply) => {
    const validation = validateWorkflowSpec(request.body);
    if (!validation.ok) {
      return reply.code(422).send(validation);
    }

    const stored = store.saveWorkflow(validation.workflow, validation);
    const draftRevision = stored.draftRevisions.at(-1);
    recordAudit(store, {
      action: "workflow.created",
      actor: "api",
      workflowId: stored.workflow.id,
      revisionId: draftRevision?.id ?? `draft.${stored.workflow.id}.r${stored.workflow.revision}`,
      correlationId: correlationIdForRequest(request),
      summary: "Created workflow draft revision.",
      secretRefs: collectSecretRefs(stored.workflow)
    });
    return reply.code(201).send({
      ok: true,
      workflow: stored.workflow
    });
  });

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({ error: "WORKFLOW_NOT_FOUND" });
    }

    return stored;
  });

  app.post<{
    Params: RouteParamsWithId;
    Body: WorkflowValidateRequest;
    Reply: WorkflowValidateResponse;
  }>("/api/workflows/:id/validate", async (request, reply) => {
    if (request.body.workflow.id !== request.params.id) {
      return reply.code(409).send({
        ok: false,
        validation: {
          ok: false,
          errors: [
            {
              code: "WORKFLOW_SCHEMA_INVALID",
              message: `Workflow id '${request.body.workflow.id}' does not match route id '${request.params.id}'.`,
              path: ["id"]
            }
          ]
        }
      });
    }

    const validation = validateWorkflowSpec(request.body.workflow);
    if (!validation.ok) {
      return {
        ok: false,
        validation
      };
    }

    const draftRevision = store.saveDraftRevision(validation.workflow, validation, "validate");
    persistCodegenArtifactManifests(
      store,
      draftRevision.workflow,
      draftRevision.id,
      draftRevision.createdAt
    );
    recordAudit(store, {
      action: "workflow.edited",
      actor: "validator",
      workflowId: draftRevision.workflowId,
      revisionId: draftRevision.id,
      correlationId: correlationIdForRequest(request),
      summary: "Validated workflow draft revision.",
      secretRefs: collectSecretRefs(draftRevision.workflow)
    });
    return {
      ok: true,
      validation: draftRevision.validation,
      workflow: draftRevision.workflow,
      draftRevision
    };
  });

  app.post<{ Params: RouteParamsWithId; Body: ApprovalRequestBody }>(
    "/api/workflows/:id/approvals",
    async (request, reply) => {
      try {
        const issues = await validateCodegenApprovalReadiness(
          store.requireWorkflow(request.params.id).workflow,
          artifactStore
        );
        if (issues.length > 0) {
          return reply.code(409).send({
            error: issues[0]?.code ?? "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
            issues
          });
        }
        const approvedRevision = store.approveWorkflow(request.params.id, request.body.approvedBy);
        const workflow = approvedRevision.workflow;
        recordAudit(store, {
          action: "workflow.approved",
          actor: request.body.approvedBy,
          workflowId: approvedRevision.workflowId,
          revisionId: approvedRevision.id,
          correlationId: correlationIdForRequest(request),
          summary: "Approved workflow revision.",
          diff: approvedRevision.diff,
          secretRefs: collectSecretRefs(workflow),
          approvedArtifactRefs: collectCodegenArtifactRefs(workflow)
        });
        return {
          workflowId: workflow.id,
          revision: workflow.revision,
          approval: workflow.approval,
          approvedRevisionId: approvedRevision.id,
          approvedRevision,
          workflow
        };
      } catch (error) {
        return reply.code(404).send({
          error: "WORKFLOW_NOT_FOUND",
          message: error instanceof Error ? error.message : "Workflow was not found."
        });
      }
    }
  );

  app.post<{
    Params: RouteParamsWithId;
    Body: WorkflowRepromptNodeRequest;
    Reply: WorkflowRepromptNodeResponse;
  }>("/api/workflows/:id/reprompt-node", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    const sourceWorkflow = request.body.currentWorkflow ?? stored?.workflow;
    if (!sourceWorkflow) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      } as never);
    }
    if (sourceWorkflow.id !== request.params.id) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_ID_MISMATCH",
        message: `Workflow id '${sourceWorkflow.id}' does not match route id '${request.params.id}'.`
      } as never);
    }

    try {
      const reprompted = repromptWorkflow(sourceWorkflow, request.body);
      const validation = validateWorkflowSpec(reprompted.workflow);
      if (!validation.ok) {
        return reply.code(422).send({
          ok: false,
          error: "WORKFLOW_REPROMPT_INVALID",
          message: validation.errors.map((error) => error.code).join(", "),
          validation
        } as never);
      }

      const draftRevision = store.saveDraftRevision(validation.workflow, validation, "reprompt");
      persistCodegenArtifactManifests(
        store,
        draftRevision.workflow,
        draftRevision.id,
        draftRevision.createdAt
      );
      recordAudit(store, {
        action: "workflow.edited",
        actor: "planner",
        workflowId: draftRevision.workflowId,
        revisionId: draftRevision.id,
        nodeId: request.body.nodeId,
        correlationId: correlationIdForRequest(request),
        summary: "Reprompted workflow node.",
        diff: reprompted.diff,
        secretRefs: collectSecretRefs(draftRevision.workflow)
      });
      return {
        ok: true,
        workflow: draftRevision.workflow,
        draftRevision,
        validation: draftRevision.validation,
        before: reprompted.before,
        after: reprompted.after,
        diff: reprompted.diff
      };
    } catch (error) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NODE_NOT_FOUND",
        message: error instanceof Error ? error.message : "Workflow node was not found."
      } as never);
    }
  });

  app.post<{
    Params: RouteParamsWithId;
    Body: WorkflowApproveRequest;
    Reply: WorkflowApproveResponse;
  }>("/api/workflows/:id/approve", async (request, reply) => {
    if (request.body.workflow.id !== request.params.id) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_ID_MISMATCH",
        message: `Workflow id '${request.body.workflow.id}' does not match route id '${request.params.id}'.`
      } as never);
    }

    const validation = validateWorkflowSpec(request.body.workflow);
    if (!validation.ok) {
      return reply.code(422).send({
        ok: false,
        error: "WORKFLOW_APPROVAL_INVALID",
        message: validation.errors.map((error) => error.code).join(", "),
        validation
      } as never);
    }

    try {
      const codegenIssues = await validateCodegenApprovalReadiness(
        validation.workflow,
        artifactStore
      );
      if (codegenIssues.length > 0) {
        return reply.code(409).send({
          ok: false,
          error: codegenIssues[0]?.code ?? "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
          message: codegenIssues.map((issue) => issue.message).join(", "),
          issues: codegenIssues
        } as never);
      }
      const approvedRevision = store.approveWorkflow(
        request.params.id,
        request.body.approvedBy,
        validation.workflow
      );
      recordAudit(store, {
        action: "workflow.approved",
        actor: request.body.approvedBy,
        workflowId: approvedRevision.workflowId,
        revisionId: approvedRevision.id,
        correlationId: correlationIdForRequest(request),
        summary: "Approved workflow revision.",
        diff: approvedRevision.diff,
        secretRefs: collectSecretRefs(approvedRevision.workflow),
        approvedArtifactRefs: collectCodegenArtifactRefs(approvedRevision.workflow)
      });
      return {
        ok: true,
        workflowId: approvedRevision.workflowId,
        approvedRevisionId: approvedRevision.id,
        approvedRevision,
        workflow: approvedRevision.workflow,
        diff: approvedRevision.diff
      };
    } catch (error) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: error instanceof Error ? error.message : "Workflow was not found."
      } as never);
    }
  });

  app.post<{
    Params: CodegenRouteParams;
    Body: CodegenReviewRequestBody;
  }>("/api/workflows/:id/codegen/:nodeId/review", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      });
    }

    const node = stored.workflow.nodes.find((candidate) => candidate.id === request.params.nodeId);
    if (node?.kind !== "codegen" || !node.codegen) {
      return reply.code(404).send({
        ok: false,
        error: "CODEGEN_NODE_NOT_FOUND",
        message: `Codegen node '${request.params.nodeId}' was not found.`
      });
    }

    const now = new Date().toISOString();
    const review = {
      status: request.body.status,
      reviewedBy: request.body.reviewedBy,
      reviewedAt: now,
      ...(request.body.notes === undefined ? {} : { notes: request.body.notes })
    };
    const reviewedCodegen = {
      ...node.codegen,
      review
    };
    const reviewedWorkflow: WorkflowSpec = {
      ...stored.workflow,
      approval: null,
      updatedAt: now,
      nodes: stored.workflow.nodes.map((candidate) =>
        candidate.id === node.id
          ? {
              ...candidate,
              config: {
                ...candidate.config,
                artifactStatus: request.body.status,
                reviewedAt: now
              },
              codegen: reviewedCodegen
            }
          : candidate
      )
    };
    const validation = validateWorkflowSpec(reviewedWorkflow);
    if (!validation.ok) {
      return reply.code(422).send({
        ok: false,
        error: "CODEGEN_REVIEW_INVALID",
        message: validation.errors.map((error) => error.code).join(", "),
        validation
      });
    }

    const draftRevision = store.saveDraftRevision(validation.workflow, validation, "validate", {
      force: true
    });
    persistCodegenArtifactManifests(
      store,
      draftRevision.workflow,
      draftRevision.id,
      draftRevision.createdAt
    );
    recordAudit(store, {
      action: "codegen.reviewed",
      actor: request.body.reviewedBy,
      workflowId: draftRevision.workflowId,
      revisionId: draftRevision.id,
      nodeId: node.id,
      correlationId: correlationIdForRequest(request),
      summary: `Reviewed generated code as ${request.body.status}.`,
      approvedArtifactRefs: request.body.status === "approved" ? node.codegen.artifacts : undefined
    });
    return {
      ok: true,
      workflow: draftRevision.workflow,
      draftRevision,
      validation: draftRevision.validation,
      node: draftRevision.workflow.nodes.find((candidate) => candidate.id === node.id)
    };
  });

  app.post<{
    Params: CodegenRouteParams;
  }>("/api/workflows/:id/codegen/:nodeId/promote", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      });
    }

    const node = stored.workflow.nodes.find((candidate) => candidate.id === request.params.nodeId);
    if (node?.kind !== "codegen" || !node.codegen) {
      return reply.code(404).send({
        ok: false,
        error: "CODEGEN_NODE_NOT_FOUND",
        message: `Codegen node '${request.params.nodeId}' was not found.`
      });
    }
    if (node.codegen.review.status !== "approved") {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
        message: `Codegen node '${node.id}' must be approved before promotion.`
      });
    }

    const skill = createPromotedSkill(stored.workflow, node);
    const artifact = createGeneratedArtifact({
      path: `promoted-skills/${skill.id}.json`,
      content: JSON.stringify(skill, null, 2),
      contentType: "application/json"
    });
    const storedArtifact = await artifactStore.putArtifact(artifact);
    const loadedSkill = JSON.parse(
      await artifactStore.readArtifact(storedArtifact.ref)
    ) as SkillMetadata;
    const promotedSkill = registerPromotedSkill(loadedSkill);

    return {
      ok: true,
      skill: promotedSkill,
      artifact: storedArtifact.ref
    };
  });

  app.post<{
    Params: RouteParamsWithId;
    Body: WorkflowStartRunRequest;
    Reply: WorkflowStartRunResponse;
  }>("/api/workflows/:id/runs", async (request, reply) => {
    const approvedRevision = store.getApprovedRevision(request.body.approvedRevisionId);
    if (!approvedRevision) {
      return reply.code(404).send({
        ok: false,
        error: "APPROVED_REVISION_NOT_FOUND",
        message: `Approved revision '${request.body.approvedRevisionId}' was not found.`
      } as never);
    }
    if (approvedRevision.workflowId !== request.params.id) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_RUN_ID_MISMATCH",
        message: `Approved revision '${approvedRevision.id}' belongs to workflow '${approvedRevision.workflowId}'.`
      } as never);
    }

    try {
      const correlationId = correlationIdForRequest(request);
      const runId = `run.${approvedRevision.workflowId}.r${approvedRevision.revision}.${Date.now()}`;
      const dag = compileWorkflowDag(approvedRevision.workflow);
      const compiledAt = new Date().toISOString();
      const result = await executeCompiledDag(dag, createNanoClawRunner(), {
        codegenArtifactStore: artifactStore,
        runId
      });
      const now = new Date().toISOString();
      const events = enrichRunEvents(
        [
          createStructuredRunEvent({
            id: "event.dag.compiled",
            timestamp: compiledAt,
            level: "info",
            message: "NanoClaw DAG compiled.",
            kind: "dag.compilation",
            metadata: {
              dagHash: dag.dagHash,
              nodeOrder: [...dag.order]
            }
          }),
          ...(result.events ?? createRunEvents(result.nodeResults, now))
        ],
        {
          workflowId: approvedRevision.workflowId,
          revisionId: approvedRevision.id,
          runId,
          correlationId
        }
      );
      const run = store.saveRun({
        id: runId,
        workflowId: approvedRevision.workflowId,
        approvedRevisionId: approvedRevision.id,
        revision: approvedRevision.revision,
        status: result.status,
        createdAt: now,
        startedAt: result.startedAt,
        finishedAt: result.finishedAt,
        events,
        result
      });
      recordRunAuditRecords(
        store,
        approvedRevision.workflow,
        approvedRevision.id,
        run,
        correlationId
      );

      return reply.code(202).send({
        ok: true,
        run
      });
    } catch (error) {
      if (error instanceof WorkflowValidationError) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_APPROVAL_REQUIRED",
          message: error.message,
          issues: error.issues
        } as never);
      }

      throw error;
    }
  });

  app.get<{ Params: RunRouteParams }>("/api/workflows/:id/runs/:runId", async (request, reply) => {
    const run = store.getRun(request.params.runId);
    if (!run) {
      return reply.code(404).send({
        ok: false,
        error: "RUN_NOT_FOUND",
        message: `Run '${request.params.runId}' was not found.`
      });
    }
    if (run.workflowId !== request.params.id) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_RUN_ID_MISMATCH",
        message: `Run '${run.id}' belongs to workflow '${run.workflowId}'.`
      });
    }

    return {
      ok: true,
      run
    };
  });

  app.get<{ Params: RunRouteParams }>(
    "/api/workflows/:id/runs/:runId/events",
    async (request, reply) => {
      const run = store.getRun(request.params.runId);
      if (!run) {
        return reply.code(404).send({
          ok: false,
          error: "RUN_NOT_FOUND",
          message: `Run '${request.params.runId}' was not found.`
        });
      }
      if (run.workflowId !== request.params.id) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_RUN_ID_MISMATCH",
          message: `Run '${run.id}' belongs to workflow '${run.workflowId}'.`
        });
      }

      return {
        ok: true,
        events: store.listRunEvents(run.id)
      };
    }
  );

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/audit", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      });
    }

    return {
      ok: true,
      audit: store.listAuditRecords(request.params.id)
    };
  });

  app.get<{ Params: RouteParamsWithId & { readonly revisionId: string } }>(
    "/api/workflows/:id/revisions/:revisionId",
    async (request, reply) => {
      const revision = store.getWorkflowRevision(request.params.revisionId);
      if (!revision) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_REVISION_NOT_FOUND",
          message: `Workflow revision '${request.params.revisionId}' was not found.`
        });
      }

      const workflowId =
        revision.approvedRevision?.workflowId ?? revision.draftRevision?.workflowId ?? null;
      if (workflowId !== request.params.id) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_REVISION_ID_MISMATCH",
          message: `Workflow revision '${request.params.revisionId}' belongs to workflow '${workflowId}'.`
        });
      }

      return {
        ok: true,
        ...revision
      };
    }
  );

  app.post<{ Params: RouteParamsWithId; Body: RevisionInput }>(
    "/api/workflows/:id/revisions",
    async (request, reply) => {
      try {
        const updated = store.createRevision(request.params.id, request.body);
        const latestDraft = store.getLatestDraftRevision(request.params.id);
        if (latestDraft) {
          recordAudit(store, {
            action: "workflow.edited",
            actor: "api",
            workflowId: latestDraft.workflowId,
            revisionId: latestDraft.id,
            correlationId: correlationIdForRequest(request),
            summary: "Created workflow draft revision.",
            secretRefs: collectSecretRefs(latestDraft.workflow)
          });
        }
        return reply.code(201).send({
          workflowId: updated.workflow.id,
          revision: updated.workflow.revision,
          workflow: updated.workflow
        });
      } catch (error) {
        return reply.code(422).send({
          error: "WORKFLOW_REVISION_INVALID",
          message: error instanceof Error ? error.message : "Workflow revision was invalid."
        });
      }
    }
  );

  app.post<{ Params: RouteParamsWithId }>(
    "/api/workflows/:id/executions",
    async (request, reply) => {
      const stored = store.getWorkflow(request.params.id);
      if (!stored) {
        return reply.code(404).send({ error: "WORKFLOW_NOT_FOUND" });
      }
      const approvedRevision = store.getLatestApprovedRevision(request.params.id);
      const workflow = approvedRevision?.workflow ?? stored.workflow;

      try {
        const dag = compileWorkflowDag(workflow);
        const result = await executeCompiledDag(dag, createNanoClawRunner(), {
          codegenArtifactStore: artifactStore
        });
        const execution = store.saveExecution({
          id: result.id,
          workflowId: workflow.id,
          revision: workflow.revision,
          createdAt: result.startedAt,
          result
        });

        return reply.code(202).send(execution);
      } catch (error) {
        if (error instanceof WorkflowValidationError) {
          return reply.code(409).send({
            error: "WORKFLOW_APPROVAL_REQUIRED",
            issues: error.issues
          });
        }

        throw error;
      }
    }
  );

  app.get<{ Params: RouteParamsWithId }>("/api/executions/:id", async (request, reply) => {
    const execution = store.getExecution(request.params.id);
    if (!execution) {
      return reply.code(404).send({ error: "EXECUTION_NOT_FOUND" });
    }

    return execution;
  });

  return app;
}

export type { WorkflowSpec };

function isValidateRequest(input: unknown): input is WorkflowValidateRequest {
  return (
    typeof input === "object" &&
    input !== null &&
    "workflow" in input &&
    typeof (input as WorkflowValidateRequest).workflow === "object"
  );
}

async function validateCodegenApprovalReadiness(
  workflow: WorkflowSpec,
  artifactStore: CodegenArtifactStore
): Promise<readonly WorkflowValidationIssue[]> {
  const issues: WorkflowValidationIssue[] = [];

  for (const [index, node] of workflow.nodes.entries()) {
    if (node.kind !== "codegen" || !node.codegen) {
      continue;
    }

    if (node.codegen.review.status !== "approved") {
      issues.push({
        code: "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
        message: `Codegen node '${node.id}' must be reviewed before workflow approval.`,
        path: ["nodes", index, "codegen", "review", "status"]
      });
    }

    for (const artifact of node.codegen.artifacts) {
      if (!(await artifactStore.verifyArtifact(artifact))) {
        issues.push({
          code: "WORKFLOW_CODEGEN_ARTIFACT_DRIFT",
          message: `Codegen artifact '${artifact.path}' is missing or has hash drift.`,
          path: ["nodes", index, "codegen", "artifacts"]
        });
      }
    }
  }

  return issues;
}

function createPromotedSkill(workflow: WorkflowSpec, node: WorkflowNode): SkillMetadata {
  const capability = promotionCapability(node);

  return {
    id: `skill.promoted.${slugify(`${workflow.id}-${node.id}`)}`,
    name: node.label,
    version: "1.0.0",
    description: node.description,
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities: [capability],
    inputSchema: node.inputs,
    outputSchema: node.outputs,
    requiredSecrets: [],
    adapterDependencies: [],
    adapterOperations: [],
    runtimeTemplate: node.runtime,
    metaprompt: `Select this promoted skill when a workflow asks to ${node.codegen?.latestPrompt ?? node.description}.`,
    validationRules: [
      "promoted from an approved codegen node",
      "fixture output must satisfy the promoted output schema"
    ],
    examples: [
      {
        id: `example.${slugify(node.id)}`,
        description: `Fixture for ${node.label}.`,
        input: defaultFixturePayload(node.inputs),
        output: defaultFixturePayload(node.outputs)
      }
    ],
    source: "promoted",
    promotedFromNodeId: node.id
  };
}

function promotionCapability(node: WorkflowNode): string {
  const text =
    `${node.label} ${node.description} ${node.codegen?.latestPrompt ?? ""}`.toLowerCase();
  if (text.includes("scrape") || text.includes("status page")) {
    return "public-status-scrape";
  }
  if (text.includes("regex")) {
    return "regex-parser";
  }
  if (text.includes("api")) {
    return "ad-hoc-api-call";
  }

  return `promoted-${slugify(node.id)}`;
}

function defaultFixturePayload(
  schemas: Readonly<Record<string, unknown>>
): Readonly<Record<string, unknown>> {
  return Object.fromEntries(Object.keys(schemas).map((port) => [port, { fixture: true }]));
}

function slugify(value: string): string {
  return (
    value
      .toLowerCase()
      .replace(/[^a-z0-9]+/gu, "-")
      .replace(/^-+|-+$/gu, "")
      .slice(0, 72) || "generated-skill"
  );
}

function createNanoClawRunner(): AdapterBackedNodeRunner | DockerNodeRunner {
  if (process.env.NANOCLAW_RUNNER === "docker") {
    return new DockerNodeRunner({
      dockerBin: process.env.NANOCLAW_DOCKER_BIN,
      hostWorkspace: process.env.NANOCLAW_HOST_WORKSPACE ?? process.cwd()
    });
  }

  return new AdapterBackedNodeRunner({
    fallbackRunner: new MockNodeRunner()
  });
}

function correlationIdForRequest(request: FastifyRequest): string {
  const header = request.headers["x-correlation-id"];
  if (Array.isArray(header)) {
    return header[0] ?? `corr.${randomUUID()}`;
  }

  return typeof header === "string" && header.length > 0 ? header : `corr.${randomUUID()}`;
}

function recordAudit(
  store: WorkflowStore,
  input: Omit<WorkflowAuditRecord, "id" | "timestamp">
): WorkflowAuditRecord {
  return store.saveAuditRecord({
    ...input,
    id: `audit.${input.action}.${Date.now()}.${randomUUID()}`,
    timestamp: new Date().toISOString()
  });
}

function persistCodegenArtifactManifests(
  store: WorkflowStore,
  workflow: WorkflowSpec,
  revisionId: string,
  createdAt: string
): readonly WorkflowArtifactManifestRecord[] {
  return workflow.nodes
    .filter((node) => node.kind === "codegen" && node.codegen)
    .map((node) => {
      const artifacts = node.codegen?.artifacts ?? [];
      return store.saveArtifactManifest({
        id: `manifest.${workflow.id}.${revisionId}.${node.id}`,
        workflowId: workflow.id,
        revisionId,
        createdAt,
        artifacts,
        manifestChecksum: checksumArtifactContent(stableJsonStringify(artifacts as never))
      });
    });
}

function collectSecretRefs(workflow: WorkflowSpec): readonly string[] {
  return [
    ...new Set(
      workflow.nodes.flatMap((node) =>
        Object.values(node.secretRefs ?? {}).filter((secretRef) => secretRef.length > 0)
      )
    )
  ].sort();
}

function collectCodegenArtifactRefs(workflow: WorkflowSpec) {
  return workflow.nodes.flatMap((node) => node.codegen?.artifacts ?? []);
}

function recordRunAuditRecords(
  store: WorkflowStore,
  workflow: WorkflowSpec,
  revisionId: string,
  run: {
    readonly id: string;
    readonly status: string;
    readonly events: readonly WorkflowRunEvent[];
    readonly result: WorkflowStartRunResponse["run"]["result"];
  },
  correlationId: string
): void {
  if (!run.result) {
    return;
  }

  const nodesById = new Map(workflow.nodes.map((node) => [node.id, node]));
  const resultByNodeId = new Map(run.result.nodeResults.map((result) => [result.nodeId, result]));

  for (const node of workflow.nodes) {
    const result = resultByNodeId.get(node.id);
    if (!result) {
      continue;
    }

    recordAudit(store, {
      action: "container.ran",
      actor: "nanoclaw",
      workflowId: workflow.id,
      revisionId,
      runId: run.id,
      nodeId: node.id,
      correlationId,
      summary: `Node '${node.id}' ${result.status}.`,
      container: containerAuditRecord(node, result)
    });

    for (const adapterCall of adapterAuditRecords(node, result.status === "succeeded")) {
      recordAudit(store, {
        action: "adapter.called",
        actor: "nanoclaw",
        workflowId: workflow.id,
        revisionId,
        runId: run.id,
        nodeId: node.id,
        correlationId,
        summary: `Adapter '${adapterCall.adapterId}' ${adapterCall.status}.`,
        adapterCall,
        secretRefs: Object.values(node.secretRefs ?? {})
      });
    }

    const delivery = deliveryAuditRecord(node, result.output, result.status === "succeeded");
    if (delivery) {
      recordAudit(store, {
        action: "delivery.completed",
        actor: "nanoclaw",
        workflowId: workflow.id,
        revisionId,
        runId: run.id,
        nodeId: node.id,
        correlationId,
        summary: `Delivery node '${node.id}' ${delivery.status}.`,
        delivery
      });
    }
  }

  recordAudit(store, {
    action: "run.completed",
    actor: "nanoclaw",
    workflowId: workflow.id,
    revisionId,
    runId: run.id,
    correlationId,
    summary: `Run '${run.id}' ${run.status}.`,
    metadata: {
      eventCount: run.events.length,
      nodeCount: nodesById.size
    }
  });
}

function containerAuditRecord(
  node: WorkflowNode,
  result: { readonly workspacePath?: string | undefined; readonly metadata?: unknown }
): WorkflowAuditContainerRecord {
  const metadata = result.metadata as { readonly network?: unknown } | undefined;
  const network =
    metadata?.network === "bridge"
      ? "bridge"
      : (node.codegen?.sandbox.network ??
        (node.determinism.externalCalls.length > 0 || node.adapterId ? "declared" : "none"));

  return {
    image: node.runtime.image,
    command: [...node.runtime.command],
    network,
    workspacePath: result.workspacePath
  };
}

function adapterAuditRecords(
  node: WorkflowNode,
  succeeded: boolean
): readonly WorkflowAuditAdapterCallRecord[] {
  return (node.adapterOperations ?? []).map((operation) => ({
    adapterId: operation.adapterId,
    operation: operation.operation,
    operationVersion: operation.operationVersion,
    status: succeeded ? "succeeded" : "failed"
  }));
}

function deliveryAuditRecord(
  node: WorkflowNode,
  output: unknown,
  succeeded: boolean
): WorkflowAuditDeliveryRecord | undefined {
  if (node.kind !== "delivery") {
    return undefined;
  }

  const delivery = typeof output === "object" && output !== null ? output : {};
  const nested =
    "delivery" in delivery && typeof delivery.delivery === "object" && delivery.delivery !== null
      ? delivery.delivery
      : delivery;
  const channels =
    typeof nested === "object" &&
    nested !== null &&
    "channels" in nested &&
    Array.isArray(nested.channels)
      ? nested.channels.filter((channel): channel is string => typeof channel === "string")
      : declaredDeliveryChannels(node);

  return {
    channels,
    status: succeeded ? "succeeded" : "failed"
  };
}

function declaredDeliveryChannels(node: WorkflowNode): readonly string[] {
  const channels = node.config.channels;
  if (Array.isArray(channels)) {
    return channels.filter((channel): channel is string => typeof channel === "string");
  }

  return typeof node.config.channel === "string" ? [node.config.channel] : ["email"];
}

function enrichRunEvents(
  events: readonly WorkflowRunEvent[],
  context: {
    readonly workflowId: string;
    readonly revisionId: string;
    readonly runId: string;
    readonly correlationId: string;
  }
): readonly WorkflowRunEvent[] {
  return events.map((event) => ({
    ...event,
    severity: event.severity ?? severityForRunEvent(event),
    kind: event.kind ?? kindForRunEvent(event),
    workflowId: event.workflowId ?? context.workflowId,
    revisionId: event.revisionId ?? context.revisionId,
    runId: event.runId ?? context.runId,
    correlationId: event.correlationId ?? context.correlationId
  }));
}

function createStructuredRunEvent(
  input: WorkflowRunEvent & {
    readonly kind: WorkflowObservabilityEventKind;
    readonly metadata?: WorkflowRunEvent["metadata"];
  }
): WorkflowRunEvent {
  return input;
}

function severityForRunEvent(event: WorkflowRunEvent): WorkflowEventSeverity {
  return event.level === "error" ? "error" : "info";
}

function kindForRunEvent(event: WorkflowRunEvent): WorkflowObservabilityEventKind {
  if (event.nodeId) {
    return "node.container";
  }
  if (event.message.toLowerCase().includes("delivery")) {
    return "delivery.event";
  }

  return "run.lifecycle";
}

function createRunEvents(
  nodeResults: readonly { readonly nodeId: string; readonly status: string }[],
  timestamp: string
): readonly WorkflowRunEvent[] {
  const events: WorkflowRunEvent[] = [
    {
      id: "event.run.started",
      timestamp,
      level: "info",
      message: "NanoClaw run started."
    }
  ];

  for (const node of nodeResults) {
    events.push({
      id: `event.node.${node.nodeId}.${node.status}`,
      timestamp,
      level: node.status === "failed" ? "error" : "info",
      message: `Node '${node.nodeId}' ${node.status}.`,
      nodeId: node.nodeId
    });
  }

  events.push({
    id: "event.run.finished",
    timestamp,
    level: nodeResults.some((node) => node.status === "failed") ? "error" : "info",
    message: "NanoClaw run finished."
  });

  return events;
}
