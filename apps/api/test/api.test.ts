import { afterEach, describe, expect, it } from "vitest";
import type { FastifyInstance } from "fastify";
import { staticContentWorkflowFixture } from "@kelpclaw/workflow-spec";
import { buildApiApp } from "../src/index.js";

let app: FastifyInstance | undefined;

afterEach(async () => {
  await app?.close();
  app = undefined;
});

describe("kelpclaw api contracts", () => {
  it("reports health", async () => {
    app = buildApiApp();

    const response = await app.inject({ method: "GET", url: "/health" });

    expect(response.statusCode).toBe(200);
    expect(response.json()).toEqual({ status: "ok", service: "kelpclaw-api" });
  });

  it("returns a mock planner workflow", async () => {
    app = buildApiApp();

    const response = await app.inject({
      method: "POST",
      url: "/api/plans/mock",
      payload: { name: "Launch Review" }
    });

    expect(response.statusCode).toBe(200);
    expect(response.json().workflow.metadata.name).toBe("Launch Review");
  });

  it("validates invalid workflows with stable errors", async () => {
    app = buildApiApp();

    const response = await app.inject({
      method: "POST",
      url: "/api/workflows/validate",
      payload: { metadata: { id: "bad" }, nodes: [], edges: [] }
    });

    expect(response.statusCode).toBe(200);
    expect(response.json().ok).toBe(false);
    expect(response.json().errors[0].code).toBe("WORKFLOW_SCHEMA_INVALID");
  });

  it("creates, approves, executes, and fetches workflows", async () => {
    app = buildApiApp();

    const created = await app.inject({
      method: "POST",
      url: "/api/workflows",
      payload: staticContentWorkflowFixture
    });
    expect(created.statusCode).toBe(201);

    const blocked = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.static-content/executions"
    });
    expect(blocked.statusCode).toBe(409);

    const approval = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.static-content/approvals",
      payload: {
        approvalId: "approval.owner-approval",
        decision: "approved",
        actorRole: "owner"
      }
    });
    expect(approval.statusCode).toBe(200);
    expect(approval.json().approvals["approval.owner-approval"]).toBe("approved");

    const execution = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.static-content/executions"
    });
    expect(execution.statusCode).toBe(202);
    expect(execution.json().result.status).toBe("succeeded");

    const fetched = await app.inject({
      method: "GET",
      url: `/api/executions/${execution.json().id}`
    });
    expect(fetched.statusCode).toBe(200);
    expect(fetched.json().workflowId).toBe("workflow.static-content");
  });
});
