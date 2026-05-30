import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { extractClaimsCommittee, type CommitteeModelSpec } from "../src/extractor/committee.js";
import type { Claim, ClaimLedger } from "../src/types/claim.js";

const models: CommitteeModelSpec[] = [
  { provider: "anthropic", model: "judge-a", weight: 1 },
  { provider: "openai", model: "judge-b", weight: 1 },
  { provider: "anthropic", model: "judge-c", weight: 1 }
];

describe("extractClaimsCommittee", () => {
  it("keeps unanimous claims at confidence 1.0", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-committee-unanimous-"));
    const ledger = await extractClaimsCommittee("report", models, {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z",
      committeeComplete: async () => ledgerWith([claim()])
    });

    expect(ledger.claims).toHaveLength(1);
    expect(ledger.claims[0]?.confidence).toBe(1);
    expect(ledger.claims[0]?.status).toBe("confirmed");
    expect(await jsonl(join(outDir, "committee-vote.jsonl"))).toHaveLength(3);
  });

  it("downgrades two-of-three agreement below quorum", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-committee-partial-"));
    const ledger = await extractClaimsCommittee("report", models, {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z",
      committeeComplete: async ({ model }) =>
        model.model === "judge-c" ? ledgerWith([]) : ledgerWith([claim()])
    });

    expect(ledger.claims).toHaveLength(1);
    expect(ledger.claims[0]?.confidence).toBeCloseTo(0.67, 2);
    expect(ledger.claims[0]?.status).toBe("inferred");
  });

  it("drops claims with no cross-model agreement", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-committee-none-"));
    const ledger = await extractClaimsCommittee("report", models, {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z",
      committeeComplete: async ({ model }) =>
        ledgerWith([
          claim({
            id: `claim-${model.model}`,
            text: `${model.model} saw a unique finding.`
          })
        ])
    });

    expect(ledger.claims).toHaveLength(0);
  });
});

function ledgerWith(claims: readonly Claim[]): ClaimLedger {
  return {
    id: "claim-ledger-test",
    generatedAt: "2026-05-30T00:00:00.000Z",
    claims
  };
}

function claim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-0001",
    text: "evil.exe executed from C:\\Users\\Public\\Downloads.",
    type: "program_execution",
    severity: "high",
    status: "confirmed",
    confidence: 1,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}

async function jsonl(path: string): Promise<unknown[]> {
  return (await readFile(path, "utf8"))
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as unknown);
}
