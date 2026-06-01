import { mkdtemp, readFile, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { runSentinel } from "../src/sentinel/index.js";
import type { ClaimStatus } from "../src/types/claim.js";

const confirmedOrDowngraded = new Set<ClaimStatus>(["confirmed", "inferred", "unverifiable"]);
const repoRoot = fileURLToPath(new URL("../../..", import.meta.url));

describe("sentinel integration", () => {
  it("runs the offline Protocol SIFT trace through all sentinel layers", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-sentinel-"));
    const result = await runSentinel({
      casePath: join(repoRoot, "examples/findevil-sift-sentinel/case.yml"),
      tracePath: join(repoRoot, "fixtures/protocol-sift-baseline/baseline.jsonl"),
      maxIterations: 3,
      evidenceRoot: join(repoRoot, "examples/findevil-sift-sentinel/case-data"),
      outDir,
      timestampMode: "skip"
    });

    expect(result.ok).toBe(true);
    await expectExists(join(outDir, "agent-execution.jsonl"));
    await expectExists(join(outDir, "claim-ledger.json"));
    await expectExists(join(outDir, "repair-trace.jsonl"));
    await expectExists(join(outDir, "taint-ledger.jsonl"));
    await expectExists(join(outDir, "firewall-events.jsonl"));
    await expectExists(join(outDir, "spoliation-check.json"));
    await expectExists(join(outDir, "evidence-manifest.json"));
    await expectExists(join(outDir, "accuracy-report.md"));
    await expectExists(join(outDir, "audit-bundle", "index.html"));
    await expectExists(join(outDir, "audit-bundle", "manifest.json"));
    await expectExists(join(outDir, "audit-bundle", "attestation.json"));
    await expect(
      readFile(join(outDir, "audit-bundle", "evidence-manifest.tsr"))
    ).rejects.toMatchObject({ code: "ENOENT" });

    expect(claimFlippedAfterRepair(result)).toBe(true);
    expect(jsonl(await readFile(join(outDir, "firewall-events.jsonl"), "utf8"))).not.toHaveLength(
      0
    );
    expect(JSON.parse(await readFile(join(outDir, "spoliation-check.json"), "utf8"))).toMatchObject(
      {
        ok: true
      }
    );
  });
});

async function expectExists(path: string): Promise<void> {
  await expect(stat(path)).resolves.toMatchObject({ size: expect.any(Number) });
}

function claimFlippedAfterRepair(result: Awaited<ReturnType<typeof runSentinel>>): boolean {
  const repairedById = new Map(result.claimLedger?.claims.map((claim) => [claim.id, claim]));
  return (
    result.baselineLedger?.claims.some((baseline) => {
      const repaired = repairedById.get(baseline.id);
      return (
        baseline.status === "unsupported" &&
        repaired !== undefined &&
        repaired.status !== baseline.status &&
        confirmedOrDowngraded.has(repaired.status)
      );
    }) ?? false
  );
}

function jsonl(input: string): unknown[] {
  return input
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as unknown);
}
