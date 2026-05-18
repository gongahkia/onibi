import type { CompiledDagNode, NodeExecutionResult, NodeRunner } from "./types.js";

export class MockNodeRunner implements NodeRunner {
  readonly visitedNodeIds: string[] = [];
  private readonly failingNodeIds: ReadonlySet<string>;

  public constructor(options: { readonly failingNodeIds?: readonly string[] } = {}) {
    this.failingNodeIds = new Set(options.failingNodeIds ?? []);
  }

  public async run(node: CompiledDagNode): Promise<NodeExecutionResult> {
    this.visitedNodeIds.push(node.id);
    const now = "2026-05-18T00:00:00.000Z";

    return {
      nodeId: node.id,
      status: this.failingNodeIds.has(node.id) ? "failed" : "succeeded",
      startedAt: now,
      finishedAt: now,
      output: {
        mocked: true
      }
    };
  }
}
