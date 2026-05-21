import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createApprovedWorkflowFixture,
  createWorkflowEdge,
  createWorkflowNode,
  createWorkflowSpec,
  createWorkflowSpecDiff,
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowBranch,
  WorkflowNodeDecisionTrace,
  WorkflowRunRecord,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";
import { App } from "../src/App.js";

vi.setConfig({ testTimeout: 10_000 });

let mockCurrentWorkflow: WorkflowSpec | null = null;
let mockBranches: WorkflowBranch[] = [];

beforeEach(() => {
  mockCurrentWorkflow = null;
  mockBranches = [mockBranch("branch.workflow.gmail-receipts-to-sheets.main", "main")];
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn(mockFetch));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("OpenClaw planner shell", () => {
  it("renders a blank planner workspace for a fresh session", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "OpenClaw" })).toBeInTheDocument();
    expect(screen.getByText("workflow.openclaw-draft")).toBeInTheDocument();
    expect(screen.getByText("Selected Edge")).toBeInTheDocument();
    expect(screen.queryByLabelText("Label")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Workflow planner")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Workspace navigation")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Search components")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Component categories")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Workflow Prompt")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Plan$/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Commands/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /Accept Plan/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Evaluate/i })).toBeDisabled();
  });

  it("renders live integration readiness and sends admin bearer auth", async () => {
    render(<App />);

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringMatching(/\/api\/secrets$/u),
        expect.any(Object)
      );
    });
    await filterCommand("Google Integration");
    expect(screen.getByText(/google\.oauth\.default is stored; ready/u)).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Command palette" }), { key: "Escape" });

    await executeCommand("Set Admin Token");
    fireEvent.change(screen.getByLabelText("Admin token"), {
      target: { value: "local-admin-token" }
    });
    fireEvent.submit(screen.getByLabelText("Admin token").closest("form")!);
    await planWorkflowFromPalette("extract transaction details from Gmail receipts into Sheets");

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        expect.stringMatching(/\/plan$/u),
        expect.objectContaining({
          headers: expect.objectContaining({
            authorization: "Bearer local-admin-token"
          })
        })
      );
    });
    const planCall = vi.mocked(fetch).mock.calls.find(([url]) => String(url).endsWith("/plan"));
    expect(JSON.parse(String(planCall?.[1]?.body))).not.toHaveProperty("currentWorkflow");
  });

  it("edits selected node labels and validates invalid port changes inline", async () => {
    render(<App />);
    await planGmailWorkflow();

    fireEvent.change(screen.getByLabelText("Label"), {
      target: { value: "Read Gmail Orders" }
    });
    expect(screen.getByText("Read Gmail Orders")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Inputs"), {
      target: { value: "{}" }
    });
    fireEvent.blur(screen.getByLabelText("Inputs"));

    await filterCommand("WORKFLOW_EDGE_TARGET_PORT_INVALID");
    expect(await screen.findByText("WORKFLOW_EDGE_TARGET_PORT_INVALID")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /approve/i })).toBeDisabled();
  });

  it("shows selected-node decision traces and exports JSONL", async () => {
    render(<App />);
    await planGmailWorkflow();

    expect(await screen.findByLabelText("Node decision trace")).toBeInTheDocument();
    expect(
      screen.getByText("Planner selected this node for the requested workflow.")
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Export Trace JSONL/i }));

    expect(await screen.findByText(/Decision trace export/u)).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringMatching(/\/decision-traces\/export$/u),
      expect.any(Object)
    );
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

  it("uses component categories and search to add concrete nodes", async () => {
    render(<App />);

    expect(screen.queryByLabelText("Component categories")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Available components")).not.toBeInTheDocument();

    await executeCommand("Add Gmail Receipts");
    expect(screen.getByLabelText("Label")).toHaveValue("Gmail Receipts");
    await filterCommand("Workflow:");
    expect(screen.getByText(/1 nodes, 0 edges/u)).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Command palette" }), { key: "Escape" });

    await executeCommand("research");
    expect(screen.getByLabelText("Label")).toHaveValue("Research Agent");
    await filterCommand("Workflow:");
    expect(screen.getByText(/2 nodes, 0 edges/u)).toBeInTheDocument();
  });

  it("opens the command palette globally and blocks disabled commands", async () => {
    render(<App />);

    const metaInput = await openCommandPaletteWithKey("meta");
    expect(metaInput).toHaveFocus();
    fireEvent.keyDown(metaInput, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Command palette" })).not.toBeInTheDocument();
    });

    const ctrlInput = await openCommandPaletteWithKey("ctrl");
    fireEvent.change(ctrlInput, { target: { value: "Accept Plan" } });
    const acceptCommand = (await screen.findAllByText("Accept Plan")).find((element) =>
      element.closest(".command-palette")
    );
    expect(acceptCommand).toBeDefined();
    expect(acceptCommand?.closest("button")).toBeDisabled();
    fireEvent.keyDown(ctrlInput, { key: "Enter" });
    expect(fetch).not.toHaveBeenCalledWith(
      expect.stringMatching(/\/accept-plan$/u),
      expect.any(Object)
    );
  });

  it("configures adapter-backed delivery skills and opt-in push channels", async () => {
    render(<App />);

    fireEvent.click(screen.getByTitle("Add delivery node"));
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
    await planGmailWorkflow();

    fireEvent.click(screen.getByRole("button", { name: /evaluate/i }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /approve/i })).toBeEnabled();
    });
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    expect(await screen.findByText("Frozen approval metadata changed.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Run$/i }));
    expect(await screen.findByText("succeeded")).toBeInTheDocument();
    expect(await screen.findByText("NanoClaw run finished.")).toBeInTheDocument();
  });

  it("plans a prompt through the mocked API and reprompts a node", async () => {
    render(<App />);

    await planWorkflowFromPalette("monitor urgent support messages and send Telegram alerts");

    expect(await screen.findByText("Classify Urgency")).toBeInTheDocument();
    expect(screen.getByText("Approve Alert")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Node Prompt"), {
      target: { value: "Classify incidents with severity and owner routing." }
    });
    fireEvent.click(screen.getByRole("button", { name: /Reprompt Node/i }));

    expect(await screen.findByText("Classify Incidents With Severity And")).toBeInTheDocument();
    expect(screen.getByTestId("approval-diff")).toHaveTextContent("Classify Incidents");
  });

  it("asks clarification questions before planning vague research prompts", async () => {
    render(<App />);

    await planWorkflowFromPalette("i want to have someone research this tasking for me");

    expect(await screen.findByRole("heading", { name: "Clarify First" })).toBeInTheDocument();
    expect(screen.getByText(/What exact topic/u)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Plan With Answers/i })).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/What exact topic/u), {
      target: { value: "OpenAI web search support for workflow research agents" }
    });
    fireEvent.change(screen.getByLabelText(/What should the agent produce/u), {
      target: { value: "A concise sourced recommendation with limitations" }
    });
    fireEvent.click(screen.getByRole("button", { name: /Plan With Answers/i }));

    expect(await screen.findByText("Research Task")).toBeInTheDocument();
    expect(screen.queryByText("Read Gmail Receipts")).not.toBeInTheDocument();
  });

  it("records plan acceptance before executable approval", async () => {
    render(<App />);
    await planGmailWorkflow();

    fireEvent.click(screen.getByRole("button", { name: /Accept Plan/i }));

    expect(await screen.findByText(/Plan accepted:/i)).toBeInTheDocument();
    expect(
      screen.getByText(/draft\.workflow\.gmail-receipts-to-sheets\.(accepted|r1\.plan-accepted)/u)
    ).toBeInTheDocument();
  });

  it("reviews and promotes generated code nodes", async () => {
    render(<App />);

    await planWorkflowFromPalette("scrape a custom public status page and summarize incidents");

    expect(await screen.findByText("Scrape Status Page")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Build Generated Node/i }));

    expect(await screen.findByRole("heading", { name: "Workspace" })).toBeInTheDocument();
    expect(await screen.findByText("Total Tokens")).toBeInTheDocument();
    expect(screen.getByText("2,750")).toBeInTheDocument();
    expect(screen.getByText("Total Cost")).toBeInTheDocument();
    expect(screen.getByText("$0.1600")).toBeInTheDocument();
    expect(screen.getByText("workflow-architect")).toBeInTheDocument();
    expect(screen.getByText(/1,500 tokens .* \$0\.0900/u)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Review Generated Code/i }));
    expect(await screen.findByText("approved")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Promote Skill/i }));
    expect(await screen.findByText("Promoted Scrape Status Page")).toBeInTheDocument();
  });

  it("renders worker job and deployment activation state", async () => {
    render(<App />);
    await planGmailWorkflow();

    fireEvent.click(screen.getByRole("button", { name: /evaluate/i }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /approve/i })).toBeEnabled();
    });
    fireEvent.click(screen.getByRole("button", { name: /approve/i }));
    expect(await screen.findByText("Frozen approval metadata changed.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Deploy$/i }));

    expect(await screen.findByText("Deployment deployed: workflow.bundle")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Deployments" })).toBeInTheDocument();
    expect(screen.getByText(/deployment\.workflow\.bundle/u)).toBeInTheDocument();
    expect(screen.getByText("worker.openclaw-test")).toBeInTheDocument();
  });

  it("renders branch tree controls, merge conflicts, and reuse decisions", async () => {
    render(<App />);
    await planGmailWorkflow();

    await executeCommand("Fork Branch");
    fireEvent.change(screen.getByLabelText("Fork name"), {
      target: { value: "Tax branch" }
    });
    fireEvent.submit(screen.getByLabelText("Fork name").closest("form")!);
    expect(await screen.findByText("Forked Tax branch")).toBeInTheDocument();

    const sourceBranchId = "branch.workflow.gmail-receipts-to-sheets.main";
    await executeCommand("Preview Merge From main");
    expect(await screen.findByText(/Merge preview conflicts/u)).toBeInTheDocument();
    await executeCommand("Use Source For both-edited");
    await executeCommand("Apply Merge Preview");
    expect(
      await screen.findByText(new RegExp(`Merged ${sourceBranchId}`, "u"))
    ).toBeInTheDocument();

    await executeCommand("Refresh Reuse Candidates");
    expect(await screen.findByText("Reuse candidates: 1")).toBeInTheDocument();
    await filterCommand("reuse-with-reeval");
    expect(screen.getByText(/Reuse scrape-status-page: reuse-with-reeval/u)).toBeInTheDocument();
  });

  it("renames, archives, hides, and restores workflow branches", async () => {
    render(<App />);
    await planGmailWorkflow();

    await executeCommand("Fork Branch");
    fireEvent.change(screen.getByLabelText("Fork name"), {
      target: { value: "Archive me" }
    });
    fireEvent.submit(screen.getByLabelText("Fork name").closest("form")!);
    expect(await screen.findByText("Forked Archive me")).toBeInTheDocument();

    await executeCommand("Rename Active Branch");
    fireEvent.change(screen.getByLabelText("Branch name"), {
      target: { value: "Archived plan" }
    });
    fireEvent.submit(screen.getByLabelText("Branch name").closest("form")!);
    expect(await screen.findByText("Renamed branch to Archived plan")).toBeInTheDocument();

    await executeCommand("Archive Active Branch");
    expect(await screen.findByText("Archived Archived plan")).toBeInTheDocument();
    await filterCommand("Plan Workflow");
    expect(screen.getByText("Plan Workflow").closest("button")).toBeDisabled();
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Command palette" }), { key: "Escape" });

    await executeCommand("Restore Active Branch");
    expect(await screen.findByText("Restored Archived plan")).toBeInTheDocument();
    await filterCommand("Plan Workflow");
    expect(screen.getByText("Plan Workflow").closest("button")).toBeEnabled();

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/branches/branch.workflow.gmail-receipts-to-sheets.tax-branch"),
      expect.objectContaining({ method: "PATCH" })
    );
  });
});

async function planGmailWorkflow() {
  await planWorkflowFromPalette("extract transaction details from Gmail receipts into Sheets");
  await screen.findByText("Read Gmail Receipts");
}

async function planWorkflowFromPalette(prompt: string) {
  await executeCommand("Plan Workflow");
  const promptInput = await screen.findByRole("textbox", { name: "Workflow prompt" });
  fireEvent.change(promptInput, { target: { value: prompt } });
  fireEvent.submit(promptInput.closest("form")!);
}

async function executeCommand(query: string) {
  const input = await filterCommand(query);
  fireEvent.keyDown(input, { key: "Enter" });
}

async function filterCommand(query: string) {
  const input = await openCommandPaletteWithKey("meta");
  fireEvent.change(input, { target: { value: query } });
  return input;
}

async function openCommandPaletteWithKey(modifier: "meta" | "ctrl") {
  const event = new KeyboardEvent("keydown", {
    key: "p",
    metaKey: modifier === "meta",
    ctrlKey: modifier === "ctrl",
    bubbles: true,
    cancelable: true
  });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  const input = await screen.findByRole("textbox", { name: "Command palette" });
  await waitFor(() => expect(input).toHaveFocus());
  return input;
}

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

  if (url.endsWith("/decision-traces/export")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    const traces = mockDecisionTraces(workflow, workflow.nodes[0]?.id ?? "manual-run");
    return jsonResponse({
      ok: true,
      export: {
        id: `decision-trace-export.${workflow.id}`,
        workflowId: workflow.id,
        exportedAt: "2026-05-18T01:00:00.000Z",
        format: "jsonl",
        redacted: true,
        lineCount: traces.length,
        records: traces,
        evalExamples: traces.map((trace) => ({
          id: `eval-example.${trace.id}`,
          traceId: trace.id,
          workflowId: trace.workflowId,
          nodeId: trace.nodeId,
          kind: trace.kind,
          createdAt: trace.createdAt,
          input: { inputSummary: trace.events[0]?.inputSummary ?? "" },
          actualDecision: trace.events[0]?.selectedAction ?? "unknown",
          outcome: "unknown",
          artifactRefs: []
        }))
      },
      jsonl: traces.map((trace) => JSON.stringify(trace)).join("\n")
    });
  }

  if (url.includes("/nodes/") && url.endsWith("/decision-traces")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    const nodeId = String(url.split("/nodes/")[1]?.split("/")[0] ?? workflow.nodes[0]?.id);
    return jsonResponse({
      ok: true,
      traces: mockDecisionTraces(workflow, decodeURIComponent(nodeId))
    });
  }

  if (url.endsWith("/decision-traces")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    return jsonResponse({
      ok: true,
      traces: workflow.nodes.flatMap((node) => mockDecisionTraces(workflow, node.id))
    });
  }

  if (url.endsWith("/branches") && (!init?.method || init.method === "GET")) {
    return jsonResponse({ ok: true, branches: mockBranches });
  }

  if (url.endsWith("/branches") && init?.method === "POST") {
    const workflowId = workflowIdFromBranchUrl(url);
    const branch = mockBranch(
      `branch.${workflowId}.tax-branch`,
      String(body.name ?? "Tax branch"),
      String(body.fromBranchId ?? mockBranches[0]?.id ?? "")
    );
    mockBranches = [...mockBranches, branch];
    return jsonResponse(
      {
        ok: true,
        branch,
        draftRevision: draftRevision(
          mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture,
          "branch-fork"
        )
      },
      201
    );
  }

  if (url.includes("/branches/") && url.endsWith("/reuse-candidates")) {
    const branchId = branchIdFromUrl(url);
    return jsonResponse({
      ok: true,
      decisions: [
        {
          id: `reuse.${branchId}.scrape-status-page`,
          workflowId: mockCurrentWorkflow?.id ?? gmailReceiptsToSheetsWorkflowFixture.id,
          branchId,
          nodeId: "scrape-status-page",
          status: "reuse-with-reeval",
          createdAt: "2026-05-18T01:00:00.000Z",
          sourceBranchId: "branch.workflow.gmail-receipts-to-sheets.main",
          sourceDraftRevisionId: "draft.workflow.gmail-receipts-to-sheets.r1.0",
          sourceEvalReportId: "eval-report.codegen.test",
          signature: mockGeneratedModuleSignature(),
          gates: [],
          reason: "Generated module signature matches.",
          artifacts: []
        }
      ]
    });
  }

  if (url.includes("/branches/") && url.endsWith("/merge-preview")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    return jsonResponse({
      ok: true,
      preview: {
        id: `merge-preview.${workflow.id}`,
        workflowId: workflow.id,
        sourceBranchId: branchIdFromUrl(url),
        targetBranchId: String(body.targetBranchId),
        mode: body.mode ?? "merge",
        status: "conflicts",
        createdAt: "2026-05-18T01:00:00.000Z",
        baseDraftRevisionId: `draft.${workflow.id}.base`,
        sourceHeadDraftRevisionId: `draft.${workflow.id}.source`,
        targetHeadDraftRevisionId: `draft.${workflow.id}.target`,
        graphDiff: {
          id: `graphdiff.${workflow.id}.merge`,
          workflowId: workflow.id,
          baseRevision: workflow.revision,
          editedRevision: workflow.revision,
          createdAt: "2026-05-18T01:00:00.000Z",
          summary: ["node.edited: 1"],
          changes: [],
          validation: { ok: true, workflow }
        },
        conflicts: [
          {
            id: "conflict.node.both-edited.read-gmail-receipts",
            kind: "both-edited",
            elementKind: "node",
            elementId: "read-gmail-receipts",
            path: ["nodes", "read-gmail-receipts"],
            message: "Both branches edited Read Gmail Receipts."
          }
        ],
        summary: ["Merge has 1 conflict."],
        validation: { ok: true, workflow }
      }
    });
  }

  if (url.includes("/branches/") && url.endsWith("/merge")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    const targetBranch =
      mockBranches.find((branch) => branch.id === body.targetBranchId) ?? mockBranches[0]!;
    return jsonResponse({
      ok: true,
      merge: {
        id: `merge.${workflow.id}`,
        workflowId: workflow.id,
        sourceBranchId: branchIdFromUrl(url),
        targetBranchId: targetBranch.id,
        mode: body.mode ?? "merge",
        status: "applied",
        createdAt: "2026-05-18T01:00:00.000Z",
        appliedAt: "2026-05-18T01:00:00.000Z",
        appliedBy: body.appliedBy,
        baseDraftRevisionId: `draft.${workflow.id}.base`,
        sourceHeadDraftRevisionId: `draft.${workflow.id}.source`,
        targetHeadDraftRevisionId: `draft.${workflow.id}.target`,
        graphDiff: {
          id: `graphdiff.${workflow.id}.merge`,
          workflowId: workflow.id,
          baseRevision: workflow.revision,
          editedRevision: workflow.revision,
          createdAt: "2026-05-18T01:00:00.000Z",
          summary: ["node.edited: 1"],
          changes: [],
          validation: { ok: true, workflow }
        },
        mergedDraftRevisionId: `draft.${workflow.id}.merged`,
        conflicts: [],
        resolutions: body.resolutions,
        summary: ["Merged branch."],
        validation: { ok: true, workflow }
      },
      branch: targetBranch,
      draftRevision: draftRevision(workflow, "branch-merge"),
      workflow,
      validation: { ok: true, workflow }
    });
  }

  if (url.includes("/branches/") && url.endsWith("/plan")) {
    const prompt = String(body.prompt ?? gmailReceiptsToSheetsWorkflowFixture.prompt);
    const workflow = prompt.includes("scrape")
      ? createCodegenWorkflow(prompt)
      : createAlertWorkflow(prompt);
    mockCurrentWorkflow = workflow;
    const branch = mockBranches.find((candidate) => candidate.id === branchIdFromUrl(url))!;
    return jsonResponse({
      ok: true,
      workflow,
      draftRevision: draftRevision(workflow, "branch-plan"),
      validation: { ok: true, workflow },
      route: taskRouteForWorkflow(workflow),
      branch,
      promptTurn: mockPromptTurn(workflow.id, branch.id, "plan", prompt)
    });
  }

  if (url.includes("/branches/") && url.endsWith("/reprompt-node")) {
    const workflow = body.currentWorkflow as WorkflowSpec;
    const branch = mockBranches.find((candidate) => candidate.id === branchIdFromUrl(url))!;
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
    mockCurrentWorkflow = nextWorkflow;
    return jsonResponse({
      ok: true,
      workflow: nextWorkflow,
      draftRevision: draftRevision(nextWorkflow, "branch-reprompt"),
      validation: { ok: true, workflow: nextWorkflow },
      before,
      after,
      diff,
      branch,
      promptTurn: mockPromptTurn(workflow.id, branch.id, "reprompt", String(body.prompt))
    });
  }

  if (url.includes("/branches/") && url.endsWith("/accept-plan")) {
    const workflow = body.workflow as WorkflowSpec;
    return jsonResponse({
      ok: true,
      workflowId: workflow.id,
      draftRevisionId: `draft.${workflow.id}.branch.accepted`,
      workflow,
      draftRevision: draftRevision(workflow, "plan-accepted"),
      validation: { ok: true, workflow }
    });
  }

  if (url.includes("/branches/") && init?.method === "PATCH") {
    const branchId = branchIdFromUrl(url);
    const branch = mockBranches.find((candidate) => candidate.id === branchId);
    if (branch) {
      const updated: WorkflowBranch = {
        ...branch,
        name: typeof body.name === "string" ? body.name : branch.name,
        status:
          body.status === "active" || body.status === "archived" ? body.status : branch.status,
        updatedAt: "2026-05-18T01:05:00.000Z"
      };
      mockBranches = mockBranches.map((candidate) =>
        candidate.id === updated.id ? updated : candidate
      );
      return jsonResponse({ ok: true, branch: updated });
    }
  }

  if (url.includes("/branches/") && (!init?.method || init.method === "GET")) {
    const workflow = mockCurrentWorkflow ?? gmailReceiptsToSheetsWorkflowFixture;
    const branch = mockBranches.find((candidate) => candidate.id === branchIdFromUrl(url));
    if (branch) {
      return jsonResponse({
        ok: true,
        branch,
        headDraftRevision: draftRevision(workflow, "branch-head"),
        promptTurns: [mockPromptTurn(workflow.id, branch.id, "plan", workflow.prompt)]
      });
    }
  }

  if (url.endsWith("/plan")) {
    const prompt = String(body.prompt ?? gmailReceiptsToSheetsWorkflowFixture.prompt);
    if (prompt.includes("research this tasking") && !Array.isArray(body.clarificationAnswers)) {
      return jsonResponse({
        ok: true,
        status: "clarification-required",
        clarification: mockClarification(prompt),
        route: {
          route: "agentic",
          rationale: "Prompt asks for research but needs more detail.",
          requiredModel: {
            mode: "live",
            role: "agentic-node-designer",
            provider: "openai",
            model: "test-model",
            retryBudget: { maxAttempts: 2, maxCostUsd: 2 }
          },
          expectedNodeKinds: ["trigger", "skill", "approval", "delivery"],
          dockerSandboxRequired: true,
          draftTestsRequired: true,
          productionDeterministic: false,
          modelInvocations: []
        }
      });
    }
    const workflow: WorkflowSpec =
      prompt.includes("urgent") || prompt.includes("Telegram")
        ? createAlertWorkflow(prompt)
        : prompt.includes("research this tasking")
          ? createResearchWorkflow(prompt)
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

  if (url.endsWith("/accept-plan")) {
    const workflow = body.workflow as WorkflowSpec;
    return jsonResponse({
      ok: true,
      workflowId: workflow.id,
      draftRevisionId: `draft.${workflow.id}.accepted`,
      workflow,
      draftRevision: {
        id: `draft.${workflow.id}.accepted`,
        workflowId: workflow.id,
        revision: workflow.revision,
        workflow,
        validation: { ok: true, workflow },
        source: "plan-accepted",
        createdAt: "2026-05-18T01:00:00.000Z"
      },
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

  if (url.includes("/codegen/") && url.endsWith("/build")) {
    const workflow = mockCurrentWorkflow ?? scheduledScrapingWorkflowFixture;
    mockCurrentWorkflow = workflow;
    const job = mockJob("build.codegen-node", workflow.id, "succeeded");
    return jsonResponse({
      ok: true,
      workflow,
      draftRevision: draftRevision(workflow, "validate"),
      validation: { ok: true, workflow },
      job,
      workspace: mockWorkspace(workflow.id, job.id),
      agentRuns: mockAgentRuns(workflow.id, job.id),
      artifacts: [],
      testReport: { id: "test-report.codegen.scrape-status-page", status: "passed" },
      evalReport: { id: "eval-report.codegen.scrape-status-page", status: "passed" }
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

function mockBranch(id: string, name: string, parentBranchId?: string): WorkflowBranch {
  return {
    id,
    workflowId: "workflow.gmail-receipts-to-sheets",
    name,
    status: "active",
    createdAt: "2026-05-18T01:00:00.000Z",
    updatedAt: "2026-05-18T01:00:00.000Z",
    createdBy: "owner@example.com",
    ...(parentBranchId ? { parentBranchId } : {}),
    baseDraftRevisionId: "draft.workflow.gmail-receipts-to-sheets.r1.0",
    headDraftRevisionId: "draft.workflow.gmail-receipts-to-sheets.r1.0",
    metadata: {}
  };
}

function mockPromptTurn(
  workflowId: string,
  branchId: string,
  source: "plan" | "reprompt" | "edit" | "merge" | "cherry-pick",
  prompt: string
) {
  return {
    id: `prompt-turn.${branchId}.${source}`,
    workflowId,
    branchId,
    source,
    prompt,
    actor: "owner@example.com",
    createdAt: "2026-05-18T01:00:00.000Z",
    baseDraftRevisionId: "draft.workflow.gmail-receipts-to-sheets.r1.0",
    resultingDraftRevisionId: "draft.workflow.gmail-receipts-to-sheets.r2.1"
  };
}

function branchIdFromUrl(url: string): string {
  const match = /\/branches\/([^/?]+)/u.exec(url);
  return decodeURIComponent(match?.[1] ?? "branch.workflow.gmail-receipts-to-sheets.main");
}

function workflowIdFromBranchUrl(url: string): string {
  const match = /\/api\/workflows\/([^/?]+)\/branches/u.exec(url);
  return decodeURIComponent(match?.[1] ?? "workflow.gmail-receipts-to-sheets");
}

function mockGeneratedModuleSignature() {
  return {
    promptHash: `sha256:${"a".repeat(64)}`,
    inputSchemaHash: `sha256:${"b".repeat(64)}`,
    outputSchemaHash: `sha256:${"c".repeat(64)}`,
    runtimeHash: `sha256:${"d".repeat(64)}`,
    sandboxHash: `sha256:${"e".repeat(64)}`,
    dependencyManifestHash: `sha256:${"f".repeat(64)}`,
    replaySeed: "fixture",
    artifactHash: `sha256:${"1".repeat(64)}`
  };
}

function mockClarification(prompt: string) {
  return {
    id: "clarify.test",
    prompt,
    reason: "The prompt needs a concrete topic and output.",
    createdAt: "2026-05-18T01:00:00.000Z",
    questions: [
      {
        id: "research-topic",
        question: "What exact topic, entity, or decision should the research focus on?",
        required: true
      },
      {
        id: "desired-output",
        question: "What should the agent produce when it is done?",
        required: true
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

function createResearchWorkflow(prompt: string): WorkflowSpec {
  return createWorkflowSpec({
    id: "workflow.research-tasking",
    name: "Research Tasking",
    prompt,
    nodes: [
      createWorkflowNode({
        id: "manual-research-request",
        kind: "trigger",
        label: "Research Request"
      }),
      createWorkflowNode({
        id: "research-task",
        kind: "skill",
        label: "Research Task",
        description: "Researches the clarified request with bounded web search.",
        config: {
          skillMode: "agentic"
        },
        agentic: {
          tools: ["web-search"],
          memoryScope: "workspace",
          stopConditions: ["summary-ready"],
          humanApprovalBoundaries: ["Before delivery."],
          networkPolicy: "declared",
          allowedHosts: ["*"],
          secretRefs: [],
          evalContract: {
            requiredFields: ["summary", "sources", "limitations"]
          },
          budget: {
            maxIterations: 3,
            maxWallClockSeconds: 300,
            maxModelCostUsd: 2,
            maxDockerRuntimeSeconds: 120,
            maxRetries: 1
          }
        }
      }),
      createWorkflowNode({
        id: "approve-research-summary",
        kind: "approval",
        label: "Approve Research Summary"
      }),
      createWorkflowNode({
        id: "deliver-research-summary",
        kind: "delivery",
        label: "Deliver Research Summary",
        inputs: {
          approved: { type: "object", additionalProperties: true }
        }
      })
    ],
    edges: [
      createWorkflowEdge({
        sourceNodeId: "manual-research-request",
        sourcePort: "request",
        targetNodeId: "research-task",
        targetPort: "request"
      }),
      createWorkflowEdge({
        sourceNodeId: "research-task",
        sourcePort: "result",
        targetNodeId: "approve-research-summary",
        targetPort: "input"
      }),
      createWorkflowEdge({
        sourceNodeId: "approve-research-summary",
        sourcePort: "approved",
        targetNodeId: "deliver-research-summary",
        targetPort: "approved"
      })
    ]
  });
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

function mockDecisionTraces(
  workflow: WorkflowSpec,
  nodeId: string
): readonly WorkflowNodeDecisionTrace[] {
  return [
    {
      id: `trace.${workflow.id}.${nodeId}.planner`,
      workflowId: workflow.id,
      nodeId,
      revisionId: `draft.${workflow.id}.r${workflow.revision}`,
      kind: "planner.node-created",
      source: "planner",
      createdAt: "2026-05-18T01:00:00.000Z",
      updatedAt: "2026-05-18T01:00:00.000Z",
      status: "recorded",
      events: [
        {
          id: `trace.${workflow.id}.${nodeId}.planner.event`,
          traceId: `trace.${workflow.id}.${nodeId}.planner`,
          workflowId: workflow.id,
          nodeId,
          revisionId: `draft.${workflow.id}.r${workflow.revision}`,
          kind: "planner.node-created",
          role: "planner",
          createdAt: "2026-05-18T01:00:00.000Z",
          summary: "Created planned node.",
          rationale: "Planner selected this node for the requested workflow.",
          alternativesConsidered: ["Use an existing skill.", "Generate a custom node."],
          selectedAction: "Use planned node.",
          inputSummary: workflow.prompt,
          promptHash: `sha256:${"c".repeat(64)}`,
          promptExcerpt: workflow.prompt,
          route: "deterministic",
          provider: "openai",
          model: "gpt-4.1",
          modelInvocationIds: [],
          affectedNodeIds: [nodeId],
          affectedEdgeIds: [],
          constraints: {
            nodeKind: workflow.nodes.find((node) => node.id === nodeId)?.kind ?? "skill"
          },
          outputArtifactRefs: [],
          evalOutcome: "not-run",
          metadata: {}
        }
      ]
    }
  ];
}

function mockWorkspace(workflowId: string, jobId: string) {
  return {
    id: "workspace.codegen.scrape-status-page",
    jobId,
    workflowId,
    rootPath: "/tmp/kelpclaw/workspaces/workspace.codegen.scrape-status-page",
    createdAt: "2026-05-18T01:00:00.000Z",
    updatedAt: "2026-05-18T01:00:00.000Z",
    mountedAgents: ["workflow-architect", "coder"],
    mounts: [
      {
        role: "workflow-architect",
        path: "roles/workflow-architect",
        mode: "rw"
      },
      {
        role: "coder",
        path: "roles/coder",
        mode: "rw"
      }
    ],
    filesCreated: ["generated/scrape-status-page.ts"],
    fileHashes: [
      {
        path: "generated/scrape-status-page.ts",
        checksum: `sha256:${"b".repeat(64)}`
      }
    ],
    artifactsProduced: [],
    logs: ["Generated node build passed."],
    logPaths: ["logs/build.log"],
    testReports: ["test-report.codegen.scrape-status-page"],
    retentionPolicy: "ephemeral",
    retentionStatus: "active"
  };
}

function mockAgentRuns(workflowId: string, jobId: string) {
  return [
    {
      id: `agent.${jobId}.workflow-architect.scrape-status-page`,
      workflowId,
      nodeId: "scrape-status-page",
      jobId,
      role: "workflow-architect",
      status: "succeeded",
      startedAt: "2026-05-18T01:00:00.000Z",
      finishedAt: "2026-05-18T01:00:01.000Z",
      inputSummary: "Design generated node.",
      outputArtifactRefs: [],
      modelProvider: "openai",
      model: "gpt-4.1",
      inputTokens: 1200,
      outputTokens: 300,
      totalTokens: 1500,
      costUsd: 0.09,
      modelInvocations: [
        {
          id: "model-invocation.workflow-architect",
          timestamp: "2026-05-18T01:00:00.000Z",
          provider: "openai",
          model: "gpt-4.1",
          role: "workflow-architect",
          rationale: "Design generated node.",
          deterministicExpected: false,
          retryBudget: { maxAttempts: 1, maxCostUsd: 1 },
          correlationId: jobId,
          inputTokens: 1200,
          outputTokens: 300,
          totalTokens: 1500,
          costUsd: 0.09,
          outputArtifactRefs: []
        }
      ]
    },
    {
      id: `agent.${jobId}.coder.scrape-status-page`,
      workflowId,
      nodeId: "scrape-status-page",
      jobId,
      role: "coder",
      status: "succeeded",
      startedAt: "2026-05-18T01:00:01.000Z",
      finishedAt: "2026-05-18T01:00:02.000Z",
      inputSummary: "Implement generated node.",
      outputArtifactRefs: [],
      modelProvider: "openai",
      model: "gpt-4.1",
      inputTokens: 800,
      outputTokens: 450,
      totalTokens: 1250,
      costUsd: 0.07,
      modelInvocations: [
        {
          id: "model-invocation.coder",
          timestamp: "2026-05-18T01:00:01.000Z",
          provider: "openai",
          model: "gpt-4.1",
          role: "coder",
          rationale: "Implement generated node.",
          deterministicExpected: false,
          retryBudget: { maxAttempts: 1, maxCostUsd: 1 },
          correlationId: jobId,
          inputTokens: 800,
          outputTokens: 450,
          totalTokens: 1250,
          costUsd: 0.07,
          outputArtifactRefs: []
        }
      ]
    }
  ];
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
  const agentic = workflow.nodes.some((node) => node.agentic);

  return {
    route: codegen ? "codegen" : agentic ? "agentic" : "adapter",
    rationale: codegen
      ? "Prompt requires generated node artifacts."
      : agentic
        ? "Prompt asks for bounded research."
        : "Prompt uses existing adapter workflow templates.",
    requiredModel: {
      mode: codegen || agentic ? "live" : "none",
      role: codegen ? "workflow-architect" : agentic ? "agentic-node-designer" : "classifier",
      provider: codegen || agentic ? "anthropic" : undefined,
      model: codegen || agentic ? "test-model" : undefined,
      retryBudget: {
        maxAttempts: 1,
        maxCostUsd: agentic ? 2 : codegen ? 1 : 0
      }
    },
    expectedNodeKinds: codegen
      ? ["trigger", "codegen", "transform", "delivery"]
      : agentic
        ? ["trigger", "skill", "approval", "delivery"]
        : ["trigger", "skill", "transform", "delivery"],
    dockerSandboxRequired: codegen || agentic,
    draftTestsRequired: codegen || agentic,
    productionDeterministic: !agentic,
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
