import { checksumArtifactContent } from "@kelpclaw/codegen";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, readdir, readFile } from "node:fs/promises";
import { relative, resolve, sep, join } from "node:path";
import type { EvidenceFileHash } from "../types/spoliation.js";

const codegenCompatibleHashMaxBytes = 16 * 1024 * 1024;

export async function hashEvidenceTree(root: string): Promise<EvidenceFileHash[]> {
  const absoluteRoot = resolve(root);
  const rootStat = await lstat(absoluteRoot);
  if (!rootStat.isDirectory()) {
    throw new Error(`Evidence root '${root}' must be a directory.`);
  }

  const hashes: EvidenceFileHash[] = [];
  await walkEvidenceTree(absoluteRoot, absoluteRoot, hashes);
  return hashes.sort((left, right) => left.path.localeCompare(right.path));
}

async function walkEvidenceTree(
  root: string,
  directory: string,
  hashes: EvidenceFileHash[]
): Promise<void> {
  const entries = (await readdir(directory, { withFileTypes: true })).sort((left, right) =>
    left.name.localeCompare(right.name)
  );

  for (const entry of entries) {
    const absolutePath = join(directory, entry.name);
    if (entry.isDirectory()) {
      await walkEvidenceTree(root, absolutePath, hashes);
      continue;
    }

    if (!entry.isFile()) {
      continue;
    }

    const stats = await lstat(absolutePath);
    const sha256 = await checksumEvidenceFile(absolutePath, stats.size);
    hashes.push({
      path: evidenceRelativePath(root, absolutePath),
      sha256,
      sizeBytes: stats.size
    });
  }
}

async function checksumEvidenceFile(filePath: string, sizeBytes: number): Promise<string> {
  if (sizeBytes <= codegenCompatibleHashMaxBytes) {
    const content = await readFile(filePath);
    const text = content.toString("utf8");
    if (Buffer.from(text, "utf8").equals(content)) {
      return checksumArtifactContent(text);
    }

    return `sha256:${createHash("sha256").update(content).digest("hex")}`;
  }

  return streamSha256(filePath);
}

function streamSha256(filePath: string): Promise<string> {
  return new Promise((resolveHash, reject) => {
    const hash = createHash("sha256");
    const stream = createReadStream(filePath);

    stream.on("data", (chunk) => hash.update(chunk));
    stream.on("error", reject);
    stream.on("end", () => resolveHash(`sha256:${hash.digest("hex")}`));
  });
}

function evidenceRelativePath(root: string, absolutePath: string): string {
  return relative(root, absolutePath).split(sep).join("/");
}
