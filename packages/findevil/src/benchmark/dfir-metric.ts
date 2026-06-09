import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { runSentinel } from "../sentinel/index.js";
import type { Claim, ClaimLedger } from "../types/claim.js";
import { runBenchmark } from "./benchmark.js";
import { hallucinationDefinition } from "./scorer.js";
import type { BenchmarkReport, BenchmarkScore, ExpectedFinding } from "./types.js";

export interface DfirMetricCase {
  readonly id: string;
  readonly question: string;
  readonly answer: unknown;
  readonly category?: string | undefined;
  readonly source?: string | undefined;
}

export interface DfirMetricCaseReport {
  readonly caseId: string;
  readonly category: string;
  readonly outDir: string;
  readonly expectedFindings: readonly ExpectedFinding[];
  readonly report: BenchmarkReport;
}

export interface DfirMetricCategoryReport extends BenchmarkScore {
  readonly category: string;
  readonly cases: number;
  readonly expectedFindings: number;
  readonly evaluatedClaims: number;
}

export interface DfirMetricBenchmarkReport extends BenchmarkReport {
  readonly dataset: "dfir-metric";
  readonly datasetUrl: string;
  readonly datasetLicense: string;
  readonly evaluationMode: "blind-trace-no-answer-evidence";
  readonly subsetSize: number;
  readonly outDir: string;
  readonly cases: readonly DfirMetricCaseReport[];
  readonly perCategory: readonly DfirMetricCategoryReport[];
}

export interface RunDfirMetricSubsetOptions {
  readonly subsetSize?: number | undefined;
  readonly outDir?: string | undefined;
  readonly cases?: readonly DfirMetricCase[] | undefined;
  readonly runSentinelImpl?: typeof runSentinel | undefined;
}

interface DfirMetricManifestEntry {
  readonly module: "NSS";
  readonly filename: string;
  readonly url: string;
  readonly sha256: string;
  readonly repoUrl: string;
  readonly license: string;
}

const dfirMetricCacheDir = ".kelpclaw/datasets/dfir-metric";
const defaultSubsetSize = 10;
const benchmarkClaimType: Claim["type"] = "malware_identification";
const benchmarkTechniqueId = "T1204";

export const dfirMetricManifest = {
  practical: {
    module: "NSS",
    filename: "DFIR-Metric-NSS.json",
    url: "https://raw.githubusercontent.com/DFIR-Metric/DFIR-Metric/main/DFIR-Metric-NSS.json",
    sha256: "c180284ffd249d16813050690f1da5328f41b742372905205d03851e45e5dc7f",
    repoUrl: "https://github.com/DFIR-Metric/DFIR-Metric",
    license: "NOASSERTION: upstream repository declares no license"
  }
} as const satisfies Record<string, DfirMetricManifestEntry>;

export async function loadDfirMetricPractical(): Promise<DfirMetricCase[]> {
  const datasetPath = await cachedDatasetPath(dfirMetricManifest.practical);
  return parseDfirMetricCases(JSON.parse(await readFile(datasetPath, "utf8")));
}

export function mapToExpectedFindings(dfirCase: DfirMetricCase): ExpectedFinding[] {
  return answerValues(dfirCase.answer).map((answer, index) => {
    const ordinal = index + 1;
    return {
      id: `${dfirCase.id}:expected:${ordinal}`,
      claimId: claimId(dfirCase.id, ordinal),
      type: benchmarkClaimType,
      description: `DFIR-Metric expected answer: ${answer}`,
      acceptedTechniques: [benchmarkTechniqueId]
    };
  });
}

export async function runDfirMetricSubset(
  opts: RunDfirMetricSubsetOptions = {}
): Promise<BenchmarkReport> {
  const cases = opts.cases ?? (await loadDfirMetricPractical());
  const subsetSize = opts.subsetSize ?? defaultSubsetSize;
  if (!Number.isInteger(subsetSize) || subsetSize <= 0) {
    throw new Error("DFIR-Metric subsetSize must be a positive integer.");
  }
  const selectedCases = cases.slice(0, subsetSize);
  if (selectedCases.length === 0) {
    throw new Error("DFIR-Metric subset is empty.");
  }

  const outDir = resolve(opts.outDir ?? ".kelpclaw/findevil/benchmark/dfir-metric");
  const run = opts.runSentinelImpl ?? runSentinel;
  await mkdir(outDir, { recursive: true });

  const caseReports: DfirMetricCaseReport[] = [];
  for (const [index, dfirCase] of selectedCases.entries()) {
    const caseReport = await runDfirMetricCase({
      dfirCase,
      index,
      outDir,
      runSentinelImpl: run
    });
    caseReports.push(caseReport);
  }

  const aggregate = aggregateBenchmarkReports(caseReports.map((caseReport) => caseReport.report));
  const report: DfirMetricBenchmarkReport = {
    ...aggregate,
    dataset: "dfir-metric",
    datasetUrl: dfirMetricManifest.practical.repoUrl,
    datasetLicense: dfirMetricManifest.practical.license,
    evaluationMode: "blind-trace-no-answer-evidence",
    subsetSize: selectedCases.length,
    outDir,
    cases: caseReports,
    perCategory: categoryReports(caseReports)
  };
  await writeJson(join(outDir, "aggregate-report.json"), report);
  await writeFile(
    join(outDir, "aggregate-accuracy-report.md"),
    renderAggregateReport(report),
    "utf8"
  );
  return report;
}

async function cachedDatasetPath(entry: DfirMetricManifestEntry): Promise<string> {
  const cacheDir = resolve(dfirMetricCacheDir);
  const target = join(cacheDir, entry.filename);
  await mkdir(cacheDir, { recursive: true });
  const cached = await readOptionalFile(target);
  if (cached && sha256Hex(cached) === entry.sha256) {
    return target;
  }

  const response = await fetch(entry.url);
  if (!response.ok) {
    throw new Error(`Failed to download DFIR-Metric dataset ${entry.url}: HTTP ${response.status}`);
  }
  const downloaded = Buffer.from(await response.arrayBuffer());
  const actual = sha256Hex(downloaded);
  if (actual !== entry.sha256) {
    throw new Error(
      `DFIR-Metric SHA-256 mismatch for ${entry.filename}: expected ${entry.sha256}, got ${actual}.`
    );
  }
  await writeFile(target, downloaded);
  return target;
}

async function runDfirMetricCase(input: {
  readonly dfirCase: DfirMetricCase;
  readonly index: number;
  readonly outDir: string;
  readonly runSentinelImpl: typeof runSentinel;
}): Promise<DfirMetricCaseReport> {
  const caseKey = `${String(input.index + 1).padStart(4, "0")}-${safePathSegment(
    input.dfirCase.id
  )}`;
  const caseOutDir = join(input.outDir, caseKey);
  const evidenceRoot = join(caseOutDir, "evidence");
  const tracePath = join(caseOutDir, "dfir-metric-trace.jsonl");
  const casePath = join(caseOutDir, "case.yml");
  const expectedFindings = mapToExpectedFindings(input.dfirCase);
  await writeCaseEvidence(evidenceRoot, input.dfirCase, expectedFindings);
  await writeFile(casePath, renderCaseManifest(input.dfirCase, expectedFindings), "utf8");
  const category = input.dfirCase.category ?? categoryFor(input.dfirCase);
  if (expectedFindings.length === 0) {
    return writeDfirMetricCaseReport({
      caseId: input.dfirCase.id,
      dfirCase: input.dfirCase,
      category,
      caseOutDir,
      expectedFindings,
      report: runBenchmark(expectedFindings, emptyLedger())
    });
  }
  await writeFile(tracePath, renderTrace(input.dfirCase, expectedFindings), "utf8");

  const sentinelResult = await input.runSentinelImpl({
    casePath,
    evidenceRoot,
    outDir: caseOutDir,
    maxIterations: 0,
    tracePath,
    timestampMode: "skip"
  });
  const ledger = sentinelResult.claimLedger ?? sentinelResult.baselineLedger ?? emptyLedger();
  const report = runBenchmark(expectedFindings, ledger);
  return writeDfirMetricCaseReport({
    caseId: input.dfirCase.id,
    dfirCase: input.dfirCase,
    category,
    caseOutDir,
    expectedFindings,
    report
  });
}

async function writeDfirMetricCaseReport(input: {
  readonly caseId: string;
  readonly dfirCase: DfirMetricCase;
  readonly category: string;
  readonly caseOutDir: string;
  readonly expectedFindings: readonly ExpectedFinding[];
  readonly report: BenchmarkReport;
}): Promise<DfirMetricCaseReport> {
  await writeJson(join(input.caseOutDir, "benchmark-report.json"), {
    caseId: input.caseId,
    category: input.category,
    report: input.report,
    expectedFindings: input.expectedFindings
  });
  await writeFile(
    join(input.caseOutDir, "accuracy-report.md"),
    renderCaseAccuracyReport(input.dfirCase, input.category, input.report),
    "utf8"
  );
  return {
    caseId: input.caseId,
    category: input.category,
    outDir: input.caseOutDir,
    expectedFindings: input.expectedFindings,
    report: input.report
  };
}

async function writeCaseEvidence(
  evidenceRoot: string,
  dfirCase: DfirMetricCase,
  expectedFindings: readonly ExpectedFinding[]
): Promise<void> {
  await mkdir(join(evidenceRoot, "answers"), { recursive: true });
  await writeFile(
    join(evidenceRoot, "question.txt"),
    [
      dfirCase.question,
      "",
      `Expected answer count retained for scoring only: ${expectedFindings.length}`,
      "Ground-truth answer values are not written into evidence or trace claims."
    ].join("\n"),
    "utf8"
  );
}

function renderCaseManifest(
  dfirCase: DfirMetricCase,
  expectedFindings: readonly ExpectedFinding[]
): string {
  return [
    `id: ${yamlScalar(dfirCase.id)}`,
    `title: ${yamlScalar(`DFIR-Metric Practical ${dfirCase.id}`)}`,
    "expectedFindings:",
    ...expectedFindings.flatMap((finding) => [
      `  - id: ${yamlScalar(finding.id)}`,
      `    claimId: ${yamlScalar(finding.claimId)}`,
      `    type: ${yamlScalar(finding.type ?? benchmarkClaimType)}`,
      `    acceptedTechniques: [${finding.acceptedTechniques.join(", ")}]`,
      `    description: ${yamlScalar(finding.description ?? "")}`
    ]),
    ""
  ].join("\n");
}

function renderTrace(
  dfirCase: DfirMetricCase,
  expectedFindings: readonly ExpectedFinding[]
): string {
  const runId = `dfir-metric-${safePathSegment(dfirCase.id)}`;
  const events = [
    ...expectedFindings.map((finding, index) => ({
      event: "claim_extracted",
      runId,
      claim: {
        id: finding.claimId,
        text: `DFIR-Metric ${dfirCase.id} unresolved answer slot ${index + 1}; requires live forensic artifact recovery.`,
        type: benchmarkClaimType,
        severity: "medium",
        confidence: 0.1,
        evidenceRefs: [],
        missingEvidence: []
      }
    })),
    {
      event: "final_report",
      runId,
      content: renderSyntheticFinalReport(dfirCase, expectedFindings)
    }
  ];
  return `${events.map((event) => JSON.stringify(event)).join("\n")}\n`;
}

function renderSyntheticFinalReport(
  dfirCase: DfirMetricCase,
  expectedFindings: readonly ExpectedFinding[]
): string {
  return [
    `DFIR-Metric practical case ${dfirCase.id}`,
    "",
    "Question:",
    dfirCase.question,
    "",
    "The trace withholds ground-truth answer values. Expected answers are used only by the scorer."
  ].join("\n");
}

function renderCaseAccuracyReport(
  dfirCase: DfirMetricCase,
  category: string,
  report: BenchmarkReport
): string {
  return [
    "# DFIR-Metric Case Accuracy Report",
    "",
    `- Case: ${dfirCase.id}`,
    `- Category: ${category}`,
    `- Expected findings: ${report.expectedFindings}`,
    `- Evaluated claims: ${report.evaluatedClaims}`,
    `- True positives: ${report.truePositives}`,
    `- False positives: ${report.falsePositives}`,
    `- False negatives: ${report.falseNegatives}`,
    `- Hallucination definition: ${report.hallucinationDefinition}`,
    `- Hallucination count: ${report.hallucinationCount}`,
    `- Hallucination rate: ${formatRatio(report.hallucinationRate)}`,
    `- Precision: ${formatRatio(report.precision)}`,
    `- Recall: ${formatRatio(report.recall)}`,
    `- F1: ${formatRatio(report.f1)}`,
    ""
  ].join("\n");
}

function renderAggregateReport(report: DfirMetricBenchmarkReport): string {
  return [
    "# DFIR-Metric Aggregate Accuracy Report",
    "",
    `- Dataset: ${report.datasetUrl}`,
    `- License: ${report.datasetLicense}`,
    `- Evaluation mode: ${report.evaluationMode}`,
    `- Cases: ${report.subsetSize}`,
    `- Expected findings: ${report.expectedFindings}`,
    `- Evaluated claims: ${report.evaluatedClaims}`,
    `- True positives: ${report.truePositives}`,
    `- False positives: ${report.falsePositives}`,
    `- False negatives: ${report.falseNegatives}`,
    `- Hallucination definition: ${report.hallucinationDefinition}`,
    `- Hallucination count: ${report.hallucinationCount}`,
    `- Hallucination rate: ${formatRatio(report.hallucinationRate)}`,
    `- Precision: ${formatRatio(report.precision)}`,
    `- Recall: ${formatRatio(report.recall)}`,
    `- F1: ${formatRatio(report.f1)}`,
    "",
    "## Per Category",
    "",
    "| Category | Cases | Expected | Claims | Hallucinations | Hallucination Rate | Precision | Recall | F1 |",
    "|---|---:|---:|---:|---:|---:|---:|---:|---:|",
    ...report.perCategory.map(
      (row) =>
        `| ${escapeMarkdown(row.category)} | ${row.cases} | ${row.expectedFindings} | ${row.evaluatedClaims} | ${row.hallucinationCount} | ${formatRatio(row.hallucinationRate)} | ${formatRatio(row.precision)} | ${formatRatio(row.recall)} | ${formatRatio(row.f1)} |`
    ),
    ""
  ].join("\n");
}

function parseDfirMetricCases(payload: unknown): DfirMetricCase[] {
  if (!isRecord(payload) || !Array.isArray(payload.questions)) {
    throw new Error("DFIR-Metric practical dataset must contain a questions array.");
  }
  return payload.questions.map((entry, index) => {
    if (!isRecord(entry)) {
      throw new Error(`DFIR-Metric question ${index} must be an object.`);
    }
    const question = stringField(entry.question) ?? stringField(entry.description);
    if (!question) {
      throw new Error(`DFIR-Metric question ${index} is missing question text.`);
    }
    return {
      id: stringField(entry.id) ?? `nss-${String(index + 1).padStart(3, "0")}`,
      question,
      answer: "answer" in entry ? entry.answer : undefined,
      category: stringField(entry.category) ?? categoryForAnswer(entry.answer),
      source: stringField(payload.sources)
    };
  });
}

function answerValues(answer: unknown): string[] {
  if (Array.isArray(answer)) {
    return answer.flatMap(answerValues);
  }
  if (typeof answer === "number" || typeof answer === "boolean") {
    return [String(answer)];
  }
  if (typeof answer !== "string") {
    return [JSON.stringify(answer)];
  }
  const raw = answer.trim();
  const xml = /^<xml>([\s\S]*)<\/xml>$/iu.exec(raw)?.[1]?.trim() ?? raw;
  const parsed = parseJsonValue(xml);
  if (Array.isArray(parsed)) {
    return parsed.flatMap(answerValues);
  }
  if (typeof parsed === "number" || typeof parsed === "boolean") {
    return [String(parsed)];
  }
  if (typeof parsed === "string") {
    return [parsed.trim()];
  }
  return [xml];
}

function parseJsonValue(input: string): unknown {
  try {
    return JSON.parse(input) as unknown;
  } catch {
    return input;
  }
}

function aggregateBenchmarkReports(reports: readonly BenchmarkReport[]): BenchmarkReport {
  const confirmedClaims = reports.reduce((sum, report) => sum + report.confirmedClaims, 0);
  const truePositives = reports.reduce((sum, report) => sum + report.truePositives, 0);
  const falsePositives = reports.reduce((sum, report) => sum + report.falsePositives, 0);
  const falseNegatives = reports.reduce((sum, report) => sum + report.falseNegatives, 0);
  const hallucinationCount = reports.reduce((sum, report) => sum + report.hallucinationCount, 0);
  const precision = ratio(truePositives, truePositives + falsePositives);
  const recall = ratio(truePositives, truePositives + falseNegatives);
  return {
    confirmedClaims,
    truePositives,
    falsePositives,
    falseNegatives,
    hallucinationCount,
    hallucinationRate: ratio(hallucinationCount, confirmedClaims),
    precision,
    recall,
    f1: precision + recall === 0 ? 0 : (2 * precision * recall) / (precision + recall),
    hallucinationDefinition,
    expectedFindings: reports.reduce((sum, report) => sum + report.expectedFindings, 0),
    evaluatedClaims: reports.reduce((sum, report) => sum + report.evaluatedClaims, 0),
    matches: reports.flatMap((report) => report.matches),
    unmatchedFalsePositiveClaims: reports.flatMap((report) => report.unmatchedFalsePositiveClaims)
  };
}

function categoryReports(caseReports: readonly DfirMetricCaseReport[]): DfirMetricCategoryReport[] {
  const byCategory = new Map<string, DfirMetricCaseReport[]>();
  for (const caseReport of caseReports) {
    const reports = byCategory.get(caseReport.category) ?? [];
    reports.push(caseReport);
    byCategory.set(caseReport.category, reports);
  }
  return [...byCategory.entries()]
    .map(([category, reports]) => {
      const aggregate = aggregateBenchmarkReports(reports.map((report) => report.report));
      return {
        category,
        cases: reports.length,
        expectedFindings: aggregate.expectedFindings,
        evaluatedClaims: aggregate.evaluatedClaims,
        truePositives: aggregate.truePositives,
        falsePositives: aggregate.falsePositives,
        falseNegatives: aggregate.falseNegatives,
        hallucinationCount: aggregate.hallucinationCount,
        hallucinationRate: aggregate.hallucinationRate,
        confirmedClaims: aggregate.confirmedClaims,
        precision: aggregate.precision,
        recall: aggregate.recall,
        f1: aggregate.f1
      };
    })
    .sort((left, right) => left.category.localeCompare(right.category));
}

function categoryFor(dfirCase: DfirMetricCase): string {
  return dfirCase.category ?? categoryForAnswer(dfirCase.answer);
}

function categoryForAnswer(answer: unknown): string {
  const values = answerValues(answer);
  if (values.every((value) => /^\d+$/u.test(value))) {
    return "nss-count";
  }
  if (values.some((value) => /^\d+:(DELETED|LIVE)-/u.test(value))) {
    return "nss-file-list";
  }
  return "nss-string-search";
}

async function writeJson(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function readOptionalFile(path: string): Promise<Buffer | undefined> {
  try {
    return await readFile(path);
  } catch (error) {
    if (isNodeError(error) && error.code === "ENOENT") {
      return undefined;
    }
    throw error;
  }
}

function emptyLedger(): ClaimLedger {
  return {
    id: "dfir-metric-empty-ledger",
    generatedAt: new Date().toISOString(),
    claims: []
  };
}

function claimId(caseId: string, ordinal: number): string {
  return `${caseId}:claim:${ordinal}`;
}

function sha256Hex(input: Buffer): string {
  return createHash("sha256").update(input).digest("hex");
}

function safePathSegment(input: string): string {
  return (
    input
      .replace(/[^A-Za-z0-9._-]+/gu, "-")
      .replace(/^-+|-+$/gu, "")
      .slice(0, 80) || "case"
  );
}

function yamlScalar(input: string): string {
  return JSON.stringify(input);
}

function ratio(numerator: number, denominator: number): number {
  return denominator === 0 ? 0 : numerator / denominator;
}

function formatRatio(value: number): string {
  return value.toFixed(3);
}

function escapeMarkdown(input: string): string {
  return input.replace(/\|/gu, "\\|");
}

function stringField(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && "code" in error;
}
