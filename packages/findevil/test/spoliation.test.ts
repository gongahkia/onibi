import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { hashEvidenceTree, spoliationCheck, writeManifest } from "../src/spoliation/index.js";

describe("spoliation guard", () => {
  it("hashes evidence deterministically and reports mutations", async () => {
    const evidenceRoot = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-"));
    const outputRoot = await mkdtemp(join(tmpdir(), "kelpclaw-spoliation-output-"));

    try {
      await mkdir(join(evidenceRoot, "nested"), { recursive: true });
      await writeFile(join(evidenceRoot, "alpha.txt"), "alpha", "utf8");
      await writeFile(join(evidenceRoot, "nested", "bravo.txt"), "bravo", "utf8");

      const before = await hashEvidenceTree(evidenceRoot);
      expect(before.map((entry) => entry.path)).toEqual(["alpha.txt", "nested/bravo.txt"]);

      await writeFile(join(evidenceRoot, "nested", "bravo.txt"), "bravo-mutated", "utf8");

      const after = await hashEvidenceTree(evidenceRoot);
      const check = spoliationCheck(before, after);

      expect(check).toMatchObject({
        ok: false,
        added: [],
        removed: [],
        changed: ["nested/bravo.txt"]
      });
      expect(check.before).toHaveLength(2);
      expect(check.after).toHaveLength(2);

      const manifestPath = join(outputRoot, "evidence-manifest.json");
      await writeManifest(manifestPath, before);
      expect(JSON.parse(await readFile(manifestPath, "utf8"))).toEqual(before);
    } finally {
      await rm(evidenceRoot, { recursive: true, force: true });
      await rm(outputRoot, { recursive: true, force: true });
    }
  });
});
