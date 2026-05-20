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

export type WorkflowAgentRole =
  | "classifier"
  | "planner"
  | "workflow-architect"
  | "agentic-node-designer"
  | "coder"
  | "tester"
  | "runner"
  | "fixer"
  | "evaluator"
  | "summarizer";

export interface WorkflowAgentBudget {
  readonly maxIterations: number;
  readonly maxWallClockSeconds: number;
  readonly maxModelCostUsd: number;
  readonly maxDockerRuntimeSeconds: number;
  readonly maxRetries: number;
}

export interface WorkflowAgenticNodePolicy {
  readonly tools: readonly string[];
  readonly memoryScope: "none" | "node" | "workflow" | "workspace";
  readonly stopConditions: readonly string[];
  readonly humanApprovalBoundaries: readonly string[];
  readonly networkPolicy: WorkflowCodegenSandboxPolicy["network"];
  readonly allowedHosts: readonly string[];
  readonly secretRefs: readonly string[];
  readonly evalContract: JsonRecord;
  readonly budget: WorkflowAgentBudget;
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
  readonly agentic?: WorkflowAgenticNodePolicy | undefined;
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
  "WORKFLOW_CODEGEN_EVAL_REQUIRED",
  "WORKFLOW_DRAFT_EVALUATION_REQUIRED",
  "WORKFLOW_DEPLOYMENT_BLOCKED",
  "WORKFLOW_RUNTIME_IMAGE_POLICY_INVALID",
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
  | "task.routing"
  | "prompt.planning"
  | "skill.matching"
  | "draft.edit"
  | "planner.feedback"
  | "draft.evaluation"
  | "node.reprompt"
  | "workflow.approval"
  | "job.lifecycle"
  | "agent.activity"
  | "workspace.artifact"
  | "dag.compilation"
  | "node.container"
  | "adapter.call"
  | "codegen.artifact"
  | "delivery.event"
  | "run.lifecycle"
  | "deployment.lifecycle";

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
  | "task.routed"
  | "planner.feedback.created"
  | "planner.feedback.decided"
  | "draft.evaluated"
  | "job.created"
  | "agent.ran"
  | "workspace.created"
  | "deployment.created"
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

export type WorkflowTaskRouteKind =
  | "deterministic"
  | "adapter"
  | "codegen"
  | "agentic"
  | "deployment";

export interface WorkflowRetryBudget {
  readonly maxAttempts: number;
  readonly maxCostUsd: number;
}

export interface WorkflowModelRequirement {
  readonly mode: "none" | "deterministic" | "live";
  readonly role: WorkflowAgentRole;
  readonly provider?: string | undefined;
  readonly model?: string | undefined;
  readonly retryBudget: WorkflowRetryBudget;
}

export interface WorkflowModelInvocationRecord {
  readonly id: string;
  readonly role: WorkflowAgentRole;
  readonly inputSummary: string;
  readonly outputArtifact: string;
  readonly provider: string;
  readonly model: string;
  readonly determinismExpectation: "deterministic" | "bounded" | "non-deterministic";
  readonly retryBudget: WorkflowRetryBudget;
  readonly correlationId: string;
  readonly createdAt: string;
}

export interface WorkflowTaskRoute {
  readonly route: WorkflowTaskRouteKind;
  readonly rationale: string;
  readonly requiredModel: WorkflowModelRequirement;
  readonly expectedNodeKinds: readonly WorkflowNodeKind[];
  readonly dockerSandboxRequired: boolean;
  readonly draftTestsRequired: boolean;
  readonly productionDeterministic: boolean;
  readonly modelInvocations: readonly WorkflowModelInvocationRecord[];
}

export type WorkflowGraphChangeKind =
  | "node.added"
  | "node.removed"
  | "node.moved"
  | "node.edited"
  | "edge.added"
  | "edge.removed"
  | "edge.reconnected";

export interface WorkflowGraphChange {
  readonly id: string;
  readonly kind: WorkflowGraphChangeKind;
  readonly elementId: string;
  readonly path: readonly (string | number)[];
  readonly before?: JsonValue | undefined;
  readonly after?: JsonValue | undefined;
}

export interface WorkflowGraphDiff {
  readonly id: string;
  readonly workflowId: string;
  readonly baseRevision: number;
  readonly editedRevision: number;
  readonly createdAt: string;
  readonly summary: readonly string[];
  readonly changes: readonly WorkflowGraphChange[];
  readonly validation: WorkflowValidationResult;
}

export type WorkflowPlannerSuggestionStatus = "suggested" | "accepted" | "rejected";
export type WorkflowPlannerConflictKind = "safe" | "invalid" | "under-specified" | "needs-repair";

export interface WorkflowPlannerSuggestion {
  readonly id: string;
  readonly status: WorkflowPlannerSuggestionStatus;
  readonly conflict: WorkflowPlannerConflictKind;
  readonly target: {
    readonly kind: "workflow" | "node" | "edge";
    readonly id?: string | undefined;
  };
  readonly title: string;
  readonly message: string;
  readonly patch?: JsonRecord | undefined;
  readonly issues: readonly WorkflowValidationIssue[];
}

export interface WorkflowPlannerFeedback {
  readonly id: string;
  readonly workflowId: string;
  readonly graphDiffId: string;
  readonly route: WorkflowTaskRoute;
  readonly createdAt: string;
  readonly status: "ready" | "warnings" | "blocked";
  readonly suggestions: readonly WorkflowPlannerSuggestion[];
  readonly issues: readonly WorkflowValidationIssue[];
}

export interface WorkflowFeedbackRequest {
  readonly baseWorkflow: WorkflowSpec;
  readonly editedWorkflow: WorkflowSpec;
  readonly prompt?: string | undefined;
}

export interface WorkflowFeedbackResponse {
  readonly ok: true;
  readonly graphDiff: WorkflowGraphDiff;
  readonly feedback: WorkflowPlannerFeedback;
}

export interface WorkflowPlannerSuggestionDecisionRequest {
  readonly suggestionId: string;
  readonly decision: "accepted" | "rejected";
}

export interface WorkflowPlannerSuggestionDecisionResponse {
  readonly ok: true;
  readonly feedback: WorkflowPlannerFeedback;
}

export type WorkflowJobType =
  | "plan.workflow"
  | "feedback.graph"
  | "evaluate.draft"
  | "build.codegen-node"
  | "test.codegen-node"
  | "approve.workflow"
  | "run.workflow"
  | "deploy.workflow"
  | "smoke.integration";

export type WorkflowJobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";

export interface WorkflowJobRetryMetadata {
  readonly attempt: number;
  readonly maxAttempts: number;
  readonly retryable: boolean;
}

export interface WorkflowJobEvent {
  readonly id: string;
  readonly jobId: string;
  readonly timestamp: string;
  readonly level: WorkflowRunEventLevel;
  readonly message: string;
  readonly kind: WorkflowObservabilityEventKind;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowJob {
  readonly id: string;
  readonly type: WorkflowJobType;
  readonly status: WorkflowJobStatus;
  readonly workflowId?: string | undefined;
  readonly revisionId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly workspaceId?: string | undefined;
  readonly correlationId: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly startedAt?: string | undefined;
  readonly claimedAt?: string | undefined;
  readonly workerId?: string | undefined;
  readonly finishedAt?: string | undefined;
  readonly retry: WorkflowJobRetryMetadata;
  readonly cancelledAt?: string | undefined;
  readonly cancellationReason?: string | undefined;
  readonly events: readonly WorkflowJobEvent[];
  readonly payload?: JsonRecord | undefined;
  readonly result?: JsonRecord | undefined;
  readonly error?: string | undefined;
}

export type WorkflowWorkspaceMountRole =
  | "workflow-architect"
  | "coder"
  | "tester"
  | "runner"
  | "fixer"
  | "evaluator";

export interface WorkflowWorkspaceMount {
  readonly role: WorkflowWorkspaceMountRole;
  readonly path: string;
  readonly mode: "ro" | "rw";
}

export interface WorkflowWorkspaceFileHash {
  readonly path: string;
  readonly checksum: string;
}

export interface WorkflowWorkspace {
  readonly id: string;
  readonly jobId: string;
  readonly workflowId: string;
  readonly revisionId?: string | undefined;
  readonly draftId?: string | undefined;
  readonly rootPath: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly mountedAgents: readonly WorkflowWorkspaceMountRole[];
  readonly mounts: readonly WorkflowWorkspaceMount[];
  readonly filesCreated: readonly string[];
  readonly fileHashes: readonly WorkflowWorkspaceFileHash[];
  readonly artifactsProduced: readonly WorkflowCodegenArtifactRef[];
  readonly logs: readonly string[];
  readonly logPaths: readonly string[];
  readonly testReports: readonly string[];
  readonly retentionPolicy: "ephemeral" | "retain-on-failure" | "retain";
  readonly retentionStatus: "active" | "retained" | "eligible-for-cleanup";
}

export type WorkflowDraftEvaluationFindingSeverity = "info" | "warn" | "error";

export interface WorkflowDraftEvaluationFinding {
  readonly id: string;
  readonly severity: WorkflowDraftEvaluationFindingSeverity;
  readonly target: {
    readonly kind: "workflow" | "node" | "edge" | "artifact";
    readonly id?: string | undefined;
  };
  readonly message: string;
  readonly issues: readonly WorkflowValidationIssue[];
}

export interface WorkflowDraftEvaluation {
  readonly id: string;
  readonly workflowId: string;
  readonly draftRevisionId: string;
  readonly jobId?: string | undefined;
  readonly status: "passed" | "failed";
  readonly readyForApproval: boolean;
  readonly createdAt: string;
  readonly finishedAt: string;
  readonly mode: "draft";
  readonly mockOnly: true;
  readonly liveProviderCalls: 0;
  readonly findings: readonly WorkflowDraftEvaluationFinding[];
  readonly events: readonly WorkflowRunEvent[];
  readonly suggestions: readonly WorkflowPlannerSuggestion[];
}

export interface GeneratedNodeTestReport {
  readonly id: string;
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly status: "passed" | "failed";
  readonly createdAt: string;
  readonly finishedAt: string;
  readonly testFiles: readonly WorkflowCodegenArtifactRef[];
  readonly resultArtifacts: readonly WorkflowCodegenArtifactRef[];
  readonly logs: readonly string[];
  readonly failureMessage?: string | undefined;
}

export interface GeneratedNodeEvalReport {
  readonly id: string;
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly status: "passed" | "failed";
  readonly createdAt: string;
  readonly finishedAt: string;
  readonly designSpec: WorkflowCodegenArtifactRef;
  readonly testReportId: string;
  readonly schemaValid: boolean;
  readonly securityValid: boolean;
  readonly replayValid: boolean;
  readonly dependencyPolicyValid: boolean;
  readonly fixHistory: readonly string[];
  readonly findings: readonly WorkflowDraftEvaluationFinding[];
}

export type WorkflowDeploymentKind =
  | "schedule.activation"
  | "skill.publication"
  | "integration.configuration"
  | "runner.configuration"
  | "workflow.bundle"
  | "generated.service";

export interface WorkflowDeploymentRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly approvedRevisionId: string;
  readonly draftEvaluationId: string;
  readonly kind: WorkflowDeploymentKind;
  readonly status: "ready" | "blocked" | "deployed" | "rolled-back";
  readonly createdAt: string;
  readonly createdBy: string;
  readonly requiredIntegrations: readonly string[];
  readonly secretRefs: readonly string[];
  readonly rollbackPlan: string;
  readonly auditRecordId: string;
  readonly metadata: JsonRecord;
}

export interface WorkflowPlanRequest {
  readonly prompt: string;
  readonly currentWorkflow?: WorkflowSpec | undefined;
  readonly preserveNodeIds?: readonly string[] | undefined;
  readonly forceDeterministic?: boolean | undefined;
}

export interface WorkflowPlanResponse {
  readonly ok: true;
  readonly workflow: WorkflowSpec;
  readonly draftRevision: WorkflowDraftRevision;
  readonly validation: WorkflowValidationResult;
  readonly route: WorkflowTaskRoute;
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
