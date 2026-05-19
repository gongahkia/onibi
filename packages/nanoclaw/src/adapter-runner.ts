import { createDefaultLiveAdapters } from "@kelpclaw/adapters";
import {
  assertNodeAdapterPolicy,
  createAdapterMetadataRegistry,
  declaredAdapterOperations
} from "./adapter-policy.js";
import { MockNodeRunner } from "./mock-runner.js";
import { secretEnvironmentName } from "./secrets.js";
import type { Adapter, AdapterInvocation, AdapterResult } from "@kelpclaw/adapters";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export interface AdapterBackedNodeRunnerOptions {
  readonly adapters?: ReadonlyMap<string, Adapter> | undefined;
  readonly fallbackRunner?: NodeRunner | undefined;
}

export class AdapterBackedNodeRunner implements NodeRunner {
  readonly adapters: ReadonlyMap<string, Adapter>;
  private readonly fallbackRunner: NodeRunner;

  public constructor(options: AdapterBackedNodeRunnerOptions = {}) {
    this.adapters = options.adapters ?? createDefaultLiveAdapters();
    this.fallbackRunner = options.fallbackRunner ?? new MockNodeRunner();
  }

  public async run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    const operations = declaredAdapterOperations(node);
    if (operations.length === 0) {
      return this.fallbackRunner.run(node, context);
    }

    assertNodeAdapterPolicy(node, createAdapterMetadataRegistry(this.adapters));

    const results: AdapterResult[] = [];
    for (const operation of operations) {
      const adapter = this.adapters.get(operation.adapterId);
      if (!adapter) {
        throw new Error(`Adapter '${operation.adapterId}' was not registered.`);
      }

      results.push(
        await adapter.invoke(
          createAdapterInvocation({
            node,
            context,
            adapterId: operation.adapterId,
            operation: operation.operation,
            operationVersion: operation.operationVersion
          })
        )
      );
    }

    return {
      status: results.every((result) => result.status === "succeeded") ? "succeeded" : "failed",
      output: createNodeOutput(node, results),
      metadata: {
        mocked: true,
        adapterResults: results.map((result) => ({
          adapterId: result.adapterId,
          operation: result.operation,
          providerResponseId: result.providerMetadata.providerResponseId,
          channel: channelForResult(result)
        })),
        auditEvents: results.flatMap((result) =>
          result.auditEvents.map((event) => ({
            id: event.id,
            timestamp: event.timestamp,
            level: event.level,
            message: event.message
          }))
        )
      }
    };
  }
}

function createAdapterInvocation(input: {
  readonly node: CompiledDagNode;
  readonly context: NodeRunContext;
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
}): AdapterInvocation {
  return {
    adapterId: input.adapterId,
    operation: input.operation,
    operationVersion: input.operationVersion,
    payload: createAdapterPayload(input.node, input.context.input),
    secretRefs: input.node.secretRefs ?? {},
    secrets: resolveAdapterSecrets(input.node.secretRefs ?? {}, input.context.resolvedSecrets),
    context: {
      workflowId: input.context.dag.workflowId,
      nodeId: input.node.id,
      runId: input.context.workspace.runId,
      attempt: input.context.attempt
    },
    idempotencyKey: `${input.context.workspace.runId}.${input.node.id}.${input.operation}.${input.context.attempt}`
  };
}

function resolveAdapterSecrets(
  secretRefs: Readonly<Record<string, string>>,
  resolvedSecrets: Readonly<Record<string, string>>
): Readonly<Record<string, string>> {
  return Object.fromEntries(
    Object.keys(secretRefs).map((secretName) => [
      secretName,
      resolvedSecrets[secretEnvironmentName(secretName)] ?? ""
    ])
  );
}

function createAdapterPayload(node: CompiledDagNode, input: JsonRecord): JsonRecord {
  const payload: JsonRecord = {
    ...node.config,
    ...input
  };

  if (node.kind === "delivery") {
    payload.summary = input;
    if (typeof payload.body !== "string") {
      payload.body = `Workflow node '${node.id}' completed.`;
    }
    if (typeof payload.subject !== "string") {
      payload.subject = node.label;
    }
  }

  return payload;
}

function createNodeOutput(
  node: CompiledDagNode,
  adapterResults: readonly AdapterResult[]
): JsonRecord {
  const first = adapterResults[0];
  if (!first) {
    return {};
  }

  if (node.kind !== "delivery") {
    const directOutput = outputMatchingDeclaredPorts(node, first.output);
    if (directOutput) {
      return directOutput;
    }

    const [firstPort] = Object.keys(node.outputs);
    return firstPort ? { [firstPort]: first.output } : first.output;
  }

  return {
    delivery: {
      status: adapterResults.every((result) => result.status === "succeeded")
        ? "succeeded"
        : "failed",
      channels: adapterResults.map(channelForResult),
      providerResponseIds: adapterResults.map(
        (result) => result.providerMetadata.providerResponseId
      ),
      adapterResults: adapterResults.map((result) => ({
        adapterId: result.adapterId,
        operation: result.operation,
        output: result.output,
        providerMetadata: {
          adapterId: result.providerMetadata.adapterId,
          provider: result.providerMetadata.provider,
          providerResponseId: result.providerMetadata.providerResponseId,
          mock: result.providerMetadata.mock,
          sequence: result.providerMetadata.sequence,
          operation: result.providerMetadata.operation
        }
      }))
    }
  };
}

function channelForResult(result: AdapterResult): string {
  return typeof result.output.channel === "string"
    ? result.output.channel
    : result.providerMetadata.provider;
}

function outputMatchingDeclaredPorts(node: CompiledDagNode, output: JsonRecord): JsonRecord | null {
  const outputPorts = Object.keys(node.outputs);
  if (outputPorts.length === 0) {
    return output;
  }

  const matchedEntries = outputPorts
    .filter((port) => output[port] !== undefined)
    .map((port) => [port, output[port] as JsonValue]);
  if (matchedEntries.length === outputPorts.length || matchedEntries.length > 0) {
    return Object.fromEntries(matchedEntries);
  }

  return null;
}
