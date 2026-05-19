import Fastify from "fastify";
import { LocalCodegenArtifactStore } from "@kelpclaw/codegen";
import {
  DockerNodeRunner,
  MockNodeRunner,
  compileWorkflowDag,
  executeCompiledDag
} from "@kelpclaw/nanoclaw";
import {
  WorkflowValidationError,
  gmailReceiptsToSheetsWorkflowFixture,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type { FastifyInstance } from "fastify";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type {
  WorkflowApproveRequest,
  WorkflowApproveResponse,
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
  createLivePlannerBackend,
  planMockWorkflowDraft,
  planWorkflowDraft,
  repromptWorkflow
} from "./planner.js";
import { InMemoryWorkflowStore } from "./store.js";
import type { RevisionInput } from "./store.js";
import type { WorkflowPlannerBackend } from "./planner.js";

interface RouteParamsWithId {
  readonly id: string;
}

interface RunRouteParams {
  readonly id: string;
  readonly runId: string;
}

interface ApprovalRequestBody {
  readonly approvedBy: string;
}

interface MockPlanRequestBody {
  readonly name?: string;
}

export interface ApiAppOptions {
  readonly store?: InMemoryWorkflowStore | undefined;
  readonly planner?: WorkflowPlannerBackend | undefined;
  readonly artifactStore?: CodegenArtifactStore | undefined;
}

export function buildApiApp(options: ApiAppOptions = {}): FastifyInstance {
  const app = Fastify({
    logger: false
  });
  const store = options.store ?? new InMemoryWorkflowStore();
  const artifactStore = options.artifactStore ?? new LocalCodegenArtifactStore();
  const planner = options.planner ?? createLivePlannerBackend({ artifactStore });

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
          message: error instanceof Error ? error.message : "Planner backend is unavailable."
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
      const dag = compileWorkflowDag(approvedRevision.workflow);
      const result = await executeCompiledDag(dag, createNanoClawRunner(), {
        codegenArtifactStore: artifactStore
      });
      const now = new Date().toISOString();
      const events = result.events ?? createRunEvents(result.nodeResults, now);
      const run = store.saveRun({
        id: `run.${approvedRevision.workflowId}.r${approvedRevision.revision}.${Date.now()}`,
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

  app.post<{ Params: RouteParamsWithId; Body: RevisionInput }>(
    "/api/workflows/:id/revisions",
    async (request, reply) => {
      try {
        const updated = store.createRevision(request.params.id, request.body);
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

function createNanoClawRunner(): MockNodeRunner | DockerNodeRunner {
  if (process.env.NANOCLAW_RUNNER === "docker") {
    return new DockerNodeRunner({
      dockerBin: process.env.NANOCLAW_DOCKER_BIN,
      hostWorkspace: process.env.NANOCLAW_HOST_WORKSPACE ?? process.cwd()
    });
  }

  return new MockNodeRunner();
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
