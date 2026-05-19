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

export type {
  WorkflowCodegenArtifactRef,
  WorkflowCodegenDependencyManifest,
  WorkflowCodegenMetadata,
  WorkflowCodegenSandboxPolicy
};
