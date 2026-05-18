import { afterEach, describe, expect, it } from "vitest";
import type { FastifyInstance } from "fastify";
import { gmailReceiptsToSheetsWorkflowFixture } from "@kelpclaw/workflow-spec";
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
    expect(response.json().workflow.prompt).toBe("Launch Review");
    expect(response.json().workflow.schemaVersion).toBe("1.0.0");
  });

  it("plans and validates draft workflow revisions through the Phase 3 routes", async () => {
    app = buildApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    expect(planned.statusCode).toBe(200);
    expect(planned.json().ok).toBe(true);
    expect(planned.json().draftRevision.source).toBe("plan");
    expect(planned.json().workflow.nodes.map((node: { id: string }) => node.id)).toContain(
      "read-gmail-receipts"
    );

    const workflow = {
      ...planned.json().workflow,
      nodes: planned
        .json()
        .workflow.nodes.map((node: { id: string; label: string }) =>
          node.id === "normalize-receipts" ? { ...node, label: "Normalize Receipt Rows" } : node
        )
    };
    const validated = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/validate`,
      payload: { workflow }
    });
    expect(validated.statusCode).toBe(200);
    expect(validated.json().ok).toBe(true);
    expect(validated.json().draftRevision.source).toBe("validate");
    expect(validated.json().draftRevision.revision).toBe(2);

    const validatedAgain = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/validate`,
      payload: { workflow: validated.json().workflow }
    });
    expect(validatedAgain.statusCode).toBe(200);
    expect(validatedAgain.json().draftRevision.id).toBe(validated.json().draftRevision.id);
  });

  it("validates invalid workflows with stable errors", async () => {
    app = buildApiApp();

    const response = await app.inject({
      method: "POST",
      url: "/api/workflows/validate",
      payload: { id: "bad", schemaVersion: "1.0.0", nodes: [], edges: [] }
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
      payload: gmailReceiptsToSheetsWorkflowFixture
    });
    expect(created.statusCode).toBe(201);
    expect(created.json().workflow.approval).toBeNull();

    const blocked = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/executions"
    });
    expect(blocked.statusCode).toBe(409);

    const approval = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/approvals",
      payload: {
        approvedBy: "owner@example.com"
      }
    });
    expect(approval.statusCode).toBe(200);
    expect(approval.json().approval.status).toBe("approved");
    expect(approval.json().approval.nodeOrder).toEqual([
      "manual-trigger",
      "read-gmail-receipts",
      "normalize-receipts",
      "append-sheet-rows"
    ]);

    const execution = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/executions"
    });
    expect(execution.statusCode).toBe(202);
    expect(execution.json().result.status).toBe("succeeded");
    expect(execution.json().result.revision).toBe(1);

    const fetched = await app.inject({
      method: "GET",
      url: `/api/executions/${execution.json().id}`
    });
    expect(fetched.statusCode).toBe(200);
    expect(fetched.json().workflowId).toBe("workflow.gmail-receipts-to-sheets");
  });

  it("creates a new draft revision after approval", async () => {
    app = buildApiApp();

    await app.inject({
      method: "POST",
      url: "/api/workflows",
      payload: gmailReceiptsToSheetsWorkflowFixture
    });
    await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/approvals",
      payload: { approvedBy: "owner@example.com" }
    });

    const revision = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/revisions",
      payload: {
        name: "Updated Receipt Sync",
        prompt: "Track receipts and include tax totals."
      }
    });

    expect(revision.statusCode).toBe(201);
    expect(revision.json().workflow.revision).toBe(2);
    expect(revision.json().workflow.approval).toBeNull();
    expect(revision.json().workflow.name).toBe("Updated Receipt Sync");
  });
});
