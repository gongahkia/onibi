import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { LocalCodegenArtifactStore } from "@kelpclaw/codegen";
import {
  NodePayloadValidationError,
  assertValidNodeInput,
  assertValidNodeOutput
} from "./payload-validation.js";
import { persistRunManifest } from "./replay.js";
import { createExecutionWorkspace, prepareNodeWorkspace } from "./workspace.js";
import type {
  CompiledDag,
  CompiledDagNode,
  DagExecutionResult,
  ExecutionWorkspace,
  NodeExecutionResult,
  NodeInputPayload,
  NodeWorkspace,
  NodeRunner
} from "./types.js";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type {
  JsonRecord,
  JsonValue,
  WorkflowNodeExecutionAttempt,
  WorkflowRunEvent
} from "@kelpclaw/workflow-spec";

export interface ExecuteCompiledDagOptions {
  readonly runId?: string | undefined;
  readonly workspaceRoot?: string | undefined;
  readonly codegenArtifactStore?: CodegenArtifactStore | undefined;
  readonly onEvent?: ((event: WorkflowRunEvent) => void) | undefined;
  readonly signal?: AbortSignal | undefined;
}

export async function executeCompiledDag(
  dag: CompiledDag,
  runner: NodeRunner,
  options: ExecuteCompiledDagOptions = {}
): Promise<DagExecutionResult> {
  const nodeResults: NodeExecutionResult[] = [];
  const nodeOutputs = new Map<string, JsonRecord>();
  const runWorkspace = await createExecutionWorkspace(dag, options);
  const eventStream = createRunEventStream(options.onEvent);
  eventStream.emit("info", "NanoClaw run started.");

  for (const nodeId of dag.order) {
    const node = dag.nodes.get(nodeId);
    if (!node) {
      throw new Error(`Compiled DAG order referenced unknown node '${nodeId}'.`);
    }

    const input = resolveNodeInputs(node, nodeOutputs);
    const inputValidation = validateNodeInput(node, input, new Date().toISOString());
    if (inputValidation) {
      nodeResults.push(inputValidation);
      eventStream.emit("error", `Node '${inputValidation.nodeId}' failed.`, inputValidation.nodeId);
      const results = emitSkippedResults(
        withSkippedResults(dag, nodeResults, node.id),
        eventStream
      );
      return finishExecution(
        createExecutionResult(dag, results, "failed", runWorkspace.runDir, eventStream),
        eventStream
      );
    }

    const finalResult = await executeNodeWithAttempts(
      dag,
      node,
      input,
      runWorkspace,
      runner,
      options
    );
    nodeResults.push(finalResult);
    eventStream.emit(
      finalResult.status === "failed" ? "error" : "info",
      `Node '${finalResult.nodeId}' ${finalResult.status}.`,
      finalResult.nodeId
    );
    if (finalResult.status === "succeeded") {
      nodeOutputs.set(node.id, finalResult.output);
    }
    if (finalResult.status === "failed") {
      const results = emitSkippedResults(
        withSkippedResults(dag, nodeResults, node.id),
        eventStream
      );
      return finishExecution(
        createExecutionResult(dag, results, "failed", runWorkspace.runDir, eventStream),
        eventStream
      );
    }
  }

  return finishExecution(
    createExecutionResult(dag, nodeResults, "succeeded", runWorkspace.runDir, eventStream),
    eventStream
  );
}

async function executeNodeWithAttempts(
  dag: CompiledDag,
  node: CompiledDagNode,
  input: JsonRecord,
  runWorkspace: ExecutionWorkspace,
  runner: NodeRunner,
  options: ExecuteCompiledDagOptions
): Promise<NodeExecutionResult> {
  const maxAttempts = Math.max(1, node.runtime.retry.maxAttempts);
  const attempts: WorkflowNodeExecutionAttempt[] = [];
  let lastResult: NodeExecutionResult | null = null;

  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    const startedAt = new Date().toISOString();
    const inputPayload = createNodeInputPayload(dag, node, input, attempt);
    const nodeWorkspace = await prepareNodeWorkspace({
      runWorkspace,
      node,
      attempt,
      inputPayload
    });
    const attemptSignal = createAttemptSignal(options.signal, node.runtime.timeoutSeconds);

    try {
      await prepareCodegenWorkspace(node, nodeWorkspace, options.codegenArtifactStore);
      const runnerResult = await runner.run(node, {
        dag,
        input,
        inputPayload,
        attempt,
        workspace: nodeWorkspace,
        signal: attemptSignal.signal
      });
      const result: NodeExecutionResult = {
        nodeId: node.id,
        status: runnerResult.status,
        startedAt,
        finishedAt: new Date().toISOString(),
        input,
        output: runnerResult.output,
        error: runnerResult.error,
        workspacePath: nodeWorkspace.attemptDir,
        stdoutPath: runnerResult.stdoutPath,
        stderrPath: runnerResult.stderrPath,
        artifacts: runnerResult.artifacts,
        metadata: {
          ...(runnerResult.exitCode === undefined ? {} : { exitCode: runnerResult.exitCode }),
          ...(runnerResult.metadata ?? {})
        }
      };
      const outputValidation =
        result.status === "succeeded" ? validateNodeOutput(node, result, startedAt) : null;
      lastResult = outputValidation ?? result;
      attempts.push(
        createAttemptRecord(attempt, lastResult, nodeWorkspace.attemptDir, attemptSignal.timedOut)
      );

      if (lastResult.status === "succeeded" || outputValidation) {
        return withAttemptMetadata(lastResult, attempts);
      }
      if (attempt < maxAttempts) {
        await waitForBackoff(node.runtime.retry.backoffSeconds, options.signal);
      }
    } catch (error) {
      lastResult = createFailedAttemptResult(node, input, startedAt, nodeWorkspace.attemptDir, {
        error,
        timedOut: attemptSignal.timedOut,
        cancelled: options.signal?.aborted ?? false
      });
      attempts.push(
        createAttemptRecord(attempt, lastResult, nodeWorkspace.attemptDir, attemptSignal.timedOut)
      );
      if (options.signal?.aborted || attempt === maxAttempts) {
        return withAttemptMetadata(lastResult, attempts);
      }
      await waitForBackoff(node.runtime.retry.backoffSeconds, options.signal);
    } finally {
      attemptSignal.dispose();
    }
  }

  if (!lastResult) {
    throw new Error(`Node '${node.id}' did not produce an execution result.`);
  }

  return withAttemptMetadata(lastResult, attempts);
}

async function prepareCodegenWorkspace(
  node: CompiledDagNode,
  workspace: NodeWorkspace,
  artifactStore: CodegenArtifactStore | undefined
): Promise<void> {
  if (node.kind !== "codegen" || !node.codegen) {
    return;
  }

  const store = artifactStore ?? new LocalCodegenArtifactStore();
  const sourceRef = node.codegen.artifacts.find(
    (artifact) => artifact.path === node.codegen?.provenance.artifactPath
  );
  if (!sourceRef) {
    throw new Error(`Codegen node '${node.id}' is missing its generated source artifact.`);
  }

  for (const artifact of node.codegen.artifacts) {
    if (!(await store.verifyArtifact(artifact))) {
      throw new Error(`Generated artifact '${artifact.path}' is missing or has hash drift.`);
    }
  }

  await store.materializeArtifacts(node.codegen.artifacts, workspace.attemptDir);
  const source = await store.readArtifact(sourceRef);
  await writeFile(join(workspace.attemptDir, "run-node.js"), source, {
    encoding: "utf8",
    mode: 0o755
  });
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

function createFailedAttemptResult(
  node: CompiledDagNode,
  input: JsonRecord,
  startedAt: string,
  workspacePath: string,
  failure: {
    readonly error: unknown;
    readonly timedOut: boolean;
    readonly cancelled: boolean;
  }
): NodeExecutionResult {
  const message = failure.timedOut
    ? `Node '${node.id}' timed out after ${node.runtime.timeoutSeconds} seconds.`
    : failure.cancelled
      ? `Node '${node.id}' was cancelled.`
      : failure.error instanceof Error
        ? failure.error.message
        : `Node '${node.id}' failed.`;

  return {
    nodeId: node.id,
    status: "failed",
    startedAt,
    finishedAt: new Date().toISOString(),
    input,
    output: {},
    error: message,
    workspacePath,
    metadata: {
      timedOut: failure.timedOut,
      cancelled: failure.cancelled
    }
  };
}

function createAttemptRecord(
  attempt: number,
  result: NodeExecutionResult,
  workspacePath: string,
  timedOut: boolean
): WorkflowNodeExecutionAttempt {
  return {
    attempt,
    status: timedOut
      ? "timed_out"
      : result.metadata?.cancelled === true
        ? "cancelled"
        : result.status === "succeeded"
          ? "succeeded"
          : "failed",
    startedAt: result.startedAt,
    finishedAt: result.finishedAt,
    exitCode: typeof result.metadata?.exitCode === "number" ? result.metadata.exitCode : undefined,
    error: result.error,
    workspacePath
  };
}

function withAttemptMetadata(
  result: NodeExecutionResult,
  attempts: readonly WorkflowNodeExecutionAttempt[]
): NodeExecutionResult {
  return {
    ...result,
    attempts,
    metadata: {
      ...(result.metadata ?? {}),
      attempts: attempts.length,
      retryCount: Math.max(0, attempts.length - 1),
      nonDeterministicRetry: attempts.length > 1
    }
  };
}

function withSkippedResults(
  dag: CompiledDag,
  nodeResults: readonly NodeExecutionResult[],
  failedNodeId: string
): readonly NodeExecutionResult[] {
  const completed = new Set(nodeResults.map((result) => result.nodeId));
  const skippedAt = new Date().toISOString();
  const skipped = dag.order
    .filter((nodeId) => !completed.has(nodeId))
    .map((nodeId) => ({
      nodeId,
      status: "skipped" as const,
      startedAt: skippedAt,
      finishedAt: skippedAt,
      output: {},
      metadata: {
        skippedBecause: failedNodeId
      }
    }));

  return [...nodeResults, ...skipped];
}

function emitSkippedResults(
  nodeResults: readonly NodeExecutionResult[],
  eventStream: RunEventStream
): readonly NodeExecutionResult[] {
  for (const result of nodeResults) {
    if (result.status === "skipped") {
      eventStream.emit("info", `Node '${result.nodeId}' skipped.`, result.nodeId);
    }
  }

  return nodeResults;
}

interface RunEventStream {
  readonly events: readonly WorkflowRunEvent[];
  emit(level: WorkflowRunEvent["level"], message: string, nodeId?: string): void;
}

function createRunEventStream(onEvent: ExecuteCompiledDagOptions["onEvent"]): RunEventStream {
  const events: WorkflowRunEvent[] = [];

  return {
    events,
    emit(level, message, nodeId) {
      const event: WorkflowRunEvent = {
        id: `event.${events.length + 1}`,
        timestamp: new Date().toISOString(),
        level,
        message,
        ...(nodeId ? { nodeId } : {})
      };
      events.push(event);
      onEvent?.(event);
    }
  };
}

function createAttemptSignal(
  parentSignal: AbortSignal | undefined,
  timeoutSeconds: number
): {
  readonly signal: AbortSignal;
  readonly timedOut: boolean;
  dispose(): void;
} {
  const controller = new AbortController();
  let timedOut = false;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort(new Error(`Timed out after ${timeoutSeconds} seconds.`));
  }, timeoutSeconds * 1000);
  const abortFromParent = () => controller.abort(parentSignal?.reason);

  if (parentSignal?.aborted) {
    abortFromParent();
  } else {
    parentSignal?.addEventListener("abort", abortFromParent, { once: true });
  }

  return {
    signal: controller.signal,
    get timedOut() {
      return timedOut;
    },
    dispose() {
      clearTimeout(timeout);
      parentSignal?.removeEventListener("abort", abortFromParent);
    }
  };
}

async function waitForBackoff(
  backoffSeconds: number,
  signal: AbortSignal | undefined
): Promise<void> {
  if (backoffSeconds <= 0) {
    return;
  }

  await new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(resolve, backoffSeconds * 1000);
    const abort = () => {
      clearTimeout(timeout);
      reject(signal?.reason ?? new Error("Execution cancelled during retry backoff."));
    };

    if (signal?.aborted) {
      abort();
      return;
    }

    signal?.addEventListener("abort", abort, { once: true });
  });
}

async function finishExecution(
  result: DagExecutionResult,
  eventStream: RunEventStream
): Promise<DagExecutionResult> {
  eventStream.emit(result.status === "failed" ? "error" : "info", "NanoClaw run finished.");
  const resultWithEvents: DagExecutionResult = {
    ...result,
    events: eventStream.events
  };
  const manifestPath = await persistRunManifest(resultWithEvents);

  return {
    ...resultWithEvents,
    metadata: {
      ...(resultWithEvents.metadata ?? {}),
      manifestPath
    }
  };
}

function createExecutionResult(
  dag: CompiledDag,
  nodeResults: readonly NodeExecutionResult[],
  status: DagExecutionResult["status"],
  workspacePath: string,
  eventStream: RunEventStream
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
    events: eventStream.events,
    deterministic: true,
    metadata: {
      dagHash: dag.dagHash,
      workspacePath
    }
  };
}
