import { randomUUID } from "node:crypto";
import { join } from "node:path";
import Fastify from "fastify";
import {
  LocalCodegenArtifactStore,
  checksumArtifactContent,
  createGeneratedArtifact
} from "@kelpclaw/codegen";
import { registerPromotedSkill } from "@kelpclaw/skill-registry";
import { createDefaultMockAdapters } from "@kelpclaw/adapters";
import {
  AdapterBackedNodeRunner,
  MockNodeRunner,
  ProductionNodeRunner,
  SecretStoreResolver,
  compileWorkflowDag,
  executeCompiledDag
} from "@kelpclaw/nanoclaw";
import {
  WorkflowValidationError,
  createWorkflowGraphDiff,
  createWorkflowPlannerFeedback,
  gmailReceiptsToSheetsWorkflowFixture,
  redactSecretString,
  stableJsonStringify,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type { FastifyInstance, FastifyRequest } from "fastify";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type { NodeRunner } from "@kelpclaw/nanoclaw";
import type { SkillMetadata } from "@kelpclaw/skill-registry";
import type {
  WorkflowArtifactManifestRecord,
  WorkflowAuditAdapterCallRecord,
  WorkflowAuditContainerRecord,
  WorkflowAuditDeliveryRecord,
  WorkflowAuditRecord,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowEventSeverity,
  WorkflowFeedbackRequest,
  WorkflowFeedbackResponse,
  WorkflowJob,
  WorkflowJobEvent,
  WorkflowJobType,
  JsonRecord,
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
  createDeterministicPlannerBackend,
  createPlannerBackendFromEnv,
  planMockWorkflowDraft,
  planWorkflowDraft,
  repromptWorkflow
} from "./planner.js";
import { routeWorkflowTask } from "./router.js";
import {
  InMemorySecretStore,
  SqliteSecretStore,
  consumeOAuthState,
  createOAuthState,
  secretReadiness
} from "./secrets.js";
import { InMemoryWorkflowStore, SqliteWorkflowStore } from "./store.js";
import type { SecretStore } from "./secrets.js";
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

interface JobRouteParams {
  readonly jobId: string;
}

interface ApprovalRequestBody {
  readonly approvedBy: string;
}

interface CreateJobRequestBody {
  readonly type: WorkflowJobType;
  readonly workflowId?: string | undefined;
  readonly revisionId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly maxAttempts?: number | undefined;
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
  readonly secretStore?: SecretStore | undefined;
  readonly adminToken?: string | null | undefined;
  readonly runner?: NodeRunner | undefined;
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

export function createConfiguredSecretStore(): SecretStore {
  if (process.env.KELPCLAW_SECRET_STORE === "memory") {
    return new InMemorySecretStore();
  }

  return new SqliteSecretStore({
    databasePath:
      process.env.KELPCLAW_SECRET_DB ??
      process.env.KELPCLAW_WORKFLOW_DB ??
      join(process.cwd(), ".kelpclaw", "workflow.sqlite"),
    masterKey: process.env.KELPCLAW_SECRET_MASTER_KEY ?? ""
  });
}

export function buildApiApp(options: ApiAppOptions = {}): FastifyInstance {
  const app = Fastify({
    logger: false
  });
  const store = options.store ?? new InMemoryWorkflowStore();
  const secretStore = options.secretStore ?? new InMemorySecretStore();
  const artifactStore = options.artifactStore ?? new LocalCodegenArtifactStore();
  const planner = options.planner ?? createPlannerBackendFromEnv({ artifactStore });
  const runner = options.runner;
  const adminToken =
    options.adminToken === undefined ? process.env.KELPCLAW_ADMIN_TOKEN : options.adminToken;

  app.addHook("preHandler", async (request, reply) => {
    if (!adminToken || isPublicRoute(request.method, request.url)) {
      return;
    }
    const header = request.headers.authorization;
    const expected = `Bearer ${adminToken}`;
    if (header !== expected) {
      return reply.code(401).send({
        ok: false,
        error: "UNAUTHORIZED",
        message: "A valid KelpClaw admin bearer token is required."
      });
    }
  });

  app.get("/health", async () => ({
    status: "ok",
    service: "kelpclaw-api"
  }));

  app.get("/api/secrets", async () => ({
    ok: true,
    secrets: publicSecretMetadata(secretStore),
    integrations: secretReadiness(secretStore)
  }));

  app.get("/api/integrations/status", async () => ({
    ok: true,
    integrations: secretReadiness(secretStore)
  }));

  app.post<{ Body: CreateJobRequestBody }>("/api/jobs", async (request, reply) => {
    const correlationId = correlationIdForRequest(request);
    const job = createJob({
      type: request.body.type,
      workflowId: request.body.workflowId,
      revisionId: request.body.revisionId,
      nodeId: request.body.nodeId,
      correlationId,
      maxAttempts: request.body.maxAttempts
    });
    const saved = store.saveJob(job);
    if (job.workflowId) {
      recordAudit(store, {
        action: "job.created",
        actor: "api",
        workflowId: job.workflowId,
        revisionId: job.revisionId ?? `job.${job.id}`,
        nodeId: job.nodeId,
        correlationId,
        summary: `Created ${job.type} job.`,
        metadata: {
          jobId: job.id,
          jobType: job.type
        }
      });
    }

    return reply.code(201).send({
      ok: true,
      job: saved
    });
  });

  app.get<{ Params: JobRouteParams }>("/api/jobs/:jobId", async (request, reply) => {
    const job = store.getJob(request.params.jobId);
    if (!job) {
      return reply.code(404).send({
        ok: false,
        error: "JOB_NOT_FOUND",
        message: `Job '${request.params.jobId}' was not found.`
      });
    }

    return {
      ok: true,
      job
    };
  });

  app.post<{ Params: JobRouteParams; Body: { readonly reason?: string | undefined } }>(
    "/api/jobs/:jobId/cancel",
    async (request, reply) => {
      const job = store.getJob(request.params.jobId);
      if (!job) {
        return reply.code(404).send({
          ok: false,
          error: "JOB_NOT_FOUND",
          message: `Job '${request.params.jobId}' was not found.`
        });
      }

      const now = new Date().toISOString();
      const cancelled =
        job.status === "succeeded" || job.status === "failed" || job.status === "cancelled"
          ? job
          : store.saveJob({
              ...job,
              status: "cancelled",
              updatedAt: now,
              finishedAt: now,
              cancelledAt: now,
              cancellationReason: request.body.reason ?? "Cancelled by API request."
            });
      const withEvent =
        cancelled.events.at(-1)?.message === "Job cancelled."
          ? cancelled
          : store.appendJobEvent(
              cancelled.id,
              createJobEvent(cancelled, "error", "Job cancelled.", {
                reason: request.body.reason ?? "Cancelled by API request."
              })
            );

      return {
        ok: true,
        job: withEvent
      };
    }
  );

  app.put<{ Body: { readonly name: string; readonly value: string } }>(
    "/api/secrets",
    async (request, reply) => {
      if (!request.body.name || typeof request.body.value !== "string") {
        return reply.code(422).send({
          ok: false,
          error: "SECRET_INVALID",
          message: "Secret name and value are required."
        });
      }

      return {
        ok: true,
        secret: secretStore.putSecret(request.body.name, request.body.value)
      };
    }
  );

  app.delete<{ Params: { readonly name: string } }>("/api/secrets/:name", async (request) => ({
    ok: true,
    deleted: secretStore.deleteSecret(request.params.name)
  }));

  app.get("/api/integrations/google/status", async () => ({
    ok: true,
    connected: (await secretStore.getSecretValue("google.oauth.default")) !== null
  }));

  app.get("/api/integrations/google/connect", async (_request, reply) => {
    const clientId = process.env.GOOGLE_CLIENT_ID;
    const redirectUri = googleRedirectUri();
    if (!clientId || !redirectUri) {
      return reply.code(503).send({
        ok: false,
        error: "GOOGLE_OAUTH_NOT_CONFIGURED",
        message: "GOOGLE_CLIENT_ID and KELPCLAW_PUBLIC_BASE_URL are required."
      });
    }
    const state = createOAuthState(secretStore);
    const url = new URL("https://accounts.google.com/o/oauth2/v2/auth");
    url.searchParams.set("client_id", clientId);
    url.searchParams.set("redirect_uri", redirectUri);
    url.searchParams.set("response_type", "code");
    url.searchParams.set("access_type", "offline");
    url.searchParams.set("prompt", "consent");
    url.searchParams.set(
      "scope",
      [
        "https://www.googleapis.com/auth/gmail.readonly",
        "https://www.googleapis.com/auth/spreadsheets"
      ].join(" ")
    );
    url.searchParams.set("state", state);

    return {
      ok: true,
      url: url.toString(),
      state
    };
  });

  app.get<{ Querystring: { readonly code?: string; readonly state?: string } }>(
    "/api/integrations/google/callback",
    async (request, reply) => {
      if (!request.query.code || !request.query.state) {
        return reply.code(422).send({
          ok: false,
          error: "GOOGLE_OAUTH_CALLBACK_INVALID",
          message: "Google OAuth code and state are required."
        });
      }
      if (!(await consumeOAuthState(secretStore, request.query.state))) {
        return reply.code(409).send({
          ok: false,
          error: "GOOGLE_OAUTH_STATE_INVALID",
          message: "Google OAuth state was not recognized or was already used."
        });
      }
      const token = await exchangeGoogleOAuthCode(request.query.code);
      secretStore.putSecret(
        "google.oauth.default",
        JSON.stringify({
          refreshToken: token.refresh_token,
          clientId: process.env.GOOGLE_CLIENT_ID,
          clientSecret: process.env.GOOGLE_CLIENT_SECRET
        })
      );

      return {
        ok: true,
        connected: true
      };
    }
  );

  app.post("/api/integrations/google/revoke", async () => {
    const secret = await secretStore.getSecretValue("google.oauth.default");
    if (secret) {
      await revokeGoogleOAuthSecret(secret);
    }

    return {
      ok: true,
      deleted: secretStore.deleteSecret("google.oauth.default")
    };
  });

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
      const correlationId = correlationIdForRequest(request);
      const route = routeWorkflowTask(request.body, {
        correlationId,
        provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
        model: process.env.KELPCLAW_PLANNER_MODEL
      });
      let workflow: WorkflowSpec;
      try {
        const routedPlanner =
          route.requiredModel.mode === "none"
            ? createDeterministicPlannerBackend({ artifactStore })
            : planner;
        workflow = await planWorkflowDraft(request.body, routedPlanner);
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
        correlationId,
        summary: `Routed workflow planning as ${route.route}.`,
        metadata: {
          route: route.route,
          modelMode: route.requiredModel.mode
        }
      });
      recordAudit(store, {
        action: "workflow.created",
        actor: "planner",
        workflowId: draftRevision.workflowId,
        revisionId: draftRevision.id,
        correlationId,
        summary: "Planned workflow draft revision.",
        secretRefs: collectSecretRefs(draftRevision.workflow)
      });

      return {
        ok: true,
        workflow: draftRevision.workflow,
        draftRevision,
        validation: draftRevision.validation,
        route
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

  app.post<{
    Params: RouteParamsWithId;
    Body: WorkflowFeedbackRequest;
    Reply: WorkflowFeedbackResponse;
  }>("/api/workflows/:id/feedback", async (request, reply) => {
    if (
      request.body.baseWorkflow.id !== request.params.id ||
      request.body.editedWorkflow.id !== request.params.id
    ) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_ID_MISMATCH",
        message: "Feedback base and edited workflows must match the route workflow id."
      } as never);
    }

    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      } as never);
    }

    const correlationId = correlationIdForRequest(request);
    const now = new Date().toISOString();
    const graphDiff = store.saveGraphDiff(
      createWorkflowGraphDiff({
        id: `graphdiff.${request.params.id}.${Date.now()}.${randomUUID()}`,
        baseWorkflow: request.body.baseWorkflow,
        editedWorkflow: request.body.editedWorkflow,
        createdAt: now
      })
    );
    const route = routeWorkflowTask(
      {
        prompt: request.body.prompt ?? request.body.editedWorkflow.prompt,
        currentWorkflow: request.body.editedWorkflow
      },
      {
        correlationId,
        provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
        model: process.env.KELPCLAW_PLANNER_MODEL,
        now
      }
    );
    const feedback = store.savePlannerFeedback(
      createWorkflowPlannerFeedback({
        id: `feedback.${request.params.id}.${Date.now()}.${randomUUID()}`,
        graphDiff,
        route,
        createdAt: now
      })
    );
    recordAudit(store, {
      action: "planner.feedback.created",
      actor: "planner",
      workflowId: request.params.id,
      revisionId:
        store.getLatestDraftRevision(request.params.id)?.id ??
        `draft.${request.params.id}.r${request.body.editedWorkflow.revision}`,
      correlationId,
      summary: `Created planner feedback with ${feedback.suggestions.length} suggestion(s).`,
      metadata: {
        graphDiffId: graphDiff.id,
        feedbackId: feedback.id,
        status: feedback.status
      }
    });

    return {
      ok: true,
      graphDiff,
      feedback
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
      const result = await executeCompiledDag(dag, runner ?? createNanoClawRunner(), {
        codegenArtifactStore: artifactStore,
        runId,
        secretResolver: new SecretStoreResolver(secretStore)
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
        const result = await executeCompiledDag(dag, runner ?? createNanoClawRunner(), {
          codegenArtifactStore: artifactStore,
          secretResolver: new SecretStoreResolver(secretStore)
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

function isPublicRoute(method: string, url: string): boolean {
  const pathname = url.split("?")[0] ?? url;
  return (
    pathname === "/health" || (method === "GET" && pathname === "/api/integrations/google/callback")
  );
}

function googleRedirectUri(): string | null {
  const baseUrl = process.env.KELPCLAW_PUBLIC_BASE_URL;
  if (!baseUrl) {
    return null;
  }

  return new URL("/api/integrations/google/callback", baseUrl).toString();
}

function publicSecretMetadata(secretStore: SecretStore) {
  return secretStore
    .listSecrets()
    .filter((secret) => !secret.name.startsWith("oauth.state."))
    .sort((left, right) => left.name.localeCompare(right.name));
}

async function exchangeGoogleOAuthCode(code: string): Promise<{ readonly refresh_token: string }> {
  const clientId = process.env.GOOGLE_CLIENT_ID;
  const clientSecret = process.env.GOOGLE_CLIENT_SECRET;
  const redirectUri = googleRedirectUri();
  if (!clientId || !clientSecret || !redirectUri) {
    throw new Error("Google OAuth client configuration is incomplete.");
  }

  const response = await fetch(
    process.env.GOOGLE_TOKEN_URL ?? "https://oauth2.googleapis.com/token",
    {
      method: "POST",
      headers: {
        "content-type": "application/x-www-form-urlencoded"
      },
      body: new URLSearchParams({
        code,
        client_id: clientId,
        client_secret: clientSecret,
        redirect_uri: redirectUri,
        grant_type: "authorization_code"
      })
    }
  );
  const payload = (await response.json()) as {
    readonly refresh_token?: string;
    readonly error?: string;
  };
  if (!response.ok || !payload.refresh_token) {
    throw new Error(payload.error ?? "Google OAuth token exchange did not return a refresh token.");
  }

  return {
    refresh_token: payload.refresh_token
  };
}

async function revokeGoogleOAuthSecret(secret: string): Promise<void> {
  const token = googleTokenForRevocation(secret);
  if (!token) {
    return;
  }

  const response = await fetch(
    process.env.GOOGLE_REVOKE_URL ?? "https://oauth2.googleapis.com/revoke",
    {
      method: "POST",
      headers: {
        "content-type": "application/x-www-form-urlencoded"
      },
      body: new URLSearchParams({ token })
    }
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Google OAuth revoke failed with ${response.status}.`);
  }
}

function googleTokenForRevocation(secret: string): string | null {
  try {
    const parsed = JSON.parse(secret) as {
      readonly refreshToken?: string;
      readonly accessToken?: string;
    };
    return parsed.refreshToken ?? parsed.accessToken ?? null;
  } catch {
    return secret.length > 0 ? secret : null;
  }
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

function createNanoClawRunner(): NodeRunner {
  if (process.env.NANOCLAW_RUNNER === "mock") {
    return new AdapterBackedNodeRunner({
      adapters: createDefaultMockAdapters(),
      fallbackRunner: new MockNodeRunner()
    });
  }

  return new ProductionNodeRunner({
    dockerBin: process.env.NANOCLAW_DOCKER_BIN,
    hostWorkspace: process.env.NANOCLAW_HOST_WORKSPACE ?? process.cwd()
  });
}

function correlationIdForRequest(request: FastifyRequest): string {
  const header = request.headers["x-correlation-id"];
  if (Array.isArray(header)) {
    return header[0] ?? `corr.${randomUUID()}`;
  }

  return typeof header === "string" && header.length > 0 ? header : `corr.${randomUUID()}`;
}

function createJob(input: {
  readonly type: WorkflowJobType;
  readonly workflowId?: string | undefined;
  readonly revisionId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly correlationId: string;
  readonly maxAttempts?: number | undefined;
}): WorkflowJob {
  const now = new Date().toISOString();
  const jobId = `job.${input.type}.${Date.now()}.${randomUUID()}`;
  const job: WorkflowJob = {
    id: jobId,
    type: input.type,
    status: "queued",
    workflowId: input.workflowId,
    revisionId: input.revisionId,
    nodeId: input.nodeId,
    correlationId: input.correlationId,
    createdAt: now,
    updatedAt: now,
    retry: {
      attempt: 0,
      maxAttempts: input.maxAttempts ?? 1,
      retryable: true
    },
    events: []
  };

  return {
    ...job,
    events: [createJobEvent(job, "info", `Queued ${input.type} job.`)]
  };
}

function createJobEvent(
  job: Pick<WorkflowJob, "id">,
  level: WorkflowJobEvent["level"],
  message: string,
  metadata?: JsonRecord | undefined
): WorkflowJobEvent {
  return {
    id: `event.${job.id}.${Date.now()}.${randomUUID()}`,
    jobId: job.id,
    timestamp: new Date().toISOString(),
    level,
    message,
    kind: "job.lifecycle",
    ...(metadata ? { metadata } : {})
  };
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
