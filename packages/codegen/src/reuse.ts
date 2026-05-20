import {
  stableJsonStringify,
  type WorkflowGeneratedModuleSignature,
  type WorkflowNode
} from "@kelpclaw/workflow-spec";
import { checksumArtifactContent } from "./artifacts.js";

export function createGeneratedModuleSignature(
  node: WorkflowNode
): WorkflowGeneratedModuleSignature {
  if (!node.codegen) {
    throw new Error(`Workflow node '${node.id}' does not have generated module metadata.`);
  }

  return {
    promptHash: hashValue(node.codegen.latestPrompt),
    inputSchemaHash: hashValue(node.inputs),
    outputSchemaHash: hashValue(node.outputs),
    runtimeHash: hashValue(node.runtime),
    sandboxHash: hashValue(node.codegen.sandbox),
    dependencyManifestHash: hashValue(node.codegen.dependencyManifest),
    replaySeed: node.codegen.replay.seed,
    artifactHash: hashValue(
      node.codegen.artifacts
        .map((artifact) => ({
          path: artifact.path,
          checksum: artifact.checksum,
          contentType: artifact.contentType
        }))
        .sort((left, right) => left.path.localeCompare(right.path))
    )
  };
}

export function generatedModuleSignaturesMatch(
  left: WorkflowGeneratedModuleSignature,
  right: WorkflowGeneratedModuleSignature
): boolean {
  return stableJsonStringify(left as never) === stableJsonStringify(right as never);
}

function hashValue(value: unknown): string {
  return checksumArtifactContent(
    typeof value === "string" ? value : stableJsonStringify(value as never)
  );
}
