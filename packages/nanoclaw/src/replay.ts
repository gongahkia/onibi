import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { compileWorkflowDag } from "./compiler.js";
import type { DagExecutionResult } from "./types.js";
import type { JsonRecord, WorkflowSpec } from "@kelpclaw/workflow-spec";

export interface NanoClawRunManifest {
  readonly schemaVersion: "nanoclaw.run.v1";
  readonly workflowSpecPath: string;
  readonly result: DagExecutionResult;
  readonly executionPath: readonly string[];
}

export async function persistRunManifest(result: DagExecutionResult): Promise<string> {
  const workspacePath = result.metadata?.workspacePath;
  if (typeof workspacePath !== "string") {
    throw new Error("Cannot persist a NanoClaw run manifest without a workspace path.");
  }

  const manifestPath = join(workspacePath, "run-manifest.json");
  const manifest: NanoClawRunManifest = {
    schemaVersion: "nanoclaw.run.v1",
    workflowSpecPath: join(workspacePath, "workflow.json"),
    result,
    executionPath: result.nodeResults
      .filter((nodeResult) => nodeResult.status !== "skipped")
      .map((nodeResult) => nodeResult.nodeId)
  };
  await writeFile(manifestPath, JSON.stringify(manifest, null, 2), "utf8");

  return manifestPath;
}

export async function replayCompletedRun(manifestPath: string): Promise<DagExecutionResult> {
  const manifest = parseRunManifest(JSON.parse(await readFile(manifestPath, "utf8")));
  const workflow = JSON.parse(await readFile(manifest.workflowSpecPath, "utf8")) as WorkflowSpec;
  const dag = compileWorkflowDag(workflow);
  const expectedPath = dag.order.slice(0, manifest.executionPath.length);

  if (expectedPath.join("\n") !== manifest.executionPath.join("\n")) {
    throw new Error("Stored NanoClaw run manifest does not match the compiled execution path.");
  }

  return {
    ...manifest.result,
    metadata: {
      ...(manifest.result.metadata ?? {}),
      replayed: true,
      replayManifestPath: manifestPath
    }
  };
}

function parseRunManifest(input: unknown): NanoClawRunManifest {
  if (!input || typeof input !== "object" || Array.isArray(input)) {
    throw new Error("NanoClaw run manifest must be a JSON object.");
  }

  const record = input as JsonRecord;
  if (
    record.schemaVersion !== "nanoclaw.run.v1" ||
    typeof record.workflowSpecPath !== "string" ||
    !record.result ||
    typeof record.result !== "object" ||
    Array.isArray(record.result) ||
    !Array.isArray(record.executionPath) ||
    !record.executionPath.every((nodeId) => typeof nodeId === "string")
  ) {
    throw new Error("NanoClaw run manifest is invalid.");
  }

  return record as unknown as NanoClawRunManifest;
}
