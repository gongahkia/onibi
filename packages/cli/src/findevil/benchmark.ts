import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { assertKnownFlags, integerOption, option, requiredOption } from "./sentinel.js";

interface DfirMetricCliReport {
  readonly dataset: "dfir-metric";
  readonly outDir: string;
  readonly subsetSize: number;
  readonly truePositives: number;
  readonly falsePositives: number;
  readonly falseNegatives: number;
  readonly confirmedClaims: number;
  readonly hallucinationDefinition: string;
  readonly hallucinationCount: number;
  readonly hallucinationRate: number;
  readonly precision: number;
  readonly recall: number;
  readonly f1: number;
  readonly perCategory?: readonly unknown[] | undefined;
}

type RunDfirMetricSubset = (opts: {
  readonly subsetSize?: number | undefined;
  readonly outDir?: string | undefined;
}) => Promise<DfirMetricCliReport>;

const requireFromHere = createRequire(import.meta.url);

export async function runFindEvilBenchmarkCommand(args: readonly string[]): Promise<void> {
  try {
    const options = parseBenchmarkArgs(args);
    const runDfirMetricSubset = await loadRunDfirMetricSubset();
    const report = await runDfirMetricSubset({
      subsetSize: options.subsetSize,
      outDir: options.outDir
    });
    process.stdout.write(
      `${JSON.stringify(
        {
          ok: true,
          dataset: report.dataset,
          subsetSize: report.subsetSize,
          outDir: report.outDir,
          aggregateReport: join(report.outDir, "aggregate-report.json"),
          aggregateAccuracyReport: join(report.outDir, "aggregate-accuracy-report.md"),
          truePositives: report.truePositives,
          falsePositives: report.falsePositives,
          falseNegatives: report.falseNegatives,
          confirmedClaims: report.confirmedClaims,
          hallucinationDefinition: report.hallucinationDefinition,
          hallucinationCount: report.hallucinationCount,
          hallucinationRate: report.hallucinationRate,
          precision: report.precision,
          recall: report.recall,
          f1: report.f1,
          perCategory: report.perCategory ?? []
        },
        null,
        2
      )}\n`
    );
    process.exitCode = 0;
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 2;
  }
}

interface ParsedBenchmarkArgs {
  readonly dataset: "dfir-metric";
  readonly subsetSize: number;
  readonly outDir: string;
}

function parseBenchmarkArgs(args: readonly string[]): ParsedBenchmarkArgs {
  assertKnownFlags(args, ["--dataset", "--subset-size", "--out"]);
  const dataset = requiredOption(args, "--dataset");
  if (dataset !== "dfir-metric") {
    throw new Error(
      "Usage: kelp-claw findevil benchmark --dataset dfir-metric [--subset-size 10] --out DIR"
    );
  }
  return {
    dataset,
    subsetSize: integerOption(option(args, "--subset-size") ?? "10", "--subset-size"),
    outDir: requiredOption(args, "--out")
  };
}

async function loadRunDfirMetricSubset(): Promise<RunDfirMetricSubset> {
  const entry = requireFromHere.resolve("@kelpclaw/findevil");
  const moduleUrl = pathToFileURL(join(dirname(entry), "benchmark", "dfir-metric.js")).href;
  const module = (await import(moduleUrl)) as {
    readonly runDfirMetricSubset?: unknown;
  };
  if (typeof module.runDfirMetricSubset !== "function") {
    throw new Error("@kelpclaw/findevil benchmark adapter does not export runDfirMetricSubset.");
  }
  return module.runDfirMetricSubset as RunDfirMetricSubset;
}
