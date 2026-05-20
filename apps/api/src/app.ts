import { createHash, randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import type { ServerResponse } from "node:http";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import Fastify from "fastify";
import {
  AgentSdkCodeGenerator,
  GeneratedNodeBuildLoop,
  LocalCodegenArtifactStore,
  checksumArtifactContent,
  createArtifactManifest,
  createAgentSdkGeneratedNodeRoleRunners,
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
  evaluateDraftWorkflow,
  executeCompiledDag
} from "@kelpclaw/nanoclaw";
import {
  WorkflowValidationError,
  createWorkflowGraphDiff,
  createWorkflowPlannerFeedback,
  createWorkflowSpecDiff,
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
  GeneratedNodeEvalReport,
  GeneratedNodeTestReport,
  JsonRecord,
  WorkflowArtifactManifestRecord,
  WorkflowAuditAdapterCallRecord,
  WorkflowAuditContainerRecord,
  WorkflowAuditDeliveryRecord,
  WorkflowAuditRecord,
  WorkflowAcceptPlanRequest,
  WorkflowAcceptPlanResponse,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowBranchMergeConflict,
  WorkflowBranchMergePreview,
  WorkflowBranchMergePreviewRequest,
  WorkflowBranchMergePreviewResponse,
  WorkflowBranchMergeRecord,
  WorkflowBranchMergeRequest,
  WorkflowBranchMergeResolution,
  WorkflowBranchMergeResponse,
  WorkflowBranchPlanRequest,
  WorkflowBranchPlanResponse,
  WorkflowBranchRepromptNodeRequest,
  WorkflowBranchRepromptNodeResponse,
  WorkflowEventSeverity,
  WorkflowCreateBranchRequest,
  WorkflowCreateBranchResponse,
  WorkflowDraftEvaluation,
  WorkflowFeedbackRequest,
  WorkflowFeedbackResponse,
  WorkflowGetBranchResponse,
  WorkflowGeneratedModuleReuseDecision,
  WorkflowGeneratedModuleReuseGate,
  WorkflowGeneratedModuleSignature,
  JsonValue,
  WorkflowJob,
  WorkflowJobEvent,
  WorkflowJobType,
  WorkflowCodegenArtifactRef,
  WorkflowDeploymentKind,
  WorkflowDeploymentRecord,
  WorkflowEdge,
  WorkflowListBranchesResponse,
  WorkflowNode,
  WorkflowObservabilityEventKind,
  WorkflowPlanRequest,
  WorkflowPlanResponse,
  WorkflowPlannerSuggestionDecisionRequest,
  WorkflowPlannerSuggestionDecisionResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowGraphChange,
  WorkflowRunEvent,
  WorkflowReuseCandidatesResponse,
  WorkflowSpec,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowValidationIssue,
  WorkflowValidateRequest,
  WorkflowValidateResponse,
  WorkflowWorkspace
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
import { InMemoryWorkflowStore, SqliteWorkflowStore, defaultBranchId } from "./store.js";
import type { SecretStore } from "./secrets.js";
import type { RevisionInput, WorkflowStore } from "./store.js";
import type { WorkflowPlannerBackend } from "./planner.js";

interface RouteParamsWithId {
  readonly id: string;
}

interface BranchRouteParams extends RouteParamsWithId {
  readonly branchId: string;
}

interface SourceBranchRouteParams extends RouteParamsWithId {
  readonly sourceBranchId: string;
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

interface WorkspaceRouteParams {
  readonly workspaceId: string;
}

interface FeedbackDecisionRouteParams extends RouteParamsWithId {
  readonly feedbackId: string;
  readonly suggestionId: string;
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
  readonly payload?: JsonRecord | undefined;
}

interface EvaluateDraftRequestBody {
  readonly workflow?: WorkflowSpec | undefined;
  readonly mockOnly?: boolean | undefined;
  readonly branchId?: string | undefined;
}

interface CodegenBuildRequestBody {
  readonly maxIterations?: number | undefined;
  readonly maxWallClockSeconds?: number | undefined;
  readonly maxModelCostUsd?: number | undefined;
  readonly runTestsInDocker?: boolean | undefined;
  readonly branchId?: string | undefined;
}

interface DeploymentRequestBody {
  readonly approvedRevisionId: string;
  readonly kind: WorkflowDeploymentKind;
  readonly createdBy: string;
  readonly rollbackPlan: string;
  readonly branchId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

interface CodegenReviewRequestBody {
  readonly status: "approved" | "rejected";
  readonly reviewedBy: string;
  readonly notes?: string | undefined;
  readonly branchId?: string | undefined;
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
    logger: false,
    routerOptions: {
      maxParamLength: 512
    }
  });
  const store = options.store ?? new InMemoryWorkflowStore();
  const secretStore = options.secretStore ?? new InMemorySecretStore();
  const artifactStore = options.artifactStore ?? new LocalCodegenArtifactStore();
  const planner = options.planner ?? createPlannerBackendFromEnv({ artifactStore });
  const runner = options.runner;
  recoverInterruptedJobs(store);
  const jobSupervisor = new ApiJobSupervisor(store);
  const jobWorker = new ApiJobWorker(store, jobSupervisor);
  jobWorker.register("smoke.integration", async (job, signal) => {
    await sleepWithSignal(numberFromJson(job.payload?.durationMs) ?? 0, signal);
    return {
      result: {
        workerId: job.workerId ?? "local",
        smokedAt: new Date().toISOString()
      }
    };
  });
  jobWorker.start();
  const adminToken =
    options.adminToken === undefined ? process.env.KELPCLAW_ADMIN_TOKEN : options.adminToken;

  app.addHook("onClose", async () => {
    jobWorker.stop();
  });

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
      maxAttempts: request.body.maxAttempts,
      payload: request.body.payload
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

  app.get<{ Params: JobRouteParams }>("/api/jobs/:jobId/events", async (request, reply) => {
    const job = store.getJob(request.params.jobId);
    if (!job) {
      return reply.code(404).send({
        ok: false,
        error: "JOB_NOT_FOUND",
        message: `Job '${request.params.jobId}' was not found.`
      });
    }

    reply.raw.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive"
    });
    let sent = 0;
    const writeAvailableEvents = () => {
      const current = store.getJob(request.params.jobId);
      if (!current) {
        writeSseEvent(reply.raw, "error", {
          message: `Job '${request.params.jobId}' was not found.`
        });
        reply.raw.end();
        return true;
      }

      for (const event of current.events.slice(sent)) {
        writeSseEvent(reply.raw, "job-event", event);
        sent += 1;
      }
      if (isTerminalJobStatus(current.status)) {
        writeSseEvent(reply.raw, "job-complete", current);
        reply.raw.end();
        return true;
      }

      return false;
    };

    if (writeAvailableEvents()) {
      return reply;
    }

    const interval = setInterval(() => {
      if (writeAvailableEvents()) {
        clearInterval(interval);
      }
    }, 250);
    request.raw.on("close", () => clearInterval(interval));

    return reply;
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
      const withEvent = jobSupervisor.cancelJob(
        job.id,
        request.body.reason ?? "Cancelled by API request.",
        now
      );

      return {
        ok: true,
        job: withEvent
      };
    }
  );

  app.get<{ Params: WorkspaceRouteParams }>(
    "/api/workspaces/:workspaceId",
    async (request, reply) => {
      const workspace = store.getWorkspace(request.params.workspaceId);
      if (!workspace) {
        return reply.code(404).send({
          ok: false,
          error: "WORKSPACE_NOT_FOUND",
          message: `Workspace '${request.params.workspaceId}' was not found.`
        });
      }

      return {
        ok: true,
        workspace
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
      updateRequestJob(
        store,
        request,
        "running",
        "Planning workflow.",
        {
          route: route.route
        },
        jobSupervisor
      );
      let workflow: WorkflowSpec;
      try {
        throwIfRequestJobCancelled(store, jobSupervisor, request);
        const routedPlanner =
          route.requiredModel.mode === "none"
            ? createDeterministicPlannerBackend({ artifactStore })
            : planner;
        workflow = await planWorkflowDraft(request.body, routedPlanner);
        throwIfRequestJobCancelled(store, jobSupervisor, request);
      } catch (error) {
        const cancelled = error instanceof JobCancelledError;
        updateRequestJob(
          store,
          request,
          cancelled ? "cancelled" : "failed",
          cancelled ? error.message : "Workflow planning failed.",
          {
            route: route.route
          },
          jobSupervisor
        );
        return reply.code(503).send({
          ok: false,
          error: cancelled ? "JOB_CANCELLED" : "PLANNER_BACKEND_UNAVAILABLE",
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
        action: "task.routed",
        actor: "router",
        workflowId: draftRevision.workflowId,
        revisionId: draftRevision.id,
        correlationId,
        summary: `Routed workflow task as ${route.route}.`,
        metadata: jsonRecord({
          route: route.route,
          rationale: route.rationale,
          requiredModel: route.requiredModel,
          modelInvocations: route.modelInvocations
        })
      });
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
      updateRequestJob(
        store,
        request,
        "succeeded",
        "Workflow planning completed.",
        {
          workflowId: draftRevision.workflowId,
          draftRevisionId: draftRevision.id
        },
        jobSupervisor
      );

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
    Body: WorkflowCreateBranchRequest;
    Reply: WorkflowCreateBranchResponse;
  }>("/api/workflows/:id/branches", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      } as never);
    }

    const sourceBranch = request.body.fromBranchId
      ? store.getBranch(request.body.fromBranchId)
      : store.getDefaultBranch(request.params.id);
    if (!sourceBranch || sourceBranch.workflowId !== request.params.id) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Source branch was not found for workflow '${request.params.id}'.`
      } as never);
    }

    const sourceDraft = request.body.fromDraftRevisionId
      ? store.getDraftRevision(request.body.fromDraftRevisionId)
      : store.getDraftRevision(sourceBranch.headDraftRevisionId);
    if (!sourceDraft || sourceDraft.workflowId !== request.params.id) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_DRAFT_NOT_FOUND",
        message: `Source draft was not found for workflow '${request.params.id}'.`
      } as never);
    }

    const branchId = `branch.${request.params.id}.${slugify(request.body.name)}.${randomUUID()}`;
    const draftRevision = store.saveDraftRevision(
      sourceDraft.workflow,
      sourceDraft.validation,
      "branch-fork",
      {
        branchId,
        force: true,
        preserveRevision: true,
        parentDraftRevisionId: sourceDraft.id,
        updateBranchHead: false
      }
    );
    const now = draftRevision.createdAt;
    const branch = store.saveBranch({
      id: branchId,
      workflowId: request.params.id,
      name: request.body.name,
      status: "active",
      createdAt: now,
      updatedAt: now,
      createdBy: request.body.createdBy,
      parentBranchId: sourceBranch.id,
      baseDraftRevisionId: sourceDraft.id,
      headDraftRevisionId: draftRevision.id,
      metadata: {
        forkedFromBranchId: sourceBranch.id
      }
    });
    store.savePromptTurn({
      id: `prompt-turn.${branch.id}.${Date.now()}.${randomUUID()}`,
      workflowId: request.params.id,
      branchId: branch.id,
      source: "edit",
      prompt: sourceDraft.workflow.prompt,
      actor: request.body.createdBy,
      createdAt: now,
      baseDraftRevisionId: sourceDraft.id,
      resultingDraftRevisionId: draftRevision.id,
      metadata: {
        action: "branch-fork"
      }
    });
    recordAudit(store, {
      action: "branch.created",
      actor: request.body.createdBy,
      workflowId: request.params.id,
      branchId: branch.id,
      revisionId: draftRevision.id,
      correlationId: correlationIdForRequest(request),
      summary: `Created workflow branch '${branch.name}'.`,
      metadata: {
        sourceBranchId: sourceBranch.id,
        sourceDraftRevisionId: sourceDraft.id
      }
    });

    return reply.code(201).send({
      ok: true,
      branch,
      draftRevision
    });
  });

  app.get<{
    Params: RouteParamsWithId;
    Reply: WorkflowListBranchesResponse;
  }>("/api/workflows/:id/branches", async (request, reply) => {
    if (!store.getWorkflow(request.params.id)) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      } as never);
    }
    return {
      ok: true,
      branches: store.listBranches(request.params.id)
    };
  });

  app.get<{
    Params: BranchRouteParams;
    Reply: WorkflowGetBranchResponse;
  }>("/api/workflows/:id/branches/:branchId", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    if (!branch || branch.workflowId !== request.params.id) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }
    const headDraftRevision = store.getDraftRevision(branch.headDraftRevisionId);
    if (!headDraftRevision) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_DRAFT_NOT_FOUND",
        message: `Branch head draft '${branch.headDraftRevisionId}' was not found.`
      } as never);
    }
    return {
      ok: true,
      branch,
      headDraftRevision,
      promptTurns: store.listPromptTurns(request.params.id, branch.id)
    };
  });

  app.get<{
    Params: BranchRouteParams;
    Reply: WorkflowReuseCandidatesResponse;
  }>("/api/workflows/:id/branches/:branchId/reuse-candidates", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    const headDraft = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    if (!branch || branch.workflowId !== request.params.id || !headDraft) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }

    const decisions = computeGeneratedModuleReuseDecisions(
      store,
      request.params.id,
      branch.id,
      headDraft.workflow
    ).map((decision) => store.saveGeneratedModuleReuseDecision(decision));

    return {
      ok: true,
      decisions
    };
  });

  app.post<{
    Params: BranchRouteParams;
    Body: WorkflowBranchPlanRequest;
    Reply: WorkflowBranchPlanResponse;
  }>("/api/workflows/:id/branches/:branchId/plan", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    const headDraft = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    if (!branch || branch.workflowId !== request.params.id || !headDraft) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }

    const correlationId = correlationIdForRequest(request);
    const planRequest: WorkflowPlanRequest = {
      ...request.body,
      currentWorkflow: request.body.currentWorkflow ?? headDraft.workflow
    };
    const route = routeWorkflowTask(planRequest, {
      correlationId,
      provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
      model: process.env.KELPCLAW_PLANNER_MODEL
    });
    const routedPlanner =
      route.requiredModel.mode === "none"
        ? createDeterministicPlannerBackend({ artifactStore })
        : planner;
    let workflow: WorkflowSpec;
    try {
      workflow = await planWorkflowDraft(planRequest, routedPlanner);
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

    let draftRevision = store.saveDraftRevision(validation.workflow, validation, "branch-plan", {
      branchId: branch.id,
      force: true,
      parentDraftRevisionId: headDraft.id
    });
    let updatedBranch = store.saveBranch({
      ...branch,
      headDraftRevisionId: draftRevision.id,
      updatedAt: draftRevision.createdAt
    });
    const reuse = applyGeneratedModuleReuse(
      store,
      request.params.id,
      branch.id,
      draftRevision.workflow
    );
    for (const decision of reuse.decisions) {
      store.saveGeneratedModuleReuseDecision(decision);
    }
    if (reuse.changed) {
      const reusedValidation = validateWorkflowSpec(reuse.workflow);
      if (!reusedValidation.ok) {
        return reply.code(422).send({
          ok: false,
          error: "WORKFLOW_CODEGEN_REUSE_INVALID",
          message: reusedValidation.errors.map((error) => error.code).join(", "),
          validation: reusedValidation
        } as never);
      }
      draftRevision = store.saveDraftRevision(
        reusedValidation.workflow,
        reusedValidation,
        "validate",
        {
          branchId: branch.id,
          force: true,
          parentDraftRevisionId: draftRevision.id
        }
      );
      updatedBranch = store.saveBranch({
        ...updatedBranch,
        headDraftRevisionId: draftRevision.id,
        updatedAt: draftRevision.createdAt
      });
    }
    persistCodegenArtifactManifests(
      store,
      draftRevision.workflow,
      draftRevision.id,
      draftRevision.createdAt
    );
    const promptTurn = store.savePromptTurn({
      id: `prompt-turn.${branch.id}.${Date.now()}.${randomUUID()}`,
      workflowId: request.params.id,
      branchId: branch.id,
      source: "plan",
      prompt: request.body.prompt,
      actor: request.body.actor ?? "planner",
      createdAt: draftRevision.createdAt,
      baseDraftRevisionId: headDraft.id,
      resultingDraftRevisionId: draftRevision.id,
      route
    });
    recordAudit(store, {
      action: "task.routed",
      actor: "router",
      workflowId: draftRevision.workflowId,
      branchId: branch.id,
      revisionId: draftRevision.id,
      correlationId,
      summary: `Routed branch workflow task as ${route.route}.`,
      metadata: jsonRecord({
        route: route.route,
        rationale: route.rationale,
        requiredModel: route.requiredModel,
        modelInvocations: route.modelInvocations
      })
    });
    recordAudit(store, {
      action: "workflow.created",
      actor: "planner",
      workflowId: draftRevision.workflowId,
      branchId: branch.id,
      revisionId: draftRevision.id,
      correlationId,
      summary: "Planned branch workflow draft revision.",
      secretRefs: collectSecretRefs(draftRevision.workflow)
    });

    return {
      ok: true,
      workflow: draftRevision.workflow,
      draftRevision,
      validation: draftRevision.validation,
      route,
      branch: updatedBranch,
      promptTurn
    };
  });

  app.post<{
    Params: BranchRouteParams;
    Body: WorkflowBranchRepromptNodeRequest;
    Reply: WorkflowBranchRepromptNodeResponse;
  }>("/api/workflows/:id/branches/:branchId/reprompt-node", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    const headDraft = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    const sourceWorkflow = request.body.currentWorkflow ?? headDraft?.workflow;
    if (!branch || branch.workflowId !== request.params.id || !headDraft || !sourceWorkflow) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
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

      const draftRevision = store.saveDraftRevision(
        validation.workflow,
        validation,
        "branch-reprompt",
        {
          branchId: branch.id,
          force: true,
          parentDraftRevisionId: headDraft.id
        }
      );
      const updatedBranch = store.saveBranch({
        ...branch,
        headDraftRevisionId: draftRevision.id,
        updatedAt: draftRevision.createdAt
      });
      persistCodegenArtifactManifests(
        store,
        draftRevision.workflow,
        draftRevision.id,
        draftRevision.createdAt
      );
      const promptTurn = store.savePromptTurn({
        id: `prompt-turn.${branch.id}.${Date.now()}.${randomUUID()}`,
        workflowId: request.params.id,
        branchId: branch.id,
        source: "reprompt",
        prompt: request.body.prompt,
        actor: request.body.actor ?? "planner",
        createdAt: draftRevision.createdAt,
        baseDraftRevisionId: headDraft.id,
        resultingDraftRevisionId: draftRevision.id
      });
      recordAudit(store, {
        action: "workflow.edited",
        actor: "planner",
        workflowId: draftRevision.workflowId,
        branchId: branch.id,
        revisionId: draftRevision.id,
        nodeId: request.body.nodeId,
        correlationId: correlationIdForRequest(request),
        summary: "Reprompted branch workflow node.",
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
        diff: reprompted.diff,
        branch: updatedBranch,
        promptTurn
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
    Params: BranchRouteParams;
    Body: WorkflowAcceptPlanRequest;
    Reply: WorkflowAcceptPlanResponse;
  }>("/api/workflows/:id/branches/:branchId/accept-plan", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    const headDraft = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    if (!branch || branch.workflowId !== request.params.id || !headDraft) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }
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
        error: "WORKFLOW_PLAN_ACCEPTANCE_INVALID",
        message: validation.errors.map((error) => error.code).join(", "),
        validation
      } as never);
    }

    const draftRevision = store.saveDraftRevision(
      validation.workflow,
      validation,
      "plan-accepted",
      {
        branchId: branch.id,
        force: true,
        preserveRevision: true,
        parentDraftRevisionId: headDraft.id
      }
    );
    store.saveBranch({
      ...branch,
      headDraftRevisionId: draftRevision.id,
      acceptedDraftRevisionId: draftRevision.id,
      updatedAt: draftRevision.createdAt
    });
    persistCodegenArtifactManifests(
      store,
      draftRevision.workflow,
      draftRevision.id,
      draftRevision.createdAt
    );
    recordAudit(store, {
      action: "plan.accepted",
      actor: request.body.acceptedBy,
      workflowId: draftRevision.workflowId,
      branchId: branch.id,
      revisionId: draftRevision.id,
      correlationId: correlationIdForRequest(request),
      summary:
        "Accepted branch workflow plan shape before implementation; production approval still requires implementation and draft evaluation.",
      secretRefs: collectSecretRefs(draftRevision.workflow),
      metadata: {
        semanticCheckpoint: "branch-plan-shape-accepted",
        productionApprovalRequired: true
      }
    });

    return {
      ok: true,
      workflowId: draftRevision.workflowId,
      draftRevisionId: draftRevision.id,
      workflow: draftRevision.workflow,
      draftRevision,
      validation: draftRevision.validation
    };
  });

  app.post<{
    Params: SourceBranchRouteParams;
    Body: WorkflowBranchMergePreviewRequest;
    Reply: WorkflowBranchMergePreviewResponse;
  }>("/api/workflows/:id/branches/:sourceBranchId/merge-preview", async (request, reply) => {
    const preview = createBranchMergePreview(
      store,
      request.params.id,
      request.params.sourceBranchId,
      {
        targetBranchId: request.body.targetBranchId,
        mode: request.body.mode ?? "merge",
        cherryPickChangeIds: request.body.cherryPickChangeIds
      }
    );
    if (!preview) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: "Source, target, or base branch draft was not found."
      } as never);
    }
    return {
      ok: true,
      preview
    };
  });

  app.post<{
    Params: SourceBranchRouteParams;
    Body: WorkflowBranchMergeRequest;
    Reply: WorkflowBranchMergeResponse;
  }>("/api/workflows/:id/branches/:sourceBranchId/merge", async (request, reply) => {
    const preview = createBranchMergePreview(
      store,
      request.params.id,
      request.params.sourceBranchId,
      {
        targetBranchId: request.body.targetBranchId,
        mode: request.body.mode ?? "merge",
        cherryPickChangeIds: request.body.cherryPickChangeIds,
        resolutions: request.body.resolutions
      }
    );
    if (!preview) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: "Source, target, or base branch draft was not found."
      } as never);
    }
    if (preview.status !== "clean" || !preview.mergedWorkflow) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_BRANCH_MERGE_CONFLICTS",
        message: `Merge has ${preview.conflicts.length} unresolved conflict(s).`,
        issues: preview.validation.ok ? [] : preview.validation.errors
      } as never);
    }

    const targetBranch = store.getBranch(request.body.targetBranchId);
    const targetHead = targetBranch
      ? store.getDraftRevision(targetBranch.headDraftRevisionId)
      : undefined;
    if (!targetBranch || !targetHead) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Target branch '${request.body.targetBranchId}' was not found.`
      } as never);
    }
    const validation = validateWorkflowSpec(preview.mergedWorkflow);
    if (!validation.ok) {
      return reply.code(422).send({
        ok: false,
        error: "WORKFLOW_BRANCH_MERGE_INVALID",
        message: validation.errors.map((error) => error.code).join(", "),
        validation
      } as never);
    }

    const source = preview.mode === "cherry-pick" ? "branch-cherry-pick" : "branch-merge";
    const draftRevision = store.saveDraftRevision(validation.workflow, validation, source, {
      branchId: targetBranch.id,
      force: true,
      parentDraftRevisionId: targetHead.id
    });
    const branch = store.saveBranch({
      ...targetBranch,
      headDraftRevisionId: draftRevision.id,
      updatedAt: draftRevision.createdAt
    });
    const merge: WorkflowBranchMergeRecord = store.saveBranchMerge({
      ...preview,
      status: "applied",
      appliedAt: draftRevision.createdAt,
      appliedBy: request.body.appliedBy,
      mergedDraftRevisionId: draftRevision.id,
      resolutions: request.body.resolutions
    });
    const graphDiff = store.saveGraphDiff(
      createWorkflowGraphDiff({
        id: `graphdiff.${request.params.id}.${Date.now()}.${randomUUID()}`,
        baseWorkflow: targetHead.workflow,
        editedWorkflow: draftRevision.workflow,
        createdAt: draftRevision.createdAt
      })
    );
    store.savePromptTurn({
      id: `prompt-turn.${branch.id}.${Date.now()}.${randomUUID()}`,
      workflowId: request.params.id,
      branchId: branch.id,
      source: preview.mode === "cherry-pick" ? "cherry-pick" : "merge",
      prompt: `${preview.mode} ${preview.sourceBranchId} into ${preview.targetBranchId}`,
      actor: request.body.appliedBy,
      createdAt: draftRevision.createdAt,
      baseDraftRevisionId: targetHead.id,
      resultingDraftRevisionId: draftRevision.id,
      metadata: {
        mergeId: merge.id,
        graphDiffId: graphDiff.id
      }
    });
    recordAudit(store, {
      action: preview.mode === "cherry-pick" ? "branch.cherry-picked" : "branch.merged",
      actor: request.body.appliedBy,
      workflowId: draftRevision.workflowId,
      branchId: branch.id,
      revisionId: draftRevision.id,
      correlationId: correlationIdForRequest(request),
      summary: `${preview.mode === "cherry-pick" ? "Cherry-picked" : "Merged"} branch '${preview.sourceBranchId}' into '${preview.targetBranchId}'.`,
      diff: graphDiffToSpecDiff(targetHead.workflow, draftRevision.workflow),
      metadata: {
        mergeId: merge.id,
        sourceBranchId: preview.sourceBranchId,
        targetBranchId: preview.targetBranchId,
        conflictsResolved: request.body.resolutions.length
      }
    });

    return {
      ok: true,
      merge,
      branch,
      draftRevision,
      workflow: draftRevision.workflow,
      validation: draftRevision.validation
    };
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
    Body: WorkflowAcceptPlanRequest;
    Reply: WorkflowAcceptPlanResponse;
  }>("/api/workflows/:id/accept-plan", async (request, reply) => {
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
        error: "WORKFLOW_PLAN_ACCEPTANCE_INVALID",
        message: validation.errors.map((error) => error.code).join(", "),
        validation
      } as never);
    }

    const draftRevision = store.saveDraftRevision(
      validation.workflow,
      validation,
      "plan-accepted",
      {
        force: true,
        preserveRevision: true
      }
    );
    persistCodegenArtifactManifests(
      store,
      draftRevision.workflow,
      draftRevision.id,
      draftRevision.createdAt
    );
    recordAudit(store, {
      action: "plan.accepted",
      actor: request.body.acceptedBy,
      workflowId: draftRevision.workflowId,
      revisionId: draftRevision.id,
      correlationId: correlationIdForRequest(request),
      summary:
        "Accepted workflow plan shape before implementation; production approval still requires implementation and draft evaluation.",
      secretRefs: collectSecretRefs(draftRevision.workflow),
      metadata: {
        semanticCheckpoint: "plan-shape-accepted",
        productionApprovalRequired: true
      }
    });

    return {
      ok: true,
      workflowId: draftRevision.workflowId,
      draftRevisionId: draftRevision.id,
      workflow: draftRevision.workflow,
      draftRevision,
      validation: draftRevision.validation
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
    updateRequestJob(
      store,
      request,
      "running",
      "Generating planner feedback.",
      {
        workflowId: request.params.id
      },
      jobSupervisor
    );
    try {
      throwIfRequestJobCancelled(store, jobSupervisor, request);
    } catch (error) {
      if (error instanceof JobCancelledError) {
        updateRequestJob(store, request, "cancelled", error.message, undefined, jobSupervisor);
        return reply.code(409).send({
          ok: false,
          error: "JOB_CANCELLED",
          message: error.message
        } as never);
      }
      throw error;
    }
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
    recordAudit(store, {
      action: "task.routed",
      actor: "router",
      workflowId: request.params.id,
      revisionId:
        store.getLatestDraftRevision(request.params.id)?.id ??
        `draft.${request.params.id}.r${request.body.editedWorkflow.revision}`,
      correlationId,
      summary: `Routed planner feedback as ${route.route}.`,
      metadata: jsonRecord({
        route: route.route,
        rationale: route.rationale,
        requiredModel: route.requiredModel,
        modelInvocations: route.modelInvocations
      })
    });
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
    updateRequestJob(
      store,
      request,
      "succeeded",
      "Planner feedback completed.",
      {
        graphDiffId: graphDiff.id,
        feedbackId: feedback.id,
        status: feedback.status
      },
      jobSupervisor
    );

    return {
      ok: true,
      graphDiff,
      feedback
    };
  });

  app.post<{
    Params: FeedbackDecisionRouteParams;
    Body: WorkflowPlannerSuggestionDecisionRequest;
    Reply: WorkflowPlannerSuggestionDecisionResponse;
  }>(
    "/api/workflows/:id/feedback/:feedbackId/suggestions/:suggestionId/decision",
    async (request, reply) => {
      if (request.body.suggestionId !== request.params.suggestionId) {
        return reply.code(409).send({
          ok: false,
          error: "SUGGESTION_ID_MISMATCH",
          message: "Suggestion decision body must match the route suggestion id."
        } as never);
      }

      const feedback = store.getPlannerFeedback(request.params.feedbackId);
      if (!feedback || feedback.workflowId !== request.params.id) {
        return reply.code(404).send({
          ok: false,
          error: "PLANNER_FEEDBACK_NOT_FOUND",
          message: `Planner feedback '${request.params.feedbackId}' was not found for workflow '${request.params.id}'.`
        } as never);
      }

      const suggestion = feedback.suggestions.find(
        (candidate) => candidate.id === request.params.suggestionId
      );
      if (!suggestion) {
        return reply.code(404).send({
          ok: false,
          error: "PLANNER_SUGGESTION_NOT_FOUND",
          message: `Planner suggestion '${request.params.suggestionId}' was not found.`
        } as never);
      }

      const updatedFeedback = store.savePlannerFeedback({
        ...feedback,
        suggestions: feedback.suggestions.map((candidate) =>
          candidate.id === suggestion.id
            ? {
                ...candidate,
                status: request.body.decision
              }
            : candidate
        )
      });
      recordAudit(store, {
        action: "planner.feedback.decided",
        actor: "api",
        workflowId: request.params.id,
        revisionId:
          store.getLatestDraftRevision(request.params.id)?.id ??
          `feedback.${request.params.feedbackId}`,
        nodeId: suggestion.target.kind === "node" ? suggestion.target.id : undefined,
        correlationId: correlationIdForRequest(request),
        summary: `Marked planner suggestion '${suggestion.id}' as ${request.body.decision}.`,
        metadata: {
          feedbackId: feedback.id,
          suggestionId: suggestion.id,
          decision: request.body.decision,
          targetKind: suggestion.target.kind,
          targetId: suggestion.target.id ?? null
        }
      });

      return {
        ok: true,
        feedback: updatedFeedback
      };
    }
  );

  app.post<{ Params: RouteParamsWithId; Body: EvaluateDraftRequestBody }>(
    "/api/workflows/:id/evaluate-draft",
    async (request, reply) => {
      const stored = store.getWorkflow(request.params.id);
      if (!stored) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }

      const branch = request.body.branchId ? store.getBranch(request.body.branchId) : undefined;
      const branchHead = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
      if (request.body.branchId && (!branch || branch.workflowId !== request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_BRANCH_NOT_FOUND",
          message: `Branch '${request.body.branchId}' was not found for workflow '${request.params.id}'.`
        });
      }

      const workflow = request.body.workflow ?? stored.workflow;
      if (workflow.id !== request.params.id) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_ID_MISMATCH",
          message: `Workflow id '${workflow.id}' does not match route id '${request.params.id}'.`
        });
      }

      const correlationId = correlationIdForRequest(request);
      updateRequestJob(
        store,
        request,
        "running",
        "Draft evaluation started.",
        {
          workflowId: workflow.id,
          ...(branch ? { branchId: branch.id } : {})
        },
        jobSupervisor
      );
      let evaluation: WorkflowDraftEvaluation;
      let draftRevision: ReturnType<WorkflowStore["getLatestDraftRevision"]>;
      try {
        throwIfRequestJobCancelled(store, jobSupervisor, request);
        const validation = validateWorkflowSpec(workflow);
        draftRevision = validation.ok
          ? store.saveDraftRevision(validation.workflow, validation, "validate", {
              branchId: branch?.id,
              force: branch !== undefined,
              parentDraftRevisionId: branchHead?.id
            })
          : store.getLatestDraftRevision(workflow.id);
        evaluation = store.saveDraftEvaluation(
          await evaluateDraftWorkflow(workflow, {
            draftRevisionId: draftRevision?.id,
            branchId: branch?.id,
            jobId: requestJobId(request),
            codegenArtifactStore: artifactStore,
            runGeneratedNodesInDocker: process.env.NANOCLAW_DRAFT_DOCKER === "1",
            dockerBin: process.env.NANOCLAW_DOCKER_BIN,
            hostWorkspace: process.env.NANOCLAW_HOST_WORKSPACE ?? process.cwd()
          })
        );
        throwIfRequestJobCancelled(store, jobSupervisor, request);
      } catch (error) {
        if (error instanceof JobCancelledError) {
          updateRequestJob(store, request, "cancelled", error.message, undefined, jobSupervisor);
          return reply.code(409).send({
            ok: false,
            error: "JOB_CANCELLED",
            message: error.message
          } as never);
        }
        throw error;
      }
      recordAudit(store, {
        action: "draft.evaluated",
        actor: "draft-evaluator",
        workflowId: workflow.id,
        branchId: branch?.id,
        revisionId: draftRevision?.id ?? `draft.${workflow.id}.r${workflow.revision}`,
        correlationId,
        summary: `Draft evaluation ${evaluation.status}.`,
        metadata: {
          evaluationId: evaluation.id,
          readyForApproval: evaluation.readyForApproval,
          mockOnly: evaluation.mockOnly
        }
      });
      updateRequestJob(
        store,
        request,
        evaluation.status === "passed" ? "succeeded" : "failed",
        "Draft evaluation completed.",
        {
          evaluationId: evaluation.id,
          readyForApproval: evaluation.readyForApproval
        },
        jobSupervisor
      );

      return {
        ok: true,
        evaluation
      };
    }
  );

  app.post<{ Params: RouteParamsWithId; Body: ApprovalRequestBody }>(
    "/api/workflows/:id/approvals",
    async (request, reply) => {
      try {
        const issues = await validateApprovalReadiness(
          store,
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
      const codegenIssues = await validateApprovalReadiness(
        store,
        validation.workflow,
        artifactStore,
        request.body.branchId
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
        validation.workflow,
        request.body.branchId
      );
      recordAudit(store, {
        action: "workflow.approved",
        actor: request.body.approvedBy,
        workflowId: approvedRevision.workflowId,
        branchId: approvedRevision.branchId,
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
    Body: CodegenBuildRequestBody;
  }>("/api/workflows/:id/codegen/:nodeId/build", async (request, reply) => {
    const stored = store.getWorkflow(request.params.id);
    if (!stored) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      });
    }

    const branch = request.body.branchId ? store.getBranch(request.body.branchId) : undefined;
    const branchHead = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    if (
      request.body.branchId &&
      (!branch || branch.workflowId !== request.params.id || !branchHead)
    ) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.body.branchId}' was not found for workflow '${request.params.id}'.`
      });
    }
    const sourceWorkflow = branchHead?.workflow ?? stored.workflow;
    const node = sourceWorkflow.nodes.find((candidate) => candidate.id === request.params.nodeId);
    if (node?.kind !== "codegen" || !node.codegen) {
      return reply.code(404).send({
        ok: false,
        error: "CODEGEN_NODE_NOT_FOUND",
        message: `Codegen node '${request.params.nodeId}' was not found.`
      });
    }

    const correlationId = correlationIdForRequest(request);
    const job =
      requestJobId(request) && store.getJob(requestJobId(request) ?? "")
        ? store.getJob(requestJobId(request) ?? "")!
        : store.saveJob(
            createJob({
              type: "build.codegen-node",
              workflowId: sourceWorkflow.id,
              revisionId:
                branchHead?.id ??
                store.getLatestDraftRevision(sourceWorkflow.id)?.id ??
                `draft.${sourceWorkflow.id}.r${sourceWorkflow.revision}`,
              nodeId: node.id,
              correlationId,
              maxAttempts: 1
            })
          );
    const startedAt = new Date().toISOString();
    const runningJob = store.appendJobEvent(
      store.saveJob({
        ...job,
        status: "running",
        startedAt: job.startedAt ?? startedAt,
        updatedAt: startedAt
      }).id,
      createJobEvent(job, "info", "Generated-node build loop started.", {
        nodeId: node.id
      })
    );
    const jobSignal = jobSupervisor.startJob(runningJob.id);
    const workspace = store.saveWorkspace(
      createWorkflowWorkspace({
        jobId: runningJob.id,
        workflowId: sourceWorkflow.id,
        branchId: branch?.id,
        revisionId:
          branchHead?.id ??
          store.getLatestDraftRevision(sourceWorkflow.id)?.id ??
          `draft.${sourceWorkflow.id}.r${sourceWorkflow.revision}`
      })
    );
    const buildLoop = new GeneratedNodeBuildLoop({
      codeGenerator: process.env.ANTHROPIC_API_KEY
        ? new AgentSdkCodeGenerator({
            apiKey: process.env.ANTHROPIC_API_KEY,
            model: process.env.KELPCLAW_CODEGEN_MODEL ?? process.env.KELPCLAW_PLANNER_MODEL
          })
        : undefined,
      roleRunners: process.env.ANTHROPIC_API_KEY
        ? createAgentSdkGeneratedNodeRoleRunners({
            apiKey: process.env.ANTHROPIC_API_KEY
          })
        : undefined
    });
    try {
      if (jobSignal?.aborted || jobSupervisor.isCancelled(runningJob.id)) {
        throw new JobCancelledError(runningJob.id);
      }
      const result = await buildLoop.build({
        workflowId: stored.workflow.id,
        nodeId: node.id,
        prompt: node.codegen.latestPrompt,
        plannerRationale: node.codegen.plannerRationale,
        inputSchema: node.inputs,
        outputSchema: node.outputs,
        runtime: node.runtime,
        sandbox: node.codegen.sandbox,
        generatedAt: new Date().toISOString(),
        job: runningJob,
        workspace,
        workspaceRoot: workspace.rootPath,
        maxIterations: request.body.maxIterations ?? 3,
        maxWallClockSeconds: request.body.maxWallClockSeconds ?? 600,
        maxModelCostUsd: request.body.maxModelCostUsd ?? 2,
        maxDockerRuntimeSeconds: 300,
        signal: jobSignal,
        runTestsInDocker: request.body.runTestsInDocker ?? false
      });
      if (jobSignal?.aborted || jobSupervisor.isCancelled(runningJob.id)) {
        throw new JobCancelledError(runningJob.id);
      }
      const buildArtifacts = [
        result.designSpecArtifact,
        result.generation.sourceArtifact,
        result.generation.dependencyManifestArtifact,
        ...result.testArtifacts,
        ...result.resultArtifacts,
        ...(result.unresolvedFailureArtifact ? [result.unresolvedFailureArtifact] : [])
      ];
      await artifactStore.putManifest(
        createArtifactManifest({
          workflowId: stored.workflow.id,
          generatedAt: result.generation.metadata.provenance.generatedAt,
          artifacts: buildArtifacts
        })
      );
      for (const run of result.agentRuns) {
        store.saveAgentRun(run);
      }
      for (const artifact of result.agentArtifacts) {
        store.saveAgentArtifact(artifact);
      }

      const artifactRefs = buildArtifacts
        .map((artifact) => ({
          path: artifact.path,
          checksum: artifact.checksum,
          contentType: artifact.contentType
        }))
        .sort((left, right) => left.path.localeCompare(right.path));
      const testReport = store.saveGeneratedNodeTestReport(
        createGeneratedNodeTestReport({
          workflowId: stored.workflow.id,
          branchId: branch?.id,
          nodeId: node.id,
          jobId: runningJob.id,
          status: result.status,
          testFiles: result.testArtifacts.map(toArtifactRef),
          resultArtifacts: result.resultArtifacts.map(toArtifactRef),
          logs: result.logs,
          failureMessage:
            result.status === "failed"
              ? result.findings.map((finding) => finding.message).join("; ") ||
                "Generated-node eval failed."
              : undefined
        })
      );
      const evalReport = store.saveGeneratedNodeEvalReport(
        createGeneratedNodeEvalReport({
          workflowId: stored.workflow.id,
          branchId: branch?.id,
          nodeId: node.id,
          jobId: runningJob.id,
          status: result.status,
          designSpec: toArtifactRef(result.designSpecArtifact),
          testReportId: testReport.id,
          fixHistory: result.fixHistory,
          schemaValid: result.schemaValid,
          securityValid: result.securityValid,
          replayValid: result.replayValid,
          dependencyPolicyValid: result.dependencyPolicyValid,
          findings: result.findings
        })
      );
      const updatedWorkspace = store.saveWorkspace(
        createUpdatedWorkspaceRecord(workspace, {
          artifacts: artifactRefs,
          files: buildArtifacts.map((artifact) => artifact.path),
          logs: result.logs,
          testReports: [testReport.id],
          retentionStatus: result.status === "failed" ? "retained" : "active"
        })
      );
      if (result.status === "failed") {
        const finishedAt = new Date().toISOString();
        const currentJob = store.getJob(runningJob.id) ?? runningJob;
        const failedJob = store.appendJobEvent(
          store.saveJob({
            ...currentJob,
            status: "failed",
            workspaceId: updatedWorkspace.id,
            updatedAt: finishedAt,
            finishedAt,
            error:
              evalReport.findings.map((finding) => finding.message).join("; ") ||
              "Generated-node eval failed.",
            result: {
              workspaceId: updatedWorkspace.id,
              testReportId: testReport.id,
              evalReportId: evalReport.id
            }
          }).id,
          createJobEvent(currentJob, "error", "Generated-node build loop failed eval.", {
            workspaceId: updatedWorkspace.id,
            testReportId: testReport.id,
            evalReportId: evalReport.id
          })
        );
        return reply.code(409).send({
          ok: false,
          error: "CODEGEN_EVAL_FAILED",
          message: failedJob.error ?? "Generated-node eval failed.",
          job: failedJob,
          workspace: updatedWorkspace,
          agentRuns: result.agentRuns,
          artifacts: artifactRefs,
          testReport,
          evalReport
        });
      }
      const workflowWithGeneratedNode: WorkflowSpec = {
        ...sourceWorkflow,
        approval: null,
        updatedAt: new Date().toISOString(),
        nodes: sourceWorkflow.nodes.map((candidate) =>
          candidate.id === node.id
            ? {
                ...candidate,
                config: {
                  ...candidate.config,
                  artifactStatus: "draft",
                  buildJobId: runningJob.id,
                  workspaceId: workspace.id
                },
                codegen: {
                  ...result.generation.metadata,
                  artifacts: artifactRefs
                }
              }
            : candidate
        )
      };
      const validation = validateWorkflowSpec(workflowWithGeneratedNode);
      if (!validation.ok) {
        throw new Error(validation.errors.map((issue) => issue.code).join(", "));
      }
      const draftRevision = store.saveDraftRevision(validation.workflow, validation, "validate", {
        branchId: branch?.id,
        force: true,
        parentDraftRevisionId: branchHead?.id
      });
      persistCodegenArtifactManifests(
        store,
        draftRevision.workflow,
        draftRevision.id,
        draftRevision.createdAt
      );
      const finishedAt = new Date().toISOString();
      const currentJob = store.getJob(runningJob.id) ?? runningJob;
      if (isTerminalJobStatus(currentJob.status)) {
        return reply.code(currentJob.status === "cancelled" ? 409 : 500).send({
          ok: false,
          error: currentJob.status === "cancelled" ? "JOB_CANCELLED" : "CODEGEN_BUILD_FAILED",
          message:
            currentJob.error ?? currentJob.cancellationReason ?? "Codegen build did not complete."
        });
      }
      const completedJob = store.appendJobEvent(
        store.saveJob({
          ...currentJob,
          status: "succeeded",
          workspaceId: updatedWorkspace.id,
          updatedAt: finishedAt,
          finishedAt,
          result: {
            draftRevisionId: draftRevision.id,
            workspaceId: updatedWorkspace.id,
            testReportId: testReport.id,
            evalReportId: evalReport.id
          }
        }).id,
        createJobEvent(runningJob, "info", "Generated-node build loop completed.", {
          draftRevisionId: draftRevision.id,
          workspaceId: updatedWorkspace.id,
          testReportId: testReport.id,
          evalReportId: evalReport.id
        })
      );

      return {
        ok: true,
        workflow: draftRevision.workflow,
        draftRevision,
        validation: draftRevision.validation,
        job: completedJob,
        workspace: updatedWorkspace,
        agentRuns: result.agentRuns,
        artifacts: artifactRefs,
        testReport,
        evalReport
      };
    } catch (error) {
      const finishedAt = new Date().toISOString();
      const currentJob = store.getJob(runningJob.id) ?? runningJob;
      if (error instanceof JobCancelledError || currentJob.status === "cancelled") {
        const cancelled = jobSupervisor.cancelJob(
          runningJob.id,
          error instanceof Error ? error.message : "Codegen build cancelled.",
          finishedAt
        );
        return reply.code(409).send({
          ok: false,
          error: "JOB_CANCELLED",
          message: cancelled.cancellationReason ?? "Codegen build cancelled."
        });
      }
      const failedJob = store.appendJobEvent(
        store.saveJob({
          ...currentJob,
          status: "failed",
          updatedAt: finishedAt,
          finishedAt,
          error:
            error instanceof Error ? redactSecretString(error.message) : "Codegen build failed."
        }).id,
        createJobEvent(runningJob, "error", "Generated-node build loop failed.")
      );

      return reply.code(500).send({
        ok: false,
        error: "CODEGEN_BUILD_FAILED",
        message: failedJob.error ?? "Codegen build failed."
      });
    } finally {
      jobSupervisor.finishJob(runningJob.id);
    }
  });

  app.get<{
    Params: CodegenRouteParams;
  }>("/api/workflows/:id/codegen/:nodeId/evals", async (request, reply) => {
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
      agentRuns: store.listAgentRuns(request.params.id, request.params.nodeId),
      agentArtifacts: store.listAgentArtifacts(request.params.id, request.params.nodeId),
      testReports: store.listGeneratedNodeTestReports(request.params.id, request.params.nodeId),
      evalReports: store.listGeneratedNodeEvalReports(request.params.id, request.params.nodeId)
    };
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

    const branch = request.body.branchId ? store.getBranch(request.body.branchId) : undefined;
    const branchHead = branch ? store.getDraftRevision(branch.headDraftRevisionId) : undefined;
    if (
      request.body.branchId &&
      (!branch || branch.workflowId !== request.params.id || !branchHead)
    ) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.body.branchId}' was not found for workflow '${request.params.id}'.`
      });
    }
    const sourceWorkflow = branchHead?.workflow ?? stored.workflow;
    const node = sourceWorkflow.nodes.find((candidate) => candidate.id === request.params.nodeId);
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
      ...sourceWorkflow,
      approval: null,
      updatedAt: now,
      nodes: sourceWorkflow.nodes.map((candidate) =>
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
      branchId: branch?.id,
      force: true,
      parentDraftRevisionId: branchHead?.id
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
      branchId: branch?.id,
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
    if (
      request.body.branchId &&
      approvedRevision.branchId &&
      request.body.branchId !== approvedRevision.branchId
    ) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_BRANCH_MISMATCH",
        message: `Approved revision '${approvedRevision.id}' belongs to branch '${approvedRevision.branchId}'.`
      } as never);
    }

    try {
      const correlationId = correlationIdForRequest(request);
      updateRequestJob(
        store,
        request,
        "running",
        "Workflow run started.",
        {
          approvedRevisionId: approvedRevision.id,
          ...(approvedRevision.branchId ? { branchId: approvedRevision.branchId } : {})
        },
        jobSupervisor
      );
      try {
        throwIfRequestJobCancelled(store, jobSupervisor, request);
      } catch (error) {
        if (error instanceof JobCancelledError) {
          updateRequestJob(store, request, "cancelled", error.message, undefined, jobSupervisor);
          return reply.code(409).send({
            ok: false,
            error: "JOB_CANCELLED",
            message: error.message
          } as never);
        }
        throw error;
      }
      const runId = `run.${approvedRevision.workflowId}.r${approvedRevision.revision}.${Date.now()}`;
      const dag = compileWorkflowDag(approvedRevision.workflow);
      const deployedRunnerConfig = latestDeployedRunnerConfiguration(
        store,
        approvedRevision.workflowId,
        approvedRevision.id,
        approvedRevision.branchId
      );
      const compiledAt = new Date().toISOString();
      const signal = requestJobId(request)
        ? jobSupervisor.signalFor(requestJobId(request) ?? "")
        : undefined;
      const result = await executeCompiledDag(dag, runner ?? createNanoClawRunner(), {
        codegenArtifactStore: artifactStore,
        runId,
        secretResolver: new SecretStoreResolver(secretStore),
        signal
      });
      throwIfRequestJobCancelled(store, jobSupervisor, request);
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
              nodeOrder: [...dag.order],
              ...(deployedRunnerConfig
                ? {
                    runnerDeploymentId: deployedRunnerConfig.id,
                    deployedRunnerConfig: deployedRunnerConfig.metadata.runnerConfig
                  }
                : {})
            }
          }),
          ...(result.events ?? createRunEvents(result.nodeResults, now))
        ],
        {
          workflowId: approvedRevision.workflowId,
          branchId: approvedRevision.branchId,
          revisionId: approvedRevision.id,
          runId,
          correlationId
        }
      );
      const run = store.saveRun({
        id: runId,
        workflowId: approvedRevision.workflowId,
        branchId: approvedRevision.branchId,
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
      updateRequestJob(
        store,
        request,
        result.status === "succeeded" ? "succeeded" : "failed",
        "Workflow run completed.",
        {
          runId: run.id,
          status: result.status
        },
        jobSupervisor
      );

      return reply.code(202).send({
        ok: true,
        run
      });
    } catch (error) {
      if (error instanceof JobCancelledError) {
        updateRequestJob(store, request, "cancelled", error.message, undefined, jobSupervisor);
        return reply.code(409).send({
          ok: false,
          error: "JOB_CANCELLED",
          message: error.message
        } as never);
      }
      if (error instanceof WorkflowValidationError) {
        updateRequestJob(
          store,
          request,
          "failed",
          "Workflow run failed validation.",
          undefined,
          jobSupervisor
        );
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

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/deployments", async (request) => ({
    ok: true,
    deployments: store.listDeployments(request.params.id)
  }));

  app.get<{ Params: RouteParamsWithId }>(
    "/api/workflows/:id/deployments/active",
    async (request) => ({
      ok: true,
      ...deploymentActivationSummary(store, request.params.id)
    })
  );

  app.post<{ Params: RouteParamsWithId; Body: DeploymentRequestBody }>(
    "/api/workflows/:id/deployments",
    async (request, reply) => {
      const approvedRevision = store.getApprovedRevision(request.body.approvedRevisionId);
      if (!approvedRevision || approvedRevision.workflowId !== request.params.id) {
        return reply.code(404).send({
          ok: false,
          error: "APPROVED_REVISION_NOT_FOUND",
          message: `Approved revision '${request.body.approvedRevisionId}' was not found for workflow '${request.params.id}'.`
        });
      }
      const deploymentBranchId = request.body.branchId ?? approvedRevision.branchId;
      if (
        request.body.branchId &&
        approvedRevision.branchId &&
        request.body.branchId !== approvedRevision.branchId
      ) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_BRANCH_MISMATCH",
          message: `Approved revision '${approvedRevision.id}' belongs to branch '${approvedRevision.branchId}'.`
        });
      }

      const latestEvaluation = store.getLatestDraftEvaluation(
        request.params.id,
        deploymentBranchId
      );
      if (!latestEvaluation || latestEvaluation.status !== "passed") {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_DRAFT_EVALUATION_REQUIRED",
          message: "Deployment requires a passing draft evaluation."
        });
      }
      if (!request.body.rollbackPlan.trim()) {
        return reply.code(422).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_BLOCKED",
          message: "Deployment requires a rollback plan."
        });
      }

      const missingSecrets = collectSecretRefs(approvedRevision.workflow)
        .map((secretRef) => secretRef.replace(/^secret:/u, ""))
        .filter(
          (secretName) => !secretStore.listSecrets().some((secret) => secret.name === secretName)
        );
      if (missingSecrets.length > 0) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_BLOCKED",
          message: `Deployment is missing secret metadata: ${missingSecrets.join(", ")}.`
        });
      }

      const requiredIntegrations = requiredIntegrationsForWorkflow(approvedRevision.workflow);
      const readiness = secretReadiness(secretStore);
      const blockedIntegrations = requiredIntegrations.filter(
        (integration) => readiness.find((item) => item.id === integration)?.ready !== true
      );
      if (blockedIntegrations.length > 0) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_BLOCKED",
          message: `Deployment is blocked by integration readiness: ${blockedIntegrations.join(", ")}.`
        });
      }
      const kindBlock = deploymentKindBlocker(request.body.kind, approvedRevision.workflow);
      if (kindBlock) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_BLOCKED",
          message: kindBlock
        });
      }

      const correlationId = correlationIdForRequest(request);
      const route = routeWorkflowTask(
        {
          prompt: `Deploy workflow '${request.params.id}' as ${request.body.kind}.`,
          currentWorkflow: approvedRevision.workflow
        },
        {
          correlationId,
          provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
          model: process.env.KELPCLAW_PLANNER_MODEL
        }
      );
      recordAudit(store, {
        action: "task.routed",
        actor: "router",
        workflowId: request.params.id,
        branchId: deploymentBranchId,
        revisionId: approvedRevision.id,
        correlationId,
        summary: `Routed workflow deployment as ${route.route}.`,
        metadata: jsonRecord({
          route: route.route,
          rationale: route.rationale,
          requiredModel: route.requiredModel,
          modelInvocations: route.modelInvocations
        })
      });
      updateRequestJob(
        store,
        request,
        "running",
        "Workflow deployment started.",
        {
          approvedRevisionId: approvedRevision.id,
          kind: request.body.kind
        },
        jobSupervisor
      );
      try {
        throwIfRequestJobCancelled(store, jobSupervisor, request);
      } catch (error) {
        if (error instanceof JobCancelledError) {
          updateRequestJob(store, request, "cancelled", error.message, undefined, jobSupervisor);
          return reply.code(409).send({
            ok: false,
            error: "JOB_CANCELLED",
            message: error.message
          });
        }
        throw error;
      }
      const audit = recordAudit(store, {
        action: "deployment.created",
        actor: request.body.createdBy,
        workflowId: request.params.id,
        branchId: deploymentBranchId,
        revisionId: approvedRevision.id,
        correlationId,
        summary: `Created ${request.body.kind} deployment record.`,
        secretRefs: collectSecretRefs(approvedRevision.workflow)
      });
      const deployment = createDeploymentRecord({
        workflowId: request.params.id,
        branchId: deploymentBranchId,
        approvedRevisionId: approvedRevision.id,
        draftEvaluationId: latestEvaluation.id,
        kind: request.body.kind,
        createdBy: request.body.createdBy,
        requiredIntegrations,
        secretRefs: collectSecretRefs(approvedRevision.workflow),
        rollbackPlan: request.body.rollbackPlan,
        auditRecordId: audit.id,
        metadata: request.body.metadata ?? {}
      });
      const nativeMetadata = await materializeNativeDeployment({
        deployment,
        approvedRevision,
        latestEvaluationId: latestEvaluation.id,
        artifactStore
      });
      const savedDeployment = store.saveDeployment({
        ...deployment,
        status: "deployed",
        metadata: {
          ...deployment.metadata,
          ...nativeMetadata
        }
      });
      updateRequestJob(
        store,
        request,
        "succeeded",
        "Workflow deployment completed.",
        {
          deploymentId: savedDeployment.id,
          status: savedDeployment.status
        },
        jobSupervisor
      );

      return reply.code(201).send({
        ok: true,
        deployment: savedDeployment
      });
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
          workflowId: request.params.id,
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

async function validateApprovalReadiness(
  store: WorkflowStore,
  workflow: WorkflowSpec,
  artifactStore: CodegenArtifactStore,
  branchId?: string | undefined
): Promise<readonly WorkflowValidationIssue[]> {
  const issues: WorkflowValidationIssue[] = [];
  const latestEvaluation = store.getLatestDraftEvaluation(workflow.id, branchId);
  if (!latestEvaluation || latestEvaluation.status !== "passed") {
    issues.push({
      code: "WORKFLOW_DRAFT_EVALUATION_REQUIRED",
      message: `Workflow '${workflow.id}' must pass draft evaluation before approval.`,
      path: ["draftEvaluation"]
    });
  }

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

    const latestEval = store
      .listGeneratedNodeEvalReports(workflow.id, node.id)
      .filter(
        (report) =>
          report.status === "passed" && (branchId === undefined || report.branchId === branchId)
      )
      .at(-1);
    if (!latestEval) {
      issues.push({
        code: "WORKFLOW_CODEGEN_EVAL_REQUIRED",
        message: `Codegen node '${node.id}' must pass generated-node eval before workflow approval.`,
        path: ["nodes", index, "codegen"]
      });
    } else if (
      !latestEval.schemaValid ||
      !latestEval.securityValid ||
      !latestEval.replayValid ||
      !latestEval.dependencyPolicyValid
    ) {
      issues.push({
        code: "WORKFLOW_CODEGEN_EVAL_REQUIRED",
        message: `Codegen node '${node.id}' generated-node eval did not satisfy approval gates.`,
        path: ["nodes", index, "codegen"]
      });
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

function createGeneratedModuleSignature(node: WorkflowNode): WorkflowGeneratedModuleSignature {
  if (!node.codegen) {
    throw new Error(`Workflow node '${node.id}' does not have generated module metadata.`);
  }

  return {
    promptHash: hashReusableModuleValue(node.codegen.latestPrompt),
    inputSchemaHash: hashReusableModuleValue(node.inputs),
    outputSchemaHash: hashReusableModuleValue(node.outputs),
    runtimeHash: hashReusableModuleValue(node.runtime),
    sandboxHash: hashReusableModuleValue(node.codegen.sandbox),
    dependencyManifestHash: hashReusableModuleValue(node.codegen.dependencyManifest),
    replaySeed: node.codegen.replay.seed,
    artifactHash: hashReusableModuleValue(
      node.codegen.artifacts
        .map((artifact) => ({
          path: artifact.path,
          checksum: artifact.checksum,
          contentType: artifact.contentType
        }))
        .sort((left, right) => left.path.localeCompare(right.path))
    )
  };
}

function generatedModuleSignaturesMatch(
  left: WorkflowGeneratedModuleSignature,
  right: WorkflowGeneratedModuleSignature
): boolean {
  return stableJsonStringify(left as never) === stableJsonStringify(right as never);
}

function hashReusableModuleValue(value: unknown): string {
  return checksumArtifactContent(
    typeof value === "string" ? value : stableJsonStringify(value as never)
  );
}

function applyGeneratedModuleReuse(
  store: WorkflowStore,
  workflowId: string,
  branchId: string,
  workflow: WorkflowSpec
): {
  readonly workflow: WorkflowSpec;
  readonly decisions: readonly WorkflowGeneratedModuleReuseDecision[];
  readonly changed: boolean;
} {
  const decisions = computeGeneratedModuleReuseDecisions(store, workflowId, branchId, workflow);
  const reusable = new Map(
    decisions
      .filter((decision) => decision.status === "reuse" || decision.status === "reuse-with-reeval")
      .map((decision) => [decision.nodeId, decision])
  );
  if (reusable.size === 0) {
    return { workflow, decisions, changed: false };
  }

  let changed = false;
  const nodes = workflow.nodes.map((node) => {
    const decision = reusable.get(node.id);
    if (!decision?.sourceDraftRevisionId || node.kind !== "codegen") {
      return node;
    }
    const sourceDraft = store.getDraftRevision(decision.sourceDraftRevisionId);
    const sourceNode = sourceDraft?.workflow.nodes.find(
      (candidate) => candidate.id === node.id && candidate.kind === "codegen"
    );
    if (!sourceNode?.codegen) {
      return node;
    }
    changed = true;
    return {
      ...node,
      config: {
        ...node.config,
        artifactStatus: "draft",
        reusedFromBranchId: decision.sourceBranchId ?? "",
        reusedFromDraftRevisionId: decision.sourceDraftRevisionId
      },
      codegen: {
        ...sourceNode.codegen,
        review: {
          status: "draft" as const,
          notes: `Reused from ${decision.sourceBranchId}; branch-local eval is required.`
        }
      }
    };
  });

  return {
    workflow: {
      ...workflow,
      approval: null,
      nodes
    },
    decisions,
    changed
  };
}

function computeGeneratedModuleReuseDecisions(
  store: WorkflowStore,
  workflowId: string,
  branchId: string,
  workflow: WorkflowSpec
): readonly WorkflowGeneratedModuleReuseDecision[] {
  return workflow.nodes
    .filter((node) => node.kind === "codegen" && node.codegen)
    .map((node) => {
      const signature = createGeneratedModuleSignature(node);
      const candidate = findGeneratedModuleReuseCandidate(
        store,
        workflowId,
        branchId,
        node,
        signature
      );
      const now = new Date().toISOString();
      if (candidate?.matching) {
        return {
          id: `reuse.${workflowId}.${branchId}.${node.id}.${Date.now()}.${randomUUID()}`,
          workflowId,
          branchId,
          nodeId: node.id,
          status: "reuse-with-reeval" as const,
          createdAt: now,
          sourceBranchId: candidate.branch.id,
          sourceDraftRevisionId: candidate.draft.id,
          sourceEvalReportId: candidate.evalReportId,
          signature,
          gates: [],
          reason:
            "Generated module signature matches a passed module from another branch; branch-local re-evaluation is required.",
          artifacts: candidate.node.codegen?.artifacts ?? []
        };
      }
      const gates = candidate?.node.codegen
        ? reuseMismatchGates(signature, createGeneratedModuleSignature(candidate.node))
        : (["evaluation"] as const);
      return {
        id: `reuse.${workflowId}.${branchId}.${node.id}.${Date.now()}.${randomUUID()}`,
        workflowId,
        branchId,
        nodeId: node.id,
        status: candidate ? ("blocked-drift" as const) : ("regenerate" as const),
        createdAt: now,
        sourceBranchId: candidate?.branch.id,
        sourceDraftRevisionId: candidate?.draft.id,
        signature,
        gates,
        reason: candidate
          ? `Generated module reuse blocked by ${gates.join(", ")} gate drift.`
          : "No compatible generated module was found on another branch.",
        artifacts: []
      };
    });
}

function findGeneratedModuleReuseCandidate(
  store: WorkflowStore,
  workflowId: string,
  branchId: string,
  node: WorkflowNode,
  signature: ReturnType<typeof createGeneratedModuleSignature>
):
  | {
      readonly matching: boolean;
      readonly branch: ReturnType<WorkflowStore["getDefaultBranch"]>;
      readonly draft: NonNullable<ReturnType<WorkflowStore["getDraftRevision"]>>;
      readonly node: WorkflowNode;
      readonly evalReportId?: string | undefined;
    }
  | undefined {
  let fallback:
    | {
        readonly matching: false;
        readonly branch: ReturnType<WorkflowStore["getDefaultBranch"]>;
        readonly draft: NonNullable<ReturnType<WorkflowStore["getDraftRevision"]>>;
        readonly node: WorkflowNode;
      }
    | undefined;
  for (const branch of store.listBranches(workflowId)) {
    if (branch.id === branchId) {
      continue;
    }
    const draft = store.getDraftRevision(branch.headDraftRevisionId);
    const candidate = draft?.workflow.nodes.find(
      (item) => item.id === node.id && item.kind === "codegen" && item.codegen
    );
    if (!draft || !candidate?.codegen) {
      continue;
    }
    const candidateSignature = createGeneratedModuleSignature(candidate);
    const latestPassedEval = store
      .listGeneratedNodeEvalReports(workflowId, candidate.id)
      .filter(
        (report) =>
          report.status === "passed" &&
          (report.branchId === undefined || report.branchId === branch.id)
      )
      .at(-1);
    if (generatedModuleSignaturesMatch(signature, candidateSignature) && latestPassedEval) {
      return {
        matching: true,
        branch,
        draft,
        node: candidate,
        evalReportId: latestPassedEval.id
      };
    }
    fallback = fallback ?? {
      matching: false,
      branch,
      draft,
      node: candidate
    };
  }
  return fallback;
}

function reuseMismatchGates(
  left: ReturnType<typeof createGeneratedModuleSignature>,
  right: ReturnType<typeof createGeneratedModuleSignature>
): readonly WorkflowGeneratedModuleReuseGate[] {
  const gates: WorkflowGeneratedModuleReuseGate[] = [];
  if (left.promptHash !== right.promptHash) {
    gates.push("prompt");
  }
  if (
    left.inputSchemaHash !== right.inputSchemaHash ||
    left.outputSchemaHash !== right.outputSchemaHash
  ) {
    gates.push("schema");
  }
  if (left.runtimeHash !== right.runtimeHash) {
    gates.push("runtime");
  }
  if (left.sandboxHash !== right.sandboxHash) {
    gates.push("sandbox");
  }
  if (left.dependencyManifestHash !== right.dependencyManifestHash) {
    gates.push("dependency");
  }
  if (left.replaySeed !== right.replaySeed || left.artifactHash !== right.artifactHash) {
    gates.push("replay");
  }
  return gates.length > 0 ? gates : ["evaluation"];
}

function createBranchMergePreview(
  store: WorkflowStore,
  workflowId: string,
  sourceBranchId: string,
  input: {
    readonly targetBranchId: string;
    readonly mode: "merge" | "cherry-pick";
    readonly cherryPickChangeIds?: readonly string[] | undefined;
    readonly resolutions?: readonly WorkflowBranchMergeResolution[] | undefined;
  }
): WorkflowBranchMergePreview | undefined {
  const sourceBranch = store.getBranch(sourceBranchId);
  const targetBranch = store.getBranch(input.targetBranchId);
  if (!sourceBranch || !targetBranch) {
    return undefined;
  }
  if (sourceBranch.workflowId !== workflowId || targetBranch.workflowId !== workflowId) {
    return undefined;
  }
  const baseDraft = store.getDraftRevision(sourceBranch.baseDraftRevisionId);
  const sourceHead = store.getDraftRevision(sourceBranch.headDraftRevisionId);
  const targetHead = store.getDraftRevision(targetBranch.headDraftRevisionId);
  if (!baseDraft || !sourceHead || !targetHead) {
    return undefined;
  }

  const sourceDiff = createWorkflowGraphDiff({
    id: `graphdiff.${workflowId}.source.${Date.now()}.${randomUUID()}`,
    baseWorkflow: baseDraft.workflow,
    editedWorkflow: sourceHead.workflow
  });
  const selectedChangeIds =
    input.mode === "cherry-pick" ? new Set(input.cherryPickChangeIds ?? []) : undefined;
  const result = mergeWorkflowGraphs({
    base: baseDraft.workflow,
    source: sourceHead.workflow,
    target: targetHead.workflow,
    sourceChanges: sourceDiff.changes,
    selectedChangeIds,
    resolutions: input.resolutions ?? []
  });
  const graphDiff = createWorkflowGraphDiff({
    id: `graphdiff.${workflowId}.merge.${Date.now()}.${randomUUID()}`,
    baseWorkflow: targetHead.workflow,
    editedWorkflow: result.workflow,
    createdAt: new Date().toISOString()
  });
  const validation = validateWorkflowSpec(result.workflow);
  const validationConflicts: WorkflowBranchMergeConflict[] = validation.ok
    ? []
    : validation.errors.map((issue, index) => ({
        id: `conflict.validation.${index}`,
        kind: "validation-blocked",
        elementKind:
          issue.path[0] === "edges" ? "edge" : issue.path[0] === "nodes" ? "node" : "workflow",
        path: issue.path,
        message: issue.message
      }));
  const conflicts = [...result.conflicts, ...validationConflicts];
  const status = conflicts.length > 0 ? "conflicts" : validation.ok ? "clean" : "blocked";

  return {
    id: `merge.${workflowId}.${Date.now()}.${randomUUID()}`,
    workflowId,
    sourceBranchId,
    targetBranchId: input.targetBranchId,
    mode: input.mode,
    status,
    createdAt: graphDiff.createdAt,
    baseDraftRevisionId: baseDraft.id,
    sourceHeadDraftRevisionId: sourceHead.id,
    targetHeadDraftRevisionId: targetHead.id,
    graphDiff,
    conflicts,
    ...(conflicts.length === 0 && validation.ok ? { mergedWorkflow: result.workflow } : {}),
    validation,
    summary:
      conflicts.length > 0
        ? [`Merge has ${conflicts.length} conflict${conflicts.length === 1 ? "" : "s"}.`]
        : graphDiff.summary
  };
}

function mergeWorkflowGraphs(input: {
  readonly base: WorkflowSpec;
  readonly source: WorkflowSpec;
  readonly target: WorkflowSpec;
  readonly sourceChanges: readonly WorkflowGraphChange[];
  readonly selectedChangeIds?: ReadonlySet<string> | undefined;
  readonly resolutions: readonly WorkflowBranchMergeResolution[];
}): {
  readonly workflow: WorkflowSpec;
  readonly conflicts: readonly WorkflowBranchMergeConflict[];
} {
  const resolutionMap = new Map(
    input.resolutions.map((resolution) => [resolution.conflictId, resolution])
  );
  const nodes = mergeElements({
    kind: "node",
    base: new Map(input.base.nodes.map((node) => [node.id, node])),
    source: new Map(input.source.nodes.map((node) => [node.id, node])),
    target: new Map(input.target.nodes.map((node) => [node.id, node])),
    sourceChanges: input.sourceChanges,
    selectedChangeIds: input.selectedChangeIds,
    resolutions: resolutionMap
  });
  const edges = mergeElements({
    kind: "edge",
    base: new Map(input.base.edges.map((edge) => [edge.id, edge])),
    source: new Map(input.source.edges.map((edge) => [edge.id, edge])),
    target: new Map(input.target.edges.map((edge) => [edge.id, edge])),
    sourceChanges: input.sourceChanges,
    selectedChangeIds: input.selectedChangeIds,
    resolutions: resolutionMap
  });
  const mergedNodes = nodes.values as WorkflowNode[];
  const mergedEdges = edges.values as WorkflowEdge[];
  const nodeIds = new Set(mergedNodes.map((node) => node.id));
  const endpointConflicts = mergedEdges
    .filter((edge) => !nodeIds.has(edge.source.nodeId) || !nodeIds.has(edge.target.nodeId))
    .map(
      (edge): WorkflowBranchMergeConflict => ({
        id: `conflict.edge.endpoint.${edge.id}`,
        kind: "missing-edge-endpoint",
        elementKind: "edge",
        elementId: edge.id,
        path: ["edges", edge.id],
        message: `Merged edge '${edge.id}' references a missing source or target node.`,
        sourceValue: edge as unknown as JsonValue
      })
    );

  return {
    workflow: {
      ...input.target,
      prompt: input.source.prompt,
      nodes: mergedNodes,
      edges: mergedEdges,
      approval: null
    },
    conflicts: [...nodes.conflicts, ...edges.conflicts, ...endpointConflicts]
  };
}

function mergeElements<T extends WorkflowNode | WorkflowEdge>(input: {
  readonly kind: "node" | "edge";
  readonly base: ReadonlyMap<string, T>;
  readonly source: ReadonlyMap<string, T>;
  readonly target: ReadonlyMap<string, T>;
  readonly sourceChanges: readonly WorkflowGraphChange[];
  readonly selectedChangeIds?: ReadonlySet<string> | undefined;
  readonly resolutions: ReadonlyMap<string, WorkflowBranchMergeResolution>;
}): { readonly values: readonly T[]; readonly conflicts: readonly WorkflowBranchMergeConflict[] } {
  const ids = new Set([...input.base.keys(), ...input.source.keys(), ...input.target.keys()]);
  const values: T[] = [];
  const conflicts: WorkflowBranchMergeConflict[] = [];
  for (const id of [...ids].sort()) {
    const baseValue = input.base.get(id);
    const sourceValue = sourceValueForMerge(input, id, baseValue);
    const targetValue = input.target.get(id);
    const conflict = conflictForElement(input.kind, id, baseValue, sourceValue, targetValue);
    if (conflict) {
      const resolution = input.resolutions.get(conflict.id);
      if (!resolution) {
        conflicts.push(conflict);
        if (targetValue) {
          values.push(targetValue);
        }
        continue;
      }
      const resolved = resolveConflictValue(resolution, conflict);
      if (resolved !== undefined) {
        values.push(resolved as unknown as T);
      }
      continue;
    }

    const merged = automaticMergeValue(baseValue, sourceValue, targetValue);
    if (merged !== undefined) {
      values.push(merged);
    }
  }
  return { values, conflicts };
}

function sourceValueForMerge<T extends WorkflowNode | WorkflowEdge>(
  input: {
    readonly kind: "node" | "edge";
    readonly base: ReadonlyMap<string, T>;
    readonly source: ReadonlyMap<string, T>;
    readonly sourceChanges: readonly WorkflowGraphChange[];
    readonly selectedChangeIds?: ReadonlySet<string> | undefined;
  },
  id: string,
  baseValue: T | undefined
): T | undefined {
  if (!input.selectedChangeIds) {
    return input.source.get(id);
  }
  const selected = input.sourceChanges.some(
    (change) =>
      change.elementId === id &&
      change.kind.startsWith(input.kind) &&
      input.selectedChangeIds?.has(change.id)
  );
  return selected ? input.source.get(id) : baseValue;
}

function automaticMergeValue<T>(
  baseValue: T | undefined,
  sourceValue: T | undefined,
  targetValue: T | undefined
): T | undefined {
  if (sameJson(sourceValue, baseValue)) {
    return targetValue;
  }
  if (sameJson(targetValue, baseValue) || sameJson(sourceValue, targetValue)) {
    return sourceValue;
  }
  return targetValue;
}

function conflictForElement<T extends WorkflowNode | WorkflowEdge>(
  elementKind: "node" | "edge",
  id: string,
  baseValue: T | undefined,
  sourceValue: T | undefined,
  targetValue: T | undefined
): WorkflowBranchMergeConflict | undefined {
  if (
    sameJson(sourceValue, baseValue) ||
    sameJson(targetValue, baseValue) ||
    sameJson(sourceValue, targetValue)
  ) {
    return undefined;
  }
  const sourceDeleted = sourceValue === undefined && baseValue !== undefined;
  const targetDeleted = targetValue === undefined && baseValue !== undefined;
  const bothAdded =
    baseValue === undefined && sourceValue !== undefined && targetValue !== undefined;
  const kind = bothAdded
    ? "add-add-id-collision"
    : sourceDeleted || targetDeleted
      ? "delete-edit"
      : classifyMergeConflict(baseValue, sourceValue, targetValue);
  return {
    id: `conflict.${elementKind}.${kind}.${id}`,
    kind,
    elementKind,
    elementId: id,
    path: [elementKind === "node" ? "nodes" : "edges", id],
    message: `Branch ${elementKind} '${id}' has conflicting ${kind.replace(/-/gu, " ")} changes.`,
    baseValue: baseValue as unknown as JsonValue,
    sourceValue: sourceValue as unknown as JsonValue,
    targetValue: targetValue as unknown as JsonValue
  };
}

function classifyMergeConflict<T extends WorkflowNode | WorkflowEdge>(
  baseValue: T | undefined,
  sourceValue: T | undefined,
  targetValue: T | undefined
): WorkflowBranchMergeConflict["kind"] {
  if (isWorkflowNode(baseValue) || isWorkflowNode(sourceValue) || isWorkflowNode(targetValue)) {
    const sourceNode = isWorkflowNode(sourceValue) ? sourceValue : undefined;
    const targetNode = isWorkflowNode(targetValue) ? targetValue : undefined;
    if (!sameJson(sourceNode?.codegen, targetNode?.codegen)) {
      return "codegen-drift";
    }
    if (!sameJson(sourceNode?.runtime, targetNode?.runtime)) {
      return "runtime-drift";
    }
    if (
      !sameJson(sourceNode?.inputs, targetNode?.inputs) ||
      !sameJson(sourceNode?.outputs, targetNode?.outputs)
    ) {
      return "schema-drift";
    }
  }
  return "both-edited";
}

function resolveConflictValue(
  resolution: WorkflowBranchMergeResolution,
  conflict: WorkflowBranchMergeConflict
): JsonValue | undefined {
  if (resolution.choice === "manual") {
    return resolution.value;
  }
  return resolution.choice === "source" ? conflict.sourceValue : conflict.targetValue;
}

function isWorkflowNode(value: unknown): value is WorkflowNode {
  return typeof value === "object" && value !== null && "kind" in value && "runtime" in value;
}

function sameJson(left: unknown, right: unknown): boolean {
  return (
    stableJsonStringify((left ?? null) as never) === stableJsonStringify((right ?? null) as never)
  );
}

function graphDiffToSpecDiff(before: WorkflowSpec, after: WorkflowSpec) {
  return createWorkflowSpecDiff(before, after);
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
  readonly payload?: JsonRecord | undefined;
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
    ...(input.payload ? { payload: input.payload } : {}),
    events: []
  };

  return {
    ...job,
    events: [createJobEvent(job, "info", `Queued ${input.type} job.`)]
  };
}

function createWorkflowWorkspace(input: {
  readonly jobId: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly revisionId?: string | undefined;
}): WorkflowWorkspace {
  const now = new Date().toISOString();
  const id = `workspace.${input.jobId}`;
  const rootPath = join(process.cwd(), ".kelpclaw", "workspaces", id);
  const mountedAgents: WorkflowWorkspace["mountedAgents"] = [
    "workflow-architect",
    "coder",
    "tester",
    "runner",
    "fixer",
    "evaluator"
  ];
  mkdirSync(rootPath, { recursive: true });
  mkdirSync(resolveWorkspacePath(rootPath, "logs"), { recursive: true });
  mkdirSync(resolveWorkspacePath(rootPath, "artifacts"), { recursive: true });

  const mounts = mountedAgents.map((role) => {
    const mountPath = resolveWorkspacePath(rootPath, `mounts/${role}`);
    mkdirSync(mountPath, { recursive: true });
    return {
      role,
      path: mountPath,
      mode: role === "workflow-architect" ? ("ro" as const) : ("rw" as const)
    };
  });

  const workspace: WorkflowWorkspace = {
    id,
    jobId: input.jobId,
    workflowId: input.workflowId,
    branchId: input.branchId,
    revisionId: input.revisionId,
    rootPath,
    createdAt: now,
    updatedAt: now,
    mountedAgents,
    mounts,
    filesCreated: [],
    fileHashes: [],
    artifactsProduced: [],
    logs: [],
    logPaths: [],
    testReports: [],
    retentionPolicy: "retain-on-failure",
    retentionStatus: "active"
  };

  return workspace;
}

function createUpdatedWorkspaceRecord(
  workspace: WorkflowWorkspace,
  input: {
    readonly artifacts: readonly WorkflowCodegenArtifactRef[];
    readonly files: readonly string[];
    readonly logs: readonly string[];
    readonly testReports: readonly string[];
    readonly retentionStatus: WorkflowWorkspace["retentionStatus"];
  }
): WorkflowWorkspace {
  const artifactRefs = input.artifacts.map((artifact) => {
    resolveWorkspacePath(workspace.rootPath, artifact.path);
    return artifact;
  });
  const filePaths = input.files.map((file) =>
    normalizeWorkspaceRelativePath(workspace.rootPath, file)
  );
  const existingLogCount = workspace.logs.length;
  const logPaths = input.logs.map((log, index) => {
    const relativeLogPath = `logs/build-${existingLogCount + index + 1}.log`;
    const absolutePath = resolveWorkspacePath(workspace.rootPath, relativeLogPath);
    mkdirSync(dirname(absolutePath), { recursive: true });
    writeFileSync(absolutePath, log, "utf8");
    return {
      relativePath: relativeLogPath,
      absolutePath
    };
  });
  const filesCreated = uniqueStrings([
    ...workspace.filesCreated,
    ...filePaths,
    ...logPaths.map((log) => log.relativePath)
  ]);

  return {
    ...workspace,
    updatedAt: new Date().toISOString(),
    filesCreated,
    fileHashes: hashWorkspaceFiles(workspace.rootPath, filesCreated),
    artifactsProduced: artifactRefs,
    logs: [...workspace.logs, ...input.logs],
    logPaths: uniqueStrings([...workspace.logPaths, ...logPaths.map((log) => log.absolutePath)]),
    testReports: uniqueStrings([...workspace.testReports, ...input.testReports]),
    retentionStatus: input.retentionStatus
  };
}

function resolveWorkspacePath(workspaceRoot: string, path: string): string {
  const root = resolve(workspaceRoot);
  const target = resolve(root, path);
  const relativePath = relative(root, target);
  if (relativePath.startsWith("..") || isAbsolute(relativePath)) {
    throw new Error(`Workspace path '${path}' must stay inside '${workspaceRoot}'.`);
  }

  return target;
}

function normalizeWorkspaceRelativePath(workspaceRoot: string, path: string): string {
  const absolutePath = resolveWorkspacePath(workspaceRoot, path);
  return relative(resolve(workspaceRoot), absolutePath);
}

function hashWorkspaceFiles(
  workspaceRoot: string,
  paths: readonly string[]
): WorkflowWorkspace["fileHashes"] {
  return paths
    .filter((path) => existsSync(resolveWorkspacePath(workspaceRoot, path)))
    .map((path) => ({
      path,
      checksum: `sha256:${createHash("sha256")
        .update(readFileSync(resolveWorkspacePath(workspaceRoot, path)))
        .digest("hex")}`
    }))
    .sort((left, right) => left.path.localeCompare(right.path));
}

function uniqueStrings(values: readonly string[]): readonly string[] {
  return [...new Set(values)].sort();
}

function createGeneratedNodeTestReport(input: {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly nodeId: string;
  readonly jobId: string;
  readonly status: GeneratedNodeTestReport["status"];
  readonly testFiles: readonly WorkflowCodegenArtifactRef[];
  readonly resultArtifacts: readonly WorkflowCodegenArtifactRef[];
  readonly logs: readonly string[];
  readonly failureMessage?: string | undefined;
}): GeneratedNodeTestReport {
  const now = new Date().toISOString();

  return {
    id: `test-report.${input.jobId}.${input.nodeId}`,
    workflowId: input.workflowId,
    branchId: input.branchId,
    nodeId: input.nodeId,
    jobId: input.jobId,
    status: input.status,
    createdAt: now,
    finishedAt: now,
    testFiles: input.testFiles,
    resultArtifacts: input.resultArtifacts,
    logs: input.logs,
    ...(input.failureMessage ? { failureMessage: input.failureMessage } : {})
  };
}

function createGeneratedNodeEvalReport(input: {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly nodeId: string;
  readonly jobId: string;
  readonly status: GeneratedNodeEvalReport["status"];
  readonly designSpec: WorkflowCodegenArtifactRef;
  readonly testReportId: string;
  readonly fixHistory: readonly string[];
  readonly schemaValid?: boolean | undefined;
  readonly securityValid?: boolean | undefined;
  readonly replayValid?: boolean | undefined;
  readonly dependencyPolicyValid?: boolean | undefined;
  readonly findings?: GeneratedNodeEvalReport["findings"] | undefined;
}): GeneratedNodeEvalReport {
  const now = new Date().toISOString();

  return {
    id: `eval-report.${input.jobId}.${input.nodeId}`,
    workflowId: input.workflowId,
    branchId: input.branchId,
    nodeId: input.nodeId,
    jobId: input.jobId,
    status: input.status,
    createdAt: now,
    finishedAt: now,
    designSpec: input.designSpec,
    testReportId: input.testReportId,
    schemaValid: input.schemaValid ?? input.status === "passed",
    securityValid: input.securityValid ?? input.status === "passed",
    replayValid: input.replayValid ?? input.status === "passed",
    dependencyPolicyValid: input.dependencyPolicyValid ?? input.status === "passed",
    fixHistory: input.fixHistory,
    findings: input.findings ?? []
  };
}

function createDeploymentRecord(input: {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly approvedRevisionId: string;
  readonly draftEvaluationId: string;
  readonly kind: WorkflowDeploymentKind;
  readonly createdBy: string;
  readonly requiredIntegrations: readonly string[];
  readonly secretRefs: readonly string[];
  readonly rollbackPlan: string;
  readonly auditRecordId: string;
  readonly metadata: JsonRecord;
}): WorkflowDeploymentRecord {
  return {
    id: `deployment.${input.workflowId}.${Date.now()}.${randomUUID()}`,
    workflowId: input.workflowId,
    branchId: input.branchId,
    approvedRevisionId: input.approvedRevisionId,
    draftEvaluationId: input.draftEvaluationId,
    kind: input.kind,
    status: "ready",
    createdAt: new Date().toISOString(),
    createdBy: input.createdBy,
    requiredIntegrations: input.requiredIntegrations,
    secretRefs: input.secretRefs,
    rollbackPlan: input.rollbackPlan,
    auditRecordId: input.auditRecordId,
    metadata: input.metadata
  };
}

function deploymentKindBlocker(
  kind: WorkflowDeploymentKind,
  workflow: WorkflowSpec
): string | null {
  const approvedCodegenNodes = workflow.nodes.filter(
    (node) => node.kind === "codegen" && node.codegen?.review.status === "approved"
  );
  if (
    (kind === "skill.publication" || kind === "generated.service") &&
    approvedCodegenNodes.length === 0
  ) {
    return `${kind} requires at least one approved generated node.`;
  }
  if (
    kind === "schedule.activation" &&
    !workflow.nodes.some((node) => typeof node.config.schedule === "string")
  ) {
    return "schedule.activation requires at least one scheduled trigger node.";
  }

  return null;
}

async function materializeNativeDeployment(input: {
  readonly deployment: WorkflowDeploymentRecord;
  readonly approvedRevision: Exclude<ReturnType<WorkflowStore["getApprovedRevision"]>, undefined>;
  readonly latestEvaluationId: string;
  readonly artifactStore: CodegenArtifactStore;
}): Promise<JsonRecord> {
  const workflow = input.approvedRevision.workflow;
  switch (input.deployment.kind) {
    case "schedule.activation": {
      const schedules = workflow.nodes
        .filter((node) => typeof node.config.schedule === "string")
        .map((node) => ({
          deploymentId: input.deployment.id,
          nodeId: node.id,
          label: node.label,
          schedule: String(node.config.schedule),
          status: "active",
          registeredAt: input.deployment.createdAt
        }));
      const artifact = await writeDeploymentArtifact(input, "schedule-activation.json", {
        deploymentId: input.deployment.id,
        approvedRevisionId: input.approvedRevision.id,
        schedules
      });
      return jsonRecord({
        activation: {
          status: "active",
          schedules
        },
        activeScheduleRegistrations: schedules,
        artifacts: [artifact]
      });
    }
    case "skill.publication": {
      const promotedSkills = workflow.nodes
        .filter((node) => node.kind === "codegen" && node.codegen?.review.status === "approved")
        .map((node) => registerPromotedSkill(createPromotedSkill(workflow, node)));
      const artifact = await writeDeploymentArtifact(input, "promoted-skills.json", {
        deploymentId: input.deployment.id,
        promotedSkills
      });
      return jsonRecord({
        skillPublications: promotedSkills.map((skill) => ({
          deploymentId: input.deployment.id,
          skillId: skill.id,
          name: skill.name,
          status: "published",
          publishedAt: input.deployment.createdAt
        })),
        promotedSkills,
        artifacts: [artifact]
      });
    }
    case "integration.configuration": {
      const bindings = requiredIntegrationsForWorkflow(workflow).map((integration) => ({
        deploymentId: input.deployment.id,
        integration,
        status: "ready",
        secretRefs: collectSecretRefs(workflow).filter((secretRef) =>
          secretRef.toLowerCase().includes(integration)
        )
      }));
      const artifact = await writeDeploymentArtifact(input, "integration-config.json", {
        deploymentId: input.deployment.id,
        bindings
      });
      return jsonRecord({
        integrationBindings: bindings,
        artifacts: [artifact]
      });
    }
    case "runner.configuration": {
      const dag = compileWorkflowDag(workflow);
      const runnerConfig = {
        deploymentId: input.deployment.id,
        approvedRevisionId: input.approvedRevision.id,
        dagHash: dag.dagHash,
        nodeOrder: [...dag.order],
        status: "active",
        activatedAt: input.deployment.createdAt
      };
      const artifact = await writeDeploymentArtifact(input, "runner-config.json", {
        ...runnerConfig
      });
      return jsonRecord({
        runner: runnerConfig,
        runnerConfig,
        artifacts: [artifact]
      });
    }
    case "workflow.bundle": {
      const artifact = await writeDeploymentArtifact(input, "workflow-bundle.json", {
        deploymentId: input.deployment.id,
        approvedRevision: input.approvedRevision,
        draftEvaluationId: input.latestEvaluationId,
        rollbackPlan: input.deployment.rollbackPlan
      });
      return jsonRecord({
        bundle: {
          ...artifact,
          deploymentId: input.deployment.id,
          approvedRevisionId: input.approvedRevision.id
        },
        artifacts: [artifact]
      });
    }
    case "generated.service": {
      const services = workflow.nodes
        .filter((node) => node.kind === "codegen" && node.codegen?.review.status === "approved")
        .map((node) => ({
          nodeId: node.id,
          runtime: node.runtime,
          artifacts: node.codegen?.artifacts ?? []
        }));
      const artifact = await writeDeploymentArtifact(input, "generated-service.json", {
        deploymentId: input.deployment.id,
        services
      });
      return jsonRecord({
        generatedServiceConfig: {
          deploymentId: input.deployment.id,
          status: "active",
          services
        },
        generatedServices: services,
        artifacts: [artifact]
      });
    }
  }
}

async function writeDeploymentArtifact(
  input: {
    readonly deployment: WorkflowDeploymentRecord;
    readonly artifactStore: CodegenArtifactStore;
  },
  filename: string,
  payload: unknown
): Promise<WorkflowCodegenArtifactRef> {
  const stored = await input.artifactStore.putArtifact(
    createGeneratedArtifact({
      path: `deployments/${input.deployment.id}/${filename}`,
      content: JSON.stringify(payload, null, 2),
      contentType: "application/json"
    })
  );
  return stored.ref;
}

function deploymentActivationSummary(
  store: WorkflowStore,
  workflowId: string
): {
  readonly activeDeployments: readonly WorkflowDeploymentRecord[];
  readonly activeSchedules: readonly JsonRecord[];
  readonly runnerConfigurations: readonly JsonRecord[];
  readonly skillPublications: readonly JsonRecord[];
  readonly integrationBindings: readonly JsonRecord[];
  readonly bundles: readonly JsonRecord[];
  readonly generatedServices: readonly JsonRecord[];
} {
  const activeDeployments = store
    .listDeployments(workflowId)
    .filter((deployment) => deployment.status === "deployed");

  return {
    activeDeployments,
    activeSchedules: activeDeployments.flatMap((deployment) =>
      jsonArrayMetadata(deployment.metadata.activeScheduleRegistrations)
    ),
    runnerConfigurations: activeDeployments.flatMap((deployment) =>
      jsonObjectMetadata(deployment.metadata.runnerConfig)
        ? [deployment.metadata.runnerConfig as JsonRecord]
        : []
    ),
    skillPublications: activeDeployments.flatMap((deployment) =>
      jsonArrayMetadata(deployment.metadata.skillPublications)
    ),
    integrationBindings: activeDeployments.flatMap((deployment) =>
      jsonArrayMetadata(deployment.metadata.integrationBindings)
    ),
    bundles: activeDeployments.flatMap((deployment) =>
      jsonObjectMetadata(deployment.metadata.bundle)
        ? [deployment.metadata.bundle as JsonRecord]
        : []
    ),
    generatedServices: activeDeployments.flatMap((deployment) =>
      jsonObjectMetadata(deployment.metadata.generatedServiceConfig)
        ? [deployment.metadata.generatedServiceConfig as JsonRecord]
        : []
    )
  };
}

function latestDeployedRunnerConfiguration(
  store: WorkflowStore,
  workflowId: string,
  approvedRevisionId: string,
  branchId?: string | undefined
): WorkflowDeploymentRecord | undefined {
  return store
    .listDeployments(workflowId)
    .filter(
      (deployment) =>
        deployment.kind === "runner.configuration" &&
        deployment.status === "deployed" &&
        deployment.approvedRevisionId === approvedRevisionId &&
        (branchId === undefined || deployment.branchId === branchId) &&
        jsonObjectMetadata(deployment.metadata.runnerConfig)
    )
    .at(-1);
}

function jsonArrayMetadata(value: unknown): readonly JsonRecord[] {
  return Array.isArray(value) ? value.filter(jsonObjectMetadata) : [];
}

function jsonObjectMetadata(value: unknown): value is JsonRecord {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function jsonRecord(value: unknown): JsonRecord {
  return JSON.parse(JSON.stringify(value)) as JsonRecord;
}

function toArtifactRef(artifact: {
  readonly path: string;
  readonly checksum: string;
  readonly contentType: WorkflowCodegenArtifactRef["contentType"];
}): WorkflowCodegenArtifactRef {
  return {
    path: artifact.path,
    checksum: artifact.checksum,
    contentType: artifact.contentType
  };
}

function requiredIntegrationsForWorkflow(workflow: WorkflowSpec): readonly string[] {
  const integrations = new Set<string>();
  for (const node of workflow.nodes) {
    for (const adapterId of node.adapterIds ?? (node.adapterId ? [node.adapterId] : [])) {
      switch (adapterId) {
        case "adapter.gmail":
        case "adapter.sheets":
          integrations.add("google");
          break;
        case "adapter.email":
          integrations.add("smtp");
          break;
        case "adapter.whatsapp":
          integrations.add("whatsapp");
          break;
        case "adapter.telegram":
          integrations.add("telegram");
          break;
      }
    }
  }

  return [...integrations].sort();
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

class JobCancelledError extends Error {
  public constructor(jobId: string) {
    super(`Job '${jobId}' was cancelled.`);
    this.name = "JobCancelledError";
  }
}

class ApiJobSupervisor {
  private readonly active = new Map<string, AbortController>();

  public constructor(private readonly store: WorkflowStore) {}

  public startJob(jobId: string): AbortSignal | undefined {
    const job = this.store.getJob(jobId);
    if (!job || isTerminalJobStatus(job.status)) {
      return undefined;
    }

    const existing = this.active.get(jobId);
    if (existing) {
      return existing.signal;
    }

    const controller = new AbortController();
    this.active.set(jobId, controller);
    return controller.signal;
  }

  public signalFor(jobId: string): AbortSignal | undefined {
    return this.active.get(jobId)?.signal;
  }

  public finishJob(jobId: string): void {
    this.active.delete(jobId);
  }

  public isCancelled(jobId: string): boolean {
    const job = this.store.getJob(jobId);
    return job?.status === "cancelled" || this.active.get(jobId)?.signal.aborted === true;
  }

  public cancelJob(
    jobId: string,
    reason: string,
    cancelledAt = new Date().toISOString()
  ): WorkflowJob {
    const job = this.store.getJob(jobId);
    if (!job) {
      throw new Error(`Unknown job '${jobId}'.`);
    }
    const controller = this.active.get(jobId);
    if (controller && !controller.signal.aborted) {
      controller.abort(new JobCancelledError(jobId));
    }

    if (isTerminalJobStatus(job.status)) {
      return job;
    }

    const cancelled = this.store.saveJob({
      ...job,
      status: "cancelled",
      updatedAt: cancelledAt,
      finishedAt: cancelledAt,
      cancelledAt,
      cancellationReason: reason
    });
    const withEvent =
      cancelled.events.at(-1)?.message === "Job cancelled."
        ? cancelled
        : this.store.appendJobEvent(
            cancelled.id,
            createJobEvent(cancelled, "error", "Job cancelled.", { reason })
          );
    this.finishJob(jobId);
    return withEvent;
  }
}

type ApiJobWorkerHandler = (
  job: WorkflowJob,
  signal: AbortSignal
) => Promise<{ readonly result?: JsonRecord | undefined } | void>;

class ApiJobWorker {
  private readonly workerId = `worker.${process.pid}.${randomUUID()}`;
  private readonly handlers = new Map<WorkflowJobType, ApiJobWorkerHandler>();
  private timer: NodeJS.Timeout | undefined;
  private tickInFlight = false;

  public constructor(
    private readonly store: WorkflowStore,
    private readonly supervisor: ApiJobSupervisor
  ) {}

  public register(type: WorkflowJobType, handler: ApiJobWorkerHandler): void {
    this.handlers.set(type, handler);
  }

  public start(): void {
    if (this.timer) {
      return;
    }
    this.timer = setInterval(() => void this.tick(), 250);
    void this.tick();
  }

  public stop(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private async tick(): Promise<void> {
    if (this.tickInFlight || this.handlers.size === 0) {
      return;
    }
    this.tickInFlight = true;
    try {
      while (true) {
        const job = this.store.claimNextQueuedJob(this.workerId, [...this.handlers.keys()]);
        if (!job) {
          return;
        }
        void this.runClaimedJob(job);
      }
    } finally {
      this.tickInFlight = false;
    }
  }

  private async runClaimedJob(job: WorkflowJob): Promise<void> {
    const handler = this.handlers.get(job.type);
    if (!handler) {
      return;
    }
    const signal = this.supervisor.startJob(job.id);
    const claimed = this.store.appendJobEvent(
      job.id,
      createJobEvent(job, "info", "Job claimed by local worker.", {
        workerId: this.workerId,
        attempt: job.retry.attempt
      })
    );

    try {
      if (signal?.aborted || this.supervisor.isCancelled(job.id)) {
        throw new JobCancelledError(job.id);
      }
      const output = await handler(claimed, signal ?? new AbortController().signal);
      if (signal?.aborted || this.supervisor.isCancelled(job.id)) {
        throw new JobCancelledError(job.id);
      }
      const current = this.store.getJob(job.id) ?? claimed;
      if (isTerminalJobStatus(current.status)) {
        return;
      }
      const finishedAt = new Date().toISOString();
      this.store.appendJobEvent(
        this.store.saveJob({
          ...current,
          status: "succeeded",
          updatedAt: finishedAt,
          finishedAt,
          ...(output?.result ? { result: output.result } : {})
        }).id,
        createJobEvent(current, "info", "Worker job completed.", output?.result)
      );
    } catch (error) {
      const current = this.store.getJob(job.id) ?? claimed;
      if (error instanceof JobCancelledError || current.status === "cancelled") {
        this.supervisor.cancelJob(
          job.id,
          error instanceof Error ? error.message : "Worker job cancelled."
        );
        return;
      }

      const finishedAt = new Date().toISOString();
      const failed = this.store.saveJob({
        ...current,
        status: "failed",
        updatedAt: finishedAt,
        finishedAt,
        error: error instanceof Error ? error.message : "Worker job failed.",
        retry: {
          ...current.retry,
          retryable: current.retry.attempt < current.retry.maxAttempts
        }
      });
      this.store.appendJobEvent(
        failed.id,
        createJobEvent(failed, "error", "Worker job failed.", {
          retryable: failed.retry.retryable,
          error: failed.error ?? "Worker job failed."
        })
      );
    } finally {
      this.supervisor.finishJob(job.id);
    }
  }
}

function recoverInterruptedJobs(store: WorkflowStore): void {
  const now = new Date().toISOString();
  for (const job of store.listJobs()) {
    if (job.status !== "running") {
      continue;
    }
    const failed = store.saveJob({
      ...job,
      status: "failed",
      updatedAt: now,
      finishedAt: now,
      error: "API restarted before this running job completed.",
      retry: {
        ...job.retry,
        retryable: true
      }
    });
    store.appendJobEvent(
      failed.id,
      createJobEvent(failed, "error", "Running job was interrupted by API restart.", {
        retryable: true
      })
    );
  }
}

function sleepWithSignal(durationMs: number, signal: AbortSignal): Promise<void> {
  if (durationMs <= 0) {
    return Promise.resolve();
  }
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(resolve, durationMs);
    const abort = () => {
      clearTimeout(timeout);
      reject(signal.reason instanceof Error ? signal.reason : new Error("Job cancelled."));
    };
    if (signal.aborted) {
      abort();
      return;
    }
    signal.addEventListener("abort", abort, { once: true });
  });
}

function numberFromJson(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function throwIfRequestJobCancelled(
  store: WorkflowStore,
  supervisor: ApiJobSupervisor,
  request: FastifyRequest
): void {
  const jobId = requestJobId(request);
  if (!jobId) {
    return;
  }
  if (supervisor.isCancelled(jobId) || store.getJob(jobId)?.status === "cancelled") {
    throw new JobCancelledError(jobId);
  }
}

function updateRequestJob(
  store: WorkflowStore,
  request: FastifyRequest,
  status: WorkflowJob["status"],
  message: string,
  metadata?: JsonRecord | undefined,
  supervisor?: ApiJobSupervisor | undefined
): WorkflowJob | undefined {
  const jobIdHeader = request.headers["x-kelpclaw-job-id"];
  const jobId = Array.isArray(jobIdHeader) ? jobIdHeader[0] : jobIdHeader;
  if (!jobId) {
    return undefined;
  }
  const job = store.getJob(jobId);
  if (!job || isTerminalJobStatus(job.status)) {
    return job;
  }

  const now = new Date().toISOString();
  const updated = store.saveJob({
    ...job,
    status,
    updatedAt: now,
    ...(status === "running" && !job.startedAt ? { startedAt: now } : {}),
    ...(isTerminalJobStatus(status) ? { finishedAt: now } : {}),
    ...(status === "cancelled" ? { cancelledAt: now, cancellationReason: message } : {})
  });

  if (status === "running") {
    supervisor?.startJob(updated.id);
  }
  if (isTerminalJobStatus(status)) {
    supervisor?.finishJob(updated.id);
  }

  return store.appendJobEvent(
    updated.id,
    createJobEvent(
      updated,
      status === "failed" || status === "cancelled" ? "error" : "info",
      message,
      metadata
    )
  );
}

function requestJobId(request: FastifyRequest): string | undefined {
  const jobIdHeader = request.headers["x-kelpclaw-job-id"];
  return Array.isArray(jobIdHeader) ? jobIdHeader[0] : jobIdHeader;
}

function isTerminalJobStatus(status: WorkflowJob["status"]): boolean {
  return status === "succeeded" || status === "failed" || status === "cancelled";
}

function writeSseEvent(response: ServerResponse, event: string, data: unknown): void {
  response.write(`event: ${event}\n`);
  response.write(`data: ${JSON.stringify(data)}\n\n`);
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
    readonly branchId?: string | undefined;
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
    branchId: event.branchId ?? context.branchId,
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
