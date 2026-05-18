import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { stableWorkflowStringify } from "@kelpclaw/workflow-spec";
import type { JsonRecord, JsonSchemaShape, JsonValue } from "@kelpclaw/workflow-spec";
import type {
  CompiledDag,
  CompiledDagNode,
  ExecutionWorkspace,
  NodeInputPayload,
  NodeWorkspace
} from "./types.js";

export interface ExecutionWorkspaceOptions {
  readonly workspaceRoot?: string | undefined;
  readonly runId?: string | undefined;
}

export async function createExecutionWorkspace(
  dag: CompiledDag,
  options: ExecutionWorkspaceOptions = {}
): Promise<ExecutionWorkspace> {
  const runId =
    options.runId ?? `run.${sanitizePathPart(dag.workflowId)}.r${dag.revision}.${randomUUID()}`;
  const runDir = join(options.workspaceRoot ?? join(tmpdir(), "kelpclaw-nanoclaw"), runId);
  const workflowSpecPath = join(runDir, "workflow.json");

  await mkdir(runDir, { recursive: true });
  await writeFile(workflowSpecPath, stableWorkflowStringify(dag.source), "utf8");

  return {
    runId,
    runDir,
    workflowSpecPath
  };
}

export async function prepareNodeWorkspace(input: {
  readonly runWorkspace: ExecutionWorkspace;
  readonly node: CompiledDagNode;
  readonly attempt: number;
  readonly inputPayload: NodeInputPayload;
}): Promise<NodeWorkspace> {
  const nodeDir = join(input.runWorkspace.runDir, "nodes", sanitizePathPart(input.node.id));
  const attemptDir = join(nodeDir, `attempt-${input.attempt}`);
  const artifactsDir = join(attemptDir, "artifacts");
  const workspace: NodeWorkspace = {
    runId: input.runWorkspace.runId,
    nodeId: input.node.id,
    attempt: input.attempt,
    nodeDir,
    attemptDir,
    inputPath: join(attemptDir, "input.json"),
    outputPath: join(attemptDir, "output.json"),
    stdoutPath: join(attemptDir, "stdout.log"),
    stderrPath: join(attemptDir, "stderr.log"),
    artifactsDir,
    workflowSpecPath: input.runWorkspace.workflowSpecPath
  };

  await mkdir(artifactsDir, { recursive: true });
  await writeFile(workspace.inputPath, JSON.stringify(input.inputPayload, null, 2), "utf8");
  await writeFile(join(attemptDir, "run-node.js"), createDefaultNodeShim(input.node), {
    encoding: "utf8",
    mode: 0o755
  });

  return workspace;
}

function createDefaultNodeShim(node: CompiledDagNode): string {
  const outputDefaults: JsonRecord = Object.fromEntries(
    Object.entries(node.outputs).map(([port, schema]) => [port, defaultValueForSchema(schema)])
  );

  return `import { dirname } from "node:path";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";

const inputPath = process.env.NANOCLAW_NODE_INPUT ?? "/workspace/input.json";
const outputPath = process.env.NANOCLAW_NODE_OUTPUT ?? "/workspace/output.json";
const inputPayload = JSON.parse(readFileSync(inputPath, "utf8"));
const defaults = ${JSON.stringify(outputDefaults, null, 2)};
const output = {};

for (const [port, fallback] of Object.entries(defaults)) {
  output[port] =
    Object.prototype.hasOwnProperty.call(inputPayload.inputs, port) ? inputPayload.inputs[port] : fallback;
}

mkdirSync(dirname(outputPath), { recursive: true });
writeFileSync(outputPath, JSON.stringify(output, null, 2), "utf8");
`;
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
      return "mocked";
    case "null":
      return null;
    case "object":
    default:
      return { mocked: true };
  }
}

function sanitizePathPart(value: string): string {
  return value.replace(/[^a-zA-Z0-9._-]+/gu, "_");
}
