import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createApprovedWorkflowFixture,
  createWorkflowSpecDiff,
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture
} from "@kelpclaw/workflow-spec";
import type { WorkflowRunRecord, WorkflowSpec } from "@kelpclaw/workflow-spec";
import { App } from "../src/App.js";

vi.setConfig({ testTimeout: 10_000 });

let mockCurrentWorkflow: WorkflowSpec | null = null;

beforeEach(() => {
  mockCurrentWorkflow = null;
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn(mockFetch));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("OpenClaw planner shell", () => {
  it("renders the planner workspace, workflow nodes, and inspector", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "OpenClaw" })).toBeInTheDocument();
    expect(screen.getByText("Gmail Receipts To Sheets")).toBeInTheDocument();
    expect(screen.getByText("Read Gmail Receipts")).toBeInTheDocument();
    expect(screen.getByText("skill")).toBeInTheDocument();
    expect(screen.getByLabelText("Label")).toHaveValue("Read Gmail Receipts");
  });

  it("renders live integration readiness and sends admin bearer auth", async () => {
    render(<App />);

    expect(await screen.findByLabelText("Integration setup")).toBeInTheDocument();
    expect(screen.getByText("google.oauth.default")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Admin token"), {
      target: { value: "local-admin-token" }
    });
    fireEvent.change(screen.getByLabelText("Workflow Prompt"), {
      target: { value: "extract transaction details from Gmail receipts into Sheets" }
    });
    fireEvent.click(screen.getByRole("button", { name: /^Plan$/i }));

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "/api/workflows/plan",
        expect.objectContaining({
          headers: expect.objectContaining({
            authorization: "Bearer local-admin-token"
          })
        })
      );
    });
  });

  it("edits selected node labels and validates invalid port changes inline", async () => {
    render(<App />);

    fireEvent.change(screen.getByLabelText("Label"), {
      target: { value: "Read Gmail Orders" }
    });
    expect(screen.getByText("Read Gmail Orders")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Inputs"), {
      target: { value: "{}" }
    });
    fireEvent.blur(screen.getByLabelText("Inputs"));

    expect(await screen.findByText("WORKFLOW_EDGE_TARGET_PORT_INVALID")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /approve/i })).toBeDisabled();
  });

  it("adds and deletes nodes on the canvas", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /Codegen/i }));
    expect(await screen.findByText("Generated Code")).toBeInTheDocument();

    fireEvent.click(screen.getByTitle("Delete selected"));
    await waitFor(() => {
      expect(screen.queryByText("Generated Code")).not.toBeInTheDocument();
    });
  });

  it("configures adapter-backed delivery skills and opt-in push channels", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /Delivery/i }));
    fireEvent.change(screen.getByLabelText("Adapter-backed skill"), {
      target: { value: "skill.email.results.deliver" }
    });
    expect(screen.getByLabelText("Adapter")).toHaveValue("adapter.email");

    fireEvent.click(screen.getByLabelText("WhatsApp"));
    expect((screen.getByLabelText("Adapter") as HTMLInputElement).value).toContain(
      "adapter.whatsapp"
    );
  });

  it("approves a frozen diff and renders NanoClaw run state", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /evaluate/i }));
    expect((await screen.findAllByText("ready")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    expect(await screen.findByText("Frozen approval metadata changed.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Run$/i }));
    expect(await screen.findByText("succeeded")).toBeInTheDocument();
    expect(screen.getByText("NanoClaw run finished.")).toBeInTheDocument();
  });

  it("plans a prompt through the mocked API and reprompts a node", async () => {
    render(<App />);

    fireEvent.change(screen.getByLabelText("Workflow Prompt"), {
      target: { value: "monitor urgent support messages and send Telegram alerts" }
    });
    fireEvent.click(screen.getByRole("button", { name: /^Plan$/i }));

    expect(await screen.findByText("Monitor Urgent Support Messages And")).toBeInTheDocument();
    expect(screen.getByText("Approve Alert")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Node Prompt"), {
      target: { value: "Classify incidents with severity and owner routing." }
    });
    fireEvent.click(screen.getByRole("button", { name: /Reprompt Node/i }));

    expect(await screen.findByText("Classify Incidents With Severity And")).toBeInTheDocument();
    expect(screen.getByTestId("approval-diff")).toHaveTextContent("Classify Incidents");
  });

  it("reviews and promotes generated code nodes", async () => {
    render(<App />);

    fireEvent.change(screen.getByLabelText("Workflow Prompt"), {
      target: { value: "scrape a custom public status page and summarize incidents" }
    });
    fireEvent.click(screen.getByRole("button", { name: /^Plan$/i }));

    expect(await screen.findByText("Scrape Status Page")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Review Generated Code/i }));
    expect(await screen.findByText("approved")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Promote Skill/i }));
    expect(await screen.findByText("Promoted Scrape Status Page")).toBeInTheDocument();
  });

  it("renders worker job and deployment activation state", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /evaluate/i }));
    expect((await screen.findAllByText("ready")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    expect(await screen.findByText("Frozen approval metadata changed.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Deploy$/i }));

    expect(await screen.findByText("Deployment deployed: workflow.bundle")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Deployments" })).toBeInTheDocument();
    expect(screen.getByText(/deployment\.workflow\.bundle/u)).toBeInTheDocument();
    expect(screen.getByText("worker.openclaw-test")).toBeInTheDocument();
  });
});

async function mockFetch(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const url = typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url;
  const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};

  if (url.endsWith("/api/secrets")) {
    return jsonResponse({
      ok: true,
      secrets: [
        {
          name: "google.oauth.default",
          createdAt: "2026-05-18T00:00:00.000Z",
          updatedAt: "2026-05-18T00:00:00.000Z"
        }
      ],
      integrations: [
        { id: "google", ready: true, requiredSecrets: ["google.oauth.default"] },
        { id: "smtp", ready: false, requiredSecrets: ["email.smtp.default"] },
        { id: "whatsapp", ready: false, requiredSecrets: ["whatsapp.cloud.default"] },
        { id: "telegram", ready: false, requiredSecrets: ["telegram.bot.default"] }
      ]
    });
  }

  if (url.endsWith("/api/integrations/google/status")) {
    return jsonResponse({ ok: true, connected: true });
  }

  if (url.endsWith("/api/jobs")) {
    return jsonResponse(
      {
        ok: true,
        job: mockJob(
          String(body.type ?? "plan.workflow"),
          String(body.workflowId ?? "workflow.test")
        )
      },
      201
    );
  }

  if (url.includes("/api/jobs/") && url.endsWith("/cancel")) {
    const job = mockJob("plan.workflow", "workflow.test", "cancelled");
    return jsonResponse({ ok: true, job });
  }

  if (url.includes("/api/jobs/") && url.endsWith("/events")) {
    const job = mockJob("plan.workflow", "workflow.test", "succeeded");
    return new Response(`event: job-complete\ndata: ${JSON.stringify(job)}\n\n`, {
      status: 200,
      headers: { "content-type": "text/event-stream" }
    });
  }

  if (url.endsWith("/plan")) {
    const prompt = String(body.prompt ?? gmailReceiptsToSheetsWorkflowFixture.prompt);
    const workflow: WorkflowSpec =
      prompt.includes("urgent") || prompt.includes("Telegram")
        ? createAlertWorkflow(prompt)
        : prompt.includes("scrape")
          ? createCodegenWorkflow(prompt)
          : gmailReceiptsToSheetsWorkflowFixture;
    mockCurrentWorkflow = workflow;

    return jsonResponse({
      ok: true,
      workflow,
      draftRevision: draftRevision(workflow, "plan"),
      validation: { ok: true, workflow },
      route: taskRouteForWorkflow(workflow)
    });
  }

  if (url.endsWith("/evaluate-draft")) {
    const workflow = body.workflow as WorkflowSpec;
    return jsonResponse({
      ok: true,
      evaluation: {
        id: `eval.${workflow.id}.r${workflow.revision}`,
        workflowId: workflow.id,
        draftRevisionId: `draft.${workflow.id}.r${workflow.revision}`,
        status: "passed",
        readyForApproval: true,
        createdAt: "2026-05-18T01:00:00.000Z",
        finishedAt: "2026-05-18T01:00:00.000Z",
        mode: "draft",
        mockOnly: true,
        liveProviderCalls: 0,
        findings: [],
        events: [],
        suggestions: []
      }
    });
  }

  if (url.endsWith("/feedback")) {
    const workflow = body.editedWorkflow as WorkflowSpec;
    return jsonResponse({
      ok: true,
      graphDiff: {
        id: `graphdiff.${workflow.id}`,
        workflowId: workflow.id,
        baseRevision: workflow.revision,
        editedRevision: workflow.revision,
        createdAt: "2026-05-18T01:00:00.000Z",
        summary: ["node.edited: 1"],
        changes: [],
        validation: { ok: true, workflow }
      },
      feedback: {
        id: `feedback.${workflow.id}`,
        workflowId: workflow.id,
        graphDiffId: `graphdiff.${workflow.id}`,
        route: taskRouteForWorkflow(workflow),
        createdAt: "2026-05-18T01:00:00.000Z",
        status: "ready",
        suggestions: [],
        issues: []
      }
    });
  }

  if (url.includes("/feedback/") && url.endsWith("/decision")) {
    return jsonResponse({
      ok: true,
      feedback: {
        id: "feedback.workflow.gmail-receipts-to-sheets",
        workflowId: "workflow.gmail-receipts-to-sheets",
        graphDiffId: "graphdiff.workflow.gmail-receipts-to-sheets",
        route: taskRouteForWorkflow(mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture),
        createdAt: "2026-05-18T01:00:00.000Z",
        status: "ready",
        suggestions: [
          {
            id: String(body.suggestionId),
            status: body.decision,
            conflict: "safe",
            target: { kind: "workflow" },
            title: "Persisted decision",
            message: "Decision was persisted.",
            issues: []
          }
        ],
        issues: []
      }
    });
  }

  if (url.endsWith("/validate")) {
    const workflow = body.workflow as WorkflowSpec;
    return jsonResponse({
      ok: true,
      workflow,
      draftRevision: draftRevision(workflow, "validate"),
      validation: { ok: true, workflow }
    });
  }

  if (url.endsWith("/reprompt-node")) {
    const workflow = body.currentWorkflow as WorkflowSpec;
    const nodeId = String(body.nodeId);
    const before = workflow.nodes.find((node) => node.id === nodeId) ?? workflow.nodes[0]!;
    const after = {
      ...before,
      label: "Classify Incidents With Severity And",
      description: String(body.prompt)
    };
    const nextWorkflow = {
      ...workflow,
      nodes: workflow.nodes.map((node) => (node.id === nodeId ? after : node))
    };
    const diff = createWorkflowSpecDiff(workflow, nextWorkflow);
    return jsonResponse({
      ok: true,
      workflow: nextWorkflow,
      draftRevision: draftRevision(nextWorkflow, "reprompt"),
      validation: { ok: true, workflow: nextWorkflow },
      before,
      after,
      diff
    });
  }

  if (url.includes("/codegen/") && url.endsWith("/review")) {
    const workflow = reviewCodegenWorkflow(mockCurrentWorkflow ?? scheduledScrapingWorkflowFixture);
    mockCurrentWorkflow = workflow;
    const node = workflow.nodes.find((candidate) => candidate.id === "scrape-status-page");
    return jsonResponse({
      ok: true,
      workflow,
      draftRevision: draftRevision(workflow, "validate"),
      validation: { ok: true, workflow },
      node
    });
  }

  if (url.includes("/codegen/") && url.endsWith("/promote")) {
    return jsonResponse({
      ok: true,
      skill: {
        id: "skill.promoted.scrape-status-page",
        name: "Scrape Status Page"
      },
      artifact: {
        path: "promoted-skills/skill.promoted.scrape-status-page.json",
        checksum: `sha256:${"a".repeat(64)}`,
        contentType: "application/json"
      }
    });
  }

  if (url.endsWith("/approve")) {
    const workflow = body.workflow as WorkflowSpec;
    const approvedWorkflow = createApprovedWorkflowFixture(workflow, {
      frozenRevision: workflow.revision
    });
    const diff = createWorkflowSpecDiff(workflow, approvedWorkflow);
    const approvedRevision = {
      id: `approved.${workflow.id}.r${workflow.revision}`,
      workflowId: workflow.id,
      revision: workflow.revision,
      approvedBy: "owner@example.com",
      createdAt: "2026-05-18T01:00:00.000Z",
      workflow: approvedWorkflow,
      draftSpecJson: "{}",
      frozenSpecJson: "{}",
      diff
    };
    return jsonResponse({
      ok: true,
      workflowId: workflow.id,
      approvedRevisionId: approvedRevision.id,
      approvedRevision,
      workflow: approvedWorkflow,
      diff
    });
  }

  if (url.endsWith("/runs")) {
    const run = createRunRecord(String(body.approvedRevisionId));
    return jsonResponse({ ok: true, run }, 202);
  }

  if (url.includes("/runs/")) {
    return jsonResponse({ ok: true, run: createRunRecord("approved.workflow.r1") });
  }

  if (url.endsWith("/deployments/active")) {
    return jsonResponse({
      ok: true,
      activeDeployments: [
        {
          id: "deployment.workflow.bundle",
          workflowId: mockCurrentWorkflow?.id ?? "workflow.gmail-receipts-to-sheets",
          approvedRevisionId: "approved.workflow.r1",
          draftEvaluationId: "eval.workflow.r1",
          kind: "workflow.bundle",
          status: "deployed",
          createdAt: "2026-05-18T01:00:00.000Z",
          createdBy: "owner@example.com",
          requiredIntegrations: [],
          secretRefs: [],
          rollbackPlan: "Rollback.",
          auditRecordId: "audit.deployment",
          metadata: {}
        }
      ],
      activeSchedules: [],
      runnerConfigurations: [
        {
          deploymentId: "deployment.runner",
          status: "active",
          dagHash: "sha256:test"
        }
      ],
      skillPublications: [],
      integrationBindings: [],
      bundles: [
        {
          deploymentId: "deployment.workflow.bundle",
          path: "deployments/deployment.workflow.bundle/workflow-bundle.json"
        }
      ],
      generatedServices: []
    });
  }

  if (url.endsWith("/deployments") && init?.method === "GET") {
    return jsonResponse({
      ok: true,
      deployments: []
    });
  }

  if (url.endsWith("/deployments")) {
    return jsonResponse(
      {
        ok: true,
        deployment: {
          id: "deployment.workflow.bundle",
          workflowId: mockCurrentWorkflow?.id ?? "workflow.gmail-receipts-to-sheets",
          approvedRevisionId: String(body.approvedRevisionId),
          draftEvaluationId: "eval.workflow.r1",
          kind: body.kind,
          status: "deployed",
          createdAt: "2026-05-18T01:00:00.000Z",
          createdBy: body.createdBy,
          requiredIntegrations: [],
          secretRefs: [],
          rollbackPlan: body.rollbackPlan,
          auditRecordId: "audit.deployment",
          metadata: { artifacts: [] }
        }
      },
      201
    );
  }

  return jsonResponse({ ok: false, message: "Unhandled mock route" }, 500);
}

function mockJob(
  type: string,
  workflowId: string,
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled" = "queued"
) {
  return {
    id: `job.${type}.test`,
    type,
    status,
    workflowId,
    correlationId: "corr.openclaw-test",
    createdAt: "2026-05-18T01:00:00.000Z",
    updatedAt: "2026-05-18T01:00:00.000Z",
    claimedAt: status === "queued" ? undefined : "2026-05-18T01:00:00.000Z",
    workerId: status === "queued" ? undefined : "worker.openclaw-test",
    retry: { attempt: 0, maxAttempts: 1, retryable: true },
    events: [
      {
        id: `event.${type}.queued`,
        jobId: `job.${type}.test`,
        timestamp: "2026-05-18T01:00:00.000Z",
        level: status === "failed" ? "error" : "info",
        message: `${type} ${status}.`,
        kind: "job.lifecycle"
      }
    ]
  };
}

function createAlertWorkflow(prompt: string): WorkflowSpec {
  const [trigger, skill, transform, delivery] = gmailReceiptsToSheetsWorkflowFixture.nodes;
  if (!trigger || !skill || !transform || !delivery) {
    throw new Error("Fixture nodes are missing.");
  }

  return {
    ...gmailReceiptsToSheetsWorkflowFixture,
    id: "workflow.monitor-urgent-support-messages-and-send-telegram-alerts",
    name: "Monitor Urgent Support Messages And",
    prompt,
    nodes: [
      trigger,
      {
        ...skill,
        id: "classify-urgency",
        label: "Classify Urgency"
      },
      {
        ...transform,
        id: "approve-alert",
        kind: "approval",
        label: "Approve Alert",
        inputs: { receipts: { type: "array", items: { type: "object" } } },
        outputs: { rows: { type: "array", items: { type: "object" } } }
      },
      {
        ...delivery,
        id: "send-alert",
        label: "Send Alert"
      }
    ]
  };
}

function createCodegenWorkflow(prompt: string): WorkflowSpec {
  return {
    ...scheduledScrapingWorkflowFixture,
    id: "workflow.scrape-a-custom-public-status-page-and-summarize-incidents",
    name: "Scrape A Custom Public Status",
    prompt
  };
}

function reviewCodegenWorkflow(workflow: WorkflowSpec): WorkflowSpec {
  return {
    ...workflow,
    nodes: workflow.nodes.map((node) =>
      node.id === "scrape-status-page" && node.codegen
        ? {
            ...node,
            codegen: {
              ...node.codegen,
              review: {
                status: "approved",
                reviewedBy: "owner@example.com",
                reviewedAt: "2026-05-18T01:00:00.000Z"
              }
            }
          }
        : node
    )
  };
}

function draftRevision(workflow: WorkflowSpec, source: string) {
  return {
    id: `draft.${workflow.id}.r${workflow.revision}.${source}`,
    workflowId: workflow.id,
    revision: workflow.revision,
    workflow,
    validation: { ok: true, workflow },
    source,
    createdAt: "2026-05-18T00:00:00.000Z"
  };
}

function taskRouteForWorkflow(workflow: WorkflowSpec) {
  const codegen = workflow.nodes.some((node) => node.kind === "codegen");

  return {
    route: codegen ? "codegen" : "adapter",
    rationale: codegen
      ? "Prompt requires generated node artifacts."
      : "Prompt uses existing adapter workflow templates.",
    requiredModel: {
      mode: codegen ? "live" : "none",
      role: codegen ? "workflow-architect" : "classifier",
      provider: codegen ? "anthropic" : undefined,
      model: codegen ? "test-model" : undefined,
      retryBudget: {
        maxAttempts: 1,
        maxCostUsd: codegen ? 1 : 0
      }
    },
    expectedNodeKinds: codegen
      ? ["trigger", "codegen", "transform", "delivery"]
      : ["trigger", "skill", "transform", "delivery"],
    dockerSandboxRequired: codegen,
    draftTestsRequired: codegen,
    productionDeterministic: true,
    modelInvocations: []
  };
}

function createRunRecord(approvedRevisionId: string): WorkflowRunRecord {
  return {
    id: "run.workflow.gmail-receipts-to-sheets.r1.1",
    workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
    approvedRevisionId,
    revision: 1,
    status: "succeeded",
    createdAt: "2026-05-18T01:00:00.000Z",
    startedAt: "2026-05-18T01:00:00.000Z",
    finishedAt: "2026-05-18T01:00:00.000Z",
    events: [
      {
        id: "event.run.finished",
        timestamp: "2026-05-18T01:00:00.000Z",
        level: "info",
        message: "NanoClaw run finished."
      }
    ],
    result: {
      id: "execution.workflow.gmail-receipts-to-sheets.r1",
      workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
      revision: 1,
      status: "succeeded",
      startedAt: "2026-05-18T01:00:00.000Z",
      finishedAt: "2026-05-18T01:00:00.000Z",
      deterministic: true,
      nodeResults: []
    }
  };
}

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      "content-type": "application/json"
    }
  });
}
