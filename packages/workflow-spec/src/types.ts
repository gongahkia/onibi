export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue };
export type JsonRecord = { [key: string]: JsonValue };

export type WorkflowNodeType = "skill" | "adapter" | "codegen" | "approval";

export interface WorkflowDockerSpec {
  readonly image: string;
  readonly command: readonly string[];
  readonly env?: Readonly<Record<string, string>> | undefined;
}

export interface WorkflowNode {
  readonly id: string;
  readonly type: WorkflowNodeType;
  readonly label: string;
  readonly skillId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly docker?: WorkflowDockerSpec | undefined;
  readonly inputs?: JsonRecord | undefined;
  readonly outputs?: readonly string[] | undefined;
}

export interface WorkflowEdge {
  readonly id?: string | undefined;
  readonly source: string;
  readonly target: string;
}

export interface WorkflowApprovalGate {
  readonly id: string;
  readonly nodeId: string;
  readonly label: string;
  readonly requiredRole: "operator" | "owner";
}

export interface WorkflowMetadata {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly createdAt?: string | undefined;
}

export interface WorkflowSpec {
  readonly metadata: WorkflowMetadata;
  readonly nodes: readonly WorkflowNode[];
  readonly edges: readonly WorkflowEdge[];
  readonly approvals?: readonly WorkflowApprovalGate[] | undefined;
}

export const workflowValidationErrorCodes = [
  "WORKFLOW_SCHEMA_INVALID",
  "WORKFLOW_NODE_ID_DUPLICATE",
  "WORKFLOW_EDGE_SOURCE_MISSING",
  "WORKFLOW_EDGE_TARGET_MISSING",
  "WORKFLOW_DAG_CYCLE"
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
