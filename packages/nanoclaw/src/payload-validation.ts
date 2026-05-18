import { Ajv } from "ajv";
import type { ErrorObject } from "ajv";
import type { JsonRecord, JsonSchemaShape } from "@kelpclaw/workflow-spec";
import type { CompiledDagNode } from "./types.js";

export interface NodePayloadValidationIssue {
  readonly nodeId: string;
  readonly direction: "input" | "output";
  readonly port: string;
  readonly message: string;
  readonly path: string;
}

export class NodePayloadValidationError extends Error {
  public readonly issues: readonly NodePayloadValidationIssue[];

  public constructor(issues: readonly NodePayloadValidationIssue[]) {
    super(issues.map((issue) => `${issue.port}: ${issue.message}`).join("; "));
    this.name = "NodePayloadValidationError";
    this.issues = issues;
  }
}

const ajv = new Ajv({
  allErrors: true,
  strict: false
});

export function assertValidNodeInput(node: CompiledDagNode, payload: JsonRecord): void {
  assertValidPortPayload(node, "input", node.inputs, payload);
}

export function assertValidNodeOutput(node: CompiledDagNode, payload: JsonRecord): void {
  assertValidPortPayload(node, "output", node.outputs, payload);
}

function assertValidPortPayload(
  node: CompiledDagNode,
  direction: "input" | "output",
  schemas: Readonly<Record<string, JsonSchemaShape>>,
  payload: JsonRecord
): void {
  const issues: NodePayloadValidationIssue[] = [];
  const declaredPorts = Object.keys(schemas).sort();

  for (const port of declaredPorts) {
    const schema = schemas[port];
    if (!schema) {
      continue;
    }

    if (!(port in payload)) {
      issues.push({
        nodeId: node.id,
        direction,
        port,
        message: `Declared ${direction} port '${port}' is missing.`,
        path: port
      });
      continue;
    }

    const validate = ajv.compile(schema);
    const valid = validate(payload[port]);
    if (!valid) {
      issues.push(...toIssues(node.id, direction, port, validate.errors ?? []));
    }
  }

  for (const port of Object.keys(payload).sort()) {
    if (!(port in schemas)) {
      issues.push({
        nodeId: node.id,
        direction,
        port,
        message: `Undeclared ${direction} port '${port}' was provided.`,
        path: port
      });
    }
  }

  if (issues.length > 0) {
    throw new NodePayloadValidationError(issues);
  }
}

function toIssues(
  nodeId: string,
  direction: "input" | "output",
  port: string,
  errors: readonly ErrorObject[]
): readonly NodePayloadValidationIssue[] {
  return errors.map((error) => ({
    nodeId,
    direction,
    port,
    message: error.message ?? `Invalid ${direction} payload.`,
    path: `${port}${error.instancePath}`
  }));
}
