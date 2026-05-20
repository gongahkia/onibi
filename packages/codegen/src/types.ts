import type {
  JsonRecord,
  JsonSchemaShape,
  WorkflowCodegenArtifactContentType,
  WorkflowCodegenArtifactRef,
  WorkflowCodegenDependencyManifest,
  WorkflowCodegenMetadata,
  WorkflowCodegenReplay,
  WorkflowCodegenReview,
  WorkflowCodegenSandboxPolicy,
  WorkflowRuntime
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowAgentRole,
  WorkflowDraftEvaluationFinding,
  WorkflowJob,
  WorkflowModelInvocationRecord,
  WorkflowWorkspace
} from "@kelpclaw/workflow-spec";

export type ArtifactContentType = WorkflowCodegenArtifactContentType;

export interface GeneratedArtifact {
  readonly path: string;
  readonly content: string;
  readonly contentType: ArtifactContentType;
  readonly checksum: string;
  readonly metadata?: JsonRecord | undefined;
}

export interface ArtifactManifest {
  readonly workflowId: string;
  readonly generatedAt: string;
  readonly artifacts: readonly GeneratedArtifact[];
}

export interface StoredGeneratedArtifact {
  readonly ref: WorkflowCodegenArtifactRef;
  readonly objectPath: string;
}

export interface StoredArtifactManifest {
  readonly manifest: ArtifactManifest;
  readonly path: string;
}

export type ReplayMode = "reuse-if-unchanged" | "always-regenerate" | "fail-on-drift";

export interface ReplayPolicy {
  readonly mode: ReplayMode;
  readonly seed: string;
}

export interface ReplayDecision {
  readonly action: "reuse" | "regenerate" | "fail";
  readonly reason: string;
}

export interface CodegenMetadataInput {
  readonly generator: string;
  readonly generatedAt: string;
  readonly sourcePrompt: string;
  readonly originalPrompt?: string | undefined;
  readonly latestPrompt?: string | undefined;
  readonly plannerRationale: string;
  readonly artifact: Pick<GeneratedArtifact, "path" | "checksum" | "contentType">;
  readonly artifacts?: readonly Pick<GeneratedArtifact, "path" | "checksum" | "contentType">[];
  readonly dependencyManifest: WorkflowCodegenDependencyManifest;
  readonly sandbox: WorkflowCodegenSandboxPolicy;
  readonly review?: WorkflowCodegenReview | undefined;
  readonly replay: WorkflowCodegenReplay;
  readonly llmBacked?: boolean | undefined;
}

export interface CodegenGenerationRequest {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly prompt: string;
  readonly plannerRationale: string;
  readonly inputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly outputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly runtime: WorkflowRuntime;
  readonly sandbox: WorkflowCodegenSandboxPolicy;
  readonly generatedAt?: string | undefined;
}

export interface CodegenGenerationResult {
  readonly sourceArtifact: GeneratedArtifact;
  readonly dependencyManifestArtifact: GeneratedArtifact;
  readonly dependencyManifest: WorkflowCodegenDependencyManifest;
  readonly metadata: WorkflowCodegenMetadata;
}

export interface CodeGenerator {
  generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult>;
}

export interface GeneratedNodeDesignSpec {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly prompt: string;
  readonly plannerRationale: string;
  readonly inputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly outputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly runtime: WorkflowRuntime;
  readonly sandbox: WorkflowCodegenSandboxPolicy;
  readonly acceptanceCriteria: readonly string[];
}

export interface CodegenAgentRunRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly role: WorkflowAgentRole;
  readonly status: "succeeded" | "failed";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly inputSummary: string;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly modelProvider: string;
  readonly model: string;
  readonly modelInvocations?: readonly WorkflowModelInvocationRecord[] | undefined;
  readonly inputTokens?: number | undefined;
  readonly outputTokens?: number | undefined;
  readonly cacheReadInputTokens?: number | undefined;
  readonly cacheCreationInputTokens?: number | undefined;
  readonly totalTokens?: number | undefined;
  readonly costUsd?: number | undefined;
  readonly error?: string | undefined;
}

export interface CodegenAgentArtifactRecord {
  readonly id: string;
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly agentRunId: string;
  readonly createdAt: string;
  readonly artifact: WorkflowCodegenArtifactRef;
}

export interface GeneratedNodeBuildLoopRequest extends CodegenGenerationRequest {
  readonly job: WorkflowJob;
  readonly workspace?: WorkflowWorkspace | undefined;
  readonly workspaceRoot?: string | undefined;
  readonly maxIterations: number;
  readonly maxReimplementationAttempts?: number | undefined;
  readonly maxWallClockSeconds: number;
  readonly maxModelCostUsd: number;
  readonly maxDockerRuntimeSeconds?: number | undefined;
  readonly runTestsInDocker: boolean;
  readonly signal?: AbortSignal | undefined;
}

export type GeneratedNodeBuildRole =
  | "workflow-architect"
  | "coder"
  | "tester"
  | "runner"
  | "fixer"
  | "evaluator";

export type GeneratedNodeFixTriageAction =
  | "targeted-patch"
  | "retry-codegen"
  | "rearchitect"
  | "give-up";

export type GeneratedNodeFixTriageScope =
  | "local-code"
  | "node-contract"
  | "workflow-design"
  | "external-blocker";

export interface GeneratedNodeFixTriageDecision {
  readonly action: GeneratedNodeFixTriageAction;
  readonly scope: GeneratedNodeFixTriageScope;
  readonly rationale: string;
  readonly confidence: number;
}

export interface GeneratedNodeRoleRunInput {
  readonly role: GeneratedNodeBuildRole;
  readonly request: GeneratedNodeBuildLoopRequest;
  readonly iteration: number;
  readonly inputSummary: string;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly previousFailure?: string | undefined;
  readonly generateCode: (request: CodegenGenerationRequest) => Promise<CodegenGenerationResult>;
}

export interface GeneratedNodeRoleRunResult {
  readonly status: "succeeded" | "failed";
  readonly inputSummary: string;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly generation?: CodegenGenerationResult | undefined;
  readonly modelProvider?: string | undefined;
  readonly model?: string | undefined;
  readonly modelCostUsd?: number | undefined;
  readonly modelInvocations?: readonly WorkflowModelInvocationRecord[] | undefined;
  readonly fixTriage?: GeneratedNodeFixTriageDecision | undefined;
  readonly error?: string | undefined;
}

export interface GeneratedNodeRoleRunner {
  readonly role: GeneratedNodeBuildRole;
  run(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult>;
}

export interface GeneratedNodeTestExecution {
  readonly status: "passed" | "failed";
  readonly logs: readonly string[];
  readonly resultArtifacts: readonly GeneratedArtifact[];
  readonly schemaValid: boolean;
  readonly securityValid: boolean;
  readonly replayValid: boolean;
  readonly dependencyPolicyValid: boolean;
  readonly findings: readonly WorkflowDraftEvaluationFinding[];
  readonly failureMessage?: string | undefined;
}

export interface DockerGeneratedNodeCommand {
  readonly executable: string;
  readonly args: readonly string[];
  readonly workspaceRoot: string;
  readonly network: "none" | "bridge";
  readonly timeoutMs: number;
  readonly inputPath: string;
  readonly outputPath: string;
  readonly stdoutPath: string;
  readonly stderrPath: string;
}

export interface DockerGeneratedNodeCommandResult {
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly timedOut?: boolean | undefined;
  readonly output?: unknown;
}

export interface DockerGeneratedNodeCommandRunner {
  run(
    command: DockerGeneratedNodeCommand,
    signal?: AbortSignal | undefined
  ): Promise<DockerGeneratedNodeCommandResult>;
}

export interface GeneratedNodeTestExecutor {
  execute(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly generation: CodegenGenerationResult;
    readonly testArtifacts: readonly GeneratedArtifact[];
    readonly iteration: number;
  }): Promise<GeneratedNodeTestExecution>;
}

export interface GeneratedNodeBuildLoopResult {
  readonly status: "passed" | "failed";
  readonly generation: CodegenGenerationResult;
  readonly designSpecArtifact: GeneratedArtifact;
  readonly testArtifacts: readonly GeneratedArtifact[];
  readonly resultArtifacts: readonly GeneratedArtifact[];
  readonly agentRuns: readonly CodegenAgentRunRecord[];
  readonly agentArtifacts: readonly CodegenAgentArtifactRecord[];
  readonly fixHistory: readonly string[];
  readonly logs: readonly string[];
  readonly schemaValid: boolean;
  readonly securityValid: boolean;
  readonly replayValid: boolean;
  readonly dependencyPolicyValid: boolean;
  readonly findings: readonly WorkflowDraftEvaluationFinding[];
  readonly unresolvedFailureArtifact?: GeneratedArtifact | undefined;
}

export type {
  WorkflowCodegenArtifactRef,
  WorkflowCodegenDependencyManifest,
  WorkflowCodegenMetadata,
  WorkflowCodegenSandboxPolicy
};
