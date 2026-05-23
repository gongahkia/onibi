import { createHash, randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import type { ServerResponse } from "node:http";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import Fastify from "fastify";
import {
  AgentSdkCodeGenerator,
  GeneratedNodeBuildLoop,
  LocalCodegenArtifactStore,
  OpenAiCodeGenerator,
  buildTbom,
  checksumArtifactContent,
  createArtifactManifest,
  createAgentSdkGeneratedNodeRoleRunners,
  createOpenAiGeneratedNodeRoleRunners,
  createGeneratedArtifact,
  synthesizeWorkflowFromTrajectory
} from "@kelpclaw/codegen";
import { getSkill, listSkills, registerPromotedSkill } from "@kelpclaw/skill-registry";
import {
  createDefaultLiveAdapters,
  createDefaultMockAdapters,
  createMcpAdapter,
  createOpenApiAdapter,
  importMcpConnector,
  importOpenApiConnector,
  testMcpConnector,
  testOpenApiConnector
} from "@kelpclaw/adapters";
import { CronExpressionParser } from "cron-parser";
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
  createWorkflowRuntime,
  createWorkflowSpecDiff,
  gmailReceiptsToSheetsWorkflowFixture,
  redactJsonRecord,
  redactSecretString,
  stableJsonStringify,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type { FastifyInstance, FastifyRequest } from "fastify";
import type {
  CodeGenerator,
  CodegenAgentRunRecord,
  CodegenArtifactStore,
  GeneratedNodeBuildRole,
  GeneratedNodeRoleRunner,
  TrajectoryRun
} from "@kelpclaw/codegen";
import type { AgentMemoryAccess, NodeRunner } from "@kelpclaw/nanoclaw";
import type { SkillMetadata } from "@kelpclaw/skill-registry";
import type {
  GeneratedNodeEvalReport,
  GeneratedNodeTestReport,
  JsonRecord,
  WorkflowArtifactManifestRecord,
  WorkflowAgentMemoryListResponse,
  WorkflowAgentRole,
  WorkflowAgentTimelineEvent,
  WorkflowAuditAdapterCallRecord,
  WorkflowAuditContainerRecord,
  WorkflowAuditDeliveryRecord,
  WorkflowAuditExportRecord,
  WorkflowAuditRecord,
  WorkflowBudgetLedger,
  WorkflowBudgetPolicy,
  WorkflowApiError,
  WorkflowAlertPolicy,
  WorkflowAcceptPlanRequest,
  WorkflowAcceptPlanResponse,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowBranch,
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
  WorkflowClarificationAnswer,
  WorkflowClarificationRequest,
  WorkflowConnectorRecord,
  WorkflowDecisionTraceEvalExample,
  WorkflowDecisionTraceKind,
  WorkflowEventSeverity,
  WorkflowCreateBranchRequest,
  WorkflowCreateBranchResponse,
  WorkflowDraftEvaluation,
  WorkflowDraftRevision,
  WorkflowDraftRevisionSource,
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
  WorkflowProviderRuntimeConfig,
  WorkflowDeploymentKind,
  WorkflowDeploymentRecord,
  WorkflowDeploymentRollbackTarget,
  WorkflowEdge,
  WorkflowListBranchesResponse,
  WorkflowNode,
  WorkflowNodeDecisionTrace,
  WorkflowNodeDecisionTraceEvent,
  WorkflowNodeDecisionTraceExport,
  WorkflowObservabilityEventKind,
  WorkflowOpsHealth,
  WorkflowPlanRequest,
  WorkflowPlanResponse,
  WorkflowPlannerSuggestionDecisionRequest,
  WorkflowPlannerSuggestionDecisionResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowGraphChange,
  WorkflowRunEvent,
  WorkflowRunRecord,
  WorkflowRouterEvalRun,
  WorkflowRouterEvalListResponse,
  WorkflowRouterEvalRunResponse,
  WorkflowRouterEvaluateResponse,
  WorkflowReuseCandidatesResponse,
  WorkflowRuntimeTruthSnapshot,
  WorkflowRetentionPolicy,
  WorkflowScheduleRecord,
  WorkflowSpec,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowTaskRoute,
  WorkflowUpdateBranchRequest,
  WorkflowUpdateBranchResponse,
  WorkflowValidationIssue,
  WorkflowValidationResult,
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
import { routeWorkflowTask, routerClassifierVersion } from "./router.js";
import { routerEvalCases, runRouterEvalCases } from "./router-evals.js";
import {
  InMemorySecretStore,
  SqliteSecretStore,
  consumeOAuthState,
  createOAuthState,
  secretReadiness
} from "./secrets.js";
import { InMemoryAgentRunStore, SqliteAgentRunStore } from "./agent-run-store.js";
import { registerAgentRunRoutes } from "./agent-run-routes.js";
import {
  attachAuthPrincipal,
  authPrincipalForRequest,
  createApiAuthContext,
  principalHasRole
} from "./auth.js";
import { ApiPolicyEngine } from "./policy-engine.js";
import { createConfiguredApiOtlpExporter } from "./otlp-exporter.js";
import { InMemoryWorkflowStore, SqliteWorkflowStore } from "./store.js";
import type { SecretStore } from "./secrets.js";
import type { AgentRunRecord, AgentRunStore } from "./agent-run-store.js";
import type { ApiRole } from "./auth.js";
import type { ApiOtlpExporter } from "./otlp-exporter.js";
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

interface AgentRunRouteParams {
  readonly id: string;
}

interface AgentRunPromoteRequestBody {
  readonly skillName?: string | undefined;
  readonly capabilities?: readonly string[] | undefined;
  readonly promotedBy?: string | undefined;
}

interface SkillRouteParams {
  readonly skillId: string;
}

interface MemoryRouteParams extends RouteParamsWithId {
  readonly memoryId: string;
}

interface DeploymentRouteParams extends RouteParamsWithId {
  readonly deploymentId: string;
}

interface ConnectorRouteParams {
  readonly connectorId: string;
}

interface ScheduleRouteParams extends RouteParamsWithId {
  readonly scheduleId: string;
}

type CodegenProvider = "anthropic" | "openai";

interface LiveGeneratedNodeProviders {
  readonly codeGenerator?: CodeGenerator | undefined;
  readonly roleRunners?:
    | Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>
    | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly maxReimplementationAttempts?: number | undefined;
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

interface BudgetPatchRequestBody {
  readonly branchId?: string | undefined;
  readonly maxWorkflowCostUsd?: number | undefined;
  readonly maxCodegenCostUsd?: number | undefined;
  readonly maxAgenticCostUsd?: number | undefined;
  readonly expensiveRetryConfirmationUsd?: number | undefined;
  readonly perAgentMaxCostUsd?: Partial<Record<string, number>> | undefined;
  readonly updatedBy?: string | undefined;
}

interface OpenApiImportRequestBody {
  readonly id?: string | undefined;
  readonly name?: string | undefined;
  readonly sourceUrl?: string | undefined;
  readonly document?: string | JsonRecord | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
}

interface McpConnectorRequestBody {
  readonly id?: string | undefined;
  readonly name?: string | undefined;
  readonly endpointUrl: string;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
}

interface RouterEvaluateRequestBody {
  readonly prompt: string;
  readonly forceDeterministic?: boolean | undefined;
}

interface AlertPolicyRequestBody {
  readonly enabled?: boolean | undefined;
  readonly events?: WorkflowAlertPolicy["events"] | undefined;
  readonly channels?: WorkflowAlertPolicy["channels"] | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  readonly updatedBy?: string | undefined;
  readonly branchId?: string | undefined;
}

interface RetentionPolicyRequestBody {
  readonly maxRunEventDays?: number | undefined;
  readonly maxSuccessfulRunWorkspaceDays?: number | undefined;
  readonly maxFailedRunWorkspaceDays?: number | undefined;
  readonly maxJobEventDays?: number | undefined;
  readonly updatedBy?: string | undefined;
  readonly branchId?: string | undefined;
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
  readonly agentRunStore?: AgentRunStore | undefined;
  readonly policyEngine?: ApiPolicyEngine | undefined;
  readonly otlpExporter?: ApiOtlpExporter | undefined;
  readonly roleTokens?: Readonly<Record<string, readonly ApiRole[]>> | undefined;
  readonly authSigningSecret?: string | null | undefined;
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

export function createConfiguredAgentRunStore(): AgentRunStore {
  if (process.env.KELPCLAW_AGENT_RUN_STORE === "memory") {
    return new InMemoryAgentRunStore();
  }

  return new SqliteAgentRunStore({
    databasePath:
      process.env.KELPCLAW_AGENT_RUN_DB ??
      process.env.KELPCLAW_WORKFLOW_DB ??
      join(process.cwd(), ".kelpclaw", "workflow.sqlite")
  });
}

function createLiveGeneratedNodeProvidersFromEnv(): LiveGeneratedNodeProviders {
  const provider = codegenProviderFromEnv();
  switch (provider) {
    case "anthropic":
      if (!process.env.ANTHROPIC_API_KEY) {
        return {};
      }
      return {
        codeGenerator: new AgentSdkCodeGenerator({
          apiKey: process.env.ANTHROPIC_API_KEY,
          model: codegenModelForProvider(provider)
        }),
        roleRunners: createAgentSdkGeneratedNodeRoleRunners({
          apiKey: process.env.ANTHROPIC_API_KEY
        })
      };
    case "openai":
      if (!process.env.OPENAI_API_KEY) {
        return {};
      }
      return {
        codeGenerator: new OpenAiCodeGenerator({
          apiKey: process.env.OPENAI_API_KEY,
          model: codegenModelForProvider(provider)
        }),
        roleRunners: createOpenAiGeneratedNodeRoleRunners({
          apiKey: process.env.OPENAI_API_KEY
        })
      };
  }
}

function codegenProviderFromEnv(): CodegenProvider {
  const provider =
    process.env.KELPCLAW_CODEGEN_PROVIDER ?? process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic";
  if (provider === "anthropic" || provider === "openai") {
    return provider;
  }

  throw new Error("KELPCLAW_CODEGEN_PROVIDER must be 'anthropic' or 'openai'.");
}

function codegenModelForProvider(provider: CodegenProvider): string | undefined {
  switch (provider) {
    case "anthropic":
      return process.env.KELPCLAW_CODEGEN_MODEL ?? process.env.KELPCLAW_PLANNER_MODEL;
    case "openai":
      return (
        process.env.KELPCLAW_OPENAI_CODEGEN_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL ??
        "gpt-5.4"
      );
  }
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
  const agentRunStore = options.agentRunStore ?? new InMemoryAgentRunStore();
  const policyEngine = options.policyEngine ?? new ApiPolicyEngine();
  const otlpExporter = options.otlpExporter ?? createConfiguredApiOtlpExporter();
  const artifactStore = options.artifactStore ?? new LocalCodegenArtifactStore();
  const planner = options.planner ?? createPlannerBackendFromEnv({ artifactStore });
  const runner = options.runner;
  let latestRouterEvalRun: WorkflowRouterEvalRun | undefined;
  recoverInterruptedJobs(store);
  recoverInterruptedRuns(store);
  const jobSupervisor = new ApiJobSupervisor(store);
  const jobWorker = new ApiJobWorker(store, jobSupervisor);
  jobWorker.register("run.workflow", async (job, signal) =>
    executeWorkflowRunJob({
      store,
      secretStore,
      artifactStore,
      runner,
      job,
      signal
    })
  );
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
  const scheduleWorker = new ApiScheduleWorker(store);
  scheduleWorker.start();
  const cleanupWorker = new ApiRetentionCleanupWorker(store);
  cleanupWorker.start();
  const adminToken =
    options.adminToken === undefined ? process.env.KELPCLAW_ADMIN_TOKEN : options.adminToken;
  const auth = createApiAuthContext({
    adminToken,
    roleTokens: options.roleTokens,
    signingSecret: options.authSigningSecret
  });

  app.addHook("onClose", async () => {
    jobWorker.stop();
    scheduleWorker.stop();
    cleanupWorker.stop();
  });

  app.addHook("preHandler", async (request, reply) => {
    if (isPublicRoute(request.method, request.url)) {
      return;
    }
    const principal = auth.authenticate(request);
    attachAuthPrincipal(request, principal);
    if (!auth.enabled || principal) {
      return;
    }
    return reply.code(401).send({
      ok: false,
      error: "UNAUTHORIZED",
      message: "A valid KelpClaw bearer token is required."
    });
  });

  app.addHook("preHandler", async (request, reply) => {
    if (
      !auth.enabled ||
      isPublicRoute(request.method, request.url) ||
      request.url.startsWith("/api/agent-runs") ||
      request.url.startsWith("/api/policies") ||
      request.url.startsWith("/api/skills")
    ) {
      return;
    }
    const principal = authPrincipalForRequest(request);
    if (!principal || !principalHasRole(principal, "admin")) {
      return reply.code(403).send({
        ok: false,
        error: "FORBIDDEN",
        message: "Role 'admin' is required."
      });
    }
  });

  app.get("/health", async () => ({
    status: "ok",
    service: "kelpclaw-api"
  }));

  app.get("/api/runtime/providers", async () => ({
    ok: true,
    providers: providerRuntimeConfigsFromEnv()
  }));

  app.get("/api/ops/health", async () => ({
    ok: true,
    health: opsHealth(store, jobWorker, scheduleWorker, latestRouterEvalRun)
  }));

  registerAgentRunRoutes(app, {
    store: agentRunStore,
    policyEngine,
    auth,
    writeSseEvent
  });

  app.post<{ Params: AgentRunRouteParams; Body: AgentRunPromoteRequestBody }>(
    "/api/agent-runs/:id/promote",
    { preHandler: auth.requireRole("reviewer") },
    async (request, reply) => {
      const run = agentRunStore.getRun(request.params.id);
      if (!run) {
        return reply.code(404).send({
          ok: false,
          error: "AGENT_RUN_NOT_FOUND",
          message: `Agent run '${request.params.id}' was not found.`
        });
      }
      const unresolvedApproval = firstUnresolvedPolicyApproval(run);
      if (unresolvedApproval) {
        return reply.code(409).send({
          ok: false,
          error: "POLICY_APPROVAL_REQUIRED",
          message: `Agent run '${run.id}' has an unresolved policy approval for event '${unresolvedApproval.id}'.`,
          eventId: unresolvedApproval.id
        });
      }
      const verification = agentRunStore.verifyAuditChain(run.id);
      if (!verification.valid) {
        return reply.code(409).send({
          ok: false,
          error: "AUDIT_CHAIN_INVALID",
          message: `Agent run '${run.id}' audit chain is invalid.`,
          verification
        });
      }

      const workflow = synthesizeWorkflowFromTrajectory(run, {
        name: request.body.skillName ?? run.title ?? `Promoted ${run.id}`
      });
      const skill = createPromotedSkillFromTrajectory(run, workflow, request.body);
      const tbom = buildTbom(workflow, run);
      const skillArtifact = createGeneratedArtifact({
        path: `promoted-skills/${skill.id}.json`,
        content: JSON.stringify(skill, null, 2),
        contentType: "application/json"
      });
      const workflowArtifact = createGeneratedArtifact({
        path: `promoted-trajectories/${run.id}.workflow.json`,
        content: JSON.stringify(workflow, null, 2),
        contentType: "application/json"
      });
      const tbomArtifact = createGeneratedArtifact({
        path: `promoted-trajectories/${run.id}.bom.json`,
        content: JSON.stringify(tbom, null, 2),
        contentType: "application/json"
      });
      const [storedSkillArtifact, storedWorkflowArtifact, storedTbomArtifact] = await Promise.all([
        artifactStore.putArtifact(skillArtifact),
        artifactStore.putArtifact(workflowArtifact),
        artifactStore.putArtifact(tbomArtifact)
      ]);
      const loadedSkill = JSON.parse(
        await artifactStore.readArtifact(storedSkillArtifact.ref)
      ) as SkillMetadata;
      const promotedSkill = registerPromotedSkill(loadedSkill);
      const otlpExport = await otlpExporter.exportPromotion({
        run,
        skill: promotedSkill,
        tbom
      });
      agentRunStore.appendAuditEvent(run.id, {
        action: "trajectory.promoted",
        summary: `Promoted agent run '${run.id}' to skill '${promotedSkill.id}'.`,
        metadata: {
          promotedBy: request.body.promotedBy ?? "reviewer",
          skillId: promotedSkill.id,
          artifactChecksum: storedSkillArtifact.ref.checksum
        }
      });

      return {
        ok: true,
        skill: promotedSkill,
        workflow,
        tbom,
        otlp: otlpExport,
        artifacts: {
          skill: storedSkillArtifact.ref,
          workflow: storedWorkflowArtifact.ref,
          tbom: storedTbomArtifact.ref
        }
      };
    }
  );

  app.get<{ Params: AgentRunRouteParams }>(
    "/api/agent-runs/:id/tbom",
    { preHandler: auth.requireRole("auditor") },
    async (request, reply) => {
      const run = agentRunStore.getRun(request.params.id);
      if (!run) {
        return reply.code(404).send({
          ok: false,
          error: "AGENT_RUN_NOT_FOUND",
          message: `Agent run '${request.params.id}' was not found.`
        });
      }
      const workflow = synthesizeWorkflowFromTrajectory(run);
      return {
        ok: true,
        tbom: buildTbom(workflow, run)
      };
    }
  );

  app.get<{ Querystring: { capability?: string | undefined; prompt?: string | undefined } }>(
    "/api/skills",
    { preHandler: auth.requireRole("auditor") },
    async (request) => {
      const query = request.query;
      const skills = listSkills().filter((skill) => {
        const capabilityMatches =
          !query.capability || skill.capabilities.includes(query.capability);
        const promptMatches =
          !query.prompt ||
          `${skill.name} ${skill.description} ${skill.metaprompt}`
            .toLowerCase()
            .includes(query.prompt.toLowerCase());
        return capabilityMatches && promptMatches;
      });
      return { ok: true, skills };
    }
  );

  app.post<{ Params: SkillRouteParams; Body: { readonly input?: JsonRecord | undefined } }>(
    "/api/skills/:skillId/invoke",
    { preHandler: auth.requireRole("operator") },
    async (request, reply) => {
      const skill = getSkill(request.params.skillId);
      if (!skill) {
        return reply.code(404).send({
          ok: false,
          error: "SKILL_NOT_FOUND",
          message: `Skill '${request.params.skillId}' was not found.`
        });
      }
      return {
        ok: true,
        skillId: skill.id,
        input: request.body.input ?? {},
        output: skill.examples[0]?.output ?? {}
      };
    }
  );

  app.post<{
    Body: RouterEvaluateRequestBody;
    Reply: WorkflowRouterEvaluateResponse | WorkflowApiError;
  }>("/api/router/evaluate", async (request, reply) => {
    const prompt = request.body.prompt?.trim();
    if (!prompt) {
      return reply.code(422).send({
        ok: false,
        error: "ROUTER_PROMPT_REQUIRED",
        message: "Router evaluation requires a non-empty prompt."
      } as never);
    }

    return {
      ok: true,
      route: routeWorkflowTask(
        {
          prompt,
          ...(request.body.forceDeterministic ? { forceDeterministic: true } : {})
        },
        {
          correlationId: correlationIdForRequest(request),
          provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
          model: process.env.KELPCLAW_PLANNER_MODEL
        }
      )
    };
  });

  app.get<{ Reply: WorkflowRouterEvalListResponse }>("/api/router/evals", async () => ({
    ok: true,
    classifierVersion: routerClassifierVersion,
    cases: routerEvalCases,
    ...(latestRouterEvalRun ? { latestRun: latestRouterEvalRun } : {})
  }));

  app.post<{ Reply: WorkflowRouterEvalRunResponse }>("/api/router/evals/run", async () => {
    latestRouterEvalRun = runRouterEvalCases({
      provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
      model: process.env.KELPCLAW_PLANNER_MODEL
    });
    return {
      ok: true,
      run: latestRouterEvalRun
    };
  });

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
      branchId: request.body.branchId,
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

  app.get("/api/connectors", async () => ({
    ok: true,
    connectors: store.listConnectors()
  }));

  app.post<{ Body: OpenApiImportRequestBody }>(
    "/api/connectors/openapi/import",
    async (request, reply) => {
      try {
        const id =
          request.body.id ??
          `connector.openapi.${slugify(request.body.name ?? request.body.sourceUrl ?? "openapi")}.${Date.now()}`;
        const connector = store.saveConnector(
          await importOpenApiConnector({
            id,
            name: request.body.name,
            sourceUrl: request.body.sourceUrl,
            document: request.body.document,
            secretRefs: request.body.secretRefs
          })
        );
        recordAudit(store, {
          action: "connector.created",
          actor: "api",
          workflowId: "connectors",
          revisionId: connector.id,
          correlationId: correlationIdForRequest(request),
          summary: `Imported OpenAPI connector '${connector.name}'.`,
          metadata: connectorSummary(connector)
        });
        return reply.code(201).send({
          ok: true,
          connector
        });
      } catch (error) {
        return reply.code(422).send({
          ok: false,
          error: "CONNECTOR_IMPORT_FAILED",
          message: error instanceof Error ? error.message : "OpenAPI import failed."
        });
      }
    }
  );

  app.post<{ Body: McpConnectorRequestBody }>("/api/connectors/mcp", async (request, reply) => {
    try {
      const id =
        request.body.id ??
        `connector.mcp.${slugify(request.body.name ?? request.body.endpointUrl)}.${Date.now()}`;
      const connector = store.saveConnector(
        await importMcpConnector({
          id,
          name: request.body.name,
          endpointUrl: request.body.endpointUrl,
          secretRefs: request.body.secretRefs
        })
      );
      recordAudit(store, {
        action: "connector.created",
        actor: "api",
        workflowId: "connectors",
        revisionId: connector.id,
        correlationId: correlationIdForRequest(request),
        summary: `Registered MCP connector '${connector.name}'.`,
        metadata: connectorSummary(connector)
      });
      return reply.code(201).send({
        ok: true,
        connector
      });
    } catch (error) {
      return reply.code(422).send({
        ok: false,
        error: "CONNECTOR_IMPORT_FAILED",
        message: error instanceof Error ? error.message : "MCP connector registration failed."
      });
    }
  });

  app.get<{ Params: ConnectorRouteParams }>(
    "/api/connectors/:connectorId",
    async (request, reply) => {
      const connector = store.getConnector(request.params.connectorId);
      if (!connector) {
        return reply.code(404).send({
          ok: false,
          error: "CONNECTOR_NOT_FOUND",
          message: `Connector '${request.params.connectorId}' was not found.`
        });
      }

      return {
        ok: true,
        connector
      };
    }
  );

  app.delete<{ Params: ConnectorRouteParams }>("/api/connectors/:connectorId", async (request) => {
    const connector = store.getConnector(request.params.connectorId);
    const deleted = store.deleteConnector(request.params.connectorId);
    if (connector && deleted) {
      recordAudit(store, {
        action: "connector.deleted",
        actor: "api",
        workflowId: "connectors",
        revisionId: connector.id,
        correlationId: correlationIdForRequest(request),
        summary: `Deleted connector '${connector.name}'.`,
        metadata: connectorSummary(connector)
      });
    }

    return {
      ok: true,
      deleted
    };
  });

  app.post<{ Params: ConnectorRouteParams }>(
    "/api/connectors/:connectorId/test",
    async (request, reply) => {
      const connector = store.getConnector(request.params.connectorId);
      if (!connector) {
        return reply.code(404).send({
          ok: false,
          error: "CONNECTOR_NOT_FOUND",
          message: `Connector '${request.params.connectorId}' was not found.`
        });
      }
      try {
        const tested = store.saveConnector(
          connector.kind === "mcp"
            ? await testMcpConnector(connector)
            : connector.kind === "openapi"
              ? await testOpenApiConnector(connector)
              : {
                  ...connector,
                  updatedAt: new Date().toISOString(),
                  lastTest: {
                    status: "succeeded",
                    testedAt: new Date().toISOString(),
                    operationCount: connector.operations.length,
                    message: `Connector has ${connector.operations.length} operation(s).`
                  }
                }
        );

        return {
          ok: true,
          connector: tested
        };
      } catch (error) {
        const failed = store.saveConnector({
          ...connector,
          updatedAt: new Date().toISOString(),
          lastTest: {
            status: "failed",
            testedAt: new Date().toISOString(),
            operationCount: connector.operations.length,
            message: error instanceof Error ? error.message : "Connector test failed."
          }
        });
        return reply.code(502).send({
          ok: false,
          error: "CONNECTOR_TEST_FAILED",
          message: failed.lastTest.message,
          connector: failed
        });
      }
    }
  );

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
      const clarification = createClarificationRequestIfNeeded(request.body, correlationId);
      const planRequest = clarification
        ? request.body
        : enrichPlanRequestWithClarifications(request.body);
      const route = routeWorkflowTask(planRequest, {
        correlationId,
        provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
        model: process.env.KELPCLAW_PLANNER_MODEL
      });
      if (clarification) {
        updateRequestJob(
          store,
          request,
          "succeeded",
          "Planning paused for clarification.",
          {
            route: route.route,
            clarificationId: clarification.id
          },
          jobSupervisor
        );
        return {
          ok: true,
          status: "clarification-required",
          clarification,
          route
        };
      }
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
        workflow = await planWorkflowDraft(planRequest, routedPlanner);
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
      recordPlannerNodeDecisionTraces(store, {
        workflow: draftRevision.workflow,
        revisionId: draftRevision.id,
        correlationId,
        prompt: planRequest.prompt,
        source: "plan",
        route,
        validationIssues: []
      });
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

  app.patch<{
    Params: BranchRouteParams;
    Body: WorkflowUpdateBranchRequest;
    Reply: WorkflowUpdateBranchResponse;
  }>("/api/workflows/:id/branches/:branchId", async (request, reply) => {
    const branch = store.getBranch(request.params.branchId);
    if (!branch || branch.workflowId !== request.params.id) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.params.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }
    const nextName = request.body.name?.trim() ?? branch.name;
    const nextStatus = request.body.status ?? branch.status;
    if (nextName.length === 0) {
      return reply.code(422).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NAME_REQUIRED",
        message: "Branch name cannot be empty."
      } as never);
    }
    if (branch.id === store.getDefaultBranch(request.params.id).id && nextStatus === "archived") {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_DEFAULT_BRANCH_ARCHIVE_BLOCKED",
        message: "The default main branch cannot be archived."
      } as never);
    }
    if (
      nextStatus === "active" &&
      store
        .listBranches(request.params.id)
        .some(
          (candidate) =>
            candidate.id !== branch.id &&
            candidate.status === "active" &&
            candidate.name.trim().toLowerCase() === nextName.toLowerCase()
        )
    ) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NAME_CONFLICT",
        message: `An active branch named '${nextName}' already exists.`
      } as never);
    }

    const updated = store.saveBranch({
      ...branch,
      name: nextName,
      status: nextStatus,
      updatedAt: new Date().toISOString(),
      metadata: jsonRecord({
        ...(branch.metadata ?? {}),
        updatedBy: request.body.updatedBy
      })
    });
    recordAudit(store, {
      action: "branch.updated",
      actor: request.body.updatedBy,
      workflowId: request.params.id,
      branchId: updated.id,
      revisionId: updated.headDraftRevisionId,
      correlationId: correlationIdForRequest(request),
      summary: `Updated workflow branch '${updated.name}'.`,
      metadata: {
        previousName: branch.name,
        nextName: updated.name,
        previousStatus: branch.status,
        nextStatus: updated.status
      }
    });

    return {
      ok: true,
      branch: updated
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
    if (branch.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch) as never);
    }

    const correlationId = correlationIdForRequest(request);
    const branchPlanInput: WorkflowPlanRequest = {
      ...request.body,
      currentWorkflow: request.body.currentWorkflow ?? headDraft.workflow
    };
    const clarification = createClarificationRequestIfNeeded(branchPlanInput, correlationId);
    const planRequest = clarification
      ? branchPlanInput
      : enrichPlanRequestWithClarifications(branchPlanInput);
    const route = routeWorkflowTask(planRequest, {
      correlationId,
      provider: process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic",
      model: process.env.KELPCLAW_PLANNER_MODEL
    });
    if (clarification) {
      return {
        ok: true,
        status: "clarification-required",
        clarification,
        route
      };
    }
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

    const finalized = finalizeBranchDraftWithReuse(store, {
      workflowId: request.params.id,
      branch,
      validation,
      source: "branch-plan",
      parentDraftRevisionId: headDraft.id,
      actor: request.body.actor ?? "planner",
      correlationId
    });
    if (!finalized.ok) {
      return reply.code(422).send({
        ok: false,
        error: "WORKFLOW_CODEGEN_REUSE_INVALID",
        message: finalized.validation.errors.map((error) => error.code).join(", "),
        validation: finalized.validation
      } as never);
    }
    const { draftRevision, branch: updatedBranch } = finalized;
    recordPlannerNodeDecisionTraces(store, {
      workflow: draftRevision.workflow,
      revisionId: draftRevision.id,
      branchId: branch.id,
      correlationId,
      prompt: planRequest.prompt,
      source: "plan",
      route,
      validationIssues: []
    });
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
      route,
      metadata: {
        reuseDecisionIds: finalized.reuseDecisions.map((decision) => decision.id),
        reuseApplied: finalized.reuseApplied
      }
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
    if (branch.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch) as never);
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

      const finalized = finalizeBranchDraftWithReuse(store, {
        workflowId: request.params.id,
        branch,
        validation,
        source: "branch-reprompt",
        parentDraftRevisionId: headDraft.id,
        actor: request.body.actor ?? "planner",
        correlationId: correlationIdForRequest(request)
      });
      if (!finalized.ok) {
        return reply.code(422).send({
          ok: false,
          error: "WORKFLOW_CODEGEN_REUSE_INVALID",
          message: finalized.validation.errors.map((error) => error.code).join(", "),
          validation: finalized.validation
        } as never);
      }
      const { draftRevision, branch: updatedBranch } = finalized;
      recordPlannerNodeDecisionTraces(store, {
        workflow: draftRevision.workflow,
        revisionId: draftRevision.id,
        branchId: branch.id,
        correlationId: correlationIdForRequest(request),
        prompt: request.body.prompt,
        source: "reprompt",
        changedNodeIds: [request.body.nodeId],
        validationIssues: []
      });
      const promptTurn = store.savePromptTurn({
        id: `prompt-turn.${branch.id}.${Date.now()}.${randomUUID()}`,
        workflowId: request.params.id,
        branchId: branch.id,
        source: "reprompt",
        prompt: request.body.prompt,
        actor: request.body.actor ?? "planner",
        createdAt: draftRevision.createdAt,
        baseDraftRevisionId: headDraft.id,
        resultingDraftRevisionId: draftRevision.id,
        metadata: {
          reuseDecisionIds: finalized.reuseDecisions.map((decision) => decision.id),
          reuseApplied: finalized.reuseApplied
        }
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
    if (branch.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch) as never);
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
    const sourceBranch = store.getBranch(request.params.sourceBranchId);
    const targetBranch = store.getBranch(request.body.targetBranchId);
    if (sourceBranch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(sourceBranch) as never);
    }
    if (targetBranch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(targetBranch) as never);
    }
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
    const sourceBranch = store.getBranch(request.params.sourceBranchId);
    const requestedTargetBranch = store.getBranch(request.body.targetBranchId);
    if (sourceBranch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(sourceBranch) as never);
    }
    if (requestedTargetBranch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(requestedTargetBranch) as never);
    }
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
    const finalized = finalizeBranchDraftWithReuse(store, {
      workflowId: request.params.id,
      branch: targetBranch,
      validation,
      source,
      parentDraftRevisionId: targetHead.id,
      actor: request.body.appliedBy,
      correlationId: correlationIdForRequest(request)
    });
    if (!finalized.ok) {
      return reply.code(422).send({
        ok: false,
        error: "WORKFLOW_CODEGEN_REUSE_INVALID",
        message: finalized.validation.errors.map((error) => error.code).join(", "),
        validation: finalized.validation
      } as never);
    }
    const { draftRevision, branch } = finalized;
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
        graphDiffId: graphDiff.id,
        reuseDecisionIds: finalized.reuseDecisions.map((decision) => decision.id),
        reuseApplied: finalized.reuseApplied
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
      if (branch?.status === "archived") {
        return reply.code(409).send(archivedBranchApiError(branch));
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
            runGeneratedNodesInDocker:
              request.body.mockOnly === true ? false : process.env.NANOCLAW_DRAFT_DOCKER !== "0",
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
      recordPlannerNodeDecisionTraces(store, {
        workflow: draftRevision.workflow,
        revisionId: draftRevision.id,
        correlationId: correlationIdForRequest(request),
        prompt: request.body.prompt,
        source: "reprompt",
        changedNodeIds: [request.body.nodeId],
        validationIssues: []
      });
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
    const branch = request.body.branchId ? store.getBranch(request.body.branchId) : undefined;
    if (request.body.branchId && (!branch || branch.workflowId !== request.params.id)) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_BRANCH_NOT_FOUND",
        message: `Branch '${request.body.branchId}' was not found for workflow '${request.params.id}'.`
      } as never);
    }
    if (branch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch) as never);
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
    if (branch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch));
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
    const budgetPolicy = ensureBudgetPolicy(store, sourceWorkflow.id, branch?.id);
    const projectedCostUsd = Math.min(
      request.body.maxModelCostUsd ?? budgetPolicy.maxCodegenCostUsd,
      budgetPolicy.maxCodegenCostUsd
    );
    const budgetCheck = budgetCheckForProjectedCost(
      store,
      budgetPolicy,
      projectedCostUsd,
      "Generated-node build"
    );
    if (!budgetCheck.ok) {
      const ledger = store.saveBudgetLedger(
        createBudgetLedger({
          workflowId: sourceWorkflow.id,
          branchId: branch?.id,
          scope: "job",
          projectedCostUsd,
          actualCostUsd: 0,
          remainingCostUsd: budgetCheck.remainingCostUsd,
          retryEstimateUsd: budgetPolicy.expensiveRetryConfirmationUsd,
          status: "blocked",
          stopReason: budgetCheck.reason
        })
      );
      recordAudit(store, {
        action: "budget.blocked",
        actor: "budget",
        workflowId: sourceWorkflow.id,
        branchId: branch?.id,
        revisionId: branchHead?.id ?? `draft.${sourceWorkflow.id}`,
        correlationId: correlationIdForRequest(request),
        summary: budgetCheck.reason,
        metadata: {
          ledgerId: ledger.id,
          projectedCostUsd,
          remainingCostUsd: budgetCheck.remainingCostUsd
        }
      });
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_BUDGET_EXCEEDED",
        message: budgetCheck.reason,
        ledger
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
    const buildLoop = new GeneratedNodeBuildLoop(createLiveGeneratedNodeProvidersFromEnv());
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
        maxReimplementationAttempts: request.body.maxReimplementationAttempts ?? 2,
        maxWallClockSeconds: request.body.maxWallClockSeconds ?? 600,
        maxModelCostUsd: Math.min(projectedCostUsd, budgetCheck.remainingCostUsd),
        maxDockerRuntimeSeconds: 300,
        signal: jobSignal,
        runTestsInDocker: request.body.runTestsInDocker ?? true
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
      saveBudgetLedgersForAgentRuns(store, budgetPolicy, result.agentRuns, runningJob.id);
      saveTimelineEventsForAgentRuns(store, result.agentRuns, branch?.id);
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
        recordCodegenNodeDecisionTraces(store, {
          workflowId: stored.workflow.id,
          branchId: branch?.id,
          nodeId: node.id,
          revisionId:
            branchHead?.id ??
            store.getLatestDraftRevision(sourceWorkflow.id)?.id ??
            `draft.${sourceWorkflow.id}.r${sourceWorkflow.revision}`,
          jobId: runningJob.id,
          correlationId,
          agentRuns: result.agentRuns,
          testReport,
          evalReport
        });
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
      recordCodegenNodeDecisionTraces(store, {
        workflowId: stored.workflow.id,
        branchId: branch?.id,
        nodeId: node.id,
        revisionId: draftRevision.id,
        jobId: runningJob.id,
        correlationId,
        agentRuns: result.agentRuns,
        testReport,
        evalReport
      });
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
    if (branch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(branch));
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
    const runBranchId = request.body.branchId ?? approvedRevision.branchId;
    const runBranch = runBranchId ? store.getBranch(runBranchId) : undefined;
    if (runBranch?.status === "archived") {
      return reply.code(409).send(archivedBranchApiError(runBranch) as never);
    }
    const requestedDeployment = request.body.deploymentId
      ? store
          .listDeployments(request.params.id)
          .find((deployment) => deployment.id === request.body.deploymentId)
      : undefined;
    if (request.body.deploymentId && !requestedDeployment) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_DEPLOYMENT_NOT_FOUND",
        message: `Deployment '${request.body.deploymentId}' was not found.`
      } as never);
    }
    const deployedRunnerConfig =
      requestedDeployment ??
      latestDeployedRunnerConfiguration(
        store,
        approvedRevision.workflowId,
        approvedRevision.id,
        approvedRevision.branchId
      );
    if (
      !deployedRunnerConfig ||
      deployedRunnerConfig.kind !== "runner.configuration" ||
      deployedRunnerConfig.status !== "deployed" ||
      deployedRunnerConfig.approvedRevisionId !== approvedRevision.id
    ) {
      return reply.code(409).send({
        ok: false,
        error: "WORKFLOW_RUN_REQUIRES_DEPLOYMENT",
        message:
          "Production runs require an active runner.configuration deployment for the approved revision."
      } as never);
    }

    try {
      const correlationId = correlationIdForRequest(request);
      const dag = compileWorkflowDag(approvedRevision.workflow);
      const now = new Date().toISOString();
      const runId = `run.${approvedRevision.workflowId}.r${approvedRevision.revision}.${Date.now()}.${randomUUID()}`;
      const run = store.saveRun({
        id: runId,
        workflowId: approvedRevision.workflowId,
        branchId: approvedRevision.branchId,
        approvedRevisionId: approvedRevision.id,
        revision: approvedRevision.revision,
        status: "queued",
        createdAt: now,
        startedAt: now,
        finishedAt: now,
        events: enrichRunEvents(
          [
            createStructuredRunEvent({
              id: "event.run.queued",
              timestamp: now,
              level: "info",
              message: "Workflow run queued.",
              kind: "run.lifecycle",
              metadata: {
                dagHash: dag.dagHash,
                nodeOrder: [...dag.order],
                runnerDeploymentId: deployedRunnerConfig.id,
                deployedRunnerConfig: jsonObjectMetadata(deployedRunnerConfig.metadata.runnerConfig)
                  ? jsonRecord(deployedRunnerConfig.metadata.runnerConfig)
                  : null
              }
            })
          ],
          {
            workflowId: approvedRevision.workflowId,
            branchId: approvedRevision.branchId,
            revisionId: approvedRevision.id,
            runId,
            correlationId
          }
        ),
        result: null
      });
      const existingJobId = requestJobId(request);
      const existingJob = existingJobId ? store.getJob(existingJobId) : undefined;
      const jobPayload = jsonRecord({
        runId,
        approvedRevisionId: approvedRevision.id,
        deploymentId: deployedRunnerConfig.id,
        ...(runBranchId ? { branchId: runBranchId } : {})
      });
      const job =
        existingJob &&
        existingJob.type === "run.workflow" &&
        !isTerminalJobStatus(existingJob.status)
          ? store.appendJobEvent(
              store.saveJob({
                ...existingJob,
                workflowId: approvedRevision.workflowId,
                branchId: runBranchId,
                revisionId: approvedRevision.id,
                payload: jobPayload,
                retry: {
                  ...existingJob.retry,
                  maxAttempts: Math.max(existingJob.retry.maxAttempts, 3),
                  backoffSeconds: existingJob.retry.backoffSeconds ?? 2,
                  retryable: true
                },
                updatedAt: now
              }).id,
              createJobEvent(existingJob, "info", "Attached queued workflow run.", {
                runId
              })
            )
          : store.saveJob(
              createJob({
                type: "run.workflow",
                workflowId: approvedRevision.workflowId,
                branchId: runBranchId,
                revisionId: approvedRevision.id,
                correlationId,
                maxAttempts: 3,
                payload: jobPayload
              })
            );

      return reply.code(202).send({
        ok: true,
        run,
        job
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

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/runs", async (request, reply) => {
    if (!store.getWorkflow(request.params.id)) {
      return reply.code(404).send({
        ok: false,
        error: "WORKFLOW_NOT_FOUND",
        message: `Workflow '${request.params.id}' was not found.`
      });
    }

    return {
      ok: true,
      runs: store.listRuns(request.params.id)
    };
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
      run,
      checkpoints: store.listRunCheckpoints(run.id)
    };
  });

  app.post<{ Params: RunRouteParams }>(
    "/api/workflows/:id/runs/:runId/replay",
    async (request, reply) => {
      const sourceRun = store.getRun(request.params.runId);
      if (!sourceRun) {
        return reply.code(404).send({
          ok: false,
          error: "RUN_NOT_FOUND",
          message: `Run '${request.params.runId}' was not found.`
        });
      }
      if (sourceRun.workflowId !== request.params.id) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_RUN_ID_MISMATCH",
          message: `Run '${sourceRun.id}' belongs to workflow '${sourceRun.workflowId}'.`
        });
      }
      const approvedRevision = store.getApprovedRevision(sourceRun.approvedRevisionId);
      if (!approvedRevision) {
        return reply.code(404).send({
          ok: false,
          error: "APPROVED_REVISION_NOT_FOUND",
          message: `Approved revision '${sourceRun.approvedRevisionId}' was not found.`
        });
      }
      const deployedRunnerConfig = latestDeployedRunnerConfiguration(
        store,
        sourceRun.workflowId,
        sourceRun.approvedRevisionId,
        sourceRun.branchId
      );
      if (!deployedRunnerConfig) {
        return reply.code(409).send({
          ok: false,
          error: "WORKFLOW_RUN_REQUIRES_DEPLOYMENT",
          message: "Replay requires an active runner.configuration deployment."
        });
      }

      const correlationId = correlationIdForRequest(request);
      const now = new Date().toISOString();
      const runId = `run.${sourceRun.workflowId}.replay.${Date.now()}.${randomUUID()}`;
      const run = store.saveRun({
        id: runId,
        workflowId: sourceRun.workflowId,
        branchId: sourceRun.branchId,
        approvedRevisionId: sourceRun.approvedRevisionId,
        revision: sourceRun.revision,
        status: "queued",
        createdAt: now,
        startedAt: now,
        finishedAt: now,
        events: enrichRunEvents(
          [
            createStructuredRunEvent({
              id: "event.run.replay-queued",
              timestamp: now,
              level: "info",
              message: "Failed run replay queued.",
              kind: "run.lifecycle",
              metadata: {
                sourceRunId: sourceRun.id,
                runnerDeploymentId: deployedRunnerConfig.id
              }
            })
          ],
          {
            workflowId: sourceRun.workflowId,
            branchId: sourceRun.branchId,
            revisionId: sourceRun.approvedRevisionId,
            runId,
            correlationId
          }
        ),
        result: null
      });
      const job = store.saveJob(
        createJob({
          type: "run.workflow",
          workflowId: sourceRun.workflowId,
          branchId: sourceRun.branchId,
          revisionId: sourceRun.approvedRevisionId,
          correlationId,
          maxAttempts: 3,
          payload: {
            runId,
            approvedRevisionId: sourceRun.approvedRevisionId,
            deploymentId: deployedRunnerConfig.id,
            sourceRunId: sourceRun.id
          }
        })
      );

      return reply.code(202).send({
        ok: true,
        run,
        job
      });
    }
  );

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

  app.get<{ Params: RouteParamsWithId; Reply: WorkflowAgentMemoryListResponse | WorkflowApiError }>(
    "/api/workflows/:id/memory",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        } as never);
      }
      store.expireAgentMemory();

      return {
        ok: true,
        memories: store.listAgentMemory(request.params.id)
      };
    }
  );

  app.delete<{ Params: MemoryRouteParams }>(
    "/api/workflows/:id/memory/:memoryId",
    async (request, reply) => {
      const memory = store
        .listAgentMemory(request.params.id, { includeExpired: true })
        .find((record) => record.id === request.params.memoryId);
      if (!memory) {
        return reply.code(404).send({
          ok: false,
          error: "AGENT_MEMORY_NOT_FOUND",
          message: `Memory '${request.params.memoryId}' was not found for workflow '${request.params.id}'.`
        });
      }

      return {
        ok: true,
        deleted: store.deleteAgentMemory(request.params.memoryId)
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

  app.get<{ Params: RouteParamsWithId; Querystring: { readonly branchId?: string } }>(
    "/api/workflows/:id/runtime-truth",
    async (request, reply) => {
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
        truth: runtimeTruthSnapshot(store, request.params.id, request.query.branchId)
      };
    }
  );

  app.get<{ Params: RouteParamsWithId; Querystring: { readonly branchId?: string } }>(
    "/api/workflows/:id/budget",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      const policy = ensureBudgetPolicy(store, request.params.id, request.query.branchId);
      return {
        ok: true,
        policy,
        ledgers: store.listBudgetLedgers(request.params.id)
      };
    }
  );

  app.patch<{ Params: RouteParamsWithId; Body: BudgetPatchRequestBody }>(
    "/api/workflows/:id/budget",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      const policy = ensureBudgetPolicy(store, request.params.id, request.body.branchId);
      const updated = store.saveBudgetPolicy({
        ...policy,
        maxWorkflowCostUsd: request.body.maxWorkflowCostUsd ?? policy.maxWorkflowCostUsd,
        maxCodegenCostUsd: request.body.maxCodegenCostUsd ?? policy.maxCodegenCostUsd,
        maxAgenticCostUsd: request.body.maxAgenticCostUsd ?? policy.maxAgenticCostUsd,
        expensiveRetryConfirmationUsd:
          request.body.expensiveRetryConfirmationUsd ?? policy.expensiveRetryConfirmationUsd,
        perAgentMaxCostUsd: request.body.perAgentMaxCostUsd
          ? sanitizePerAgentBudget(request.body.perAgentMaxCostUsd)
          : policy.perAgentMaxCostUsd,
        updatedAt: new Date().toISOString(),
        updatedBy: request.body.updatedBy ?? "api"
      });
      recordAudit(store, {
        action: "budget.updated",
        actor: updated.updatedBy,
        workflowId: request.params.id,
        branchId: updated.branchId,
        revisionId: `budget.${request.params.id}`,
        correlationId: correlationIdForRequest(request),
        summary: "Updated workflow budget policy.",
        metadata: jsonRecord(updated)
      });
      return {
        ok: true,
        policy: updated,
        ledgers: store.listBudgetLedgers(request.params.id)
      };
    }
  );

  app.get<{ Params: RouteParamsWithId; Querystring: { readonly branchId?: string } }>(
    "/api/workflows/:id/alerts",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      return {
        ok: true,
        policy: ensureAlertPolicy(store, request.params.id, request.query.branchId)
      };
    }
  );

  app.patch<{ Params: RouteParamsWithId; Body: AlertPolicyRequestBody }>(
    "/api/workflows/:id/alerts",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      const current = ensureAlertPolicy(store, request.params.id, request.body.branchId);
      const updated = store.saveAlertPolicy({
        ...current,
        enabled: request.body.enabled ?? current.enabled,
        events: request.body.events ?? current.events,
        channels: request.body.channels ?? current.channels,
        secretRefs: request.body.secretRefs ?? current.secretRefs,
        updatedAt: new Date().toISOString(),
        updatedBy: request.body.updatedBy ?? "api"
      });

      return {
        ok: true,
        policy: updated
      };
    }
  );

  app.get<{ Params: RouteParamsWithId; Querystring: { readonly branchId?: string } }>(
    "/api/workflows/:id/retention",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      return {
        ok: true,
        policy: ensureRetentionPolicy(store, request.params.id, request.query.branchId)
      };
    }
  );

  app.patch<{ Params: RouteParamsWithId; Body: RetentionPolicyRequestBody }>(
    "/api/workflows/:id/retention",
    async (request, reply) => {
      if (!store.getWorkflow(request.params.id)) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_NOT_FOUND",
          message: `Workflow '${request.params.id}' was not found.`
        });
      }
      const current = ensureRetentionPolicy(store, request.params.id, request.body.branchId);
      const updated = store.saveRetentionPolicy({
        ...current,
        maxRunEventDays: request.body.maxRunEventDays ?? current.maxRunEventDays,
        maxSuccessfulRunWorkspaceDays:
          request.body.maxSuccessfulRunWorkspaceDays ?? current.maxSuccessfulRunWorkspaceDays,
        maxFailedRunWorkspaceDays:
          request.body.maxFailedRunWorkspaceDays ?? current.maxFailedRunWorkspaceDays,
        maxJobEventDays: request.body.maxJobEventDays ?? current.maxJobEventDays,
        updatedAt: new Date().toISOString(),
        updatedBy: request.body.updatedBy ?? "api"
      });
      recordAudit(store, {
        action: "retention.cleaned",
        actor: updated.updatedBy,
        workflowId: request.params.id,
        branchId: updated.branchId,
        revisionId: `retention.${request.params.id}`,
        correlationId: correlationIdForRequest(request),
        summary: "Updated workflow retention policy.",
        metadata: jsonRecord(updated)
      });
      return {
        ok: true,
        policy: updated
      };
    }
  );

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/agent-timeline", async (request) => ({
    ok: true,
    events: store.listAgentTimelineEvents(request.params.id)
  }));

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/decision-traces", async (request) => ({
    ok: true,
    traces: store.listNodeDecisionTraces(request.params.id)
  }));

  app.get<{ Params: CodegenRouteParams }>(
    "/api/workflows/:id/nodes/:nodeId/decision-traces",
    async (request) => ({
      ok: true,
      traces: store.listNodeDecisionTraces(request.params.id, request.params.nodeId)
    })
  );

  app.get<{ Params: RouteParamsWithId }>(
    "/api/workflows/:id/decision-traces/export",
    async (request) => {
      const exportRecord = createDecisionTraceExportRecord(store, request.params.id);
      return {
        ok: true,
        export: exportRecord,
        jsonl: exportRecord.records.map((record) => stableJsonStringify(record)).join("\n")
      };
    }
  );

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/audit/export", async (request) => {
    const exportRecord = createAuditExportRecord(store, request.params.id);
    recordAudit(store, {
      action: "audit.exported",
      actor: "api",
      workflowId: request.params.id,
      revisionId: `audit-export.${request.params.id}`,
      correlationId: correlationIdForRequest(request),
      summary: "Exported redacted workflow audit records.",
      metadata: {
        exportId: exportRecord.id,
        lineCount: exportRecord.lineCount
      }
    });
    return {
      ok: true,
      export: exportRecord,
      jsonl: exportRecord.records.map((record) => stableJsonStringify(record)).join("\n")
    };
  });

  app.get<{ Params: RouteParamsWithId }>(
    "/api/workflows/:id/deployments/active",
    async (request) => ({
      ok: true,
      ...deploymentActivationSummary(store, request.params.id)
    })
  );

  app.get<{ Params: RouteParamsWithId }>("/api/workflows/:id/schedules", async (request) => ({
    ok: true,
    schedules: store.listSchedules(request.params.id)
  }));

  app.post<{ Params: ScheduleRouteParams }>(
    "/api/workflows/:id/schedules/:scheduleId/pause",
    async (request, reply) => {
      const schedule = store.getSchedule(request.params.scheduleId);
      if (!schedule || schedule.workflowId !== request.params.id) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_SCHEDULE_NOT_FOUND",
          message: `Schedule '${request.params.scheduleId}' was not found.`
        });
      }
      const updated = store.saveSchedule({
        ...schedule,
        status: "paused",
        updatedAt: new Date().toISOString()
      });
      recordAudit(store, {
        action: "schedule.updated",
        actor: "api",
        workflowId: updated.workflowId,
        branchId: updated.branchId,
        revisionId: updated.approvedRevisionId,
        correlationId: correlationIdForRequest(request),
        summary: `Paused schedule '${updated.id}'.`,
        metadata: scheduleSummary(updated)
      });
      return {
        ok: true,
        schedule: updated
      };
    }
  );

  app.post<{ Params: ScheduleRouteParams }>(
    "/api/workflows/:id/schedules/:scheduleId/resume",
    async (request, reply) => {
      const schedule = store.getSchedule(request.params.scheduleId);
      if (!schedule || schedule.workflowId !== request.params.id) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_SCHEDULE_NOT_FOUND",
          message: `Schedule '${request.params.scheduleId}' was not found.`
        });
      }
      const now = new Date();
      const updated = store.saveSchedule({
        ...schedule,
        status: "active",
        updatedAt: now.toISOString(),
        nextFireAt: nextCronFire(schedule.cron, schedule.timezone, now).toISOString(),
        lastError: undefined
      });
      recordAudit(store, {
        action: "schedule.updated",
        actor: "api",
        workflowId: updated.workflowId,
        branchId: updated.branchId,
        revisionId: updated.approvedRevisionId,
        correlationId: correlationIdForRequest(request),
        summary: `Resumed schedule '${updated.id}'.`,
        metadata: scheduleSummary(updated)
      });
      return {
        ok: true,
        schedule: updated
      };
    }
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
      const deploymentBranch = deploymentBranchId ? store.getBranch(deploymentBranchId) : undefined;
      if (deploymentBranch?.status === "archived") {
        return reply.code(409).send(archivedBranchApiError(deploymentBranch));
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
      if (savedDeployment.kind === "schedule.activation") {
        registerSchedulesForDeployment(store, savedDeployment, approvedRevision);
      }
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

  app.post<{ Params: DeploymentRouteParams }>(
    "/api/workflows/:id/deployments/:deploymentId/undeploy",
    async (request, reply) => {
      const deployment = store
        .listDeployments(request.params.id)
        .find((candidate) => candidate.id === request.params.deploymentId);
      if (!deployment) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_NOT_FOUND",
          message: `Deployment '${request.params.deploymentId}' was not found.`
        });
      }
      if (deployment.status !== "deployed") {
        return {
          ok: true,
          deployment
        };
      }

      const updated = store.saveDeployment({
        ...deployment,
        status: "undeployed",
        metadata: {
          ...deployment.metadata,
          undeployedAt: new Date().toISOString()
        }
      });
      disableSchedulesForDeployment(store, deployment.id, "disabled");
      recordAudit(store, {
        action: "deployment.undeployed",
        actor: "api",
        workflowId: request.params.id,
        branchId: deployment.branchId,
        revisionId: deployment.approvedRevisionId,
        correlationId: correlationIdForRequest(request),
        summary: `Undeployed ${deployment.kind} deployment '${deployment.id}'.`,
        metadata: {
          deploymentId: deployment.id,
          kind: deployment.kind
        }
      });
      return {
        ok: true,
        deployment: updated,
        active: deploymentActivationSummary(store, request.params.id)
      };
    }
  );

  app.post<{ Params: DeploymentRouteParams }>(
    "/api/workflows/:id/deployments/:deploymentId/rollback",
    async (request, reply) => {
      const deployments = store.listDeployments(request.params.id);
      const deployment = deployments.find(
        (candidate) => candidate.id === request.params.deploymentId
      );
      if (!deployment) {
        return reply.code(404).send({
          ok: false,
          error: "WORKFLOW_DEPLOYMENT_NOT_FOUND",
          message: `Deployment '${request.params.deploymentId}' was not found.`
        });
      }
      const rollbackTarget = deployments
        .filter(
          (candidate) =>
            candidate.id !== deployment.id &&
            candidate.kind === deployment.kind &&
            candidate.status === "deployed" &&
            candidate.createdAt < deployment.createdAt
        )
        .at(-1);
      const updated = store.saveDeployment({
        ...deployment,
        status: "rolled-back",
        metadata: {
          ...deployment.metadata,
          rolledBackAt: new Date().toISOString(),
          rollbackTarget: jsonRecord(
            rollbackTarget
              ? createDeploymentRollbackTarget(deployment, rollbackTarget)
              : createDeploymentRollbackTarget(deployment)
          )
        }
      });
      disableSchedulesForDeployment(store, deployment.id, "disabled");
      recordAudit(store, {
        action: "deployment.rolled-back",
        actor: "api",
        workflowId: request.params.id,
        branchId: deployment.branchId,
        revisionId: deployment.approvedRevisionId,
        correlationId: correlationIdForRequest(request),
        summary: `Rolled back ${deployment.kind} deployment '${deployment.id}'.`,
        metadata: {
          deploymentId: deployment.id,
          rollbackTargetId: rollbackTarget?.id ?? null
        }
      });
      return {
        ok: true,
        deployment: updated,
        rollbackTarget: updated.metadata.rollbackTarget,
        active: deploymentActivationSummary(store, request.params.id)
      };
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
        const result = await executeCompiledDag(dag, runner ?? createNanoClawRunner(store), {
          codegenArtifactStore: artifactStore,
          secretResolver: new SecretStoreResolver(secretStore),
          agentMemory: createAgentMemoryAccess(store)
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

function createClarificationRequestIfNeeded(
  request: WorkflowPlanRequest,
  correlationId: string
): WorkflowClarificationRequest | null {
  const prompt = request.prompt.trim();
  if (!promptNeedsClarification(prompt) || hasRequiredClarificationAnswers(request)) {
    return null;
  }

  const id = `clarify.${createHash("sha256")
    .update(`${correlationId}:${prompt}`)
    .digest("hex")
    .slice(0, 16)}`;
  const researchLike = /\b(research|investigate|agent|reason|compare|tasking)\b/iu.test(prompt);
  const questions = researchLike
    ? [
        {
          id: "research-topic",
          question: "What exact topic, entity, or decision should the research focus on?",
          required: true,
          placeholder: "Example: vendors for SOC2 monitoring in Singapore fintech teams"
        },
        {
          id: "desired-output",
          question: "What should the agent produce when it is done?",
          required: true,
          placeholder: "Example: ranked options with sources, risks, and recommendation"
        },
        {
          id: "scope",
          question: "Are there source, geography, freshness, or exclusion constraints?",
          required: false,
          placeholder: "Example: official docs and 2025+ sources only"
        }
      ]
    : [
        {
          id: "goal",
          question: "What outcome should this workflow accomplish?",
          required: true,
          placeholder: "Example: summarize new support tickets and email a daily digest"
        },
        {
          id: "inputs",
          question: "What input source should start or feed the workflow?",
          required: true,
          placeholder: "Example: Gmail label, uploaded CSV, webhook, or manual text"
        },
        {
          id: "delivery",
          question: "Where should the final result go?",
          required: false,
          placeholder: "Example: email, Sheets, Slack, or leave in workspace"
        }
      ];

  return {
    id,
    prompt,
    reason:
      "The prompt does not include enough concrete target, input, or output detail to plan safely.",
    createdAt: new Date().toISOString(),
    questions
  };
}

function promptNeedsClarification(prompt: string): boolean {
  const normalized = prompt.toLowerCase();
  const words = normalized.split(/\s+/u).filter(Boolean);
  if (words.length < 5) {
    return true;
  }

  return [
    /\b(this|that|it|something|someone|stuff|thing|tasking)\b/iu,
    /\b(help me|do this|handle this|figure this out)\b/iu,
    /\bresearch\s+(this|it|that)\b/iu
  ].some((pattern) => pattern.test(normalized));
}

function hasRequiredClarificationAnswers(request: WorkflowPlanRequest): boolean {
  const answers = usableClarificationAnswers(request.clarificationAnswers);
  if (answers.length === 0) {
    return false;
  }
  if (answers.length >= 2) {
    return true;
  }

  return answers.some((answer) => answer.answer.trim().split(/\s+/u).filter(Boolean).length >= 6);
}

function enrichPlanRequestWithClarifications(request: WorkflowPlanRequest): WorkflowPlanRequest {
  const answers = usableClarificationAnswers(request.clarificationAnswers);
  if (answers.length === 0) {
    return request;
  }

  const clarificationText = answers
    .map((answer) => `- ${questionLabel(answer.questionId)}: ${answer.answer.trim()}`)
    .join("\n");
  return {
    ...request,
    prompt: `${request.prompt.trim()}\n\nClarifications:\n${clarificationText}`
  };
}

function usableClarificationAnswers(
  answers: readonly WorkflowClarificationAnswer[] | undefined
): readonly WorkflowClarificationAnswer[] {
  return (answers ?? []).filter((answer) => answer.answer.trim().length > 0);
}

function questionLabel(questionId: string): string {
  return questionId
    .split("-")
    .filter(Boolean)
    .map((part) => `${part.charAt(0).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}

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

function createPromotedSkillFromTrajectory(
  run: TrajectoryRun,
  workflow: WorkflowSpec,
  input: AgentRunPromoteRequestBody
): SkillMetadata {
  const firstStep = run.events[0];
  const lastStep = run.events.at(-1);
  const name = input.skillName?.trim() || run.title || `Promoted Trajectory ${run.id}`;
  const capabilities =
    input.capabilities && input.capabilities.length > 0
      ? input.capabilities
      : [trajectoryCapability(run)];

  return {
    id: `skill.promoted.${slugify(`${run.id}-${name}`)}`,
    name,
    version: "1.0.0",
    description: `Replays ${run.events.length} recorded tool calls from ${run.sourceAgent}.`,
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities,
    inputSchema: { request: { type: "object", additionalProperties: true } },
    outputSchema: { result: { type: "object", additionalProperties: true } },
    requiredSecrets: [],
    adapterDependencies: [
      ...new Set(
        run.events.map((event) => event.toolName).filter((tool) => tool.startsWith("adapter."))
      )
    ].sort(),
    adapterOperations: [],
    runtimeTemplate: workflow.nodes[0]?.runtime ?? createWorkflowRuntime(),
    metaprompt: `Select this promoted trajectory skill when a task asks to replay ${[
      ...new Set(run.events.map((event) => event.toolName))
    ].join(", ")} steps captured from ${run.sourceAgent}.`,
    validationRules: [
      "promoted from a verified hash-chained agent trajectory",
      "recorded tool args and results must match content hashes before replay"
    ],
    examples: [
      {
        id: `example.${slugify(run.id)}`,
        description: `Fixture for recorded run ${run.id}.`,
        input: firstStep?.args ?? {},
        output:
          lastStep?.result && typeof lastStep.result === "object" && !Array.isArray(lastStep.result)
            ? lastStep.result
            : { result: lastStep?.result ?? null }
      }
    ],
    source: "promoted",
    promotedFromNodeId: workflow.nodes.find((node) => node.kind === "agent-step")?.id
  };
}

function firstUnresolvedPolicyApproval(run: AgentRunRecord) {
  return run.events.find(
    (event) =>
      event.status === "pending" &&
      event.policyDecision?.action === "require-approval" &&
      policyApprovalStatus(run, event.id) !== "approved"
  );
}

function policyApprovalStatus(
  run: AgentRunRecord,
  eventId: string
): "approved" | "denied" | undefined {
  for (const auditEvent of run.auditEvents) {
    if (auditEvent.eventId !== eventId) {
      continue;
    }
    if (auditEvent.action === "policy.approved") {
      return "approved";
    }
    if (auditEvent.metadata?.approvalStatus === "denied") {
      return "denied";
    }
  }
  return undefined;
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

function trajectoryCapability(run: TrajectoryRun): string {
  const text = run.events
    .map((event) => `${event.toolName} ${JSON.stringify(event.args)}`)
    .join(" ")
    .toLowerCase();
  if (text.includes("bash")) {
    return "terminal-tool-sequence";
  }
  if (text.includes("gmail")) {
    return "gmail-tool-sequence";
  }
  if (text.includes("github")) {
    return "github-tool-sequence";
  }
  return "agent-tool-sequence";
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

type SuccessfulWorkflowValidation = Extract<WorkflowValidationResult, { readonly ok: true }>;

type BranchDraftFinalizationResult =
  | {
      readonly ok: true;
      readonly draftRevision: WorkflowDraftRevision;
      readonly branch: WorkflowBranch;
      readonly reuseDecisions: readonly WorkflowGeneratedModuleReuseDecision[];
      readonly reuseApplied: boolean;
    }
  | {
      readonly ok: false;
      readonly validation: Exclude<WorkflowValidationResult, { readonly ok: true }>;
    };

function finalizeBranchDraftWithReuse(
  store: WorkflowStore,
  input: {
    readonly workflowId: string;
    readonly branch: WorkflowBranch;
    readonly validation: SuccessfulWorkflowValidation;
    readonly source: WorkflowDraftRevisionSource;
    readonly parentDraftRevisionId: string;
    readonly actor: string;
    readonly correlationId: string;
  }
): BranchDraftFinalizationResult {
  let draftRevision = store.saveDraftRevision(
    input.validation.workflow,
    input.validation,
    input.source,
    {
      branchId: input.branch.id,
      force: true,
      parentDraftRevisionId: input.parentDraftRevisionId,
      updateBranchHead: false
    }
  );
  const reuse = applyGeneratedModuleReuse(
    store,
    input.workflowId,
    input.branch.id,
    draftRevision.workflow
  );
  const reuseDecisions = reuse.decisions.map((decision) =>
    store.saveGeneratedModuleReuseDecision(decision)
  );

  if (reuse.changed) {
    const reusedValidation = validateWorkflowSpec(reuse.workflow);
    if (!reusedValidation.ok) {
      return {
        ok: false,
        validation: reusedValidation
      };
    }
    draftRevision = store.saveDraftRevision(
      reusedValidation.workflow,
      reusedValidation,
      input.source,
      {
        branchId: input.branch.id,
        force: true,
        parentDraftRevisionId: draftRevision.id,
        updateBranchHead: false
      }
    );
  }

  const branch = store.saveBranch({
    ...input.branch,
    headDraftRevisionId: draftRevision.id,
    updatedAt: draftRevision.createdAt,
    metadata: jsonRecord({
      ...(input.branch.metadata ?? {}),
      latestReuseDecisionIds: reuseDecisions.map((decision) => decision.id),
      latestReuseApplied: reuse.changed,
      latestReuseDraftRevisionId: draftRevision.id,
      latestReuseSource: input.source
    })
  });
  persistCodegenArtifactManifests(
    store,
    draftRevision.workflow,
    draftRevision.id,
    draftRevision.createdAt
  );

  if (reuse.changed) {
    recordAudit(store, {
      action: "codegen.reused",
      actor: input.actor,
      workflowId: draftRevision.workflowId,
      branchId: input.branch.id,
      revisionId: draftRevision.id,
      correlationId: input.correlationId,
      summary: "Applied compatible generated module reuse to branch draft.",
      metadata: jsonRecord({
        source: input.source,
        reuseDecisionIds: reuseDecisions.map((decision) => decision.id),
        nodeIds: reuseDecisions
          .filter((decision) => decision.status === "reuse-with-reeval")
          .map((decision) => decision.nodeId)
      })
    });
  }

  return {
    ok: true,
    draftRevision,
    branch,
    reuseDecisions,
    reuseApplied: reuse.changed
  };
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

function createNanoClawRunner(store?: WorkflowStore): NodeRunner {
  if (process.env.NANOCLAW_RUNNER === "mock") {
    return new AdapterBackedNodeRunner({
      adapters: createRegisteredAdapters(store, true),
      fallbackRunner: new MockNodeRunner()
    });
  }

  return new AdapterBackedNodeRunner({
    adapters: createRegisteredAdapters(store, false),
    fallbackRunner: new ProductionNodeRunner({
      dockerBin: process.env.NANOCLAW_DOCKER_BIN,
      hostWorkspace: process.env.NANOCLAW_HOST_WORKSPACE ?? process.cwd()
    })
  });
}

function createRegisteredAdapters(store: WorkflowStore | undefined, mock: boolean) {
  const adapters = mock
    ? createDefaultMockAdapters()
    : createDefaultLiveAdapters({
        smtp: {
          host: process.env.SMTP_HOST,
          port: process.env.SMTP_PORT ? Number(process.env.SMTP_PORT) : undefined,
          secure: process.env.SMTP_SECURE === "1",
          username: process.env.SMTP_USERNAME,
          password: process.env.SMTP_PASSWORD,
          from: process.env.SMTP_FROM
        }
      });
  for (const connector of store?.listConnectors() ?? []) {
    if (connector.kind === "openapi") {
      const adapter = createOpenApiAdapter(connector);
      adapters.set(adapter.metadata.id, adapter);
    }
    if (connector.kind === "mcp") {
      const adapter = createMcpAdapter(connector);
      adapters.set(adapter.metadata.id, adapter);
    }
  }

  return adapters;
}

function createAgentMemoryAccess(store: WorkflowStore): AgentMemoryAccess {
  return {
    list(input) {
      const records = store.listAgentMemory(undefined, {
        namespace: input.namespace
      });
      switch (input.memoryScope) {
        case "none":
          return [];
        case "node":
          return records.filter(
            (record) =>
              record.scope === "node" &&
              record.workflowId === input.workflowId &&
              record.nodeId === input.nodeId
          );
        case "workflow":
          return records.filter(
            (record) =>
              (record.scope === "workflow" || record.scope === "node") &&
              record.workflowId === input.workflowId &&
              (record.branchId ?? "") === (input.branchId ?? "")
          );
        case "workspace":
          return records.filter((record) => record.shareable);
      }
    },
    save(record) {
      return store.saveAgentMemory(record);
    }
  };
}

async function executeWorkflowRunJob(input: {
  readonly store: WorkflowStore;
  readonly secretStore: SecretStore;
  readonly artifactStore: CodegenArtifactStore;
  readonly runner?: NodeRunner | undefined;
  readonly job: WorkflowJob;
  readonly signal: AbortSignal;
}): Promise<{ readonly result: JsonRecord }> {
  const payload = input.job.payload ?? {};
  const runId = stringFromJson(payload.runId);
  const approvedRevisionId = stringFromJson(payload.approvedRevisionId ?? input.job.revisionId);
  const deploymentId = stringFromJson(payload.deploymentId);
  if (!runId || !approvedRevisionId) {
    throw new Error("run.workflow job payload requires runId and approvedRevisionId.");
  }
  const approvedRevision = input.store.getApprovedRevision(approvedRevisionId);
  if (!approvedRevision) {
    throw new Error(`Approved revision '${approvedRevisionId}' was not found.`);
  }
  const run = input.store.getRun(runId);
  if (!run) {
    throw new Error(`Run '${runId}' was not found.`);
  }
  const deployment =
    deploymentId === undefined
      ? latestDeployedRunnerConfiguration(
          input.store,
          approvedRevision.workflowId,
          approvedRevision.id,
          approvedRevision.branchId
        )
      : input.store
          .listDeployments(approvedRevision.workflowId)
          .find((candidate) => candidate.id === deploymentId);
  if (
    !deployment ||
    deployment.status !== "deployed" ||
    deployment.kind !== "runner.configuration"
  ) {
    throw new Error("Workflow run requires an active runner.configuration deployment.");
  }

  const dag = compileWorkflowDag(approvedRevision.workflow);
  const correlationId = input.job.correlationId;
  const context = {
    workflowId: approvedRevision.workflowId,
    branchId: approvedRevision.branchId,
    revisionId: approvedRevision.id,
    runId,
    correlationId
  };
  let events = [...run.events];
  const startedAt = new Date().toISOString();
  const saveRunState = (patch: Partial<WorkflowRunRecord>) => {
    const current = input.store.getRun(runId) ?? run;
    input.store.saveRun({
      ...current,
      ...patch,
      events
    });
  };
  const appendRunEvent = (event: WorkflowRunEvent) => {
    const [enriched] = enrichRunEvents([event], context);
    if (!enriched) {
      return;
    }
    events = [...events, enriched];
    saveRunState({});
  };

  events = [
    ...events,
    ...enrichRunEvents(
      [
        createStructuredRunEvent({
          id: "event.dag.compiled",
          timestamp: startedAt,
          level: "info",
          message: "NanoClaw DAG compiled.",
          kind: "dag.compilation",
          metadata: {
            dagHash: dag.dagHash,
            nodeOrder: [...dag.order],
            runnerDeploymentId: deployment.id,
            deployedRunnerConfig: jsonObjectMetadata(deployment.metadata.runnerConfig)
              ? jsonRecord(deployment.metadata.runnerConfig)
              : null
          }
        })
      ],
      context
    )
  ];
  saveRunState({
    status: "running",
    startedAt,
    finishedAt: startedAt
  });

  try {
    const result = await executeCompiledDag(
      dag,
      input.runner ?? createNanoClawRunner(input.store),
      {
        codegenArtifactStore: input.artifactStore,
        runId,
        approvedRevisionId: approvedRevision.id,
        secretResolver: new SecretStoreResolver(input.secretStore),
        agentMemory: createAgentMemoryAccess(input.store),
        signal: input.signal,
        checkpointStore: {
          getCheckpoint: ({ runId: lookupRunId, nodeId, inputHash }) =>
            input.store.getRunCheckpoint(lookupRunId, nodeId, inputHash),
          saveCheckpoint: (checkpoint) => input.store.saveRunCheckpoint(checkpoint)
        },
        onEvent: appendRunEvent
      }
    );
    const finishedAt = result.finishedAt;
    const finalResult = {
      ...result,
      events
    };
    const completed = input.store.saveRun({
      ...(input.store.getRun(runId) ?? run),
      status: result.status,
      startedAt: result.startedAt,
      finishedAt,
      events,
      result: finalResult
    });
    recordRuntimeNodeDecisionTraces(
      input.store,
      approvedRevision.workflow,
      approvedRevision.id,
      completed,
      correlationId
    );
    recordRunAuditRecords(
      input.store,
      approvedRevision.workflow,
      approvedRevision.id,
      completed,
      correlationId
    );
    if (result.status === "failed") {
      await recordRunFailureAlert(
        input.store,
        input.secretStore,
        completed,
        approvedRevision.id,
        correlationId
      );
    }

    return {
      result: {
        runId,
        status: result.status
      }
    };
  } catch (error) {
    const failedAt = new Date().toISOString();
    const failedEvent = enrichRunEvents(
      [
        createStructuredRunEvent({
          id: `event.run.failed.${Date.now()}`,
          timestamp: failedAt,
          level: "error",
          message:
            error instanceof Error ? redactSecretString(error.message) : "Workflow run failed.",
          kind: "run.lifecycle"
        })
      ],
      context
    );
    events = [...events, ...failedEvent];
    const failed = input.store.saveRun({
      ...(input.store.getRun(runId) ?? run),
      status: input.signal.aborted ? "cancelled" : "failed",
      finishedAt: failedAt,
      events,
      result: null
    });
    await recordRunFailureAlert(
      input.store,
      input.secretStore,
      failed,
      approvedRevision.id,
      correlationId
    );
    throw error;
  }
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
  readonly branchId?: string | undefined;
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
    branchId: input.branchId,
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
          timezone: typeof node.config.timezone === "string" ? node.config.timezone : "UTC",
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

function registerSchedulesForDeployment(
  store: WorkflowStore,
  deployment: WorkflowDeploymentRecord,
  approvedRevision: Exclude<ReturnType<WorkflowStore["getApprovedRevision"]>, undefined>
): void {
  const now = new Date();
  for (const registration of jsonArrayMetadata(deployment.metadata.activeScheduleRegistrations)) {
    const nodeId = stringFromJson(registration.nodeId);
    const cron = stringFromJson(registration.schedule);
    if (!nodeId || !cron) {
      continue;
    }
    const timezone = stringFromJson(registration.timezone) ?? "UTC";
    let nextFireAt: string;
    try {
      nextFireAt = nextCronFire(cron, timezone, now).toISOString();
    } catch {
      nextFireAt = now.toISOString();
    }
    const schedule = store.saveSchedule({
      id: `schedule.${deployment.id}.${nodeId}`,
      workflowId: deployment.workflowId,
      ...(deployment.branchId ? { branchId: deployment.branchId } : {}),
      deploymentId: deployment.id,
      approvedRevisionId: approvedRevision.id,
      nodeId,
      label: stringFromJson(registration.label) ?? nodeId,
      cron,
      timezone,
      status: "active",
      createdAt: deployment.createdAt,
      updatedAt: now.toISOString(),
      nextFireAt,
      missedCount: 0,
      ...(Date.parse(nextFireAt) === now.getTime()
        ? { lastError: "Schedule cron could not be parsed during deployment registration." }
        : {})
    });
    recordAudit(store, {
      action: "schedule.updated",
      actor: "api",
      workflowId: deployment.workflowId,
      branchId: deployment.branchId,
      revisionId: approvedRevision.id,
      correlationId: `schedule.${deployment.id}`,
      summary: `Registered schedule '${schedule.id}'.`,
      metadata: scheduleSummary(schedule)
    });
  }
}

function disableSchedulesForDeployment(
  store: WorkflowStore,
  deploymentId: string,
  status: WorkflowScheduleRecord["status"]
): void {
  const now = new Date().toISOString();
  for (const schedule of store
    .listSchedules()
    .filter((item) => item.deploymentId === deploymentId)) {
    store.saveSchedule({
      ...schedule,
      status,
      updatedAt: now
    });
  }
}

function nextCronFire(cron: string, timezone: string, currentDate: Date = new Date()): Date {
  return CronExpressionParser.parse(cron, {
    currentDate,
    tz: timezone || "UTC"
  })
    .next()
    .toDate();
}

function revisionFromApprovedRevisionId(approvedRevisionId: string): number {
  const match = /\.r(?<revision>\d+)$/u.exec(approvedRevisionId);
  return match?.groups?.revision ? Number(match.groups.revision) : 1;
}

function connectorSummary(connector: WorkflowConnectorRecord): JsonRecord {
  return {
    id: connector.id,
    name: connector.name,
    kind: connector.kind,
    adapterId: connector.adapterId,
    operationCount: connector.operations.length,
    allowedHosts: [...connector.allowedHosts],
    authKinds: connector.auth.map((auth) => auth.scheme),
    lastTestStatus: connector.lastTest.status,
    lastTestedAt: connector.lastTest.testedAt ?? null
  };
}

function scheduleSummary(schedule: WorkflowScheduleRecord): JsonRecord {
  return {
    id: schedule.id,
    workflowId: schedule.workflowId,
    branchId: schedule.branchId ?? null,
    deploymentId: schedule.deploymentId,
    approvedRevisionId: schedule.approvedRevisionId,
    nodeId: schedule.nodeId,
    cron: schedule.cron,
    timezone: schedule.timezone,
    status: schedule.status,
    nextFireAt: schedule.nextFireAt,
    lastFireAt: schedule.lastFireAt ?? null,
    lastRunId: schedule.lastRunId ?? null,
    missedCount: schedule.missedCount,
    lastError: schedule.lastError ?? null
  };
}

function createDeploymentRollbackTarget(
  deployment: WorkflowDeploymentRecord,
  previousDeployment?: WorkflowDeploymentRecord | undefined
): WorkflowDeploymentRollbackTarget {
  return {
    deploymentId: deployment.id,
    workflowId: deployment.workflowId,
    ...(deployment.branchId ? { branchId: deployment.branchId } : {}),
    approvedRevisionId: deployment.approvedRevisionId,
    ...(previousDeployment ? { previousDeploymentId: previousDeployment.id } : {}),
    rollbackPlan: deployment.rollbackPlan,
    artifactRefs: deploymentArtifactRefs(deployment),
    createdAt: new Date().toISOString()
  };
}

function deploymentArtifactRefs(
  deployment: WorkflowDeploymentRecord
): readonly WorkflowCodegenArtifactRef[] {
  return [
    ...jsonArrayMetadata(deployment.metadata.artifacts),
    ...(jsonObjectMetadata(deployment.metadata.bundle) ? [deployment.metadata.bundle] : [])
  ].filter(isWorkflowArtifactRef);
}

function isWorkflowArtifactRef(
  value: JsonRecord
): value is JsonRecord & WorkflowCodegenArtifactRef {
  return (
    typeof value.path === "string" &&
    typeof value.checksum === "string" &&
    typeof value.contentType === "string"
  );
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

function providerRuntimeConfigsFromEnv(): readonly WorkflowProviderRuntimeConfig[] {
  const plannerProvider = providerFromEnv("KELPCLAW_PLANNER_PROVIDER");
  const agenticProvider = providerFromEnv("KELPCLAW_AGENTIC_PROVIDER", plannerProvider);
  const codegenProvider = providerFromEnv("KELPCLAW_CODEGEN_PROVIDER", plannerProvider);
  const configs: WorkflowProviderRuntimeConfig[] = [
    providerRuntimeConfig({
      role: "planner",
      provider: plannerProvider,
      model: modelForProviderRole("planner", plannerProvider)
    }),
    providerRuntimeConfig({
      role: "agentic-research",
      provider: agenticProvider,
      model: modelForProviderRole("agentic-research", agenticProvider)
    }),
    providerRuntimeConfig({
      role: "codegen",
      provider: codegenProvider,
      model: modelForProviderRole("codegen", codegenProvider)
    })
  ];

  for (const role of [
    "workflow-architect",
    "coder",
    "tester",
    "runner",
    "fixer",
    "evaluator"
  ] as const) {
    configs.push(
      providerRuntimeConfig({
        role,
        provider: codegenProvider,
        model: modelForProviderRole(role, codegenProvider)
      })
    );
  }

  return configs;
}

function providerRuntimeConfig(input: {
  readonly role: WorkflowProviderRuntimeConfig["role"];
  readonly provider: WorkflowProviderRuntimeConfig["provider"];
  readonly model: string;
}): WorkflowProviderRuntimeConfig {
  const missingCredential =
    input.provider === "deterministic"
      ? undefined
      : input.provider === "openai" && !process.env.OPENAI_API_KEY
        ? "OPENAI_API_KEY"
        : input.provider === "anthropic" && !process.env.ANTHROPIC_API_KEY
          ? "ANTHROPIC_API_KEY"
          : undefined;

  return {
    role: input.role,
    provider: input.provider,
    model: input.model,
    configured: missingCredential === undefined,
    ...(missingCredential ? { missingCredential } : {}),
    tokenAccounting: input.provider !== "deterministic",
    costAccounting: input.provider !== "deterministic",
    retryBudget: {
      maxAttempts: input.role === "planner" ? 1 : 2,
      maxCostUsd: input.role === "agentic-research" ? 2 : input.role === "planner" ? 1 : 2
    },
    runtimeLimits: {
      maxWallClockSeconds: input.role === "planner" ? 120 : 600,
      maxDockerRuntimeSeconds: input.role === "runner" ? 120 : 0
    }
  };
}

function providerFromEnv(
  key: "KELPCLAW_PLANNER_PROVIDER" | "KELPCLAW_AGENTIC_PROVIDER" | "KELPCLAW_CODEGEN_PROVIDER",
  fallback: WorkflowProviderRuntimeConfig["provider"] = "anthropic"
): WorkflowProviderRuntimeConfig["provider"] {
  const value = process.env[key] ?? fallback;
  return value === "openai" || value === "anthropic" || value === "deterministic"
    ? value
    : fallback;
}

function modelForProviderRole(
  role: WorkflowProviderRuntimeConfig["role"],
  provider: WorkflowProviderRuntimeConfig["provider"]
): string {
  if (provider === "deterministic") {
    return "deterministic";
  }
  if (provider === "openai") {
    return (
      openAiRoleModel(role) ??
      process.env.KELPCLAW_OPENAI_CODEGEN_MODEL ??
      process.env.KELPCLAW_CODEGEN_MODEL ??
      process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
      process.env.KELPCLAW_PLANNER_MODEL ??
      "gpt-5.4"
    );
  }

  return (
    anthropicRoleModel(role) ??
    process.env.KELPCLAW_CODEGEN_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    "claude-sonnet-4-5-20250929"
  );
}

function openAiRoleModel(role: WorkflowProviderRuntimeConfig["role"]): string | undefined {
  switch (role) {
    case "planner":
      return process.env.KELPCLAW_OPENAI_PLANNER_MODEL;
    case "agentic-research":
      return process.env.KELPCLAW_OPENAI_AGENTIC_MODEL ?? process.env.KELPCLAW_AGENTIC_MODEL;
    case "workflow-architect":
      return process.env.KELPCLAW_OPENAI_WORKFLOW_ARCHITECT_MODEL;
    case "coder":
      return process.env.KELPCLAW_OPENAI_CODER_MODEL;
    case "tester":
      return process.env.KELPCLAW_OPENAI_TESTER_MODEL;
    case "runner":
      return process.env.KELPCLAW_OPENAI_RUNNER_MODEL;
    case "fixer":
      return process.env.KELPCLAW_OPENAI_FIXER_MODEL;
    case "evaluator":
      return process.env.KELPCLAW_OPENAI_EVALUATOR_MODEL;
    case "codegen":
      return process.env.KELPCLAW_OPENAI_CODEGEN_MODEL;
  }
}

function anthropicRoleModel(role: WorkflowProviderRuntimeConfig["role"]): string | undefined {
  switch (role) {
    case "planner":
      return process.env.KELPCLAW_PLANNER_MODEL;
    case "agentic-research":
      return process.env.KELPCLAW_ANTHROPIC_AGENTIC_MODEL ?? process.env.KELPCLAW_AGENTIC_MODEL;
    case "workflow-architect":
      return process.env.KELPCLAW_WORKFLOW_ARCHITECT_MODEL ?? process.env.KELPCLAW_ARCHITECT_MODEL;
    case "coder":
      return process.env.KELPCLAW_CODER_MODEL;
    case "tester":
      return process.env.KELPCLAW_TESTER_MODEL;
    case "runner":
      return process.env.KELPCLAW_RUNNER_MODEL;
    case "fixer":
      return process.env.KELPCLAW_FIXER_MODEL;
    case "evaluator":
      return process.env.KELPCLAW_EVALUATOR_MODEL;
    case "codegen":
      return process.env.KELPCLAW_CODEGEN_MODEL;
  }
}

function defaultBudgetPolicy(
  workflowId: string,
  branchId?: string | undefined
): WorkflowBudgetPolicy {
  return {
    workflowId,
    ...(branchId ? { branchId } : {}),
    maxWorkflowCostUsd: 5,
    maxCodegenCostUsd: 2,
    maxAgenticCostUsd: 2,
    expensiveRetryConfirmationUsd: 0.25,
    perAgentMaxCostUsd: {},
    updatedAt: new Date().toISOString(),
    updatedBy: "system"
  };
}

function ensureBudgetPolicy(
  store: WorkflowStore,
  workflowId: string,
  branchId?: string | undefined
): WorkflowBudgetPolicy {
  return (
    store.getBudgetPolicy(workflowId, branchId) ??
    store.saveBudgetPolicy(defaultBudgetPolicy(workflowId, branchId))
  );
}

function defaultAlertPolicy(
  workflowId: string,
  branchId?: string | undefined
): WorkflowAlertPolicy {
  return {
    workflowId,
    ...(branchId ? { branchId } : {}),
    enabled: false,
    events: ["run.failed", "job.failed", "schedule.missed", "deployment.failed"],
    channels: [],
    secretRefs: {},
    updatedAt: new Date().toISOString(),
    updatedBy: "system"
  };
}

function ensureAlertPolicy(
  store: WorkflowStore,
  workflowId: string,
  branchId?: string | undefined
): WorkflowAlertPolicy {
  return (
    store.getAlertPolicy(workflowId, branchId) ??
    store.saveAlertPolicy(defaultAlertPolicy(workflowId, branchId))
  );
}

function defaultRetentionPolicy(
  workflowId: string,
  branchId?: string | undefined
): WorkflowRetentionPolicy {
  return {
    workflowId,
    ...(branchId ? { branchId } : {}),
    maxRunEventDays: 14,
    maxSuccessfulRunWorkspaceDays: 14,
    maxFailedRunWorkspaceDays: 30,
    maxJobEventDays: 14,
    updatedAt: new Date().toISOString(),
    updatedBy: "system"
  };
}

function ensureRetentionPolicy(
  store: WorkflowStore,
  workflowId: string,
  branchId?: string | undefined
): WorkflowRetentionPolicy {
  return (
    store.getRetentionPolicy(workflowId, branchId) ??
    store.saveRetentionPolicy(defaultRetentionPolicy(workflowId, branchId))
  );
}

async function recordRunFailureAlert(
  store: WorkflowStore,
  secretStore: SecretStore,
  run: WorkflowRunRecord,
  approvedRevisionId: string,
  correlationId: string
): Promise<void> {
  const policy = store.getAlertPolicy(run.workflowId, run.branchId);
  if (!policy?.enabled || !policy.events.includes("run.failed")) {
    return;
  }
  const now = new Date().toISOString();
  const deliveryEvents: WorkflowRunEvent[] = [];
  for (const channel of policy.channels) {
    const delivered = await deliverAlertChannel(
      policy,
      secretStore,
      run,
      channel,
      approvedRevisionId
    );
    deliveryEvents.push(
      createStructuredRunEvent({
        id: `event.alert.${channel}.${Date.now()}.${deliveryEvents.length}`,
        timestamp: new Date().toISOString(),
        level: delivered.ok ? "info" : "warn",
        message: delivered.ok
          ? `Run failure alert delivered through ${channel}.`
          : `Run failure alert delivery failed for ${channel}: ${delivered.error}`,
        kind: "alert.lifecycle",
        workflowId: run.workflowId,
        branchId: run.branchId,
        revisionId: approvedRevisionId,
        runId: run.id,
        correlationId,
        metadata: {
          channel,
          status: delivered.ok ? "succeeded" : "failed"
        }
      })
    );
  }
  store.saveRun({
    ...run,
    events: [
      ...run.events,
      createStructuredRunEvent({
        id: `event.alert.run-failed.${Date.now()}`,
        timestamp: now,
        level: "warn",
        message: "Run failure alert policy matched.",
        kind: "alert.lifecycle",
        workflowId: run.workflowId,
        branchId: run.branchId,
        revisionId: approvedRevisionId,
        runId: run.id,
        correlationId,
        metadata: {
          channels: [...policy.channels],
          secretRefs: Object.keys(policy.secretRefs)
        }
      }),
      ...deliveryEvents
    ]
  });
}

async function deliverAlertChannel(
  policy: WorkflowAlertPolicy,
  secretStore: SecretStore,
  run: WorkflowRunRecord,
  channel: WorkflowAlertPolicy["channels"][number],
  approvedRevisionId: string
): Promise<{ readonly ok: true } | { readonly ok: false; readonly error: string }> {
  if (channel === "webhook") {
    return { ok: false, error: "webhook alert delivery is not configured in this runtime" };
  }
  const adapters = createDefaultLiveAdapters({
    smtp: {
      host: process.env.SMTP_HOST,
      port: process.env.SMTP_PORT ? Number(process.env.SMTP_PORT) : undefined,
      secure: process.env.SMTP_SECURE === "1",
      username: process.env.SMTP_USERNAME,
      password: process.env.SMTP_PASSWORD,
      from: process.env.SMTP_FROM
    }
  });
  const adapterId = channel === "email" ? "adapter.email" : "adapter.telegram";
  const operation = channel === "email" ? "email.results.send" : "telegram.alert.send";
  const secretName = channel === "email" ? "email.delivery" : "telegram.botToken";
  const secretRef =
    policy.secretRefs[secretName] ??
    (channel === "email" ? "secret:email.smtp.default" : "secret:telegram.bot.default");
  const adapter = adapters.get(adapterId);
  if (!adapter) {
    return { ok: false, error: `adapter '${adapterId}' is unavailable` };
  }
  try {
    const secretValue = await new SecretStoreResolver(secretStore).resolve(secretRef);
    const subject = `KelpClaw run failed: ${run.workflowId}`;
    const body = `Run ${run.id} failed for ${run.workflowId} at ${run.finishedAt}.`;
    await adapter.invoke({
      adapterId,
      operation,
      operationVersion: "1.0.0",
      payload:
        channel === "email"
          ? {
              to: process.env.KELPCLAW_ALERT_EMAIL_TO ?? "owner@example.com",
              subject,
              body,
              summary: {
                workflowId: run.workflowId,
                runId: run.id,
                approvedRevisionId
              }
            }
          : {
              text: body
            },
      secretRefs: {
        [secretName]: secretRef
      },
      secrets: {
        [secretName]: secretValue
      },
      idempotencyKey: `alert.${run.id}.${channel}`,
      context: {
        workflowId: run.workflowId,
        nodeId: `alert.${channel}`,
        runId: run.id,
        attempt: 1
      }
    });
    return { ok: true };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "unknown error" };
  }
}

function sanitizePerAgentBudget(
  value: Partial<Record<string, number>>
): WorkflowBudgetPolicy["perAgentMaxCostUsd"] {
  return Object.fromEntries(
    Object.entries(value).filter((entry): entry is [string, number] => {
      const amount = entry[1];
      return typeof amount === "number" && Number.isFinite(amount) && amount >= 0;
    })
  ) as WorkflowBudgetPolicy["perAgentMaxCostUsd"];
}

function budgetCheckForProjectedCost(
  store: WorkflowStore,
  policy: WorkflowBudgetPolicy,
  projectedCostUsd: number,
  operation: string
):
  | { readonly ok: true; readonly remainingCostUsd: number }
  | {
      readonly ok: false;
      readonly remainingCostUsd: number;
      readonly reason: string;
    } {
  const actualCostUsd = workflowActualCostUsd(store, policy.workflowId);
  const remainingCostUsd = Math.max(0, policy.maxWorkflowCostUsd - actualCostUsd);
  if (projectedCostUsd > remainingCostUsd) {
    return {
      ok: false,
      remainingCostUsd,
      reason: `${operation} is blocked because projected cost $${projectedCostUsd.toFixed(
        4
      )} exceeds remaining workflow budget $${remainingCostUsd.toFixed(4)}.`
    };
  }

  return { ok: true, remainingCostUsd };
}

function workflowActualCostUsd(store: WorkflowStore, workflowId: string): number {
  return store
    .listAgentRuns(workflowId)
    .reduce((total, run) => total + (run.costUsd ?? agentRunInvocationCostUsd(run)), 0);
}

function agentRunInvocationCostUsd(run: CodegenAgentRunRecord): number {
  return (run.modelInvocations ?? []).reduce(
    (total, invocation) => total + (invocation.costUsd ?? 0),
    0
  );
}

function createBudgetLedger(input: {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly jobId?: string | undefined;
  readonly agentRunId?: string | undefined;
  readonly scope: WorkflowBudgetLedger["scope"];
  readonly projectedCostUsd: number;
  readonly actualCostUsd: number;
  readonly remainingCostUsd: number;
  readonly retryEstimateUsd: number;
  readonly status: WorkflowBudgetLedger["status"];
  readonly stopReason?: string | undefined;
}): WorkflowBudgetLedger {
  const now = new Date().toISOString();
  return {
    id: `budget.${input.scope}.${input.workflowId}.${Date.now()}.${randomUUID()}`,
    workflowId: input.workflowId,
    ...(input.branchId ? { branchId: input.branchId } : {}),
    ...(input.jobId ? { jobId: input.jobId } : {}),
    ...(input.agentRunId ? { agentRunId: input.agentRunId } : {}),
    scope: input.scope,
    projectedCostUsd: input.projectedCostUsd,
    actualCostUsd: input.actualCostUsd,
    remainingCostUsd: input.remainingCostUsd,
    retryEstimateUsd: input.retryEstimateUsd,
    status: input.status,
    ...(input.stopReason ? { stopReason: input.stopReason } : {}),
    createdAt: now,
    updatedAt: now
  };
}

function saveBudgetLedgersForAgentRuns(
  store: WorkflowStore,
  policy: WorkflowBudgetPolicy,
  agentRuns: readonly CodegenAgentRunRecord[],
  jobId: string
): void {
  let cumulativeActualCostUsd = workflowActualCostUsd(store, policy.workflowId);
  const jobActualCostUsd = agentRuns.reduce(
    (total, run) => total + (run.costUsd ?? agentRunInvocationCostUsd(run)),
    0
  );
  const remainingAfterJob = Math.max(0, policy.maxWorkflowCostUsd - cumulativeActualCostUsd);
  store.saveBudgetLedger(
    createBudgetLedger({
      workflowId: policy.workflowId,
      branchId: policy.branchId,
      jobId,
      scope: "job",
      projectedCostUsd: Math.min(policy.maxCodegenCostUsd, policy.maxWorkflowCostUsd),
      actualCostUsd: jobActualCostUsd,
      remainingCostUsd: remainingAfterJob,
      retryEstimateUsd: policy.expensiveRetryConfirmationUsd,
      status:
        remainingAfterJob <= 0
          ? "exhausted"
          : jobActualCostUsd >= policy.expensiveRetryConfirmationUsd
            ? "confirmation-required"
            : "within-budget"
    })
  );

  cumulativeActualCostUsd -= jobActualCostUsd;
  for (const run of agentRuns) {
    const actualCostUsd = run.costUsd ?? agentRunInvocationCostUsd(run);
    cumulativeActualCostUsd += actualCostUsd;
    store.saveBudgetLedger(
      createBudgetLedger({
        workflowId: run.workflowId,
        branchId: policy.branchId,
        jobId: run.jobId,
        agentRunId: run.id,
        scope: "agent",
        projectedCostUsd: policy.perAgentMaxCostUsd[run.role] ?? policy.maxCodegenCostUsd,
        actualCostUsd,
        remainingCostUsd: Math.max(0, policy.maxWorkflowCostUsd - cumulativeActualCostUsd),
        retryEstimateUsd: policy.expensiveRetryConfirmationUsd,
        status:
          actualCostUsd >= policy.expensiveRetryConfirmationUsd
            ? "confirmation-required"
            : "within-budget"
      })
    );
  }
}

function saveTimelineEventsForAgentRuns(
  store: WorkflowStore,
  agentRuns: readonly CodegenAgentRunRecord[],
  branchId?: string | undefined
): void {
  let cumulativeCostUsd = 0;
  for (const run of agentRuns) {
    const costUsd = run.costUsd ?? agentRunInvocationCostUsd(run);
    cumulativeCostUsd += costUsd;
    store.saveAgentTimelineEvent({
      id: `timeline.${run.id}.${run.finishedAt}`,
      workflowId: run.workflowId,
      ...(branchId ? { branchId } : {}),
      jobId: run.jobId,
      nodeId: run.nodeId,
      agentRunId: run.id,
      role: run.role,
      timestamp: run.finishedAt,
      status: run.status,
      title: `${run.role} ${run.status}`,
      summary: run.error ?? run.inputSummary,
      decision: timelineDecisionForRun(run),
      fixTriageAction: fixTriageActionFromSummary(run.inputSummary),
      outputArtifactRefs: run.outputArtifactRefs,
      ...(run.inputTokens !== undefined ? { inputTokens: run.inputTokens } : {}),
      ...(run.outputTokens !== undefined ? { outputTokens: run.outputTokens } : {}),
      ...(run.totalTokens !== undefined ? { totalTokens: run.totalTokens } : {}),
      ...(costUsd > 0 ? { costUsd } : {}),
      cumulativeCostUsd,
      metadata: {
        provider: run.modelProvider,
        model: run.model
      }
    });
  }
}

function timelineDecisionForRun(run: CodegenAgentRunRecord): string {
  if (run.error) {
    return run.error;
  }
  if (run.role === "fixer") {
    return run.inputSummary;
  }
  return run.outputArtifactRefs.length > 0
    ? `Produced ${run.outputArtifactRefs.length} artifact reference(s).`
    : "Completed role step.";
}

function fixTriageActionFromSummary(
  summary: string
): WorkflowAgentTimelineEvent["fixTriageAction"] | undefined {
  if (summary.includes("smallest local patch")) return "targeted-patch";
  if (summary.includes("Regenerate")) return "retry-codegen";
  if (summary.includes("Rebuild from a revised architecture")) return "rearchitect";
  if (summary.includes("External blocker")) return "give-up";
  return undefined;
}

function codegenDecisionKindForRole(
  role: CodegenAgentRunRecord["role"]
): WorkflowDecisionTraceKind {
  switch (role) {
    case "workflow-architect":
      return "codegen.architect";
    case "coder":
      return "codegen.coder";
    case "tester":
      return "codegen.tester";
    case "runner":
      return "codegen.runner";
    case "fixer":
      return "codegen.fixer";
    case "evaluator":
      return "codegen.evaluator";
    default:
      return "codegen.evaluator";
  }
}

function codegenAlternativesForRun(run: CodegenAgentRunRecord): readonly string[] {
  if (run.role === "fixer") {
    return [
      "Apply a targeted local patch.",
      "Regenerate code against the same contract.",
      "Rearchitect before coding again.",
      "Stop on external blocker."
    ];
  }
  if (run.role === "workflow-architect") {
    return ["Reuse the current node contract.", "Revise the generated-node design."];
  }
  return ["Continue the existing generated-node loop.", "Stop and report an unresolved failure."];
}

function codegenSelectedAction(run: CodegenAgentRunRecord): string {
  const triage = fixTriageActionFromSummary(run.inputSummary);
  if (triage) {
    return triage;
  }
  if (run.error) {
    return "record-failure";
  }
  return `complete-${run.role}`;
}

function plannerAlternativesForNode(
  node: WorkflowNode,
  route?: WorkflowTaskRoute | undefined
): readonly string[] {
  const alternatives = [
    "Use deterministic workflow primitives.",
    "Use adapter-backed skills.",
    "Generate a custom code node.",
    "Use an agentic runtime node."
  ];
  if (route?.route === "deterministic") {
    return alternatives.filter((item) => !item.includes("agentic"));
  }
  if (node.kind === "codegen") {
    return ["Use an existing skill or adapter.", "Generate a custom code node."];
  }
  if (node.agentic) {
    return ["Use a deterministic transform.", "Use an agentic runtime node."];
  }
  return alternatives;
}

function failureClassFromMessage(message: string): string {
  const normalized = message.toLowerCase();
  if (normalized.includes("schema")) return "schema";
  if (normalized.includes("dependency")) return "dependency";
  if (normalized.includes("network")) return "network";
  if (normalized.includes("budget")) return "budget";
  if (normalized.includes("timeout") || normalized.includes("timed out")) return "timeout";
  if (normalized.includes("secret") || normalized.includes("credential")) return "secret";
  return "runtime";
}

function sha256String(value: string): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function runtimeTruthSnapshot(
  store: WorkflowStore,
  workflowId: string,
  branchId?: string | undefined
): WorkflowRuntimeTruthSnapshot {
  const stored = store.requireWorkflow(workflowId);
  const branch = branchId ? store.getBranch(branchId) : store.getDefaultBranch(workflowId);
  const draftId = branch?.headDraftRevisionId ?? store.getLatestDraftRevision(workflowId)?.id;
  const draft = draftId
    ? store.getDraftRevision(draftId)
    : store.getLatestDraftRevision(workflowId);
  const acceptedDraft = stored.draftRevisions
    .filter(
      (revision) =>
        (!branch?.id || revision.branchId === branch.id) && revision.source === "plan-accepted"
    )
    .at(-1);
  const latestEvaluation = store.getLatestDraftEvaluation(workflowId, branch?.id);
  const approvedRevision = stored.approvedRevisions
    .filter((revision) => !branch?.id || revision.branchId === branch.id)
    .at(-1);
  const activeDeployments = store
    .listDeployments(workflowId)
    .filter(
      (deployment) =>
        deployment.status === "deployed" &&
        (!branch?.id || deployment.branchId === branch.id) &&
        (!approvedRevision || deployment.approvedRevisionId === approvedRevision.id)
    );
  const runnerDeployment = activeDeployments.find(
    (deployment) => deployment.kind === "runner.configuration"
  );
  const workflow = draft?.workflow ?? stored.workflow;
  const planned = workflow.nodes.length > 0;
  const accepted = acceptedDraft !== undefined;
  const generated =
    accepted &&
    workflow.nodes
      .filter((node) => node.kind === "codegen")
      .every((node) => node.codegen !== undefined && node.codegen.artifacts.length > 0);
  const evaluated = latestEvaluation?.status === "passed";
  const approved = approvedRevision !== undefined;
  const deployed = activeDeployments.length > 0;
  const runnable = deployed && runnerDeployment !== undefined && approved;
  const blockingReasons = [
    planned ? "" : "Create a planned workflow graph.",
    accepted ? "" : "Accept the plan before production approval.",
    generated ? "" : "Build generated/custom nodes before approval.",
    evaluated ? "" : "Run and pass draft evaluation.",
    approved ? "" : "Approve an immutable workflow revision.",
    deployed ? "" : "Deploy the approved revision.",
    runnerDeployment ? "" : "Deploy a runner.configuration before production run."
  ].filter((reason) => reason.length > 0);

  return {
    workflowId,
    ...(branch?.id ? { branchId: branch.id } : {}),
    stage: lifecycleStage({
      planned,
      accepted,
      generated,
      evaluated,
      approved,
      deployed,
      runnable
    }),
    planned,
    accepted,
    generated,
    evaluated,
    approved,
    deployed,
    runnable,
    ...(draft?.id ? { draftRevisionId: draft.id } : {}),
    ...(acceptedDraft?.id ? { acceptedDraftRevisionId: acceptedDraft.id } : {}),
    ...(latestEvaluation?.id ? { evaluationId: latestEvaluation.id } : {}),
    ...(approvedRevision?.id ? { approvedRevisionId: approvedRevision.id } : {}),
    ...(runnerDeployment?.id ? { runnerDeploymentId: runnerDeployment.id } : {}),
    activeDeploymentIds: activeDeployments.map((deployment) => deployment.id),
    blockingReasons,
    updatedAt: new Date().toISOString()
  };
}

function lifecycleStage(input: {
  readonly planned: boolean;
  readonly accepted: boolean;
  readonly generated: boolean;
  readonly evaluated: boolean;
  readonly approved: boolean;
  readonly deployed: boolean;
  readonly runnable: boolean;
}): WorkflowRuntimeTruthSnapshot["stage"] {
  if (input.runnable) return "runnable";
  if (input.deployed) return "deployed";
  if (input.approved) return "approved";
  if (input.evaluated) return "evaluated";
  if (input.generated) return "generated";
  if (input.accepted) return "accepted";
  if (input.planned) return "planned";
  return "empty";
}

function createAuditExportRecord(
  store: WorkflowStore,
  workflowId: string
): WorkflowAuditExportRecord {
  const auditRecords = store.listAuditRecords(workflowId).map((record) =>
    redactJsonRecord(
      jsonRecord({
        recordType: "audit",
        ...JSON.parse(JSON.stringify(record))
      }),
      {
        secretRefs: record.secretRefs
      }
    )
  );
  const decisionTraceRecords = store
    .listNodeDecisionTraces(workflowId)
    .map((trace) => redactJsonRecord(jsonRecord({ recordType: "node-decision-trace", ...trace })));
  const records = [...auditRecords, ...decisionTraceRecords];
  return {
    id: `audit-export.${workflowId}.${Date.now()}`,
    workflowId,
    exportedAt: new Date().toISOString(),
    format: "jsonl",
    redacted: true,
    lineCount: records.length,
    records
  };
}

function createDecisionTraceExportRecord(
  store: WorkflowStore,
  workflowId: string
): WorkflowNodeDecisionTraceExport {
  const traces = store.listNodeDecisionTraces(workflowId);
  const evalExamples = traces.map(decisionTraceEvalExample);
  const records = [
    ...traces.map((trace) =>
      redactJsonRecord(jsonRecord({ recordType: "node-decision-trace", ...trace }))
    ),
    ...evalExamples.map((example) =>
      redactJsonRecord(jsonRecord({ recordType: "decision-trace-eval-example", ...example }))
    )
  ];

  return {
    id: `decision-trace-export.${workflowId}.${Date.now()}`,
    workflowId,
    exportedAt: new Date().toISOString(),
    format: "jsonl",
    redacted: true,
    lineCount: records.length,
    records,
    evalExamples
  };
}

function decisionTraceEvalExample(
  trace: WorkflowNodeDecisionTrace
): WorkflowDecisionTraceEvalExample {
  const event = trace.events.at(-1)!;
  const outcome =
    event.evalOutcome === "passed"
      ? "pass"
      : event.evalOutcome === "failed"
        ? "fail"
        : event.evalOutcome === "blocked"
          ? "blocked"
          : "unknown";
  return {
    id: `eval-example.${trace.id}`,
    traceId: trace.id,
    workflowId: trace.workflowId,
    ...(trace.branchId ? { branchId: trace.branchId } : {}),
    nodeId: trace.nodeId,
    kind: trace.kind,
    createdAt: trace.createdAt,
    input: jsonRecord({
      inputSummary: event.inputSummary,
      constraints: event.constraints,
      promptHash: event.promptHash,
      route: event.route,
      role: event.role
    }),
    actualDecision: event.selectedAction,
    outcome,
    ...(event.failureClass ? { failureClass: event.failureClass } : {}),
    artifactRefs: event.outputArtifactRefs,
    metadata: jsonRecord({
      rationale: event.rationale,
      alternativesConsidered: event.alternativesConsidered,
      modelInvocationIds: event.modelInvocationIds,
      costUsd: event.costUsd,
      totalTokens: event.totalTokens
    })
  };
}

function archivedBranchApiError(branch: WorkflowBranch): WorkflowApiError {
  return {
    ok: false,
    error: "WORKFLOW_BRANCH_ARCHIVED",
    message: `Branch '${branch.name}' is archived and cannot be modified.`
  };
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
        case "adapter.github":
          integrations.add("github");
          break;
        case "adapter.slack":
          integrations.add("slack");
          break;
        case "adapter.discord":
          integrations.add("discord");
          break;
        case "adapter.notion":
          integrations.add("notion");
          break;
        case "adapter.linear":
          integrations.add("linear");
          break;
        case "adapter.jira":
          integrations.add("jira");
          break;
        case "adapter.airtable":
          integrations.add("airtable");
          break;
        case "adapter.webhook":
          integrations.add("webhook");
          break;
        case "adapter.database":
          integrations.add("database");
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

  public isActive(): boolean {
    return this.timer !== undefined;
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
      const retryable = current.retry.attempt < current.retry.maxAttempts;
      const nextRunAt = retryable
        ? new Date(
            Date.now() +
              (current.retry.backoffSeconds ?? Math.min(60, current.retry.attempt * 2)) * 1000
          ).toISOString()
        : undefined;
      const failed = this.store.saveJob({
        ...current,
        status: retryable ? "queued" : "failed",
        updatedAt: finishedAt,
        ...(retryable
          ? { startedAt: undefined, claimedAt: undefined, workerId: undefined }
          : { finishedAt }),
        error: error instanceof Error ? error.message : "Worker job failed.",
        retry: {
          ...current.retry,
          retryable,
          ...(nextRunAt ? { nextRunAt } : {})
        }
      });
      this.store.appendJobEvent(
        failed.id,
        createJobEvent(
          failed,
          "error",
          retryable ? "Worker job scheduled for retry." : "Worker job failed.",
          {
            retryable: failed.retry.retryable,
            ...(failed.retry.nextRunAt ? { nextRunAt: failed.retry.nextRunAt } : {}),
            error: failed.error ?? "Worker job failed."
          }
        )
      );
    } finally {
      this.supervisor.finishJob(job.id);
    }
  }
}

class ApiScheduleWorker {
  private timer: NodeJS.Timeout | undefined;
  private tickInFlight = false;

  public constructor(private readonly store: WorkflowStore) {}

  public start(): void {
    if (this.timer) {
      return;
    }
    this.timer = setInterval(() => this.tick(), 1000);
    this.tick();
  }

  public stop(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  public isActive(): boolean {
    return this.timer !== undefined;
  }

  private tick(): void {
    if (this.tickInFlight) {
      return;
    }
    this.tickInFlight = true;
    try {
      const now = new Date();
      for (const schedule of this.store.listSchedules()) {
        if (schedule.status !== "active" || Date.parse(schedule.nextFireAt) > now.getTime()) {
          continue;
        }
        this.enqueueSchedule(schedule, now);
      }
    } finally {
      this.tickInFlight = false;
    }
  }

  private enqueueSchedule(schedule: WorkflowScheduleRecord, now: Date): void {
    const fireAt = schedule.nextFireAt;
    const runId = `run.${schedule.workflowId}.schedule.${Date.parse(fireAt)}.${randomUUID()}`;
    const job = this.store.saveJob(
      createJob({
        type: "run.workflow",
        workflowId: schedule.workflowId,
        branchId: schedule.branchId,
        revisionId: schedule.approvedRevisionId,
        correlationId: `schedule.${schedule.id}.${Date.parse(fireAt)}`,
        maxAttempts: 3,
        payload: {
          runId,
          approvedRevisionId: schedule.approvedRevisionId,
          scheduleId: schedule.id,
          deploymentId: schedule.deploymentId,
          fireAt
        }
      })
    );
    this.store.saveRun({
      id: runId,
      workflowId: schedule.workflowId,
      branchId: schedule.branchId,
      approvedRevisionId: schedule.approvedRevisionId,
      revision: revisionFromApprovedRevisionId(schedule.approvedRevisionId),
      status: "queued",
      createdAt: now.toISOString(),
      startedAt: now.toISOString(),
      finishedAt: now.toISOString(),
      events: [
        createStructuredRunEvent({
          id: "event.schedule.queued",
          timestamp: now.toISOString(),
          level: "info",
          message: "Scheduled workflow run queued.",
          kind: "schedule.lifecycle",
          workflowId: schedule.workflowId,
          branchId: schedule.branchId,
          revisionId: schedule.approvedRevisionId,
          runId,
          correlationId: job.correlationId,
          metadata: {
            scheduleId: schedule.id,
            fireAt
          }
        })
      ],
      result: null
    });
    this.store.saveSchedule({
      ...schedule,
      updatedAt: now.toISOString(),
      lastFireAt: fireAt,
      lastRunId: runId,
      lastJobId: job.id,
      nextFireAt: nextCronFire(schedule.cron, schedule.timezone, now).toISOString(),
      lastError: undefined
    });
  }
}

class ApiRetentionCleanupWorker {
  private timer: NodeJS.Timeout | undefined;

  public constructor(private readonly store: WorkflowStore) {}

  public start(): void {
    if (this.timer) {
      return;
    }
    this.timer = setInterval(() => this.tick(), 60_000);
    this.tick();
  }

  public stop(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private tick(): void {
    const now = new Date();
    for (const workflowId of collectWorkflowIds(this.store)) {
      const policy = ensureRetentionPolicy(this.store, workflowId);
      this.cleanupRunEvents(workflowId, policy, now);
      this.cleanupWorkspaces(workflowId, policy, now);
    }
  }

  private cleanupRunEvents(workflowId: string, policy: WorkflowRetentionPolicy, now: Date): void {
    const cutoff = now.getTime() - policy.maxRunEventDays * 24 * 60 * 60 * 1000;
    for (const run of this.store.listRuns(workflowId)) {
      const retained = run.events.filter((event) => Date.parse(event.timestamp) >= cutoff);
      if (retained.length === run.events.length) {
        continue;
      }
      const updated = this.store.saveRun({
        ...run,
        events: [
          ...retained,
          createStructuredRunEvent({
            id: `event.retention.${Date.now()}.${run.id}`,
            timestamp: now.toISOString(),
            level: "info",
            message: `Retention cleanup removed ${run.events.length - retained.length} old run event(s).`,
            kind: "retention.lifecycle"
          })
        ]
      });
      recordAudit(this.store, {
        action: "retention.cleaned",
        actor: "retention-worker",
        workflowId,
        branchId: run.branchId,
        revisionId: run.approvedRevisionId,
        runId: run.id,
        correlationId: `retention.${run.id}`,
        summary: "Cleaned old run events by retention policy.",
        metadata: {
          removedEvents: run.events.length - retained.length,
          remainingEvents: updated.events.length
        }
      });
    }
  }

  private cleanupWorkspaces(workflowId: string, policy: WorkflowRetentionPolicy, now: Date): void {
    for (const workspace of this.store.listWorkspaces(workflowId)) {
      if (workspace.retentionStatus !== "active") {
        continue;
      }
      const retentionDays =
        workspace.retentionPolicy === "retain-on-failure"
          ? policy.maxFailedRunWorkspaceDays
          : policy.maxSuccessfulRunWorkspaceDays;
      const cutoff = now.getTime() - retentionDays * 24 * 60 * 60 * 1000;
      if (Date.parse(workspace.updatedAt) >= cutoff) {
        continue;
      }
      const updated = this.store.saveWorkspace({
        ...workspace,
        updatedAt: now.toISOString(),
        retentionStatus: "eligible-for-cleanup"
      });
      recordAudit(this.store, {
        action: "retention.cleaned",
        actor: "retention-worker",
        workflowId,
        branchId: workspace.branchId,
        revisionId: workspace.revisionId ?? `workspace.${workspace.id}`,
        correlationId: `retention.${workspace.id}`,
        summary: "Marked workspace eligible for cleanup by retention policy.",
        metadata: {
          workspaceId: updated.id,
          rootPath: updated.rootPath,
          retentionStatus: updated.retentionStatus
        }
      });
    }
  }
}

function opsHealth(
  store: WorkflowStore,
  jobWorker: ApiJobWorker,
  scheduleWorker: ApiScheduleWorker,
  latestRouterEvalRun?: WorkflowRouterEvalRun | undefined
): WorkflowOpsHealth {
  const checkedAt = new Date().toISOString();
  let databaseWritable = true;
  try {
    store.listJobs();
  } catch {
    databaseWritable = false;
  }
  const jobs = store.listJobs();
  const schedules = store.listSchedules();
  const dueNow = Date.now();
  const runs = collectWorkflowIds(store).flatMap((workflowId) => [...store.listRuns(workflowId)]);
  const runningRuns = runs.filter((run) => run.status === "running").length;
  const resumableRuns = runs.filter((run) => run.status === "resuming").length;
  const failedRuns = runs.filter((run) => run.status === "failed").length;
  const failedJobs = jobs.filter((job) => job.status === "failed").length;
  const failedConnectorTests = store
    .listConnectors()
    .filter((connector) => connector.lastTest.status === "failed").length;
  const allMemories = store.listAgentMemory(undefined, { includeExpired: true });
  const expiredMemories = allMemories.filter(
    (memory) => memory.expiresAt !== undefined && Date.parse(memory.expiresAt) <= Date.now()
  ).length;

  return {
    status: databaseWritable && failedJobs === 0 ? "ok" : "degraded",
    databaseWritable,
    worker: {
      active: jobWorker.isActive(),
      queuedJobs: jobs.filter((job) => job.status === "queued").length,
      runningJobs: jobs.filter((job) => job.status === "running").length,
      failedJobs
    },
    scheduler: {
      active: scheduleWorker.isActive(),
      activeSchedules: schedules.filter((schedule) => schedule.status === "active").length,
      dueSchedules: schedules.filter(
        (schedule) => schedule.status === "active" && Date.parse(schedule.nextFireAt) <= dueNow
      ).length
    },
    runs: {
      running: runningRuns,
      resumable: resumableRuns,
      failed: failedRuns
    },
    connectors: {
      total: store.listConnectors().length,
      failedTests: failedConnectorTests
    },
    memory: {
      total: allMemories.length,
      expired: expiredMemories
    },
    router: {
      classifierVersion: routerClassifierVersion,
      evalCases: routerEvalCases.length,
      ...(latestRouterEvalRun ? { lastEvalPassed: latestRouterEvalRun.passed } : {})
    },
    checkedAt
  };
}

function recoverInterruptedJobs(store: WorkflowStore): void {
  const now = new Date().toISOString();
  for (const job of store.listJobs()) {
    if (job.status !== "running") {
      continue;
    }
    const retryable = job.retry.attempt < job.retry.maxAttempts;
    const nextRunAt = retryable
      ? new Date(Date.now() + (job.retry.backoffSeconds ?? 2) * 1000).toISOString()
      : undefined;
    const failed = store.saveJob({
      ...job,
      status: retryable ? "queued" : "failed",
      updatedAt: now,
      ...(retryable ? { claimedAt: undefined, workerId: undefined } : { finishedAt: now }),
      error: "API restarted before this running job completed.",
      retry: {
        ...job.retry,
        retryable,
        ...(nextRunAt ? { nextRunAt } : {})
      }
    });
    store.appendJobEvent(
      failed.id,
      createJobEvent(failed, "error", "Running job was interrupted by API restart.", {
        retryable,
        ...(nextRunAt ? { nextRunAt } : {})
      })
    );
  }
}

function recoverInterruptedRuns(store: WorkflowStore): void {
  const now = new Date().toISOString();
  for (const workflow of collectWorkflowIds(store)) {
    for (const run of store.listRuns(workflow)) {
      if (run.status !== "running" && run.status !== "resuming") {
        continue;
      }
      store.saveRun({
        ...run,
        status: "resuming",
        finishedAt: now,
        events: [
          ...run.events,
          createStructuredRunEvent({
            id: `event.run.recovered.${Date.now()}`,
            timestamp: now,
            level: "warn",
            message: "Run was marked resumable after API restart.",
            kind: "run.lifecycle"
          })
        ]
      });
    }
  }
}

function collectWorkflowIds(store: WorkflowStore): readonly string[] {
  return [
    ...new Set([
      ...store.listJobs().flatMap((job) => (job.workflowId ? [job.workflowId] : [])),
      ...store.listSchedules().map((schedule) => schedule.workflowId)
    ])
  ].sort();
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

function stringFromJson(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
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

function recordPlannerNodeDecisionTraces(
  store: WorkflowStore,
  input: {
    readonly workflow: WorkflowSpec;
    readonly revisionId: string;
    readonly branchId?: string | undefined;
    readonly correlationId: string;
    readonly prompt: string;
    readonly source: "plan" | "reprompt";
    readonly route?: WorkflowTaskRoute | undefined;
    readonly changedNodeIds?: readonly string[] | undefined;
    readonly validationIssues?: readonly WorkflowValidationIssue[] | undefined;
  }
): readonly WorkflowNodeDecisionTrace[] {
  const now = new Date().toISOString();
  const prompt = input.prompt.trim() || input.workflow.prompt;
  const promptHash = sha256String(prompt);
  const promptExcerpt = redactSecretString(prompt).slice(0, 500) || "No prompt text captured.";
  const changedNodeIds = new Set(
    input.changedNodeIds ?? input.workflow.nodes.map((node) => node.id)
  );
  const traces: WorkflowNodeDecisionTrace[] = [];

  for (const node of input.workflow.nodes.filter((candidate) => changedNodeIds.has(candidate.id))) {
    const kind: WorkflowDecisionTraceKind =
      input.source === "reprompt" ? "planner.node-updated" : "planner.node-created";
    const traceId = `trace.${input.workflow.id}.${node.id}.${Date.now()}.${randomUUID()}`;
    const affectedEdges = input.workflow.edges
      .filter((edge) => edge.source.nodeId === node.id || edge.target.nodeId === node.id)
      .map((edge) => edge.id);
    const rationale =
      node.codegen?.plannerRationale ??
      node.description ??
      input.route?.rationale ??
      "Planner selected this node to satisfy the workflow prompt.";
    const event: WorkflowNodeDecisionTraceEvent = {
      id: `${traceId}.event.${randomUUID()}`,
      traceId,
      workflowId: input.workflow.id,
      ...(input.branchId ? { branchId: input.branchId } : {}),
      nodeId: node.id,
      revisionId: input.revisionId,
      kind,
      role: "planner",
      createdAt: now,
      summary: `${input.source === "reprompt" ? "Updated" : "Created"} ${node.kind} node '${node.label}'.`,
      rationale: redactSecretString(rationale),
      alternativesConsidered: plannerAlternativesForNode(node, input.route),
      selectedAction: `Use ${node.kind} node '${node.label}'.`,
      inputSummary: promptExcerpt,
      promptHash,
      promptExcerpt,
      ...(input.route?.route ? { route: input.route.route } : {}),
      ...(input.route?.requiredModel.provider
        ? { provider: input.route.requiredModel.provider }
        : {}),
      ...(input.route?.requiredModel.model ? { model: input.route.requiredModel.model } : {}),
      modelInvocationIds: input.route?.modelInvocations.map((invocation) => invocation.id) ?? [],
      affectedNodeIds: [node.id],
      affectedEdgeIds: affectedEdges,
      constraints: jsonRecord({
        nodeKind: node.kind,
        runtime: node.runtime,
        determinism: node.determinism,
        inputPorts: Object.keys(node.inputs),
        outputPorts: Object.keys(node.outputs),
        skillId: node.skillId,
        adapterIds: node.adapterIds ?? (node.adapterId ? [node.adapterId] : []),
        sandbox: node.codegen?.sandbox,
        approvalBoundaries: node.agentic?.humanApprovalBoundaries ?? [],
        validationIssues: (input.validationIssues ?? []).map((issue) => ({
          code: issue.code,
          message: issue.message,
          path: issue.path
        }))
      }),
      outputArtifactRefs: node.codegen?.artifacts ?? [],
      evalOutcome: "not-run",
      metadata: jsonRecord({
        correlationId: input.correlationId,
        routeRationale: input.route?.rationale,
        expectedNodeKinds: input.route?.expectedNodeKinds,
        productionDeterministic: input.route?.productionDeterministic
      })
    };
    const trace = store.saveNodeDecisionTrace({
      id: traceId,
      workflowId: input.workflow.id,
      ...(input.branchId ? { branchId: input.branchId } : {}),
      nodeId: node.id,
      revisionId: input.revisionId,
      kind,
      source: "planner",
      createdAt: now,
      updatedAt: now,
      status: "recorded",
      events: [event]
    });
    traces.push(trace);
    recordAudit(store, {
      action: "decision.trace.recorded",
      actor: "planner",
      workflowId: input.workflow.id,
      branchId: input.branchId,
      revisionId: input.revisionId,
      nodeId: node.id,
      correlationId: input.correlationId,
      summary: `Recorded planner decision trace for node '${node.id}'.`,
      metadata: {
        traceId,
        kind
      }
    });
  }

  return traces;
}

function recordCodegenNodeDecisionTraces(
  store: WorkflowStore,
  input: {
    readonly workflowId: string;
    readonly branchId?: string | undefined;
    readonly nodeId: string;
    readonly revisionId: string;
    readonly jobId: string;
    readonly correlationId: string;
    readonly agentRuns: readonly CodegenAgentRunRecord[];
    readonly testReport?: GeneratedNodeTestReport | undefined;
    readonly evalReport?: GeneratedNodeEvalReport | undefined;
  }
): readonly WorkflowNodeDecisionTrace[] {
  const traces: WorkflowNodeDecisionTrace[] = [];
  let cumulativeCostUsd = 0;
  for (const run of input.agentRuns) {
    const now = run.finishedAt || new Date().toISOString();
    const kind = codegenDecisionKindForRole(run.role);
    const traceId = `trace.${input.workflowId}.${input.nodeId}.${run.role}.${Date.now()}.${randomUUID()}`;
    const costUsd = agentRunInvocationCostUsd(run);
    cumulativeCostUsd += costUsd;
    const event: WorkflowNodeDecisionTraceEvent = {
      id: `${traceId}.event.${randomUUID()}`,
      traceId,
      workflowId: input.workflowId,
      ...(input.branchId ? { branchId: input.branchId } : {}),
      nodeId: input.nodeId,
      revisionId: input.revisionId,
      jobId: input.jobId,
      agentRunId: run.id,
      kind,
      role: run.role,
      createdAt: now,
      summary: run.error ?? run.inputSummary,
      rationale: run.error ?? timelineDecisionForRun(run),
      alternativesConsidered: codegenAlternativesForRun(run),
      selectedAction: codegenSelectedAction(run),
      inputSummary: run.inputSummary,
      provider: run.modelProvider,
      model: run.model,
      modelInvocationIds: run.modelInvocations?.map((invocation) => invocation.id) ?? [],
      affectedNodeIds: [input.nodeId],
      affectedEdgeIds: [],
      constraints: jsonRecord({
        role: run.role,
        status: run.status,
        testReportId: input.testReport?.id,
        evalReportId: input.evalReport?.id,
        schemaValid: input.evalReport?.schemaValid,
        securityValid: input.evalReport?.securityValid,
        replayValid: input.evalReport?.replayValid,
        dependencyPolicyValid: input.evalReport?.dependencyPolicyValid
      }),
      outputArtifactRefs: run.outputArtifactRefs,
      ...(input.evalReport ? { evalOutcome: input.evalReport.status } : { evalOutcome: "not-run" }),
      ...(run.error ? { failureClass: failureClassFromMessage(run.error) } : {}),
      ...(fixTriageActionFromSummary(run.inputSummary)
        ? { fixTriageAction: fixTriageActionFromSummary(run.inputSummary) }
        : {}),
      ...(run.inputTokens !== undefined ? { inputTokens: run.inputTokens } : {}),
      ...(run.outputTokens !== undefined ? { outputTokens: run.outputTokens } : {}),
      ...(run.totalTokens !== undefined ? { totalTokens: run.totalTokens } : {}),
      ...(costUsd > 0 ? { costUsd } : {}),
      metadata: jsonRecord({
        correlationId: input.correlationId,
        cumulativeCostUsd,
        failureFindings: input.evalReport?.findings.map((finding) => finding.message) ?? [],
        fixHistory: input.evalReport?.fixHistory ?? []
      })
    };
    const trace = store.saveNodeDecisionTrace({
      id: traceId,
      workflowId: input.workflowId,
      ...(input.branchId ? { branchId: input.branchId } : {}),
      nodeId: input.nodeId,
      revisionId: input.revisionId,
      kind,
      source: "codegen",
      createdAt: run.startedAt,
      updatedAt: now,
      status: run.status === "succeeded" ? "succeeded" : "failed",
      events: [event]
    });
    traces.push(trace);
    recordAudit(store, {
      action: "decision.trace.recorded",
      actor: run.role,
      workflowId: input.workflowId,
      branchId: input.branchId,
      revisionId: input.revisionId,
      nodeId: input.nodeId,
      correlationId: input.correlationId,
      summary: `Recorded ${run.role} decision trace for node '${input.nodeId}'.`,
      metadata: {
        traceId,
        kind,
        agentRunId: run.id,
        modelInvocationIds: [...event.modelInvocationIds]
      }
    });
  }

  return traces;
}

function recordRuntimeNodeDecisionTraces(
  store: WorkflowStore,
  workflow: WorkflowSpec,
  approvedRevisionId: string,
  run: WorkflowRunRecord,
  correlationId: string
): readonly WorkflowNodeDecisionTrace[] {
  const traces: WorkflowNodeDecisionTrace[] = [];
  for (const nodeResult of run.result?.nodeResults ?? []) {
    const runtimeEvents = jsonArrayMetadata(nodeResult.metadata?.runtimeDecisionTraceEvents);
    if (runtimeEvents.length === 0) {
      continue;
    }
    const node = workflow.nodes.find((candidate) => candidate.id === nodeResult.nodeId);
    const now = new Date().toISOString();
    const traceId = `trace.${workflow.id}.${nodeResult.nodeId}.runtime.${Date.now()}.${randomUUID()}`;
    const role: WorkflowAgentRole = node?.agentic ? "agentic-node-designer" : "runner";
    const events: WorkflowNodeDecisionTraceEvent[] = runtimeEvents.map((runtimeEvent) => ({
      id: `${traceId}.event.${randomUUID()}`,
      traceId,
      workflowId: workflow.id,
      nodeId: nodeResult.nodeId,
      revisionId: approvedRevisionId,
      jobId: run.id,
      kind: runtimeTraceKindFromJson(runtimeEvent.kind),
      role,
      createdAt: now,
      summary: stringFromJson(runtimeEvent.summary) ?? "Runtime decision recorded.",
      rationale: stringFromJson(runtimeEvent.rationale) ?? "Runtime policy decision recorded.",
      alternativesConsidered: [],
      selectedAction: stringFromJson(runtimeEvent.selectedAction) ?? "record runtime decision",
      inputSummary: nodeResult.input ? stableJsonStringify(nodeResult.input).slice(0, 500) : "{}",
      modelInvocationIds: [],
      affectedNodeIds: [nodeResult.nodeId],
      affectedEdgeIds: workflow.edges
        .filter(
          (edge) =>
            edge.source.nodeId === nodeResult.nodeId || edge.target.nodeId === nodeResult.nodeId
        )
        .map((edge) => edge.id),
      constraints: jsonRecord({
        runId: run.id,
        status: nodeResult.status,
        metadata: nodeResult.metadata ?? {}
      }),
      outputArtifactRefs: [],
      evalOutcome: nodeResult.status === "failed" ? ("failed" as const) : ("passed" as const),
      metadata: jsonRecord({
        correlationId
      })
    }));
    const trace = store.saveNodeDecisionTrace({
      id: traceId,
      workflowId: workflow.id,
      nodeId: nodeResult.nodeId,
      revisionId: approvedRevisionId,
      kind: events[0]?.kind ?? "runtime.agent-policy",
      source: "runtime",
      createdAt: now,
      updatedAt: now,
      status: nodeResult.status === "failed" ? "failed" : "recorded",
      events
    });
    traces.push(trace);
  }

  return traces;
}

function runtimeTraceKindFromJson(value: unknown): WorkflowDecisionTraceKind {
  return value === "runtime.router-classification" ||
    value === "runtime.agent-policy" ||
    value === "runtime.tool-call" ||
    value === "runtime.memory-read" ||
    value === "runtime.memory-write"
    ? value
    : "runtime.agent-policy";
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
  if (event.level === "error") {
    return "error";
  }
  if (event.level === "warn") {
    return "warn";
  }
  return "info";
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
