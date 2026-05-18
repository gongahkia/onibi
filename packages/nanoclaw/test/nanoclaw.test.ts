import { describe, expect, it } from "vitest";
import {
  WorkflowValidationError,
  approvedGmailReceiptsToSheetsWorkflowFixture,
  cyclicWorkflowFixture,
  gmailReceiptsToSheetsWorkflowFixture
} from "@kelpclaw/workflow-spec";
import {
  DockerNodeRunner,
  MockNodeRunner,
  compileWorkflowDag,
  executeCompiledDag
} from "../src/index.js";

describe("nanoclaw dag runtime", () => {
  it("compiles only approved workflow revisions", () => {
    expect(() => compileWorkflowDag(gmailReceiptsToSheetsWorkflowFixture)).toThrow(
      WorkflowValidationError
    );

    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);

    expect(dag.order).toEqual([
      "manual-trigger",
      "read-gmail-receipts",
      "normalize-receipts",
      "append-sheet-rows"
    ]);
    expect(dag.nodes.get("normalize-receipts")?.dependencies).toEqual(["read-gmail-receipts"]);
  });

  it("rejects cyclic workflow specs before execution", () => {
    expect(() => compileWorkflowDag(cyclicWorkflowFixture)).toThrow(WorkflowValidationError);
  });

  it("executes compiled dags through a mock runner in approved order", async () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new MockNodeRunner();
    const result = await executeCompiledDag(dag, runner);

    expect(result).toMatchObject({
      id: "execution.workflow.gmail-receipts-to-sheets.r1",
      workflowId: "workflow.gmail-receipts-to-sheets",
      revision: 1,
      status: "succeeded",
      deterministic: true
    });
    expect(runner.visitedNodeIds).toEqual(dag.order);
  });

  it("stops execution when a node fails", async () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new MockNodeRunner({ failingNodeIds: ["read-gmail-receipts"] });
    const result = await executeCompiledDag(dag, runner);

    expect(result.status).toBe("failed");
    expect(runner.visitedNodeIds).toEqual(["manual-trigger", "read-gmail-receipts"]);
  });

  it("constructs Docker-per-node commands without executing them", () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new DockerNodeRunner({ hostWorkspace: "/tmp/kelpclaw" });
    const command = runner.buildCommand(dag.nodes.get("manual-trigger")!);

    expect(command).toEqual([
      "docker",
      "run",
      "--rm",
      "--network",
      "none",
      "--volume",
      "/tmp/kelpclaw:/workspace",
      "--workdir",
      "/workspace",
      "node:20-alpine",
      "node",
      "/workspace/run-node.js"
    ]);
  });
});
