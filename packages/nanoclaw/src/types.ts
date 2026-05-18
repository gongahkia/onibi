import type {
  JsonRecord,
  WorkflowApprovalRecord,
  WorkflowCodegenMetadata,
  WorkflowDeterminism,
  WorkflowEdge,
  WorkflowExecutionResult,
  WorkflowNode,
  WorkflowNodeExecutionResult,
  WorkflowNodeKind,
  WorkflowPortRef,
  WorkflowRuntime,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";

export interface CompiledNodeInputBinding {
  readonly edgeId: string;
  readonly inputPort: string;
  readonly source: WorkflowPortRef;
}

export interface CompiledDagNode {
  readonly id: string;
  readonly kind: WorkflowNodeKind;
  readonly label: string;
  readonly description: string;
  readonly inputs: WorkflowNode["inputs"];
  readonly outputs: WorkflowNode["outputs"];
  readonly config: JsonRecord;
  readonly runtime: WorkflowRuntime;
  readonly determinism: WorkflowDeterminism;
  readonly skillId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly codegen?: WorkflowCodegenMetadata | undefined;
  readonly dependencies: readonly string[];
  readonly dependents: readonly string[];
  readonly inputBindings: readonly CompiledNodeInputBinding[];
}

export interface CompiledDag {
  readonly workflowId: string;
  readonly revision: number;
  readonly approval: WorkflowApprovalRecord;
  readonly dagHash: string;
  readonly nodes: ReadonlyMap<string, CompiledDagNode>;
  readonly edges: readonly WorkflowEdge[];
  readonly order: readonly string[];
  readonly source: WorkflowSpec;
}

export type NodeExecutionResult = WorkflowNodeExecutionResult;
export type DagExecutionResult = WorkflowExecutionResult;

export interface NodeRunner {
  run(node: CompiledDagNode): Promise<NodeExecutionResult>;
}
