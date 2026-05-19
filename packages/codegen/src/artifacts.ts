import { createHash } from "node:crypto";
import type {
  ArtifactContentType,
  ArtifactManifest,
  CodegenMetadataInput,
  GeneratedArtifact,
  WorkflowCodegenMetadata
} from "./types.js";

export function createGeneratedArtifact(input: {
  readonly path: string;
  readonly content: string;
  readonly contentType: ArtifactContentType;
  readonly metadata?: GeneratedArtifact["metadata"];
}): GeneratedArtifact {
  assertSafeArtifactPath(input.path);

  return {
    path: input.path,
    content: input.content,
    contentType: input.contentType,
    checksum: checksumArtifactContent(input.content),
    metadata: input.metadata
  };
}

export function createArtifactManifest(input: {
  readonly workflowId: string;
  readonly generatedAt: string;
  readonly artifacts: readonly GeneratedArtifact[];
}): ArtifactManifest {
  return {
    workflowId: input.workflowId,
    generatedAt: input.generatedAt,
    artifacts: [...input.artifacts].sort((left, right) => left.path.localeCompare(right.path))
  };
}

export function checksumArtifactContent(content: string): string {
  return `sha256:${createHash("sha256").update(content, "utf8").digest("hex")}`;
}

export function createCodegenMetadata(input: CodegenMetadataInput): WorkflowCodegenMetadata {
  assertSafeArtifactPath(input.artifact.path);
  assertSafeArtifactPath(input.dependencyManifest.path);

  return {
    originalPrompt: input.originalPrompt ?? input.sourcePrompt,
    latestPrompt: input.latestPrompt ?? input.sourcePrompt,
    plannerRationale: input.plannerRationale,
    provenance: {
      generator: input.generator,
      generatedAt: input.generatedAt,
      sourcePrompt: input.sourcePrompt,
      artifactPath: input.artifact.path,
      artifactChecksum: input.artifact.checksum
    },
    artifacts: [
      ...(input.artifacts ?? [input.artifact]),
      {
        path: input.dependencyManifest.path,
        checksum: input.dependencyManifest.checksum,
        contentType: "application/json" as const
      }
    ]
      .filter(
        (artifact, index, artifacts) =>
          artifacts.findIndex((candidate) => candidate.path === artifact.path) === index
      )
      .map((artifact) => ({
        path: artifact.path,
        checksum: artifact.checksum,
        contentType: artifact.contentType
      }))
      .sort((left, right) => left.path.localeCompare(right.path)),
    dependencyManifest: input.dependencyManifest,
    sandbox: input.sandbox,
    review: input.review ?? {
      status: "draft"
    },
    replay: input.replay,
    llmBacked: input.llmBacked ?? false
  };
}

export function assertSafeArtifactPath(path: string): void {
  if (path.length === 0) {
    throw new Error("Generated artifact path cannot be empty.");
  }

  if (path.startsWith("/") || path.includes("\\") || path.split("/").includes("..")) {
    throw new Error(
      `Generated artifact path '${path}' must be relative and stay inside the workspace.`
    );
  }
}
