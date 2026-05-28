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
  | "delivery"
  | "agent-step";

export const agentStepSourceAgents = [
  "claude-code",
  "codex-cli",
  "cursor",
  "aider",
  "gemini-cli",
  "opencode",
  "goose",
  "cline",
  "continue-dev",
  "copilot",
  "custom"
] as const;
export type AgentStepSourceAgent = (typeof agentStepSourceAgents)[number];

export const agentStepClassifications = [
  "Public",
  "Internal",
  "Confidential",
  "Restricted"
] as const;
export type AgentStepClassification = (typeof agentStepClassifications)[number];

export const agentStepStatuses = [
  "pending",
  "running",
  "succeeded",
  "failed",
  "denied",
  "cancelled"
] as const;
export type AgentStepStatus = (typeof agentStepStatuses)[number];

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

export type WorkflowGeneratedModuleReuseStatus =
  | "reuse"
  | "reuse-with-reeval"
  | "regenerate"
  | "blocked-drift";

export type WorkflowGeneratedModuleReuseGate =
  | "prompt"
  | "schema"
  | "runtime"
  | "sandbox"
  | "dependency"
  | "network"
  | "replay"
  | "evaluation"
  | "unresolved-failure";

export interface WorkflowGeneratedModuleSignature {
  readonly promptHash: string;
  readonly inputSchemaHash: string;
  readonly outputSchemaHash: string;
  readonly runtimeHash: string;
  readonly sandboxHash: string;
  readonly dependencyManifestHash: string;
  readonly replaySeed: string;
  readonly artifactHash: string;
}

export interface WorkflowGeneratedModuleReuseDecision {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId: string;
  readonly nodeId: string;
  readonly status: WorkflowGeneratedModuleReuseStatus;
  readonly createdAt: string;
  readonly sourceBranchId?: string | undefined;
  readonly sourceDraftRevisionId?: string | undefined;
  readonly sourceEvalReportId?: string | undefined;
  readonly signature: WorkflowGeneratedModuleSignature;
  readonly gates: readonly WorkflowGeneratedModuleReuseGate[];
  readonly reason: string;
  readonly artifacts: readonly WorkflowCodegenArtifactRef[];
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

export type WorkflowAgenticToolGrantKind = "builtin" | "mcp" | "adapter";
export type WorkflowAgenticToolSideEffect = "none" | "read" | "write";

export interface WorkflowAgenticToolGrant {
  readonly kind: WorkflowAgenticToolGrantKind;
  readonly name: string;
  readonly connectorId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly operation?: string | undefined;
  readonly operationVersion?: string | undefined;
  readonly allowedHosts: readonly string[];
  readonly secretRefs: readonly string[];
  readonly sideEffect: WorkflowAgenticToolSideEffect;
}

export type WorkflowAgentMemoryScope = "none" | "node" | "workflow" | "workspace";

export interface WorkflowAgenticNodePolicy {
  readonly tools: readonly string[];
  readonly toolGrants?: readonly WorkflowAgenticToolGrant[] | undefined;
  readonly memoryScope: WorkflowAgentMemoryScope;
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

export type WorkflowNodeCompensationStrategy = "none" | "manual" | "adapter-operation";

export interface WorkflowNodeCompensation {
  readonly strategy: WorkflowNodeCompensationStrategy;
  readonly adapterOperation?: WorkflowAdapterOperationRef | undefined;
  readonly inputFrom?: "node-input" | "node-output" | "config" | undefined;
  readonly instructions?: string | undefined;
}

export interface AgentStepMetadata {
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId: string;
  readonly parentToolUseId?: string | undefined;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: AgentStepStatus;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
  readonly classification?: AgentStepClassification | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
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
  readonly agentStep?: AgentStepMetadata | undefined;
  readonly compensation?: WorkflowNodeCompensation | undefined;
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

export interface WorkflowPlanningQuestion {
  readonly question: string;
  readonly blocking: boolean;
}

export interface WorkflowPlannerRevisionMetadata {
  readonly revision: number;
  readonly mode: "initial" | "revision" | "manual";
  readonly summary: string;
  readonly createdAt: string;
}

export interface WorkflowPlanningMetadata {
  readonly requiredCapabilities: readonly string[];
  readonly optionalCapabilities: readonly string[];
  readonly deferredIdeas: readonly string[];
  readonly acceptanceCriteria: readonly string[];
  readonly implementationGuidance: readonly string[];
  readonly validationGuidance: readonly string[];
  readonly openQuestions: readonly WorkflowPlanningQuestion[];
  readonly nodeResponsibilities: Readonly<Record<string, readonly string[]>>;
  readonly plannerRevision: WorkflowPlannerRevisionMetadata;
}

export interface WorkflowSpec {
  readonly id: string;
  readonly schemaVersion: WorkflowSchemaVersion;
  readonly name: string;
  readonly prompt: string;
  readonly revision: number;
  readonly planning?: WorkflowPlanningMetadata | undefined;
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
  "WORKFLOW_PLAN_CAPABILITY_MISSING",
  "WORKFLOW_PLAN_ACCEPTANCE_CRITERIA_MISSING",
  "WORKFLOW_PLAN_RESPONSIBILITY_MISSING",
  "WORKFLOW_PLAN_BLOCKING_QUESTION",
  "WORKFLOW_DEPLOYMENT_BLOCKED",
  "AGENT_STEP_METADATA_MISSING",
  "AGENT_STEP_METADATA_FORBIDDEN",
  "AGENT_STEP_EXECUTION_UNSUPPORTED",
  "POLICY_DENIED",
  "POLICY_APPROVAL_REQUIRED",
  "AUDIT_CHAIN_INVALID",
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

export type WorkflowDraftRevisionSource =
  | "plan"
  | "plan-accepted"
  | "branch-fork"
  | "branch-plan"
  | "branch-reprompt"
  | "branch-merge"
  | "branch-cherry-pick"
  | "validate"
  | "reprompt"
  | "revision";

export interface WorkflowDraftRevision {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly parentDraftRevisionId?: string | undefined;
  readonly revision: number;
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
  readonly source: WorkflowDraftRevisionSource;
  readonly createdAt: string;
}

export type WorkflowBranchStatus = "active" | "archived";

export interface WorkflowBranch {
  readonly id: string;
  readonly workflowId: string;
  readonly name: string;
  readonly status: WorkflowBranchStatus;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly createdBy: string;
  readonly parentBranchId?: string | undefined;
  readonly baseDraftRevisionId: string;
  readonly headDraftRevisionId: string;
  readonly acceptedDraftRevisionId?: string | undefined;
  readonly latestApprovedRevisionId?: string | undefined;
  readonly latestDraftEvaluationId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export type WorkflowPromptTurnSource = "plan" | "reprompt" | "edit" | "merge" | "cherry-pick";

export interface WorkflowPromptTurn {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId: string;
  readonly source: WorkflowPromptTurnSource;
  readonly prompt: string;
  readonly actor: string;
  readonly createdAt: string;
  readonly baseDraftRevisionId?: string | undefined;
  readonly resultingDraftRevisionId?: string | undefined;
  readonly route?: WorkflowTaskRoute | undefined;
  readonly metadata?: JsonRecord | undefined;
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
  readonly branchId?: string | undefined;
  readonly revision: number;
  readonly approvedBy: string;
  readonly createdAt: string;
  readonly workflow: WorkflowSpec;
  readonly draftSpecJson: string;
  readonly frozenSpecJson: string;
  readonly diff: WorkflowSpecDiff;
}

export type WorkflowRunStatus =
  | "queued"
  | "running"
  | "paused"
  | "resuming"
  | "succeeded"
  | "failed"
  | "cancelled";
export type WorkflowRunEventLevel = "info" | "warn" | "error";
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
  | "budget.lifecycle"
  | "node.decision"
  | "workspace.artifact"
  | "dag.compilation"
  | "node.container"
  | "adapter.call"
  | "codegen.artifact"
  | "delivery.event"
  | "run.lifecycle"
  | "deployment.lifecycle"
  | "connector.lifecycle"
  | "checkpoint.lifecycle"
  | "schedule.lifecycle"
  | "alert.lifecycle"
  | "retention.lifecycle"
  | "compensation.required";

export interface WorkflowObservabilityContext {
  readonly workflowId: string;
  readonly revisionId: string;
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
  readonly runId?: string | undefined;
  readonly correlationId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowRunRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly approvedRevisionId: string;
  readonly revision: number;
  readonly status: WorkflowRunStatus;
  readonly createdAt: string;
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly events: readonly WorkflowRunEvent[];
  readonly result: WorkflowExecutionResult | null;
}

export type WorkflowRunCheckpointStatus =
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "cancelled";

export interface WorkflowRunCheckpoint {
  readonly id: string;
  readonly runId: string;
  readonly workflowId: string;
  readonly approvedRevisionId: string;
  readonly nodeId: string;
  readonly attempt: number;
  readonly status: WorkflowRunCheckpointStatus;
  readonly inputHash: string;
  readonly idempotencyKey: string;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
  readonly output?: JsonRecord | undefined;
  readonly error?: string | undefined;
  readonly workspacePath?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export type WorkflowAuditAction =
  | "workflow.created"
  | "workflow.edited"
  | "plan.accepted"
  | "branch.created"
  | "branch.updated"
  | "branch.merged"
  | "branch.cherry-picked"
  | "codegen.reused"
  | "workflow.approved"
  | "codegen.reviewed"
  | "task.routed"
  | "planner.feedback.created"
  | "planner.feedback.decided"
  | "draft.evaluated"
  | "job.created"
  | "agent.ran"
  | "decision.trace.recorded"
  | "budget.updated"
  | "budget.blocked"
  | "workspace.created"
  | "deployment.created"
  | "deployment.undeployed"
  | "deployment.rolled-back"
  | "audit.exported"
  | "trajectory.promoted"
  | "policy.denied"
  | "policy.approved"
  | "tbom.exported"
  | "secret.referenced"
  | "container.ran"
  | "adapter.called"
  | "delivery.completed"
  | "run.completed"
  | "connector.created"
  | "connector.deleted"
  | "schedule.updated"
  | "retention.cleaned";

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
  readonly branchId?: string | undefined;
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

export interface WorkflowTaskRouteScore {
  readonly route: WorkflowTaskRouteKind;
  readonly score: number;
  readonly positiveSignals: readonly string[];
  readonly negativeSignals: readonly string[];
}

export interface WorkflowTaskRouteAlternative {
  readonly route: WorkflowTaskRouteKind;
  readonly score: number;
  readonly reason: string;
  readonly suppressed: boolean;
}

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
  readonly durationMs?: number | undefined;
  readonly durationApiMs?: number | undefined;
  readonly inputTokens?: number | undefined;
  readonly outputTokens?: number | undefined;
  readonly cacheReadInputTokens?: number | undefined;
  readonly cacheCreationInputTokens?: number | undefined;
  readonly totalTokens?: number | undefined;
  readonly costUsd?: number | undefined;
  readonly modelUsage?: JsonRecord | undefined;
  readonly failureReason?: string | undefined;
}

export type WorkflowLifecycleStage =
  | "empty"
  | "planned"
  | "accepted"
  | "generated"
  | "evaluated"
  | "approved"
  | "deployed"
  | "runnable";

export interface WorkflowRuntimeTruthSnapshot {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly stage: WorkflowLifecycleStage;
  readonly planned: boolean;
  readonly accepted: boolean;
  readonly generated: boolean;
  readonly evaluated: boolean;
  readonly approved: boolean;
  readonly deployed: boolean;
  readonly runnable: boolean;
  readonly draftRevisionId?: string | undefined;
  readonly acceptedDraftRevisionId?: string | undefined;
  readonly evaluationId?: string | undefined;
  readonly approvedRevisionId?: string | undefined;
  readonly runnerDeploymentId?: string | undefined;
  readonly activeDeploymentIds: readonly string[];
  readonly blockingReasons: readonly string[];
  readonly updatedAt: string;
}

export interface WorkflowProviderRuntimeConfig {
  readonly role:
    | "planner"
    | "agentic-research"
    | "codegen"
    | "workflow-architect"
    | "coder"
    | "tester"
    | "runner"
    | "fixer"
    | "evaluator";
  readonly provider: "anthropic" | "openai" | "openweight" | "deterministic";
  readonly model: string;
  readonly configured: boolean;
  readonly missingCredential?: string | undefined;
  readonly tokenAccounting: boolean;
  readonly costAccounting: boolean;
  readonly retryBudget: WorkflowRetryBudget;
  readonly runtimeLimits: JsonRecord;
}

export interface WorkflowBudgetPolicy {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly maxWorkflowCostUsd: number;
  readonly maxCodegenCostUsd: number;
  readonly maxAgenticCostUsd: number;
  readonly expensiveRetryConfirmationUsd: number;
  readonly perAgentMaxCostUsd: Partial<Record<WorkflowAgentRole, number>>;
  readonly updatedAt: string;
  readonly updatedBy: string;
}

export interface WorkflowBudgetLedger {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly jobId?: string | undefined;
  readonly agentRunId?: string | undefined;
  readonly scope: "workflow" | "job" | "agent";
  readonly projectedCostUsd: number;
  readonly actualCostUsd: number;
  readonly remainingCostUsd: number;
  readonly retryEstimateUsd: number;
  readonly status: "within-budget" | "confirmation-required" | "blocked" | "exhausted";
  readonly stopReason?: string | undefined;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface WorkflowAgentTimelineEvent {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly jobId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly agentRunId?: string | undefined;
  readonly role: WorkflowAgentRole;
  readonly timestamp: string;
  readonly status: "started" | "succeeded" | "failed" | "blocked";
  readonly title: string;
  readonly summary: string;
  readonly decision?: string | undefined;
  readonly fixTriageAction?:
    | "targeted-patch"
    | "retry-codegen"
    | "rearchitect"
    | "give-up"
    | undefined;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly inputTokens?: number | undefined;
  readonly outputTokens?: number | undefined;
  readonly totalTokens?: number | undefined;
  readonly costUsd?: number | undefined;
  readonly cumulativeCostUsd: number;
  readonly metadata?: JsonRecord | undefined;
}

export type WorkflowDecisionTraceKind =
  | "planner.node-created"
  | "planner.node-updated"
  | "planner.edge-designed"
  | "codegen.architect"
  | "codegen.coder"
  | "codegen.tester"
  | "codegen.runner"
  | "codegen.fixer"
  | "codegen.evaluator"
  | "runtime.router-classification"
  | "runtime.agent-policy"
  | "runtime.tool-call"
  | "runtime.memory-read"
  | "runtime.memory-write";

export interface WorkflowNodeDecisionTraceEvent {
  readonly id: string;
  readonly traceId: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly nodeId: string;
  readonly revisionId?: string | undefined;
  readonly jobId?: string | undefined;
  readonly agentRunId?: string | undefined;
  readonly kind: WorkflowDecisionTraceKind;
  readonly role: WorkflowAgentRole;
  readonly createdAt: string;
  readonly summary: string;
  readonly rationale: string;
  readonly alternativesConsidered: readonly string[];
  readonly selectedAction: string;
  readonly inputSummary: string;
  readonly promptHash?: string | undefined;
  readonly promptExcerpt?: string | undefined;
  readonly route?: WorkflowTaskRouteKind | undefined;
  readonly provider?: string | undefined;
  readonly model?: string | undefined;
  readonly modelInvocationIds: readonly string[];
  readonly affectedNodeIds: readonly string[];
  readonly affectedEdgeIds: readonly string[];
  readonly constraints: JsonRecord;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly evalOutcome?: "passed" | "failed" | "blocked" | "not-run" | undefined;
  readonly failureClass?: string | undefined;
  readonly fixTriageAction?:
    | "targeted-patch"
    | "retry-codegen"
    | "rearchitect"
    | "give-up"
    | undefined;
  readonly inputTokens?: number | undefined;
  readonly outputTokens?: number | undefined;
  readonly totalTokens?: number | undefined;
  readonly costUsd?: number | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowNodeDecisionTrace {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly nodeId: string;
  readonly revisionId?: string | undefined;
  readonly kind: WorkflowDecisionTraceKind;
  readonly source: "planner" | "codegen" | "runtime";
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly status: "recorded" | "succeeded" | "failed" | "blocked";
  readonly events: readonly WorkflowNodeDecisionTraceEvent[];
}

export interface WorkflowDecisionTraceEvalExample {
  readonly id: string;
  readonly traceId: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly nodeId: string;
  readonly kind: WorkflowDecisionTraceKind;
  readonly createdAt: string;
  readonly input: JsonRecord;
  readonly expectedDecision?: string | undefined;
  readonly actualDecision: string;
  readonly outcome: "pass" | "fail" | "blocked" | "unknown";
  readonly failureClass?: string | undefined;
  readonly artifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowNodeDecisionTraceExport {
  readonly id: string;
  readonly workflowId: string;
  readonly exportedAt: string;
  readonly format: "jsonl";
  readonly redacted: true;
  readonly lineCount: number;
  readonly records: readonly JsonRecord[];
  readonly evalExamples: readonly WorkflowDecisionTraceEvalExample[];
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
  readonly classifierVersion: string;
  readonly confidence: number;
  readonly scores: readonly WorkflowTaskRouteScore[];
  readonly alternatives: readonly WorkflowTaskRouteAlternative[];
  readonly matchedSignals: readonly string[];
}

export interface WorkflowRouterEvalCase {
  readonly id: string;
  readonly prompt: string;
  readonly expectedRoute: WorkflowTaskRouteKind;
  readonly minConfidence: number;
  readonly forceDeterministic?: boolean | undefined;
  readonly expectedNodeKinds?: readonly WorkflowNodeKind[] | undefined;
}

export interface WorkflowRouterEvalCaseResult {
  readonly id: string;
  readonly prompt: string;
  readonly expectedRoute: WorkflowTaskRouteKind;
  readonly actualRoute: WorkflowTaskRouteKind;
  readonly confidence: number;
  readonly passed: boolean;
  readonly route: WorkflowTaskRoute;
  readonly failures: readonly string[];
}

export interface WorkflowRouterEvalRun {
  readonly id: string;
  readonly classifierVersion: string;
  readonly createdAt: string;
  readonly passed: boolean;
  readonly total: number;
  readonly failed: number;
  readonly results: readonly WorkflowRouterEvalCaseResult[];
}

export interface WorkflowRouterEvaluateResponse {
  readonly ok: true;
  readonly route: WorkflowTaskRoute;
}

export interface WorkflowRouterEvalListResponse {
  readonly ok: true;
  readonly classifierVersion: string;
  readonly cases: readonly WorkflowRouterEvalCase[];
  readonly latestRun?: WorkflowRouterEvalRun | undefined;
}

export interface WorkflowRouterEvalRunResponse {
  readonly ok: true;
  readonly run: WorkflowRouterEvalRun;
}

export interface WorkflowAgentMemoryRecord {
  readonly id: string;
  readonly scope: Exclude<WorkflowAgentMemoryScope, "none">;
  readonly namespace: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly runId?: string | undefined;
  readonly nodeId?: string | undefined;
  readonly tags: readonly string[];
  readonly contentHash: string;
  readonly content: JsonRecord;
  readonly shareable: boolean;
  readonly sourceTraceId?: string | undefined;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly expiresAt?: string | undefined;
}

export interface WorkflowAgentMemoryListResponse {
  readonly ok: true;
  readonly memories: readonly WorkflowAgentMemoryRecord[];
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
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

export type WorkflowBranchMergeMode = "merge" | "cherry-pick";
export type WorkflowBranchMergeStatus = "clean" | "conflicts" | "blocked" | "applied";
export type WorkflowBranchMergeConflictKind =
  | "both-edited"
  | "delete-edit"
  | "add-add-id-collision"
  | "missing-edge-endpoint"
  | "schema-drift"
  | "runtime-drift"
  | "codegen-drift"
  | "validation-blocked";

export interface WorkflowBranchMergeConflict {
  readonly id: string;
  readonly kind: WorkflowBranchMergeConflictKind;
  readonly elementKind: "workflow" | "node" | "edge";
  readonly elementId?: string | undefined;
  readonly path: readonly (string | number)[];
  readonly message: string;
  readonly baseValue?: JsonValue | undefined;
  readonly sourceValue?: JsonValue | undefined;
  readonly targetValue?: JsonValue | undefined;
}

export interface WorkflowBranchMergeResolution {
  readonly conflictId: string;
  readonly choice: "source" | "target" | "manual";
  readonly value?: JsonValue | undefined;
}

export interface WorkflowBranchMergePreview {
  readonly id: string;
  readonly workflowId: string;
  readonly sourceBranchId: string;
  readonly targetBranchId: string;
  readonly mode: WorkflowBranchMergeMode;
  readonly status: WorkflowBranchMergeStatus;
  readonly createdAt: string;
  readonly baseDraftRevisionId: string;
  readonly sourceHeadDraftRevisionId: string;
  readonly targetHeadDraftRevisionId: string;
  readonly graphDiff: WorkflowGraphDiff;
  readonly conflicts: readonly WorkflowBranchMergeConflict[];
  readonly mergedWorkflow?: WorkflowSpec | undefined;
  readonly validation: WorkflowValidationResult;
  readonly summary: readonly string[];
}

export interface WorkflowBranchMergeRecord extends WorkflowBranchMergePreview {
  readonly status: WorkflowBranchMergeStatus;
  readonly appliedAt?: string | undefined;
  readonly appliedBy?: string | undefined;
  readonly mergedDraftRevisionId?: string | undefined;
  readonly resolutions: readonly WorkflowBranchMergeResolution[];
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
  readonly nextRunAt?: string | undefined;
  readonly backoffSeconds?: number | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
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
  readonly branchId?: string | undefined;
  readonly approvedRevisionId: string;
  readonly draftEvaluationId: string;
  readonly kind: WorkflowDeploymentKind;
  readonly status: "ready" | "blocked" | "deployed" | "rolled-back" | "undeployed";
  readonly createdAt: string;
  readonly createdBy: string;
  readonly requiredIntegrations: readonly string[];
  readonly secretRefs: readonly string[];
  readonly rollbackPlan: string;
  readonly auditRecordId: string;
  readonly metadata: JsonRecord;
}

export interface WorkflowDeploymentActivationRecord {
  readonly deploymentId: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly approvedRevisionId: string;
  readonly kind: WorkflowDeploymentKind;
  readonly status: "active" | "inactive";
  readonly artifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly runnerConfig?: JsonRecord | undefined;
  readonly rollbackTarget?: string | undefined;
  readonly activatedAt: string;
}

export interface WorkflowDeploymentRollbackTarget {
  readonly deploymentId: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly approvedRevisionId: string;
  readonly previousDeploymentId?: string | undefined;
  readonly rollbackPlan: string;
  readonly artifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly createdAt: string;
}

export type WorkflowConnectorKind = "http" | "openapi" | "mcp";
export type WorkflowConnectorAuthScheme = "none" | "apiKey" | "bearer" | "basic" | "oauth";

export interface WorkflowConnectorAuthRequirement {
  readonly name: string;
  readonly scheme: WorkflowConnectorAuthScheme;
  readonly location?: "header" | "query" | "cookie" | "body" | undefined;
  readonly parameterName?: string | undefined;
  readonly secretRef?: string | undefined;
  readonly description?: string | undefined;
}

export interface WorkflowConnectorOperation {
  readonly name: string;
  readonly version: string;
  readonly description: string;
  readonly inputSchema: JsonSchemaShape;
  readonly outputSchema: JsonSchemaShape;
  readonly method?: string | undefined;
  readonly path?: string | undefined;
  readonly toolName?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowConnectorTestResult {
  readonly status: "untested" | "succeeded" | "failed";
  readonly testedAt?: string | undefined;
  readonly message?: string | undefined;
  readonly operationCount?: number | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface WorkflowConnectorRecord {
  readonly id: string;
  readonly name: string;
  readonly kind: WorkflowConnectorKind;
  readonly adapterId: string;
  readonly sourceUrl?: string | undefined;
  readonly endpointUrl?: string | undefined;
  readonly transport?: "streamable-http" | "stdio" | undefined;
  readonly allowedHosts: readonly string[];
  readonly auth: readonly WorkflowConnectorAuthRequirement[];
  readonly operations: readonly WorkflowConnectorOperation[];
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly lastTest: WorkflowConnectorTestResult;
  readonly metadata?: JsonRecord | undefined;
}

export type WorkflowScheduleStatus = "active" | "paused" | "disabled";

export interface WorkflowScheduleRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly deploymentId: string;
  readonly approvedRevisionId: string;
  readonly nodeId: string;
  readonly label: string;
  readonly cron: string;
  readonly timezone: string;
  readonly status: WorkflowScheduleStatus;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly nextFireAt: string;
  readonly lastFireAt?: string | undefined;
  readonly lastRunId?: string | undefined;
  readonly lastJobId?: string | undefined;
  readonly lastError?: string | undefined;
  readonly missedCount: number;
}

export interface WorkflowAlertPolicy {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly enabled: boolean;
  readonly events: readonly (
    | "run.failed"
    | "job.failed"
    | "schedule.missed"
    | "deployment.failed"
  )[];
  readonly channels: readonly ("email" | "telegram" | "webhook")[];
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly updatedAt: string;
  readonly updatedBy: string;
}

export interface WorkflowRetentionPolicy {
  readonly workflowId: string;
  readonly branchId?: string | undefined;
  readonly maxRunEventDays: number;
  readonly maxSuccessfulRunWorkspaceDays: number;
  readonly maxFailedRunWorkspaceDays: number;
  readonly maxJobEventDays: number;
  readonly updatedAt: string;
  readonly updatedBy: string;
}

export interface WorkflowOpsHealth {
  readonly status: "ok" | "degraded";
  readonly databaseWritable: boolean;
  readonly worker: {
    readonly active: boolean;
    readonly queuedJobs: number;
    readonly runningJobs: number;
    readonly failedJobs: number;
  };
  readonly scheduler: {
    readonly active: boolean;
    readonly activeSchedules: number;
    readonly dueSchedules: number;
  };
  readonly runs: {
    readonly running: number;
    readonly resumable: number;
    readonly failed: number;
  };
  readonly connectors: {
    readonly total: number;
    readonly failedTests: number;
  };
  readonly memory: {
    readonly total: number;
    readonly expired: number;
  };
  readonly router: {
    readonly classifierVersion: string;
    readonly evalCases: number;
    readonly lastEvalPassed?: boolean | undefined;
  };
  readonly checkedAt: string;
}

export interface WorkflowAuditExportRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly exportedAt: string;
  readonly format: "jsonl";
  readonly redacted: true;
  readonly lineCount: number;
  readonly records: readonly JsonRecord[];
}

export interface WorkflowPlanRequest {
  readonly prompt: string;
  readonly currentWorkflow?: WorkflowSpec | undefined;
  readonly preserveNodeIds?: readonly string[] | undefined;
  readonly forceDeterministic?: boolean | undefined;
  readonly clarificationRequestId?: string | undefined;
  readonly clarificationAnswers?: readonly WorkflowClarificationAnswer[] | undefined;
}

export interface WorkflowClarificationQuestion {
  readonly id: string;
  readonly question: string;
  readonly required: boolean;
  readonly placeholder?: string | undefined;
}

export interface WorkflowClarificationAnswer {
  readonly questionId: string;
  readonly answer: string;
}

export interface WorkflowClarificationRequest {
  readonly id: string;
  readonly prompt: string;
  readonly reason: string;
  readonly createdAt: string;
  readonly questions: readonly WorkflowClarificationQuestion[];
}

export interface WorkflowPlanSuccessResponse {
  readonly ok: true;
  readonly status?: "planned" | undefined;
  readonly workflow: WorkflowSpec;
  readonly draftRevision: WorkflowDraftRevision;
  readonly validation: WorkflowValidationResult;
  readonly route: WorkflowTaskRoute;
}

export interface WorkflowPlanClarificationResponse {
  readonly ok: true;
  readonly status: "clarification-required";
  readonly clarification: WorkflowClarificationRequest;
  readonly route: WorkflowTaskRoute;
}

export type WorkflowPlanResponse = WorkflowPlanSuccessResponse | WorkflowPlanClarificationResponse;

export interface WorkflowCreateBranchRequest {
  readonly name: string;
  readonly createdBy: string;
  readonly fromBranchId?: string | undefined;
  readonly fromDraftRevisionId?: string | undefined;
}

export interface WorkflowCreateBranchResponse {
  readonly ok: true;
  readonly branch: WorkflowBranch;
  readonly draftRevision: WorkflowDraftRevision;
}

export interface WorkflowListBranchesResponse {
  readonly ok: true;
  readonly branches: readonly WorkflowBranch[];
}

export interface WorkflowGetBranchResponse {
  readonly ok: true;
  readonly branch: WorkflowBranch;
  readonly headDraftRevision: WorkflowDraftRevision;
  readonly promptTurns: readonly WorkflowPromptTurn[];
}

export interface WorkflowUpdateBranchRequest {
  readonly name?: string | undefined;
  readonly status?: WorkflowBranchStatus | undefined;
  readonly updatedBy: string;
}

export interface WorkflowUpdateBranchResponse {
  readonly ok: true;
  readonly branch: WorkflowBranch;
}

export interface WorkflowBranchPlanRequest extends WorkflowPlanRequest {
  readonly actor?: string | undefined;
}

export type WorkflowBranchPlanResponse =
  | (WorkflowPlanSuccessResponse & {
      readonly branch: WorkflowBranch;
      readonly promptTurn: WorkflowPromptTurn;
    })
  | WorkflowPlanClarificationResponse;

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

export interface WorkflowBranchRepromptNodeRequest extends WorkflowRepromptNodeRequest {
  readonly actor?: string | undefined;
}

export interface WorkflowBranchRepromptNodeResponse extends WorkflowRepromptNodeResponse {
  readonly branch: WorkflowBranch;
  readonly promptTurn: WorkflowPromptTurn;
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
  readonly branchId?: string | undefined;
}

export interface WorkflowAcceptPlanRequest {
  readonly workflow: WorkflowSpec;
  readonly acceptedBy: string;
}

export interface WorkflowAcceptPlanResponse {
  readonly ok: true;
  readonly workflowId: string;
  readonly draftRevisionId: string;
  readonly workflow: WorkflowSpec;
  readonly draftRevision: WorkflowDraftRevision;
  readonly validation: WorkflowValidationResult;
}

export interface WorkflowBranchMergePreviewRequest {
  readonly targetBranchId: string;
  readonly mode?: WorkflowBranchMergeMode | undefined;
  readonly cherryPickChangeIds?: readonly string[] | undefined;
}

export interface WorkflowBranchMergePreviewResponse {
  readonly ok: true;
  readonly preview: WorkflowBranchMergePreview;
}

export interface WorkflowBranchMergeRequest extends WorkflowBranchMergePreviewRequest {
  readonly appliedBy: string;
  readonly resolutions: readonly WorkflowBranchMergeResolution[];
}

export interface WorkflowBranchMergeResponse {
  readonly ok: true;
  readonly merge: WorkflowBranchMergeRecord;
  readonly branch: WorkflowBranch;
  readonly draftRevision: WorkflowDraftRevision;
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
}

export interface WorkflowReuseCandidatesResponse {
  readonly ok: true;
  readonly decisions: readonly WorkflowGeneratedModuleReuseDecision[];
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
  readonly branchId?: string | undefined;
  readonly deploymentId?: string | undefined;
}

export interface WorkflowStartRunResponse {
  readonly ok: true;
  readonly run: WorkflowRunRecord;
  readonly job?: WorkflowJob | undefined;
}

export interface WorkflowFetchRunResponse {
  readonly ok: true;
  readonly run: WorkflowRunRecord;
  readonly checkpoints?: readonly WorkflowRunCheckpoint[] | undefined;
}

export interface WorkflowListRunsResponse {
  readonly ok: true;
  readonly runs: readonly WorkflowRunRecord[];
}

export interface WorkflowListSchedulesResponse {
  readonly ok: true;
  readonly schedules: readonly WorkflowScheduleRecord[];
}

export interface WorkflowConnectorListResponse {
  readonly ok: true;
  readonly connectors: readonly WorkflowConnectorRecord[];
}

export interface WorkflowConnectorResponse {
  readonly ok: true;
  readonly connector: WorkflowConnectorRecord;
}

export interface WorkflowApiError {
  readonly ok: false;
  readonly error: string;
  readonly message: string;
  readonly validation?: WorkflowValidationResult | undefined;
  readonly issues?: readonly WorkflowValidationIssue[] | undefined;
}
