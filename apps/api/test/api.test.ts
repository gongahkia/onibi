import { afterEach, describe, expect, it, vi } from "vitest";
import type { FastifyInstance } from "fastify";
import { createDefaultMockAdapters } from "@kelpclaw/adapters";
import { AdapterBackedNodeRunner, MockNodeRunner } from "@kelpclaw/nanoclaw";
import { clearPromotedSkillsForTests } from "@kelpclaw/skill-registry";
import { gmailReceiptsToSheetsWorkflowFixture } from "@kelpclaw/workflow-spec";
import type { WorkflowJob } from "@kelpclaw/workflow-spec";
import {
  buildApiApp,
  createDeterministicPlannerBackend,
  createPlannerBackendFromEnv,
  InMemorySecretStore
} from "../src/index.js";

let app: FastifyInstance | undefined;

afterEach(async () => {
  await app?.close();
  app = undefined;
  clearPromotedSkillsForTests();
  vi.unstubAllGlobals();
});

function buildTestApiApp(): FastifyInstance {
  const secretStore = new InMemorySecretStore();
  secretStore.putSecret("google.oauth.default", "test-google-token");
  secretStore.putSecret("email.smtp.default", "test-email-secret");
  secretStore.putSecret("whatsapp.cloud.default", "test-whatsapp-token");
  secretStore.putSecret("telegram.bot.default", "test-telegram-token");
  return buildApiApp({
    planner: createDeterministicPlannerBackend(),
    secretStore,
    runner: new AdapterBackedNodeRunner({
      adapters: createDefaultMockAdapters(),
      fallbackRunner: new MockNodeRunner()
    })
  });
}

describe("kelpclaw api contracts", () => {
  it("reports health", async () => {
    app = buildApiApp();

    const response = await app.inject({ method: "GET", url: "/health" });

    expect(response.statusCode).toBe(200);
    expect(response.json()).toEqual({ status: "ok", service: "kelpclaw-api" });
  });

  it("returns a mock planner workflow", async () => {
    app = buildTestApiApp();

    const response = await app.inject({
      method: "POST",
      url: "/api/plans/mock",
      payload: { name: "Launch Review" }
    });

    expect(response.statusCode).toBe(200);
    expect(response.json().workflow.prompt).toBe("Launch Review");
    expect(response.json().workflow.schemaVersion).toBe("1.0.0");
  });

  it("requires admin bearer auth when configured", async () => {
    app = buildApiApp({
      adminToken: "test-admin-token",
      planner: createDeterministicPlannerBackend()
    });

    const unauthorized = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: { prompt: "extract transaction details from Gmail receipts into Sheets" }
    });
    const authorized = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      headers: { authorization: "Bearer test-admin-token" },
      payload: { prompt: "extract transaction details from Gmail receipts into Sheets" }
    });

    expect(unauthorized.statusCode).toBe(401);
    expect(authorized.statusCode).toBe(200);
  });

  it("stores secret metadata without returning raw values", async () => {
    const secretStore = new InMemorySecretStore();
    app = buildApiApp({
      adminToken: "test-admin-token",
      planner: createDeterministicPlannerBackend(),
      secretStore
    });

    const stored = await app.inject({
      method: "PUT",
      url: "/api/secrets",
      headers: { authorization: "Bearer test-admin-token" },
      payload: { name: "email.smtp.default", value: "smtp-secret" }
    });
    const listed = await app.inject({
      method: "GET",
      url: "/api/secrets",
      headers: { authorization: "Bearer test-admin-token" }
    });

    expect(stored.statusCode).toBe(200);
    expect(stored.json().secret).toMatchObject({ name: "email.smtp.default" });
    expect(listed.json().secrets).toEqual([
      expect.objectContaining({ name: "email.smtp.default" })
    ]);
    expect(JSON.stringify(listed.json())).not.toContain("smtp-secret");
  });

  it("creates and consumes a Google OAuth callback state", async () => {
    const previous = {
      publicBaseUrl: process.env.KELPCLAW_PUBLIC_BASE_URL,
      clientId: process.env.GOOGLE_CLIENT_ID,
      clientSecret: process.env.GOOGLE_CLIENT_SECRET,
      tokenUrl: process.env.GOOGLE_TOKEN_URL
    };
    process.env.KELPCLAW_PUBLIC_BASE_URL = "http://127.0.0.1:8787";
    process.env.GOOGLE_CLIENT_ID = "google-client";
    process.env.GOOGLE_CLIENT_SECRET = "google-secret";
    process.env.GOOGLE_TOKEN_URL = "https://oauth.test/token";
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ refresh_token: "refresh-token" }), {
            status: 200,
            headers: { "content-type": "application/json" }
          })
      )
    );
    const secretStore = new InMemorySecretStore();
    app = buildApiApp({
      adminToken: "test-admin-token",
      planner: createDeterministicPlannerBackend(),
      secretStore
    });

    try {
      const connect = await app.inject({
        method: "GET",
        url: "/api/integrations/google/connect",
        headers: { authorization: "Bearer test-admin-token" }
      });
      const callback = await app.inject({
        method: "GET",
        url: `/api/integrations/google/callback?code=auth-code&state=${connect.json().state}`
      });

      expect(connect.statusCode).toBe(200);
      expect(connect.json().url).toContain("accounts.google.com");
      expect(callback.statusCode).toBe(200);
      await expect(secretStore.getSecretValue("google.oauth.default")).resolves.toContain(
        "refresh-token"
      );
    } finally {
      restoreEnv("KELPCLAW_PUBLIC_BASE_URL", previous.publicBaseUrl);
      restoreEnv("GOOGLE_CLIENT_ID", previous.clientId);
      restoreEnv("GOOGLE_CLIENT_SECRET", previous.clientSecret);
      restoreEnv("GOOGLE_TOKEN_URL", previous.tokenUrl);
    }
  });

  it("configures deterministic planner mode from environment", async () => {
    const previousMode = process.env.KELPCLAW_PLANNER_MODE;
    process.env.KELPCLAW_PLANNER_MODE = "deterministic";
    app = buildApiApp();

    try {
      const response = await app.inject({
        method: "POST",
        url: "/api/workflows/plan",
        payload: {
          prompt: "scrape a custom public status page and summarize incidents"
        }
      });

      expect(response.statusCode).toBe(200);
      expect(
        response.json().workflow.nodes.some((node: { kind: string }) => node.kind === "codegen")
      ).toBe(true);
    } finally {
      if (previousMode === undefined) {
        delete process.env.KELPCLAW_PLANNER_MODE;
      } else {
        process.env.KELPCLAW_PLANNER_MODE = previousMode;
      }
    }
  });

  it("rejects unsupported planner provider configuration", () => {
    const previousMode = process.env.KELPCLAW_PLANNER_MODE;
    const previousProvider = process.env.KELPCLAW_PLANNER_PROVIDER;
    process.env.KELPCLAW_PLANNER_MODE = "live";
    process.env.KELPCLAW_PLANNER_PROVIDER = "unsupported";

    try {
      expect(() => createPlannerBackendFromEnv()).toThrow("KELPCLAW_PLANNER_PROVIDER");
    } finally {
      if (previousMode === undefined) {
        delete process.env.KELPCLAW_PLANNER_MODE;
      } else {
        process.env.KELPCLAW_PLANNER_MODE = previousMode;
      }
      if (previousProvider === undefined) {
        delete process.env.KELPCLAW_PLANNER_PROVIDER;
      } else {
        process.env.KELPCLAW_PLANNER_PROVIDER = previousProvider;
      }
    }
  });

  it("accepts OpenAI as a live planner provider", () => {
    const previousMode = process.env.KELPCLAW_PLANNER_MODE;
    const previousProvider = process.env.KELPCLAW_PLANNER_PROVIDER;
    process.env.KELPCLAW_PLANNER_MODE = "live";
    process.env.KELPCLAW_PLANNER_PROVIDER = "openai";

    try {
      expect(() => createPlannerBackendFromEnv()).not.toThrow();
    } finally {
      if (previousMode === undefined) {
        delete process.env.KELPCLAW_PLANNER_MODE;
      } else {
        process.env.KELPCLAW_PLANNER_MODE = previousMode;
      }
      if (previousProvider === undefined) {
        delete process.env.KELPCLAW_PLANNER_PROVIDER;
      } else {
        process.env.KELPCLAW_PLANNER_PROVIDER = previousProvider;
      }
    }
  });

  it("plans and validates draft workflow revisions through the Phase 3 routes", async () => {
    app = buildTestApiApp();

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

  it("routes adapter prompts without a live model", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });

    expect(planned.statusCode).toBe(200);
    expect(planned.json().route.route).toBe("adapter");
    expect(planned.json().route.requiredModel.mode).toBe("none");
  });

  it("asks clarifying questions before planning vague prompts", async () => {
    app = buildTestApiApp();

    const clarification = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "i want to have someone research this tasking for me"
      }
    });

    expect(clarification.statusCode).toBe(200);
    expect(clarification.json()).toMatchObject({
      ok: true,
      status: "clarification-required",
      route: { route: "agentic" }
    });
    expect(
      clarification
        .json()
        .clarification.questions.map((question: { readonly id: string }) => question.id)
    ).toContain("research-topic");
  });

  it("routes clarified research prompts to an agentic graph instead of the Gmail demo template", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "i want to have someone research this tasking for me",
        clarificationRequestId: "clarify.test",
        clarificationAnswers: [
          {
            questionId: "research-topic",
            answer: "OpenAI web search support for production workflow research agents"
          },
          {
            questionId: "desired-output",
            answer: "A concise sourced recommendation with limitations and next steps"
          }
        ]
      }
    });

    expect(planned.statusCode).toBe(200);
    expect(planned.json().route.route).toBe("agentic");
    expect(planned.json().workflow.nodes.map((node: { id: string }) => node.id)).toEqual([
      "manual-research-request",
      "research-task",
      "approve-research-summary",
      "deliver-research-summary"
    ]);
    expect(planned.json().workflow.nodes.map((node: { id: string }) => node.id)).not.toContain(
      "read-gmail-receipts"
    );
    expect(planned.json().workflow.nodes[1].agentic.tools).toContain("web-search");
  });

  it("routes codegen prompts through OpenAI when selected", async () => {
    const previousProvider = process.env.KELPCLAW_PLANNER_PROVIDER;
    process.env.KELPCLAW_PLANNER_PROVIDER = "openai";
    app = buildTestApiApp();

    try {
      const planned = await app.inject({
        method: "POST",
        url: "/api/workflows/plan",
        payload: {
          prompt: "scrape a custom public status page and summarize incidents"
        }
      });

      expect(planned.statusCode).toBe(200);
      expect(planned.json().route.route).toBe("codegen");
      expect(planned.json().route.requiredModel.provider).toBe("openai");
      expect(planned.json().route.modelInvocations[0].provider).toBe("openai");
    } finally {
      if (previousProvider === undefined) {
        delete process.env.KELPCLAW_PLANNER_PROVIDER;
      } else {
        process.env.KELPCLAW_PLANNER_PROVIDER = previousProvider;
      }
    }
  });

  it("returns graph feedback without mutating the edited draft", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const baseWorkflow = planned.json().workflow;
    const editedWorkflow = {
      ...baseWorkflow,
      nodes: baseWorkflow.nodes.map((node: { id: string; label: string }) =>
        node.id === "normalize-receipts" ? { ...node, label: "Normalize With Tax" } : node
      )
    };
    const feedback = await app.inject({
      method: "POST",
      url: `/api/workflows/${baseWorkflow.id}/feedback`,
      payload: {
        baseWorkflow,
        editedWorkflow
      }
    });

    expect(feedback.statusCode).toBe(200);
    expect(
      feedback.json().graphDiff.changes.map((change: { kind: string }) => change.kind)
    ).toContain("node.edited");
    expect(feedback.json().feedback.status).toBe("ready");
  });

  it("persists planner suggestion decisions without mutating the workflow", async () => {
    app = buildTestApiApp();
    const workflow = gmailReceiptsToSheetsWorkflowFixture;
    await app.inject({
      method: "POST",
      url: "/api/workflows",
      payload: workflow
    });
    const editedWorkflow = {
      ...workflow,
      nodes: workflow.nodes.slice(1)
    };
    const feedback = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/feedback`,
      payload: {
        baseWorkflow: workflow,
        editedWorkflow
      }
    });
    const suggestionId = feedback.json().feedback.suggestions[0].id;
    const decided = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/feedback/${feedback.json().feedback.id}/suggestions/${suggestionId}/decision`,
      payload: {
        suggestionId,
        decision: "rejected"
      }
    });
    const stored = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}`
    });

    expect(decided.statusCode).toBe(200);
    expect(decided.json().feedback.suggestions[0].status).toBe("rejected");
    expect(stored.json().workflow.nodes[0].label).toBe(workflow.nodes[0]?.label);
  });

  it("records plan acceptance without creating a production approval", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const accepted = await app.inject({
      method: "POST",
      url: `/api/workflows/${planned.json().workflow.id}/accept-plan`,
      payload: {
        workflow: planned.json().workflow,
        acceptedBy: "owner@example.com"
      }
    });
    const stored = await app.inject({
      method: "GET",
      url: `/api/workflows/${planned.json().workflow.id}`
    });

    expect(accepted.statusCode).toBe(200);
    expect(accepted.json().draftRevision.source).toBe("plan-accepted");
    expect(accepted.json().workflow.approval).toBeNull();
    expect(stored.json().latestApprovedRevisionId).toBeNull();
  });

  it("keeps branch planning and acceptance local to the selected branch", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflowId = planned.json().workflow.id;
    const listed = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches`
    });
    const mainBranch = listed.json().branches[0];
    const forked = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches`,
      payload: {
        name: "Support alerts",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const branchPlan = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/plan`,
      payload: {
        prompt: "monitor urgent support messages and send Telegram alerts",
        actor: "owner@example.com"
      }
    });
    const accepted = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/accept-plan`,
      payload: {
        workflow: branchPlan.json().workflow,
        acceptedBy: "owner@example.com"
      }
    });
    const main = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches/${mainBranch.id}`
    });
    const branch = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}`
    });

    expect(listed.statusCode).toBe(200);
    expect(forked.statusCode).toBe(201);
    expect(branchPlan.statusCode).toBe(200);
    expect(branchPlan.json().draftRevision.source).toBe("branch-plan");
    expect(accepted.json().draftRevision.source).toBe("plan-accepted");
    expect(main.json().headDraftRevision.workflow.prompt).toContain("Gmail receipts");
    expect(branch.json().headDraftRevision.workflow.prompt).toContain("urgent support");
    expect(branch.json().promptTurns.map((turn: { source: string }) => turn.source)).toContain(
      "plan"
    );
  });

  it("previews clean branch merges and conflicting branch edits", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflowId = planned.json().workflow.id;
    const listed = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches`
    });
    const mainBranch = listed.json().branches[0];
    const forked = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches`,
      payload: {
        name: "Tax parsing",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const sourceReprompt = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/reprompt-node`,
      payload: {
        nodeId: "read-gmail-receipts",
        prompt: "Read Gmail receipts and extract tax totals.",
        actor: "owner@example.com"
      }
    });
    const cleanPreview = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/merge-preview`,
      payload: {
        targetBranchId: mainBranch.id
      }
    });
    const applied = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/merge`,
      payload: {
        targetBranchId: mainBranch.id,
        appliedBy: "owner@example.com",
        resolutions: []
      }
    });
    const targetReprompt = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${mainBranch.id}/reprompt-node`,
      payload: {
        nodeId: "read-gmail-receipts",
        prompt: "Read Gmail receipts and extract merchant names.",
        actor: "owner@example.com",
        currentWorkflow: planned.json().workflow
      }
    });
    const conflicting = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${forked.json().branch.id}/merge-preview`,
      payload: {
        targetBranchId: mainBranch.id
      }
    });

    expect(sourceReprompt.statusCode).toBe(200);
    expect(cleanPreview.json().preview.status).toBe("clean");
    expect(applied.statusCode).toBe(200);
    expect(applied.json().draftRevision.source).toBe("branch-merge");
    expect(targetReprompt.statusCode).toBe(200);
    expect(conflicting.json().preview.status).toBe("conflicts");
    expect(conflicting.json().preview.conflicts[0].kind).toBe("both-edited");
  });

  it("creates, cancels, and streams job events", async () => {
    app = buildTestApiApp();

    const created = await app.inject({
      method: "POST",
      url: "/api/jobs",
      payload: {
        type: "feedback.graph",
        workflowId: "workflow.test"
      }
    });
    const cancelled = await app.inject({
      method: "POST",
      url: `/api/jobs/${created.json().job.id}/cancel`,
      payload: {
        reason: "test cancellation"
      }
    });
    const events = await app.inject({
      method: "GET",
      url: `/api/jobs/${created.json().job.id}/events`
    });

    expect(created.statusCode).toBe(201);
    expect(cancelled.json().job.status).toBe("cancelled");
    expect(events.statusCode).toBe(200);
    expect(events.body).toContain("event: job-complete");
  });

  it("claims queued jobs through the local durable worker", async () => {
    app = buildTestApiApp();

    const created = await app.inject({
      method: "POST",
      url: "/api/jobs",
      payload: {
        type: "smoke.integration",
        workflowId: "workflow.worker",
        payload: {
          durationMs: 1
        }
      }
    });
    const completed = await waitForJobStatus(app, created.json().job.id, "succeeded");

    expect(created.statusCode).toBe(201);
    expect(completed.status).toBe("succeeded");
    expect(completed.workerId).toMatch(/^worker\./u);
    expect(completed.claimedAt).toBeDefined();
    expect(completed.retry.attempt).toBe(1);
    expect(completed.result?.workerId).toBe(completed.workerId);
    expect(completed.events.map((event: { message: string }) => event.message)).toContain(
      "Job claimed by local worker."
    );
  });

  it("cancels active local worker jobs", async () => {
    app = buildTestApiApp();

    const created = await app.inject({
      method: "POST",
      url: "/api/jobs",
      payload: {
        type: "smoke.integration",
        workflowId: "workflow.worker",
        payload: {
          durationMs: 1000
        }
      }
    });
    await waitForJobStatus(app, created.json().job.id, "running");
    const cancelled = await app.inject({
      method: "POST",
      url: `/api/jobs/${created.json().job.id}/cancel`,
      payload: {
        reason: "stop worker"
      }
    });
    const terminal = await waitForJobStatus(app, created.json().job.id, "cancelled");

    expect(cancelled.statusCode).toBe(200);
    expect(terminal.status).toBe("cancelled");
    expect(terminal.cancellationReason).toBe("stop worker");
  });

  it("plans codegen fallback nodes with persisted artifact metadata", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });

    expect(planned.statusCode).toBe(200);
    const codegenNode = planned
      .json()
      .workflow.nodes.find((node: { kind: string }) => node.kind === "codegen");
    expect(codegenNode.codegen.review.status).toBe("draft");
    expect(codegenNode.codegen.plannerRationale).toContain("codegen node");
    expect(
      codegenNode.codegen.artifacts.map((artifact: { path: string }) => artifact.path)
    ).toEqual(["generated/package-manifest.json", "generated/scrape-status-page.ts"]);
  });

  it("blocks approval until generated code is reviewed", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const workflow = planned.json().workflow;
    const evaluated = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/evaluate-draft`,
      payload: { workflow, mockOnly: true }
    });
    expect(evaluated.statusCode).toBe(200);
    const blockedApproval = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow,
        approvedBy: "owner@example.com"
      }
    });

    expect(blockedApproval.statusCode).toBe(409);
    expect(blockedApproval.json().issues[0].code).toBe("WORKFLOW_CODEGEN_REVIEW_REQUIRED");
  });

  it("reviews generated code before approval", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const workflow = planned.json().workflow;
    const built = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/build`,
      payload: {
        runTestsInDocker: false
      }
    });
    expect(built.statusCode).toBe(200);
    expect(built.json().workspace.mountedAgents).toContain("evaluator");
    expect(
      built
        .json()
        .workspace.fileHashes.some(
          (file: { readonly path: string }) => file.path === "generated/scrape-status-page.ts"
        )
    ).toBe(true);
    const evals = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/evals`
    });
    expect(evals.statusCode).toBe(200);
    expect(evals.json().evalReports[0].status).toBe("passed");
    const reviewed = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/review`,
      payload: {
        status: "approved",
        reviewedBy: "owner@example.com",
        notes: "fixture reviewed"
      }
    });

    expect(reviewed.statusCode).toBe(200);
    expect(reviewed.json().node.codegen.review.status).toBe("approved");
    const evaluated = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/evaluate-draft`,
      payload: { workflow: reviewed.json().workflow, mockOnly: true }
    });
    expect(evaluated.statusCode).toBe(200);

    const approved = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow: reviewed.json().workflow,
        approvedBy: "owner@example.com"
      }
    });
    expect(approved.statusCode).toBe(200);
  });

  it("reuses generated modules across branches only after a passing eval", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const workflow = planned.json().workflow;
    const built = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/build`,
      payload: {
        runTestsInDocker: false
      }
    });
    const listed = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/branches`
    });
    const mainBranch = listed.json().branches[0];
    const forked = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/branches`,
      payload: {
        name: "Reuse candidate",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const reuse = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/branches/${forked.json().branch.id}/reuse-candidates`
    });
    const reprompted = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/branches/${forked.json().branch.id}/reprompt-node`,
      payload: {
        nodeId: "scrape-status-page",
        prompt: "Scrape the status page with a different output schema.",
        actor: "owner@example.com"
      }
    });
    const blocked = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/branches/${forked.json().branch.id}/reuse-candidates`
    });

    expect(built.statusCode).toBe(200);
    expect(reuse.statusCode).toBe(200);
    expect(reuse.json().decisions[0]).toMatchObject({
      nodeId: "scrape-status-page",
      status: "reuse-with-reeval",
      sourceBranchId: mainBranch.id,
      sourceEvalReportId: expect.any(String)
    });
    expect(reprompted.statusCode).toBe(200);
    expect(blocked.json().decisions[0].status).toBe("blocked-drift");
    expect(blocked.json().decisions[0].gates).toContain("prompt");
  });

  it("automatically applies safe generated module reuse after branch reprompt and merge", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const workflow = planned.json().workflow;
    const built = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/build`,
      payload: {
        runTestsInDocker: false
      }
    });
    const reviewed = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/review`,
      payload: {
        status: "approved",
        reviewedBy: "owner@example.com"
      }
    });
    const listed = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/branches`
    });
    const mainBranch = listed.json().branches[0];
    const forked = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/branches`,
      payload: {
        name: "Reuse reprompt",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const reprompted = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/branches/${forked.json().branch.id}/reprompt-node`,
      payload: {
        nodeId: "summarize-incidents",
        prompt: "Summarize incidents with customer-facing wording.",
        actor: "owner@example.com"
      }
    });
    const reusedNode = reprompted
      .json()
      .workflow.nodes.find((node: { readonly id: string }) => node.id === "scrape-status-page");
    const blockedApproval = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow: reprompted.json().workflow,
        approvedBy: "owner@example.com",
        branchId: forked.json().branch.id
      }
    });
    const merged = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/branches/${forked.json().branch.id}/merge`,
      payload: {
        targetBranchId: mainBranch.id,
        appliedBy: "owner@example.com",
        resolutions: []
      }
    });

    expect(built.statusCode).toBe(200);
    expect(reviewed.statusCode).toBe(200);
    expect(reprompted.statusCode).toBe(200);
    expect(reprompted.json().draftRevision.source).toBe("branch-reprompt");
    expect(reprompted.json().promptTurn.metadata).toMatchObject({ reuseApplied: true });
    expect(reprompted.json().branch.metadata).toMatchObject({ latestReuseApplied: true });
    expect(reusedNode.config.reusedFromBranchId).toBe(mainBranch.id);
    expect(reusedNode.codegen.review.status).toBe("draft");
    expect(blockedApproval.statusCode).toBe(409);
    expect(merged.statusCode).toBe(200);
    expect(merged.json().draftRevision.source).toBe("branch-merge");
    expect(merged.json().branch.metadata.latestReuseApplied).toBe(true);
  });

  it("updates branch metadata and keeps archived branches read-only but readable", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflowId = planned.json().workflow.id;
    const listed = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches`
    });
    const mainBranch = listed.json().branches[0];
    const alpha = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches`,
      payload: {
        name: "Alpha",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const beta = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches`,
      payload: {
        name: "Beta",
        createdBy: "owner@example.com",
        fromBranchId: mainBranch.id
      }
    });
    const duplicate = await app.inject({
      method: "PATCH",
      url: `/api/workflows/${workflowId}/branches/${beta.json().branch.id}`,
      payload: {
        name: "Alpha",
        updatedBy: "owner@example.com"
      }
    });
    const archived = await app.inject({
      method: "PATCH",
      url: `/api/workflows/${workflowId}/branches/${beta.json().branch.id}`,
      payload: {
        status: "archived",
        updatedBy: "owner@example.com"
      }
    });
    const renamedToArchivedName = await app.inject({
      method: "PATCH",
      url: `/api/workflows/${workflowId}/branches/${alpha.json().branch.id}`,
      payload: {
        name: "Beta",
        updatedBy: "owner@example.com"
      }
    });
    const archiveMain = await app.inject({
      method: "PATCH",
      url: `/api/workflows/${workflowId}/branches/${mainBranch.id}`,
      payload: {
        status: "archived",
        updatedBy: "owner@example.com"
      }
    });
    const archivedBranchRead = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflowId}/branches/${beta.json().branch.id}`
    });
    const archivedPlan = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflowId}/branches/${beta.json().branch.id}/plan`,
      payload: {
        prompt: "monitor urgent support messages and send Telegram alerts",
        actor: "owner@example.com"
      }
    });

    expect(duplicate.statusCode).toBe(409);
    expect(archived.statusCode).toBe(200);
    expect(archived.json().branch.status).toBe("archived");
    expect(renamedToArchivedName.statusCode).toBe(200);
    expect(archiveMain.statusCode).toBe(409);
    expect(archivedBranchRead.statusCode).toBe(200);
    expect(archivedPlan.statusCode).toBe(409);
    expect(archivedPlan.json().error).toBe("WORKFLOW_BRANCH_ARCHIVED");
  });

  it("promotes reviewed generated code into a reusable skill for future planning", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const workflow = planned.json().workflow;
    await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/review`,
      payload: {
        status: "approved",
        reviewedBy: "owner@example.com"
      }
    });
    const promoted = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/codegen/scrape-status-page/promote`
    });
    expect(promoted.statusCode).toBe(200);
    expect(promoted.json().skill.id).toContain("skill.promoted.");

    const replanned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "scrape a custom public status page and summarize incidents"
      }
    });
    const reusedNode = replanned
      .json()
      .workflow.nodes.find((node: { id: string }) => node.id === "scrape-status-page");
    expect(reusedNode.kind).toBe("skill");
    expect(reusedNode.skillId).toBe(promoted.json().skill.id);
  });

  it("reprompts, approves, runs, and fetches Phase 3 workflows", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflow = planned.json().workflow;

    const reprompted = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/reprompt-node`,
      payload: {
        nodeId: "normalize-receipts",
        prompt: "Normalize receipts with merchant category and tax columns.",
        currentWorkflow: workflow
      }
    });
    expect(reprompted.statusCode).toBe(200);
    expect(reprompted.json().before.label).toBe("Normalize Receipts");
    expect(reprompted.json().after.label).toBe("Normalize Receipts With Merchant Category");
    expect(reprompted.json().draftRevision.revision).toBe(2);

    const invalid = {
      ...reprompted.json().workflow,
      edges: [
        {
          id: "edge.invalid",
          source: { nodeId: "missing-node", port: "result" },
          target: { nodeId: "normalize-receipts", port: "receipts" }
        }
      ]
    };
    const blockedApproval = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow: invalid,
        approvedBy: "owner@example.com"
      }
    });
    expect(blockedApproval.statusCode).toBe(422);
    expect(blockedApproval.json().validation.errors[0].code).toBe(
      "WORKFLOW_EDGE_SOURCE_NODE_MISSING"
    );
    const evaluated = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/evaluate-draft`,
      payload: { workflow: reprompted.json().workflow, mockOnly: true }
    });
    expect(evaluated.statusCode).toBe(200);

    const approved = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow: reprompted.json().workflow,
        approvedBy: "owner@example.com"
      }
    });
    expect(approved.statusCode).toBe(200);
    expect(approved.json().approvedRevisionId).toBe(`approved.${workflow.id}.r2`);
    expect(approved.json().diff.summary).toContain("Frozen approval metadata changed.");

    const missingRun = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/runs`,
      payload: {
        approvedRevisionId: "approved.missing.r1"
      }
    });
    expect(missingRun.statusCode).toBe(404);

    const blockedRun = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/runs`,
      headers: {
        "x-correlation-id": "corr.api-test"
      },
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId
      }
    });
    expect(blockedRun.statusCode).toBe(409);
    expect(blockedRun.json().error).toBe("WORKFLOW_RUN_REQUIRES_DEPLOYMENT");

    const runnerDeployment = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/deployments`,
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId,
        kind: "runner.configuration",
        createdBy: "owner@example.com",
        rollbackPlan: "Rollback to the previous approved revision."
      }
    });
    expect(runnerDeployment.statusCode).toBe(201);

    const run = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/runs`,
      headers: {
        "x-correlation-id": "corr.api-test"
      },
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId,
        deploymentId: runnerDeployment.json().deployment.id
      }
    });
    expect(run.statusCode).toBe(202);
    expect(run.json().run.status).toBe("succeeded");
    expect(run.json().run.events.map((event: { message: string }) => event.message)).toContain(
      "NanoClaw run finished."
    );
    expect(run.json().run.events[0].workflowId).toBe(workflow.id);
    expect(run.json().run.events[0].revisionId).toBe(approved.json().approvedRevisionId);
    expect(run.json().run.events[0].correlationId).toBe("corr.api-test");

    const fetchedRun = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/runs/${run.json().run.id}`
    });
    expect(fetchedRun.statusCode).toBe(200);
    expect(fetchedRun.json().run.id).toBe(run.json().run.id);

    const runEvents = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/runs/${run.json().run.id}/events`
    });
    expect(runEvents.statusCode).toBe(200);
    expect(runEvents.json().events.at(-1).kind).toBe("run.lifecycle");

    const audit = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/audit`
    });
    expect(audit.statusCode).toBe(200);
    expect(audit.json().audit.map((record: { action: string }) => record.action)).toContain(
      "run.completed"
    );

    const fetchedRevision = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/revisions/${approved.json().approvedRevisionId}`
    });
    expect(fetchedRevision.statusCode).toBe(200);
    expect(fetchedRevision.json().approvedRevision.id).toBe(approved.json().approvedRevisionId);
  });

  it("validates invalid workflows with stable errors", async () => {
    app = buildTestApiApp();

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
    app = buildTestApiApp();

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
    const evaluated = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/evaluate-draft",
      payload: { workflow: created.json().workflow, mockOnly: true }
    });
    expect(evaluated.statusCode).toBe(200);

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
      "append-sheet-rows",
      "deliver-results-email"
    ]);

    const execution = await app.inject({
      method: "POST",
      url: "/api/workflows/workflow.gmail-receipts-to-sheets/executions"
    });
    expect(execution.statusCode).toBe(202);
    expect(execution.json().result.status).toBe("succeeded");
    expect(execution.json().result.revision).toBe(1);
    expect(execution.json().result.nodeResults.at(-1).output.delivery.channels).toEqual(["email"]);

    const fetched = await app.inject({
      method: "GET",
      url: `/api/executions/${execution.json().id}`
    });
    expect(fetched.statusCode).toBe(200);
    expect(fetched.json().workflowId).toBe("workflow.gmail-receipts-to-sheets");
  });

  it("creates workflow-native deployment records only after approval and draft eval", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflow = planned.json().workflow;
    const blocked = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/deployments`,
      payload: {
        approvedRevisionId: `approved.${workflow.id}.r1`,
        kind: "workflow.bundle",
        createdBy: "owner@example.com",
        rollbackPlan: "Rollback to the previous approved revision."
      }
    });
    expect(blocked.statusCode).toBe(404);

    await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/evaluate-draft`,
      payload: { workflow, mockOnly: true }
    });
    const approved = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow,
        approvedBy: "owner@example.com"
      }
    });
    const deployed = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/deployments`,
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId,
        kind: "workflow.bundle",
        createdBy: "owner@example.com",
        rollbackPlan: "Rollback to the previous approved revision."
      }
    });
    const runnerDeployment = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/deployments`,
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId,
        kind: "runner.configuration",
        createdBy: "owner@example.com",
        rollbackPlan: "Rollback to the previous approved revision."
      }
    });
    const deployments = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/deployments`
    });
    const active = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/deployments/active`
    });
    const run = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/runs`,
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId
      }
    });

    expect(approved.statusCode).toBe(200);
    expect(deployed.statusCode).toBe(201);
    expect(deployed.json().deployment.status).toBe("deployed");
    expect(deployed.json().deployment.metadata.bundle.path).toContain("workflow-bundle.json");
    expect(runnerDeployment.statusCode).toBe(201);
    expect(runnerDeployment.json().deployment.metadata.runnerConfig.status).toBe("active");
    expect(deployments.json().deployments).toHaveLength(2);
    expect(active.json().runnerConfigurations[0].deploymentId).toBe(
      runnerDeployment.json().deployment.id
    );
    expect(run.statusCode).toBe(202);
    expect(run.json().run.events[0].metadata.runnerDeploymentId).toBe(
      runnerDeployment.json().deployment.id
    );
  });

  it("activates scheduled deployments as durable local registrations", async () => {
    app = buildTestApiApp();

    const planned = await app.inject({
      method: "POST",
      url: "/api/workflows/plan",
      payload: {
        prompt: "extract transaction details from Gmail receipts into Sheets"
      }
    });
    const workflow = {
      ...planned.json().workflow,
      nodes: planned.json().workflow.nodes.map((node: { readonly id: string; config: object }) =>
        node.id === "manual-trigger"
          ? {
              ...node,
              config: {
                ...node.config,
                schedule: "0 8 * * *",
                timezone: "UTC"
              }
            }
          : node
      )
    };
    await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/evaluate-draft`,
      payload: { workflow, mockOnly: true }
    });
    const approved = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/approve`,
      payload: {
        workflow,
        approvedBy: "owner@example.com"
      }
    });
    const deployed = await app.inject({
      method: "POST",
      url: `/api/workflows/${workflow.id}/deployments`,
      payload: {
        approvedRevisionId: approved.json().approvedRevisionId,
        kind: "schedule.activation",
        createdBy: "owner@example.com",
        rollbackPlan: "Disable the schedule registration."
      }
    });
    const active = await app.inject({
      method: "GET",
      url: `/api/workflows/${workflow.id}/deployments/active`
    });

    expect(deployed.statusCode).toBe(201);
    expect(deployed.json().deployment.metadata.activeScheduleRegistrations[0].nodeId).toBe(
      "manual-trigger"
    );
    expect(active.json().activeSchedules[0].schedule).toBe("0 8 * * *");
  });

  it("creates a new draft revision after approval", async () => {
    app = buildTestApiApp();

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

function restoreEnv(name: string, value: string | undefined): void {
  if (value === undefined) {
    delete process.env[name];
  } else {
    process.env[name] = value;
  }
}

async function waitForJobStatus(
  app: FastifyInstance,
  jobId: string,
  status: string
): Promise<WorkflowJob> {
  const deadline = Date.now() + 3000;
  while (Date.now() < deadline) {
    const response = await app.inject({
      method: "GET",
      url: `/api/jobs/${jobId}`
    });
    const job = response.json().job as WorkflowJob;
    if (job.status === status) {
      return job;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }

  throw new Error(`Timed out waiting for job '${jobId}' to reach '${status}'.`);
}
