import type { CompiledDag, DagExecutionResult, NodeExecutionResult, NodeRunner } from "./types.js";

export async function executeCompiledDag(
  dag: CompiledDag,
  runner: NodeRunner
): Promise<DagExecutionResult> {
  const nodeResults: NodeExecutionResult[] = [];

  for (const nodeId of dag.order) {
    const node = dag.nodes.get(nodeId);
    if (!node) {
      throw new Error(`Compiled DAG order referenced unknown node '${nodeId}'.`);
    }

    const result = await runner.run(node);
    nodeResults.push(result);
    if (result.status === "failed") {
      return {
        workflowId: dag.workflowId,
        status: "failed",
        nodeResults
      };
    }
  }

  return {
    workflowId: dag.workflowId,
    status: "succeeded",
    nodeResults
  };
}
