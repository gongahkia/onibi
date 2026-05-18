import type {
  WorkflowApprovalRecord,
  WorkflowExecutionResult,
  WorkflowNodeExecutionResult,
  WorkflowNodeKind,
  WorkflowRuntime,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";

export interface CompiledDagNode {
  readonly id: string;
  readonly kind: WorkflowNodeKind;
  readonly label: string;
  readonly runtime: WorkflowRuntime;
  readonly dependencies: readonly string[];
  readonly dependents: readonly string[];
}

export interface CompiledDag {
  readonly workflowId: string;
  readonly revision: number;
  readonly approval: WorkflowApprovalRecord;
  readonly nodes: ReadonlyMap<string, CompiledDagNode>;
  readonly order: readonly string[];
  readonly source: WorkflowSpec;
}

export type NodeExecutionResult = WorkflowNodeExecutionResult;
export type DagExecutionResult = WorkflowExecutionResult;

export interface NodeRunner {
  run(node: CompiledDagNode): Promise<NodeExecutionResult>;
}
