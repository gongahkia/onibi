import Fastify from "fastify";
import { compileWorkflowDag, executeCompiledDag, MockNodeRunner } from "@kelpclaw/nanoclaw";
import { staticContentWorkflowFixture, validateWorkflowSpec } from "@kelpclaw/workflow-spec";
import type { FastifyInstance } from "fastify";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";
import { InMemoryWorkflowStore } from "./store.js";

interface RouteParamsWithId {
  readonly id: string;
}

interface ApprovalRequestBody {
  readonly approvalId: string;
  readonly decision: "approved" | "rejected";
  readonly actorRole: "operator" | "owner";
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
    const name = request.body?.name ?? staticContentWorkflowFixture.metadata.name;

    return {
      workflow: {
        ...staticContentWorkflowFixture,
        metadata: {
          ...staticContentWorkflowFixture.metadata,
          name
        }
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
      workflow: stored.workflow,
      approvals: stored.approvals
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
        const updated = store.setApproval(
          request.params.id,
          request.body.approvalId,
          request.body.decision
        );
        return {
          workflowId: updated.workflow.metadata.id,
          approvals: updated.approvals
        };
      } catch (error) {
        return reply.code(404).send({
          error: "APPROVAL_NOT_FOUND",
          message: error instanceof Error ? error.message : "Approval gate was not found."
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

      if (!store.approvalsSatisfied(request.params.id)) {
        return reply.code(409).send({ error: "WORKFLOW_APPROVAL_REQUIRED" });
      }

      const dag = compileWorkflowDag(stored.workflow);
      const result = await executeCompiledDag(dag, new MockNodeRunner());
      const execution = store.saveExecution({
        id: `execution.${stored.workflow.metadata.id}.${Date.now()}`,
        workflowId: stored.workflow.metadata.id,
        createdAt: new Date().toISOString(),
        result
      });

      return reply.code(202).send(execution);
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
