import type { JsonRecord, JsonSchemaShape, JsonValue } from "@kelpclaw/workflow-spec";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";

export class DeterministicNodeRunner implements NodeRunner {
  readonly visitedNodeIds: string[] = [];
  private readonly failingNodeIds: ReadonlySet<string>;

  public constructor(options: { readonly failingNodeIds?: readonly string[] } = {}) {
    this.failingNodeIds = new Set(options.failingNodeIds ?? []);
  }

  public async run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    this.visitedNodeIds.push(node.id);

    return {
      status: this.failingNodeIds.has(node.id) ? "failed" : "succeeded",
      output: this.failingNodeIds.has(node.id)
        ? {}
        : createDeterministicOutput(node, context.input),
      metadata: {
        deterministic: true
      }
    };
  }
}

function createDeterministicOutput(node: CompiledDagNode, input: JsonRecord): JsonRecord {
  const output: JsonRecord = {};
  for (const [port, schema] of Object.entries(node.outputs)) {
    const forwardedValue = input[port];
    output[port] = forwardedValue === undefined ? defaultValueForSchema(schema) : forwardedValue;
  }

  return output;
}

function defaultValueForSchema(schema: JsonSchemaShape): JsonValue {
  switch (schema.type) {
    case "array":
      return [];
    case "boolean":
      return false;
    case "integer":
    case "number":
      return 0;
    case "string":
      return "deterministic";
    case "null":
      return null;
    case "object":
    default:
      return { deterministic: true };
  }
}
