import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { attackCatalog } from "../src/attack/index.js";
import { runBenchmark } from "../src/benchmark/benchmark.js";
import { score } from "../src/benchmark/scorer.js";
import type { ExpectedFinding } from "../src/benchmark/types.js";
import { claimLedgerSchema, type Claim } from "../src/types/claim.js";

const testEvidenceHash = `sha256:${"0".repeat(64)}`;
const repoRoot = fileURLToPath(new URL("../../..", import.meta.url));

const expectedFindings: readonly ExpectedFinding[] = [
  {
    id: "finding-001",
    claimId: "claim-001",
    acceptedTechniques: ["T1059"]
  },
  {
    id: "finding-002",
    claimId: "claim-002",
    acceptedTechniques: ["T1547"]
  },
  {
    id: "finding-003",
    claimId: "claim-003",
    acceptedTechniques: ["T1071"]
  }
];

describe("ground-truth benchmark scoring", () => {
  it("scores confirmed claims against expected ATT&CK techniques", () => {
    const ledger = claimLedgerSchema.parse({
      id: "ledger-benchmark-test",
      generatedAt: "2026-05-30T00:00:00.000Z",
      claims: [
        claim("claim-001", "confirmed", "T1059"),
        claim("claim-002", "confirmed", "T1059"),
        claim("claim-003", "inferred", "T1071"),
        claim("claim-extra", "confirmed", "T1003")
      ]
    });

    expect(score(ledger, expectedFindings)).toEqual({
      truePositives: 1,
      falsePositives: 2,
      falseNegatives: 2,
      precision: 1 / 3,
      recall: 1 / 3,
      f1: 1 / 3
    });

    const report = runBenchmark({ expectedFindings }, ledger);
    expect(report.matches).toMatchObject([
      { expectedFindingId: "finding-001", truePositive: true },
      {
        expectedFindingId: "finding-002",
        truePositive: false,
        falsePositive: true,
        falseNegative: true
      },
      { expectedFindingId: "finding-003", truePositive: false, falseNegative: true }
    ]);
    expect(report.unmatchedFalsePositiveClaims).toMatchObject([
      { claimId: "claim-extra", falsePositive: true }
    ]);
  });

  it("parses expected findings from the case manifest shape", () => {
    const ledger = claimLedgerSchema.parse({
      id: "ledger-yaml-benchmark-test",
      generatedAt: "2026-05-30T00:00:00.000Z",
      claims: [claim("claim-001", "confirmed", "T1059")]
    });
    const report = runBenchmark(
      [
        "expectedFindings:",
        "  - id: finding-001",
        "    claimId: claim-001",
        "    type: program_execution",
        "    acceptedTechniques: [T1059, T1204]",
        "    description: PowerShell execution."
      ].join("\n"),
      ledger
    );

    expect(report).toMatchObject({
      truePositives: 1,
      falsePositives: 0,
      falseNegatives: 0,
      precision: 1,
      recall: 1,
      f1: 1
    });
  });

  it("parses the CFReDS Hacking Case pilot manifest", async () => {
    const manifest = await readFile(
      join(repoRoot, "examples/findevil-cfreds-hacking-case/case.yml"),
      "utf8"
    );
    const report = runBenchmark(
      manifest,
      claimLedgerSchema.parse({
        id: "ledger-hacking-case-smoke-test",
        generatedAt: "2026-05-30T00:00:00.000Z",
        claims: [claim("claim-001", "confirmed", "T1005")]
      })
    );

    expect(report.expectedFindings).toBe(8);
    expect(report.truePositives).toBe(1);
  });
});

function claim(
  id: string,
  status: Claim["status"],
  techniqueId: keyof typeof attackCatalog
): Claim {
  return {
    id,
    text: `${id} synthetic claim`,
    type: "program_execution",
    severity: "high",
    status,
    confidence: 0.8,
    attackTechniques: [attackCatalog[techniqueId]],
    evidenceRefs: [
      {
        artifact: "fixture.txt",
        locator: `claim:${id}`,
        supports: "synthetic",
        hash: testEvidenceHash
      }
    ],
    missingEvidence: []
  };
}
