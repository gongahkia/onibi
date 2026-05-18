import {
  NodePayloadValidationError,
  assertValidNodeInput,
  assertValidNodeOutput
} from "./payload-validation.js";
import type {
  CompiledDag,
  CompiledDagNode,
  DagExecutionResult,
  NodeExecutionResult,
  NodeInputPayload,
  NodeRunner
} from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export interface ExecuteCompiledDagOptions {
  readonly signal?: AbortSignal | undefined;
}

export async function executeCompiledDag(
  dag: CompiledDag,
  runner: NodeRunner,
  options: ExecuteCompiledDagOptions = {}
): Promise<DagExecutionResult> {
  const nodeResults: NodeExecutionResult[] = [];
  const nodeOutputs = new Map<string, JsonRecord>();

  for (const nodeId of dag.order) {
    const node = dag.nodes.get(nodeId);
    if (!node) {
      throw new Error(`Compiled DAG order referenced unknown node '${nodeId}'.`);
    }

    const startedAt = new Date().toISOString();
    const input = resolveNodeInputs(node, nodeOutputs);
    const inputPayload = createNodeInputPayload(dag, node, input, 1);
    const inputValidation = validateNodeInput(node, input, startedAt);
    if (inputValidation) {
      nodeResults.push(inputValidation);
      return createExecutionResult(dag, nodeResults, "failed");
    }

    const runnerResult = await runner.run(node, {
      dag,
      input,
      inputPayload,
      attempt: 1,
      signal: options.signal
    });
    const result: NodeExecutionResult = {
      nodeId: node.id,
      status: runnerResult.status,
      startedAt,
      finishedAt: new Date().toISOString(),
      input,
      output: runnerResult.output,
      error: runnerResult.error,
      metadata: {
        ...(runnerResult.exitCode === undefined ? {} : { exitCode: runnerResult.exitCode }),
        ...(runnerResult.metadata ?? {})
      }
    };

    const outputValidation =
      result.status === "succeeded" ? validateNodeOutput(node, result, startedAt) : null;
    const finalResult = outputValidation ?? result;
    nodeResults.push(finalResult);
    if (finalResult.status === "succeeded") {
      nodeOutputs.set(node.id, finalResult.output);
    }
    if (finalResult.status === "failed") {
      return createExecutionResult(dag, nodeResults, "failed");
    }
  }

  return createExecutionResult(dag, nodeResults, "succeeded");
}

function resolveNodeInputs(
  node: CompiledDagNode,
  nodeOutputs: ReadonlyMap<string, JsonRecord>
): JsonRecord {
  const input: JsonRecord = {};

  for (const binding of node.inputBindings) {
    const sourceOutput = nodeOutputs.get(binding.source.nodeId);
    if (!sourceOutput || !(binding.source.port in sourceOutput)) {
      throw new Error(
        `Node '${node.id}' input '${binding.inputPort}' depends on missing output '${binding.source.nodeId}.${binding.source.port}'.`
      );
    }
    input[binding.inputPort] = sourceOutput[binding.source.port] as JsonValue;
  }

  return input;
}

function createNodeInputPayload(
  dag: CompiledDag,
  node: CompiledDagNode,
  input: JsonRecord,
  attempt: number
): NodeInputPayload {
  return {
    workflowId: dag.workflowId,
    revision: dag.revision,
    nodeId: node.id,
    attempt,
    inputs: input,
    config: node.config,
    metadata: {
      dagHash: dag.dagHash,
      dependencies: [...node.dependencies],
      inputBindings: node.inputBindings.map((binding) => ({
        edgeId: binding.edgeId,
        inputPort: binding.inputPort,
        source: {
          nodeId: binding.source.nodeId,
          port: binding.source.port
        }
      }))
    }
  };
}

function validateNodeInput(
  node: CompiledDagNode,
  input: JsonRecord,
  startedAt: string
): NodeExecutionResult | null {
  try {
    assertValidNodeInput(node, input);
    return null;
  } catch (error) {
    if (!(error instanceof NodePayloadValidationError)) {
      throw error;
    }

    return {
      nodeId: node.id,
      status: "failed",
      startedAt,
      finishedAt: new Date().toISOString(),
      input,
      output: {
        validationErrors: error.issues.map((issue) => ({ ...issue }))
      },
      error: error.message,
      metadata: {
        validationDirection: "input"
      }
    };
  }
}

function validateNodeOutput(
  node: CompiledDagNode,
  result: NodeExecutionResult,
  startedAt: string
): NodeExecutionResult | null {
  try {
    assertValidNodeOutput(node, result.output);
    return null;
  } catch (error) {
    if (!(error instanceof NodePayloadValidationError)) {
      throw error;
    }

    return {
      ...result,
      status: "failed",
      startedAt,
      finishedAt: new Date().toISOString(),
      error: error.message,
      metadata: {
        ...(result.metadata ?? {}),
        validationDirection: "output",
        validationErrors: error.issues.map((issue) => ({ ...issue }))
      }
    };
  }
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
