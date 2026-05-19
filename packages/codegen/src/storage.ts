import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import type {
  ArtifactManifest,
  GeneratedArtifact,
  StoredArtifactManifest,
  StoredGeneratedArtifact,
  WorkflowCodegenArtifactRef
} from "./types.js";
import { assertSafeArtifactPath, checksumArtifactContent } from "./artifacts.js";

export interface CodegenArtifactStore {
  putArtifact(artifact: GeneratedArtifact): Promise<StoredGeneratedArtifact>;
  putManifest(manifest: ArtifactManifest): Promise<StoredArtifactManifest>;
  readArtifact(ref: WorkflowCodegenArtifactRef): Promise<string>;
  verifyArtifact(ref: WorkflowCodegenArtifactRef): Promise<boolean>;
  materializeArtifacts(
    refs: readonly WorkflowCodegenArtifactRef[],
    targetRoot: string
  ): Promise<readonly string[]>;
}

export class LocalCodegenArtifactStore implements CodegenArtifactStore {
  public readonly root: string;

  public constructor(root = defaultCodegenArtifactStoreRoot()) {
    this.root = root;
  }

  public async putArtifact(artifact: GeneratedArtifact): Promise<StoredGeneratedArtifact> {
    assertSafeArtifactPath(artifact.path);
    const checksum = checksumArtifactContent(artifact.content);
    if (checksum !== artifact.checksum) {
      throw new Error(`Generated artifact '${artifact.path}' checksum does not match content.`);
    }

    const objectPath = this.objectPathForChecksum(artifact.checksum);
    await mkdir(dirname(objectPath), { recursive: true });
    await writeFile(objectPath, artifact.content, "utf8");

    return {
      ref: {
        path: artifact.path,
        checksum: artifact.checksum,
        contentType: artifact.contentType
      },
      objectPath
    };
  }

  public async putManifest(manifest: ArtifactManifest): Promise<StoredArtifactManifest> {
    for (const artifact of manifest.artifacts) {
      await this.putArtifact(artifact);
    }

    const manifestPath = join(
      this.root,
      "workflows",
      sanitizePathPart(manifest.workflowId),
      `${sanitizePathPart(manifest.generatedAt)}.json`
    );
    await mkdir(dirname(manifestPath), { recursive: true });
    await writeFile(manifestPath, JSON.stringify(manifest, null, 2), "utf8");

    return {
      manifest,
      path: manifestPath
    };
  }

  public async readArtifact(ref: WorkflowCodegenArtifactRef): Promise<string> {
    assertSafeArtifactPath(ref.path);
    const content = await readFile(this.objectPathForChecksum(ref.checksum), "utf8");
    const checksum = checksumArtifactContent(content);
    if (checksum !== ref.checksum) {
      throw new Error(`Generated artifact '${ref.path}' content hash drifted.`);
    }

    return content;
  }

  public async verifyArtifact(ref: WorkflowCodegenArtifactRef): Promise<boolean> {
    try {
      await this.readArtifact(ref);
      return true;
    } catch {
      return false;
    }
  }

  public async materializeArtifacts(
    refs: readonly WorkflowCodegenArtifactRef[],
    targetRoot: string
  ): Promise<readonly string[]> {
    const materialized: string[] = [];
    for (const ref of refs) {
      assertSafeArtifactPath(ref.path);
      const targetPath = join(targetRoot, ref.path);
      const content = await this.readArtifact(ref);
      await mkdir(dirname(targetPath), { recursive: true });
      await writeFile(targetPath, content, "utf8");
      materialized.push(targetPath);
    }

    return materialized.sort();
  }

  private objectPathForChecksum(checksum: string): string {
    const hash = checksum.replace(/^sha256:/u, "");
    if (!/^[a-f0-9]{64}$/u.test(hash)) {
      throw new Error(`Generated artifact checksum '${checksum}' is invalid.`);
    }

    return join(this.root, "objects", "sha256", hash.slice(0, 2), hash);
  }
}

export function defaultCodegenArtifactStoreRoot(cwd = process.cwd()): string {
  return process.env.KELPCLAW_ARTIFACT_STORE ?? join(cwd, ".kelpclaw", "artifacts");
}

function sanitizePathPart(value: string): string {
  return value.replace(/[^a-zA-Z0-9._-]+/gu, "_");
}
