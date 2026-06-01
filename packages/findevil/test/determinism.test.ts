import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";
import { computeClaimLedgerHash } from "../src/sentinel/determinism.js";
import { runSentinel } from "../src/sentinel/index.js";
import { claimLedgerSchema, claimTypes, type Claim } from "../src/types/claim.js";

const repoRoot = fileURLToPath(new URL("../../..", import.meta.url));
const fixtureTracePath = join(repoRoot, "fixtures/protocol-sift-baseline/baseline.jsonl");
const fixtureCasePath = join(repoRoot, "examples/findevil-sift-sentinel/case.yml");
const fixtureEvidenceRoot = join(repoRoot, "examples/findevil-sift-sentinel/case-data");
const deterministicRunId = "findevil-sift-sentinel-demo-001-deterministic";
const deterministicGeneratedAt = "1970-01-01T00:00:00.000Z";
const directProgramExecutionEvidence = [
  "prefetch_entry",
  "amcache_execution_record",
  "shimcache_indicator",
  "sysmon_process_create"
] as const;

describe("sentinel deterministic mode", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("replays the fixture twice with identical claim-ledger hashes", async () => {
    vi.stubEnv("KELP_FINDEVIL_MODELS", "");
    stubTimestampAuthority();
    const [first, second] = await Promise.all([
      deterministicFixtureRun(),
      deterministicFixtureRun()
    ]);
    expect(first).toBe(second);
  });

  it("refuses live Claude Code repair in deterministic mode", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-determinism-repair-"));
    await expect(
      runSentinel({
        casePath: fixtureCasePath,
        tracePath: fixtureTracePath,
        maxIterations: 1,
        evidenceRoot: fixtureEvidenceRoot,
        outDir,
        deterministic: true,
        repairRunnerMode: "claude-code"
      })
    ).rejects.toThrow("refuses live Claude Code repair");
  });
});

function stubTimestampAuthority(): void {
  const grantedTimestampResponse = new Uint8Array([
    0x30, 0x07, 0x30, 0x03, 0x02, 0x01, 0x00, 0x30, 0x00
  ]);
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response(grantedTimestampResponse, {
        status: 200,
        headers: { "content-type": "application/timestamp-reply" }
      })
  );
}

async function deterministicFixtureRun(): Promise<string> {
  const outDir = await mkdtemp(join(tmpdir(), "findevil-determinism-"));
  const trace = parseJsonl(await readFile(fixtureTracePath, "utf8"));
  const finalReport = finalReportFromTrace(trace);
  await seedExtractorCache(outDir, finalReport, ledgerFromTrace(trace));
  const result = await runSentinel({
    casePath: fixtureCasePath,
    tracePath: fixtureTracePath,
    maxIterations: 3,
    evidenceRoot: fixtureEvidenceRoot,
    outDir,
    deterministic: true
  });
  expect(result.claimLedger).toBeDefined();
  return computeClaimLedgerHash(result.claimLedger);
}

async function seedExtractorCache(
  outDir: string,
  finalReport: string,
  ledger: ReturnType<typeof ledgerFromTrace>
): Promise<void> {
  const cacheDir = join(outDir, ".extractor-cache");
  await mkdir(cacheDir, { recursive: true });
  await writeFile(
    join(cacheDir, `${sha256Hex(finalReport)}.json`),
    `${JSON.stringify(ledger, null, 2)}\n`,
    "utf8"
  );
}

function ledgerFromTrace(trace: readonly Record<string, unknown>[]) {
  return claimLedgerSchema.parse({
    id: `claim-ledger-${deterministicRunId}-baseline`,
    runId: deterministicRunId,
    generatedAt: deterministicGeneratedAt,
    claims: trace
      .filter((event) => event.event === "claim_extracted" && isRecord(event.claim))
      .map((event, index) => normalizeFixtureClaim(event.claim as Record<string, unknown>, index))
  });
}

function normalizeFixtureClaim(raw: Record<string, unknown>, index: number): Claim {
  const type = claimType(raw.type);
  const evidenceRefs = Array.isArray(raw.evidenceRefs) ? raw.evidenceRefs.filter(isRecord) : [];
  const sourceLocator = stringValue(raw.sourceLocator);
  return {
    id: stringValue(raw.id) ?? `claim-${String(index + 1).padStart(4, "0")}`,
    text: stringValue(raw.text) ?? "Protocol SIFT claim without text.",
    type,
    severity: claimSeverity(raw.severity),
    status: "unverifiable",
    confidence: numberValue(raw.confidence) ?? 0.5,
    attackTechniques: [],
    evidenceRefs: evidenceRefs as Claim["evidenceRefs"],
    missingEvidence:
      type === "program_execution" && evidenceRefs.length === 0
        ? [...directProgramExecutionEvidence]
        : [],
    ...(sourceLocator ? { sourceLocator } : {})
  };
}

function finalReportFromTrace(trace: readonly Record<string, unknown>[]): string {
  return (
    trace
      .filter((event) => event.event === "final_report")
      .map((event) => stringValue(event.content))
      .filter((content): content is string => content !== undefined)
      .at(-1) ?? ""
  );
}

function parseJsonl(input: string): Record<string, unknown>[] {
  return input
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line) as Record<string, unknown>);
}

function sha256Hex(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function claimType(input: unknown): Claim["type"] {
  return typeof input === "string" && claimTypes.includes(input as Claim["type"])
    ? (input as Claim["type"])
    : "incident_conclusion";
}

function claimSeverity(input: unknown): Claim["severity"] {
  const value = typeof input === "string" ? input.toLowerCase().replace(/[^a-z]/gu, "") : "";
  if (
    value === "informational" ||
    value === "low" ||
    value === "medium" ||
    value === "high" ||
    value === "critical"
  ) {
    return value;
  }
  return "informational";
}

function numberValue(input: unknown): number | undefined {
  return typeof input === "number" && Number.isFinite(input) ? input : undefined;
}

function stringValue(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}
