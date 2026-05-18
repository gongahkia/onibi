import Fastify from "fastify";
import { MockNodeRunner, compileWorkflowDag, executeCompiledDag } from "@kelpclaw/nanoclaw";
import {
  WorkflowValidationError,
  gmailReceiptsToSheetsWorkflowFixture,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type { FastifyInstance } from "fastify";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";
import { InMemoryWorkflowStore } from "./store.js";
import type { RevisionInput } from "./store.js";

interface RouteParamsWithId {
  readonly id: string;
}

interface ApprovalRequestBody {
  readonly approvedBy: string;
}

interface MockPlanRequestBody {
  readonly name?: string;
}

export interface ApiAppOptions {
  readonly store?: InMemoryWorkflowStore | undefined;
}

export function buildApiApp(options: ApiAppOptions = {}): FastifyInstance {
  const app = Fastify({
    logger: false
  });
  const store = options.store ?? new InMemoryWorkflowStore();

  app.get("/health", async () => ({
    status: "ok",
    service: "kelpclaw-api"
  }));

  app.post<{ Body: MockPlanRequestBody }>("/api/plans/mock", async (request) => {
    const name = request.body?.name ?? gmailReceiptsToSheetsWorkflowFixture.name;

    return {
      workflow: {
        ...gmailReceiptsToSheetsWorkflowFixture,
        name
      }
    };
  });

  app.post("/api/workflows/validate", async (request) => validateWorkflowSpec(request.body));

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

  app.post<{ Params: RouteParamsWithId; Body: ApprovalRequestBody }>(
    "/api/workflows/:id/approvals",
    async (request, reply) => {
      try {
        const updated = store.approveWorkflow(request.params.id, request.body.approvedBy);
        return {
          workflowId: updated.workflow.id,
          revision: updated.workflow.revision,
          approval: updated.workflow.approval,
          workflow: updated.workflow
        };
      } catch (error) {
        return reply.code(404).send({
          error: "WORKFLOW_NOT_FOUND",
          message: error instanceof Error ? error.message : "Workflow was not found."
        });
      }
    }
  );

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

      try {
        const dag = compileWorkflowDag(stored.workflow);
        const result = await executeCompiledDag(dag, new MockNodeRunner());
        const execution = store.saveExecution({
          id: result.id,
          workflowId: stored.workflow.id,
          revision: stored.workflow.revision,
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
