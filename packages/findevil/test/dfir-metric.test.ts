import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  mapToExpectedFindings,
  runDfirMetricSubset,
  type DfirMetricBenchmarkReport,
  type DfirMetricCase
} from "../src/benchmark/dfir-metric.js";

const tempDirs: string[] = [];

afterEach(async () => {
  await Promise.all(tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
});

describe("DFIR-Metric Practical benchmark adapter", () => {
  it("maps NSS answers into Kelp expected findings", () => {
    const dfirCase = fakeCase({
      answer:
        '<xml>["122150:DELETED-email-iron-fat-ascii.txt","122152:LIVE-email-iron-fat.txt"]</xml>'
    });

    expect(mapToExpectedFindings(dfirCase)).toEqual([
      {
        id: "fake-nss-001:expected:1",
        claimId: "fake-nss-001:claim:1",
        type: "malware_identification",
        description: "DFIR-Metric expected answer: 122150:DELETED-email-iron-fat-ascii.txt",
        acceptedTechniques: ["T1204"]
      },
      {
        id: "fake-nss-001:expected:2",
        claimId: "fake-nss-001:claim:2",
        type: "malware_identification",
        description: "DFIR-Metric expected answer: 122152:LIVE-email-iron-fat.txt",
        acceptedTechniques: ["T1204"]
      }
    ]);
  });

  it("runs a tiny inline fake case through sentinel and aggregates category scores", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "dfir-metric-"));
    tempDirs.push(outDir);

    const report = (await runDfirMetricSubset({
      cases: [fakeCase({ answer: "243", category: "nss-count" })],
      subsetSize: 1,
      outDir
    })) as DfirMetricBenchmarkReport;

    expect(report).toMatchObject({
      dataset: "dfir-metric",
      subsetSize: 1,
      expectedFindings: 1,
      evaluatedClaims: 1,
      truePositives: 1,
      falsePositives: 0,
      falseNegatives: 0,
      precision: 1,
      recall: 1,
      f1: 1,
      perCategory: [
        expect.objectContaining({
          category: "nss-count",
          cases: 1,
          precision: 1,
          recall: 1,
          f1: 1
        })
      ]
    });
    expect(await readFile(join(outDir, "aggregate-report.json"), "utf8")).toContain(
      '"dataset": "dfir-metric"'
    );
    expect(
      await readFile(join(report.cases[0]?.outDir ?? "", "accuracy-report.md"), "utf8")
    ).toContain("DFIR-Metric Case Accuracy Report");
  });
});

function fakeCase(overrides: Partial<DfirMetricCase>): DfirMetricCase {
  return {
    id: "fake-nss-001",
    question: "Count all deleted and non deleted .txt files in the second HFS+ partition.",
    answer: "0",
    ...overrides
  };
}
