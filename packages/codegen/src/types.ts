import type {
  JsonRecord,
  WorkflowCodegenMetadata,
  WorkflowCodegenReplay
} from "@kelpclaw/workflow-spec";

export type ArtifactContentType =
  | "application/json"
  | "text/markdown"
  | "text/plain"
  | "text/typescript";

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
  readonly artifact: Pick<GeneratedArtifact, "path" | "checksum">;
  readonly replay: WorkflowCodegenReplay;
}

export type { WorkflowCodegenMetadata };
