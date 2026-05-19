import type {
  JsonRecord,
  WorkflowAdapterOperationRef,
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
  readonly adapterIds?: readonly string[] | undefined;
  readonly adapterOperations?: readonly WorkflowAdapterOperationRef[] | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
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

export interface ExecutionWorkspace {
  readonly runId: string;
  readonly runDir: string;
  readonly workflowSpecPath: string;
}

export interface NodeWorkspace {
  readonly runId: string;
  readonly nodeId: string;
  readonly attempt: number;
  readonly nodeDir: string;
  readonly attemptDir: string;
  readonly inputPath: string;
  readonly outputPath: string;
  readonly stdoutPath: string;
  readonly stderrPath: string;
  readonly artifactsDir: string;
  readonly workflowSpecPath: string;
}

export interface NodeRunContext {
  readonly dag: CompiledDag;
  readonly input: JsonRecord;
  readonly inputPayload: NodeInputPayload;
  readonly attempt: number;
  readonly workspace: NodeWorkspace;
  readonly signal?: AbortSignal | undefined;
}

export interface NodeRunnerResult {
  readonly status: "succeeded" | "failed";
  readonly output: JsonRecord;
  readonly exitCode?: number | undefined;
  readonly error?: string | undefined;
  readonly stdoutPath?: string | undefined;
  readonly stderrPath?: string | undefined;
  readonly artifacts?: readonly string[] | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface NodeRunner {
  run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult>;
}
