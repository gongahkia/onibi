import type { GeneratedArtifact, WorkflowCodegenDependencyManifest } from "./types.js";
import { createGeneratedArtifact } from "./artifacts.js";

export interface DependencyManifestInput {
  readonly path?: string | undefined;
  readonly packageManager: WorkflowCodegenDependencyManifest["packageManager"];
  readonly dependencies?: readonly string[] | undefined;
  readonly devDependencies?: readonly string[] | undefined;
  readonly installCommand?: readonly string[] | undefined;
}

export function createDependencyManifestArtifact(
  input: DependencyManifestInput
): GeneratedArtifact {
  const manifest = normalizeDependencyManifest(input);
  assertDependencyManifestPolicy(manifest);

  return createGeneratedArtifact({
    path: manifest.path,
    content: JSON.stringify(
      {
        packageManager: manifest.packageManager,
        dependencies: manifest.dependencies,
        devDependencies: manifest.devDependencies,
        installCommand: manifest.installCommand
      },
      null,
      2
    ),
    contentType: "application/json"
  });
}

export function dependencyManifestFromArtifact(
  artifact: Pick<GeneratedArtifact, "path" | "checksum">,
  input: DependencyManifestInput
): WorkflowCodegenDependencyManifest {
  const manifest = normalizeDependencyManifest({ ...input, path: artifact.path });
  return {
    ...manifest,
    checksum: artifact.checksum
  };
}

export function assertDependencyManifestPolicy(
  manifest: Pick<
    WorkflowCodegenDependencyManifest,
    "packageManager" | "dependencies" | "devDependencies" | "installCommand"
  >
): void {
  const dependencies = [...manifest.dependencies, ...manifest.devDependencies];
  if (manifest.packageManager === "none") {
    if (dependencies.length > 0 || manifest.installCommand.length > 0) {
      throw new Error("Dependency manifest with packageManager 'none' cannot install packages.");
    }
    return;
  }

  if (manifest.installCommand.length === 0) {
    throw new Error("Dependency manifest with packages must include an install command.");
  }

  const unpinned = dependencies.find((dependency) => !isPinnedDependency(dependency));
  if (unpinned) {
    throw new Error(`Generated dependency '${unpinned}' must be pinned to an explicit version.`);
  }
}

function normalizeDependencyManifest(
  input: DependencyManifestInput
): Omit<WorkflowCodegenDependencyManifest, "checksum"> {
  return {
    path: input.path ?? "generated/package-manifest.json",
    packageManager: input.packageManager,
    dependencies: [...(input.dependencies ?? [])].sort(),
    devDependencies: [...(input.devDependencies ?? [])].sort(),
    installCommand: [...(input.installCommand ?? [])]
  };
}

function isPinnedDependency(dependency: string): boolean {
  const separator = dependency.lastIndexOf("@");
  return separator > 0 && separator < dependency.length - 1 && !dependency.endsWith("@latest");
}
