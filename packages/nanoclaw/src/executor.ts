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
      return createExecutionResult(dag, nodeResults, "failed");
    }
  }

  return createExecutionResult(dag, nodeResults, "succeeded");
}

function createExecutionResult(
  dag: CompiledDag,
  nodeResults: readonly NodeExecutionResult[],
  status: DagExecutionResult["status"]
): DagExecutionResult {
  const startedAt = nodeResults[0]?.startedAt ?? dag.approval.approvedAt;
  const finishedAt = nodeResults.at(-1)?.finishedAt ?? startedAt;

  return {
    id: `execution.${dag.workflowId}.r${dag.revision}`,
    workflowId: dag.workflowId,
    revision: dag.revision,
    status,
    startedAt,
    finishedAt,
    nodeResults,
    deterministic: true
  };
}
