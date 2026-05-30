import {
  type EvidenceFileHash,
  type SpoliationCheck
} from "../types/spoliation.js";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { checksumArtifactContent } from "@kelpclaw/codegen";

export { hashEvidenceTree } from "./hashing.js";
export { checkReadOnlyMount } from "./mount.js";
export type { ReadOnlyMountCheck, ReadOnlyMountWarning } from "./mount.js";

export function spoliationCheck(
  before: readonly EvidenceFileHash[],
  after: readonly EvidenceFileHash[]
): SpoliationCheck {
  const sortedBefore = sortHashes(before);
  const sortedAfter = sortHashes(after);
  const beforeByPath = new Map(sortedBefore.map((entry) => [entry.path, entry]));
  const afterByPath = new Map(sortedAfter.map((entry) => [entry.path, entry]));
  const added = sortedAfter
    .filter((entry) => !beforeByPath.has(entry.path))
    .map((entry) => entry.path);
  const removed = sortedBefore
    .filter((entry) => !afterByPath.has(entry.path))
    .map((entry) => entry.path);
  const changed = sortedAfter
    .filter((entry) => {
      const previous = beforeByPath.get(entry.path);
      return (
        previous !== undefined &&
        (previous.sha256 !== entry.sha256 || previous.sizeBytes !== entry.sizeBytes)
      );
    })
    .map((entry) => entry.path);

  return {
    id: spoliationCheckId(sortedBefore, sortedAfter),
    root: ".",
    checkedAt: new Date().toISOString(),
    ok: added.length === 0 && removed.length === 0 && changed.length === 0,
    before: sortedBefore,
    after: sortedAfter,
    added,
    removed,
    changed
  };
}

export async function writeManifest(
  manifestPath: string,
  hashes: readonly EvidenceFileHash[]
): Promise<void> {
  await mkdir(dirname(manifestPath), { recursive: true });
  await writeFile(manifestPath, `${JSON.stringify(sortHashes(hashes), null, 2)}\n`, "utf8");
}

function sortHashes(hashes: readonly EvidenceFileHash[]): EvidenceFileHash[] {
  return [...hashes].sort((left, right) => left.path.localeCompare(right.path));
}

function spoliationCheckId(
  before: readonly EvidenceFileHash[],
  after: readonly EvidenceFileHash[]
): string {
  return `spoliation-check-${checksumArtifactContent(
    JSON.stringify({ before, after })
  ).replace(/^sha256:/u, "")}`;
}
