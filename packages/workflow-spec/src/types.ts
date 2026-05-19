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

export type WorkflowCodegenArtifactContentType =
  | "application/json"
  | "text/markdown"
  | "text/plain"
  | "text/typescript";

export interface WorkflowCodegenArtifactRef {
  readonly path: string;
  readonly checksum: string;
  readonly contentType: WorkflowCodegenArtifactContentType;
}

export interface WorkflowCodegenDependencyManifest {
  readonly path: string;
  readonly checksum: string;
  readonly packageManager: "none" | "npm" | "pnpm";
  readonly dependencies: readonly string[];
  readonly devDependencies: readonly string[];
  readonly installCommand: readonly string[];
}

export interface WorkflowCodegenSandboxMount {
  readonly source: string;
  readonly target: string;
  readonly mode: "ro" | "rw";
}

export interface WorkflowCodegenSandboxPolicy {
  readonly network: "none" | "declared";
  readonly allowedHosts: readonly string[];
  readonly mounts: readonly WorkflowCodegenSandboxMount[];
  readonly resources: WorkflowRuntimeResources;
}

export interface WorkflowCodegenReview {
  readonly status: "draft" | "approved" | "rejected";
  readonly reviewedBy?: string | undefined;
  readonly reviewedAt?: string | undefined;
  readonly notes?: string | undefined;
}

export interface WorkflowCodegenReplay {
  readonly mode: "reuse-if-unchanged" | "always-regenerate" | "fail-on-drift";
  readonly seed: string;
}

export interface WorkflowCodegenMetadata {
  readonly originalPrompt: string;
  readonly latestPrompt: string;
  readonly plannerRationale: string;
  readonly provenance: WorkflowCodegenProvenance;
  readonly artifacts: readonly WorkflowCodegenArtifactRef[];
  readonly dependencyManifest: WorkflowCodegenDependencyManifest;
  readonly sandbox: WorkflowCodegenSandboxPolicy;
  readonly review: WorkflowCodegenReview;
  readonly replay: WorkflowCodegenReplay;
  readonly llmBacked: boolean;
}

export interface WorkflowAdapterOperationRef {
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
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
  readonly adapterIds?: readonly string[] | undefined;
  readonly adapterOperations?: readonly WorkflowAdapterOperationRef[] | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
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

export interface WorkflowNodeExecutionAttempt {
  readonly attempt: number;
  readonly status: "succeeded" | "failed" | "timed_out" | "cancelled";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly exitCode?: number | undefined;
  readonly error?: string | undefined;
  readonly workspacePath?: string | undefined;
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
  readonly input?: JsonRecord | undefined;
  readonly output: JsonRecord;
  readonly error?: string | undefined;
  readonly workspacePath?: string | undefined;
  readonly stdoutPath?: string | undefined;
  readonly stderrPath?: string | undefined;
  readonly artifacts?: readonly string[] | undefined;
  readonly attempts?: readonly WorkflowNodeExecutionAttempt[] | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowExecutionResult {
  readonly id: string;
  readonly workflowId: string;
  readonly revision: number;
  readonly status: "succeeded" | "failed";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly nodeResults: readonly WorkflowNodeExecutionResult[];
  readonly events?: readonly WorkflowRunEvent[] | undefined;
  readonly deterministic: true;
  readonly metadata?: JsonRecord | undefined;
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
  "WORKFLOW_CODEGEN_METADATA_MISSING",
  "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
  "WORKFLOW_CODEGEN_DEPENDENCY_POLICY_INVALID",
  "WORKFLOW_CODEGEN_SANDBOX_INVALID",
  "WORKFLOW_CODEGEN_ARTIFACT_DRIFT",
  "WORKFLOW_ADAPTER_DECLARATION_INVALID",
  "WORKFLOW_ADAPTER_SECRET_MISSING",
  "WORKFLOW_ADAPTER_NETWORK_POLICY_INVALID",
  "WORKFLOW_DELIVERY_CHANNEL_POLICY_INVALID"
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

export type WorkflowDraftRevisionSource = "plan" | "validate" | "reprompt" | "revision";

export interface WorkflowDraftRevision {
  readonly id: string;
  readonly workflowId: string;
  readonly revision: number;
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
  readonly source: WorkflowDraftRevisionSource;
  readonly createdAt: string;
}

export interface WorkflowDiffLine {
  readonly kind: "same" | "added" | "removed";
  readonly text: string;
}

export interface WorkflowSpecDiff {
  readonly changed: boolean;
  readonly summary: readonly string[];
  readonly lines: readonly WorkflowDiffLine[];
}

export interface WorkflowApprovedRevision {
  readonly id: string;
  readonly workflowId: string;
  readonly revision: number;
  readonly approvedBy: string;
  readonly createdAt: string;
  readonly workflow: WorkflowSpec;
  readonly draftSpecJson: string;
  readonly frozenSpecJson: string;
  readonly diff: WorkflowSpecDiff;
}

export type WorkflowRunStatus = "queued" | "running" | "succeeded" | "failed";
export type WorkflowRunEventLevel = "info" | "error";
export type WorkflowEventSeverity = "debug" | "info" | "warn" | "error" | "critical";
export type WorkflowObservabilityEventKind =
  | "prompt.planning"
  | "skill.matching"
  | "draft.edit"
  | "node.reprompt"
  | "workflow.approval"
  | "dag.compilation"
  | "node.container"
  | "adapter.call"
  | "codegen.artifact"
  | "delivery.event"
  | "run.lifecycle";

export interface WorkflowObservabilityContext {
  readonly workflowId: string;
  readonly revisionId: string;
  readonly runId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly correlationId: string;
}

export interface WorkflowObservabilityEvent extends WorkflowObservabilityContext {
  readonly id: string;
  readonly timestamp: string;
  readonly severity: WorkflowEventSeverity;
  readonly kind: WorkflowObservabilityEventKind;
  readonly message: string;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowRunEvent {
  readonly id: string;
  readonly timestamp: string;
  readonly level: WorkflowRunEventLevel;
  readonly message: string;
  readonly severity?: WorkflowEventSeverity | undefined;
  readonly kind?: WorkflowObservabilityEventKind | undefined;
  readonly workflowId?: string | undefined;
  readonly revisionId?: string | undefined;
  readonly runId?: string | undefined;
  readonly correlationId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowRunRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly approvedRevisionId: string;
  readonly revision: number;
  readonly status: WorkflowRunStatus;
  readonly createdAt: string;
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly events: readonly WorkflowRunEvent[];
  readonly result: WorkflowExecutionResult | null;
}

export type WorkflowAuditAction =
  | "workflow.created"
  | "workflow.edited"
  | "workflow.approved"
  | "codegen.reviewed"
  | "secret.referenced"
  | "container.ran"
  | "adapter.called"
  | "delivery.completed"
  | "run.completed";

export interface WorkflowAuditContainerRecord {
  readonly image: string;
  readonly command: readonly string[];
  readonly network: "none" | "declared" | "bridge";
  readonly workspacePath?: string | undefined;
}

export interface WorkflowAuditAdapterCallRecord {
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
  readonly status: "succeeded" | "failed";
}

export interface WorkflowAuditDeliveryRecord {
  readonly channels: readonly string[];
  readonly status: "succeeded" | "failed";
}

export interface WorkflowAuditRecord extends WorkflowObservabilityContext {
  readonly id: string;
  readonly timestamp: string;
  readonly action: WorkflowAuditAction;
  readonly actor: string;
  readonly summary: string;
  readonly diff?: WorkflowSpecDiff | undefined;
  readonly approvedArtifactRefs?: readonly WorkflowCodegenArtifactRef[] | undefined;
  readonly secretRefs?: readonly string[] | undefined;
  readonly container?: WorkflowAuditContainerRecord | undefined;
  readonly adapterCall?: WorkflowAuditAdapterCallRecord | undefined;
  readonly delivery?: WorkflowAuditDeliveryRecord | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowArtifactManifestRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly revisionId: string;
  readonly createdAt: string;
  readonly artifacts: readonly WorkflowCodegenArtifactRef[];
  readonly manifestChecksum: string;
}

export interface WorkflowPlanRequest {
  readonly prompt: string;
  readonly currentWorkflow?: WorkflowSpec | undefined;
  readonly preserveNodeIds?: readonly string[] | undefined;
}

export interface WorkflowPlanResponse {
  readonly ok: true;
  readonly workflow: WorkflowSpec;
  readonly draftRevision: WorkflowDraftRevision;
  readonly validation: WorkflowValidationResult;
}

export interface WorkflowRepromptNodeRequest {
  readonly nodeId: string;
  readonly prompt: string;
  readonly currentWorkflow?: WorkflowSpec | undefined;
}

export interface WorkflowRepromptNodeResponse {
  readonly ok: true;
  readonly workflow: WorkflowSpec;
  readonly draftRevision: WorkflowDraftRevision;
  readonly validation: WorkflowValidationResult;
  readonly before: WorkflowNode;
  readonly after: WorkflowNode;
  readonly diff: WorkflowSpecDiff;
}

export interface WorkflowValidateRequest {
  readonly workflow: WorkflowSpec;
}

export interface WorkflowValidateResponse {
  readonly ok: boolean;
  readonly validation: WorkflowValidationResult;
  readonly workflow?: WorkflowSpec | undefined;
  readonly draftRevision?: WorkflowDraftRevision | undefined;
}

export interface WorkflowApproveRequest {
  readonly workflow: WorkflowSpec;
  readonly approvedBy: string;
}

export interface WorkflowApproveResponse {
  readonly ok: true;
  readonly workflowId: string;
  readonly approvedRevisionId: string;
  readonly approvedRevision: WorkflowApprovedRevision;
  readonly workflow: WorkflowSpec;
  readonly diff: WorkflowSpecDiff;
}

export interface WorkflowStartRunRequest {
  readonly approvedRevisionId: string;
}

export interface WorkflowStartRunResponse {
  readonly ok: true;
  readonly run: WorkflowRunRecord;
}

export interface WorkflowFetchRunResponse {
  readonly ok: true;
  readonly run: WorkflowRunRecord;
}

export interface WorkflowApiError {
  readonly ok: false;
  readonly error: string;
  readonly message: string;
  readonly validation?: WorkflowValidationResult | undefined;
  readonly issues?: readonly WorkflowValidationIssue[] | undefined;
}
