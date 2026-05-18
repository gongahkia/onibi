import type { JsonRecord, WorkflowDockerSpec, WorkflowSpec } from "@kelpclaw/workflow-spec";

export interface CompiledDagNode {
  readonly id: string;
  readonly label: string;
  readonly docker?: WorkflowDockerSpec | undefined;
  readonly dependencies: readonly string[];
  readonly dependents: readonly string[];
}

export interface CompiledDag {
  readonly workflowId: string;
  readonly nodes: ReadonlyMap<string, CompiledDagNode>;
  readonly order: readonly string[];
  readonly source: WorkflowSpec;
}

export interface NodeExecutionResult {
  readonly nodeId: string;
  readonly status: "succeeded" | "failed";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly output: JsonRecord;
}

export interface DagExecutionResult {
  readonly workflowId: string;
  readonly status: "succeeded" | "failed";
  readonly nodeResults: readonly NodeExecutionResult[];
}

export interface NodeRunner {
  run(node: CompiledDagNode): Promise<NodeExecutionResult>;
}
