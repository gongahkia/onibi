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

export interface NodeInputPayload {
  readonly workflowId: string;
  readonly revision: number;
  readonly nodeId: string;
  readonly attempt: number;
  readonly inputs: JsonRecord;
  readonly config: JsonRecord;
  readonly metadata: JsonRecord;
}

export interface NodeRunContext {
  readonly dag: CompiledDag;
  readonly input: JsonRecord;
  readonly inputPayload: NodeInputPayload;
  readonly attempt: number;
  readonly signal?: AbortSignal | undefined;
}

export interface NodeRunnerResult {
  readonly status: "succeeded" | "failed";
  readonly output: JsonRecord;
  readonly exitCode?: number | undefined;
  readonly error?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface NodeRunner {
  run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult>;
}
