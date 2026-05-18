export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue };
export type JsonRecord = { [key: string]: JsonValue };

export const workflowSchemaVersion = "1.0.0" as const;
export type WorkflowSchemaVersion = typeof workflowSchemaVersion;

export type WorkflowNodeKind =
  | "trigger"
  | "skill"
  | "codegen"
  | "transform"
  | "approval"
  | "delivery";

export type JsonSchemaShape = JsonRecord;
export type WorkflowReplayBehavior =
  | "none"
  | "record"
  | "replay"
  | "reuse-if-unchanged"
  | "fail-on-drift";

export interface WorkflowRuntimeRetry {
  readonly maxAttempts: number;
  readonly backoffSeconds: number;
}

export interface WorkflowRuntimeResources {
  readonly cpu: string;
  readonly memoryMb: number;
}

export interface WorkflowRuntime {
  readonly image: string;
  readonly command: readonly string[];
  readonly timeoutSeconds: number;
  readonly retry: WorkflowRuntimeRetry;
  readonly environment: Readonly<Record<string, string>>;
  readonly resources: WorkflowRuntimeResources;
}

export interface WorkflowSeededRandomness {
  readonly enabled: boolean;
  readonly seed?: string | undefined;
}

export interface WorkflowDeterminism {
  readonly externalCalls: readonly string[];
  readonly seededRandomness: WorkflowSeededRandomness;
  readonly replayBehavior: WorkflowReplayBehavior;
}

export interface WorkflowCodegenProvenance {
  readonly generator: string;
  readonly generatedAt: string;
  readonly sourcePrompt: string;
  readonly artifactPath: string;
  readonly artifactChecksum: string;
}

export interface WorkflowCodegenReplay {
  readonly mode: "reuse-if-unchanged" | "always-regenerate" | "fail-on-drift";
  readonly seed: string;
}

export interface WorkflowCodegenMetadata {
  readonly provenance: WorkflowCodegenProvenance;
  readonly replay: WorkflowCodegenReplay;
}

export interface WorkflowNode {
  readonly id: string;
  readonly kind: WorkflowNodeKind;
  readonly label: string;
  readonly description: string;
  readonly inputs: Readonly<Record<string, JsonSchemaShape>>;
  readonly outputs: Readonly<Record<string, JsonSchemaShape>>;
  readonly config: JsonRecord;
  readonly runtime: WorkflowRuntime;
  readonly determinism: WorkflowDeterminism;
  readonly skillId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly codegen?: WorkflowCodegenMetadata | undefined;
}

export interface WorkflowPortRef {
  readonly nodeId: string;
  readonly port: string;
}

export interface WorkflowEdge {
  readonly id: string;
  readonly source: WorkflowPortRef;
  readonly target: WorkflowPortRef;
}

export interface WorkflowApprovalRecord {
  readonly status: "approved";
  readonly approvedBy: string;
  readonly approvedAt: string;
  readonly frozenRevision: number;
  readonly frozenDagHash: string;
  readonly nodeOrder: readonly string[];
}

export interface WorkflowSpec {
  readonly id: string;
  readonly schemaVersion: WorkflowSchemaVersion;
  readonly name: string;
  readonly prompt: string;
  readonly revision: number;
  readonly nodes: readonly WorkflowNode[];
  readonly edges: readonly WorkflowEdge[];
  readonly approval: WorkflowApprovalRecord | null;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface WorkflowNodeExecutionResult {
  readonly nodeId: string;
  readonly status: "succeeded" | "failed" | "skipped";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly output: JsonRecord;
}

export interface WorkflowExecutionResult {
  readonly id: string;
  readonly workflowId: string;
  readonly revision: number;
  readonly status: "succeeded" | "failed";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly nodeResults: readonly WorkflowNodeExecutionResult[];
  readonly deterministic: true;
}

export const workflowValidationErrorCodes = [
  "WORKFLOW_SCHEMA_INVALID",
  "WORKFLOW_SCHEMA_VERSION_UNSUPPORTED",
  "WORKFLOW_NODE_ID_DUPLICATE",
  "WORKFLOW_EDGE_SOURCE_NODE_MISSING",
  "WORKFLOW_EDGE_TARGET_NODE_MISSING",
  "WORKFLOW_EDGE_SOURCE_PORT_INVALID",
  "WORKFLOW_EDGE_TARGET_PORT_INVALID",
  "WORKFLOW_DAG_CYCLE",
  "WORKFLOW_EXECUTION_UNAPPROVED",
  "WORKFLOW_CODEGEN_METADATA_MISSING"
] as const;

export type WorkflowValidationErrorCode = (typeof workflowValidationErrorCodes)[number];

export interface WorkflowValidationIssue {
  readonly code: WorkflowValidationErrorCode;
  readonly message: string;
  readonly path: readonly (string | number)[];
}

export type WorkflowValidationResult =
  | { readonly ok: true; readonly workflow: WorkflowSpec }
  | { readonly ok: false; readonly errors: readonly WorkflowValidationIssue[] };
