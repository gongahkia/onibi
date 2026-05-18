import { describe, expect, it } from "vitest";
import {
  WorkflowValidationError,
  cyclicWorkflowFixture,
  staticContentWorkflowFixture
} from "@kelpclaw/workflow-spec";
import {
  DockerNodeRunner,
  MockNodeRunner,
  compileWorkflowDag,
  executeCompiledDag
} from "../src/index.js";

describe("nanoclaw dag runtime", () => {
  it("compiles deterministic topological order", () => {
    const dag = compileWorkflowDag(staticContentWorkflowFixture);

    expect(dag.order).toEqual(["collect-brief", "draft-copy", "owner-approval", "send-email"]);
    expect(dag.nodes.get("draft-copy")?.dependencies).toEqual(["collect-brief"]);
  });

  it("rejects cyclic workflow specs before execution", () => {
    expect(() => compileWorkflowDag(cyclicWorkflowFixture)).toThrow(WorkflowValidationError);
  });

  it("executes compiled dags through a mock runner in order", async () => {
    const dag = compileWorkflowDag(staticContentWorkflowFixture);
    const runner = new MockNodeRunner();
    const result = await executeCompiledDag(dag, runner);

    expect(result.status).toBe("succeeded");
    expect(runner.visitedNodeIds).toEqual(dag.order);
  });

  it("stops execution when a node fails", async () => {
    const dag = compileWorkflowDag(staticContentWorkflowFixture);
    const runner = new MockNodeRunner({ failingNodeIds: ["draft-copy"] });
    const result = await executeCompiledDag(dag, runner);

    expect(result.status).toBe("failed");
    expect(runner.visitedNodeIds).toEqual(["collect-brief", "draft-copy"]);
  });

  it("constructs Docker-per-node commands without executing them", () => {
    const dag = compileWorkflowDag(staticContentWorkflowFixture);
    const runner = new DockerNodeRunner({ hostWorkspace: "/tmp/kelpclaw" });
    const command = runner.buildCommand(dag.nodes.get("collect-brief")!);

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
      "collect-brief.js"
    ]);
  });
});
