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

let mockCurrentWorkflow: WorkflowSpec | null = null;

beforeEach(() => {
  mockCurrentWorkflow = null;
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
    expect(screen.getByLabelText("Adapter")).toHaveValue("adapter.email.fake");

    fireEvent.click(screen.getByLabelText("WhatsApp"));
    expect((screen.getByLabelText("Adapter") as HTMLInputElement).value).toContain(
      "adapter.whatsapp.fake"
    );
  });

  it("approves a frozen diff and renders NanoClaw run state", async () => {
    render(<App />);

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
});

async function mockFetch(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const url = typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url;
  const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};

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
      validation: { ok: true, workflow }
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

  return jsonResponse({ ok: false, message: "Unhandled mock route" }, 500);
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
