import { createHash, createPrivateKey, generateKeyPairSync, sign as signBytes } from "node:crypto";
import { copyFile, mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve, sep } from "node:path";
import { normalizeClaudeCodeHook } from "@kelpclaw/agent-hooks";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";
import { extractClaims } from "../extractor/index.js";
import { classifyToolCall, firewallEventFromDecision } from "../firewall/index.js";
import { linkEvidence } from "../linker/index.js";
import { runRepairLoop, type RepairAgentRunner, type RepairTraceRow } from "../repair/index.js";
import { hashEvidenceTree, spoliationCheck } from "../spoliation/index.js";
import {
  claimLedgerSchema,
  claimSchema,
  claimTypes,
  type Claim,
  type ClaimLedger,
  type ClaimStatus,
  type EvidenceRef
} from "../types/claim.js";
import type { FirewallEvent } from "../types/firewall.js";
import type { EvidenceFileHash, SpoliationCheck } from "../types/spoliation.js";
import type { TaintLedgerEntry } from "../types/taint.js";
import { extractTaintSpans } from "../taint/index.js";
import { verifyClaim } from "../verifier/index.js";
import { renderAccuracyReport } from "./accuracy-report.js";
import { buildReviewerHtml } from "./reviewer-html.js";
import { runProtocolSift } from "./sift-runner.js";
import type {
  SentinelMode,
  SentinelOptions,
  SentinelOutputPaths,
  SentinelResult
} from "./types.js";

export type {
  SentinelMode,
  SentinelOptions,
  SentinelOutputPaths,
  SentinelResult
} from "./types.js";
export { renderAccuracyReport } from "./accuracy-report.js";
export { buildReviewerHtml } from "./reviewer-html.js";

interface NormalizedSentinelOptions {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly maxRuntimeSeconds: number;
  readonly mode: SentinelMode;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
  readonly firewallEnabled: boolean;
  readonly spoliationEnabled: boolean;
  readonly claimExtractionEnabled: boolean;
}

interface AgentExecution {
  readonly rawEvents: readonly JsonRecord[];
  readonly finalReport: string;
  readonly traceClaims: readonly JsonRecord[];
}

interface NormalizedAgentRow {
  readonly [key: string]: unknown;
  readonly sourceAgent: string;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly args: JsonRecord;
  readonly status: string;
}

interface AuditKeyFile {
  readonly algorithm: "ed25519";
  readonly keyId: string;
  readonly publicKeyPem: string;
  readonly privateKeyPem: string;
}

export type SentinelOutputPathsWithCommittee = SentinelOutputPaths & {
  readonly committeeVotes: string;
};

export type SentinelResultWithCommittee = Omit<SentinelResult, "outputs"> & {
  readonly outputs: SentinelOutputPathsWithCommittee;
};

const outputNames = {
  agentExecution: "agent-execution.jsonl",
  claimLedger: "claim-ledger.json",
  committeeVotes: "committee-vote.jsonl",
  repairTrace: "repair-trace.jsonl",
  taintLedger: "taint-ledger.jsonl",
  firewallEvents: "firewall-events.jsonl",
  spoliationCheck: "spoliation-check.json",
  evidenceManifest: "evidence-manifest.json",
  accuracyReport: "accuracy-report.md",
  auditBundle: "audit-bundle"
} as const;

const defaultSiftMaxRuntimeSeconds = 900;

const directProgramExecutionEvidence = [
  "prefetch_entry",
  "amcache_execution_record",
  "shimcache_indicator",
  "sysmon_process_create"
] as const;

export async function runSentinel(opts: SentinelOptions): Promise<SentinelResultWithCommittee> {
  const options = await normalizeOptions(opts);
  const outputs = outputPaths(options.outDir);
  const caseMetadata = await readCaseMetadata(options.casePath);
  const runId = `${caseMetadata.id ?? "findevil-sentinel"}-${Date.now().toString(36)}`;

  await mkdir(options.outDir, { recursive: true });
  await initializeJsonl(outputs.agentExecution);
  await initializeJsonl(outputs.committeeVotes);
  await initializeJsonl(outputs.repairTrace);
  await initializeJsonl(outputs.taintLedger);
  await initializeJsonl(outputs.firewallEvents);

  const beforeHashes = await hashEvidenceTree(options.evidenceRoot);
  const taintLedger = options.firewallEnabled
    ? await extractEvidenceTaintLedger(options.evidenceRoot, beforeHashes, runId)
    : [];
  await writeJsonl(outputs.taintLedger, taintLedger);

  const agentExecution = await runAgent(options, outputs.agentExecution, runId);
  const firewallEvents: FirewallEvent[] = [];
  const agentRows = await normalizeAgentExecution({
    runId,
    rawEvents: agentExecution.rawEvents,
    taintLedger,
    firewallEnabled: options.firewallEnabled,
    firewallEvents
  });
  await writeJsonl(outputs.agentExecution, agentRows);
  await writeJsonl(outputs.firewallEvents, firewallEvents);

  let baselineLedger: ClaimLedger | undefined;
  let repairedLedger: ClaimLedger | undefined;
  let repairTrace: readonly RepairTraceRow[] = [];

  if (options.claimExtractionEnabled) {
    const extractedLedger = await extractTraceBackedClaims({
      runId,
      outDir: options.outDir,
      finalReport: agentExecution.finalReport,
      traceClaims: agentExecution.traceClaims
    });
    baselineLedger = verifyLedger(extractedLedger);
    const linkedLedger = verifyLedger(linkLedger(baselineLedger, options.evidenceRoot));
    const repairResult = await runRepairLoop(baselineLedger, options.maxIterations, {
      tracePath: outputs.repairTrace,
      runner: evidenceBackedRepairRunner(linkedLedger),
      now: () => new Date().toISOString()
    });
    repairedLedger = repairResult.ledger;
    repairTrace = repairResult.trace;
    await writeJson(outputs.claimLedger, repairedLedger);
    await writeFile(
      outputs.accuracyReport,
      renderAccuracyReport({
        baselineLedger,
        repairedLedger,
        repairTrace,
        firewallEvents
      }),
      "utf8"
    );
  } else {
    await writeJson(outputs.claimLedger, emptyLedger(runId));
    await writeFile(
      outputs.accuracyReport,
      "# KelpClaw Find Evil Accuracy Report\n\nClaim extraction skipped for firewall-only run.\n",
      "utf8"
    );
  }

  let check: SpoliationCheck | undefined;
  if (options.spoliationEnabled) {
    const afterHashes = await hashEvidenceTree(options.evidenceRoot);
    check = spoliationCheck(beforeHashes, afterHashes);
    await writeJson(outputs.spoliationCheck, check);
  }
  await writeJson(outputs.evidenceManifest, evidenceManifest(caseMetadata, options, beforeHashes));

  const uncorrectedPolicyDenials = 0;
  const status = uncorrectedPolicyDenials > 0 ? "policy_denied" : "succeeded";
  await writeAuditBundle({
    runId,
    outDir: options.outDir,
    outputs,
    evidenceRoot: options.evidenceRoot,
    ok: status === "succeeded",
    mode: options.mode,
    policyDenials: firewallEvents.length,
    uncorrectedPolicyDenials
  });

  return {
    ok: status === "succeeded",
    status,
    runId,
    mode: options.mode,
    outDir: options.outDir,
    outputs,
    ...(baselineLedger ? { baselineLedger } : {}),
    ...(repairedLedger ? { claimLedger: repairedLedger } : {}),
    repairTrace,
    firewallEvents,
    taintLedger,
    ...(check ? { spoliationCheck: check } : {}),
    policyDenials: firewallEvents.length,
    uncorrectedPolicyDenials
  };
}

async function normalizeOptions(opts: SentinelOptions): Promise<NormalizedSentinelOptions> {
  if (!isNonEmptyString(opts.casePath)) {
    throw new Error("Sentinel option casePath is required.");
  }
  if (!isNonEmptyString(opts.evidenceRoot)) {
    throw new Error("Sentinel option evidenceRoot is required.");
  }
  if (!isNonEmptyString(opts.outDir)) {
    throw new Error("Sentinel option outDir is required.");
  }
  if (!Number.isInteger(opts.maxIterations) || opts.maxIterations < 0) {
    throw new Error("Sentinel option maxIterations must be a non-negative integer.");
  }
  const tracePath = isNonEmptyString(opts.tracePath)
    ? await resolveTracePath(opts.tracePath)
    : undefined;
  const siftCommand = isNonEmptyString(opts.siftCommand) ? opts.siftCommand.trim() : undefined;
  if ((tracePath ? 1 : 0) + (siftCommand ? 1 : 0) !== 1) {
    throw new Error("Sentinel requires exactly one of tracePath or siftCommand.");
  }
  const casePath = resolve(opts.casePath);
  const evidenceRoot = resolve(opts.evidenceRoot);
  const [caseStats, evidenceStats] = await Promise.all([stat(casePath), stat(evidenceRoot)]);
  if (!caseStats.isFile()) {
    throw new Error(`Sentinel casePath must be a file: ${casePath}`);
  }
  if (!evidenceStats.isDirectory()) {
    throw new Error(`Sentinel evidenceRoot must be a directory: ${evidenceRoot}`);
  }
  const mode = opts.mode ?? "sentinel";
  if (mode !== "sentinel" && mode !== "verify" && mode !== "firewall") {
    throw new Error("Sentinel mode must be sentinel, verify, or firewall.");
  }
  const maxRuntimeSeconds = await resolveMaxRuntimeSeconds(opts, casePath);
  return {
    casePath,
    evidenceRoot,
    outDir: resolve(opts.outDir),
    maxIterations: opts.maxIterations,
    maxRuntimeSeconds,
    mode,
    ...(siftCommand ? { siftCommand } : {}),
    ...(tracePath ? { tracePath } : {}),
    firewallEnabled: opts.skipFirewall === true ? false : mode !== "verify",
    spoliationEnabled: opts.skipSpoliation === true ? false : mode !== "verify",
    claimExtractionEnabled: opts.skipClaimExtraction === true ? false : mode !== "firewall"
  };
}

async function resolveTracePath(path: string): Promise<string> {
  const absolute = resolve(path);
  const pathStat = await stat(absolute);
  if (pathStat.isFile()) {
    return absolute;
  }
  if (!pathStat.isDirectory()) {
    throw new Error(`Sentinel tracePath must be a file or directory: ${absolute}`);
  }
  const candidates = [join(absolute, "agent-execution.jsonl"), join(absolute, "baseline.jsonl")];
  for (const candidate of candidates) {
    try {
      if ((await stat(candidate)).isFile()) {
        return candidate;
      }
    } catch (error) {
      if (!isNotFound(error)) {
        throw error;
      }
    }
  }
  throw new Error(
    `Sentinel trace directory must contain agent-execution.jsonl or baseline.jsonl: ${absolute}`
  );
}

async function resolveMaxRuntimeSeconds(opts: SentinelOptions, casePath: string): Promise<number> {
  const explicit = (opts as { readonly maxRuntimeSeconds?: unknown }).maxRuntimeSeconds;
  if (explicit !== undefined) {
    return positiveRuntimeSeconds(explicit, "maxRuntimeSeconds");
  }
  const caseBudget = siftIntegrationRuntimeSeconds(await readFile(casePath, "utf8"));
  return caseBudget ?? defaultSiftMaxRuntimeSeconds;
}

function siftIntegrationRuntimeSeconds(content: string): number | undefined {
  let inSiftIntegration = false;
  for (const line of content.split(/\r?\n/u)) {
    if (/^\S/u.test(line)) {
      inSiftIntegration = /^siftIntegration:\s*$/u.test(line);
      continue;
    }
    if (!inSiftIntegration) {
      continue;
    }
    const match = /^\s+maxRuntimeSeconds:\s*(\d+(?:\.\d+)?)\s*$/u.exec(line);
    if (match?.[1]) {
      return positiveRuntimeSeconds(Number(match[1]), "siftIntegration.maxRuntimeSeconds");
    }
  }
  return undefined;
}

function positiveRuntimeSeconds(input: unknown, name: string): number {
  const value = typeof input === "number" ? input : Number(input);
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error(`Sentinel option ${name} must be a positive number.`);
  }
  return value;
}

function outputPaths(outDir: string): SentinelOutputPathsWithCommittee {
  return {
    agentExecution: join(outDir, outputNames.agentExecution),
    claimLedger: join(outDir, outputNames.claimLedger),
    committeeVotes: join(outDir, outputNames.committeeVotes),
    repairTrace: join(outDir, outputNames.repairTrace),
    taintLedger: join(outDir, outputNames.taintLedger),
    firewallEvents: join(outDir, outputNames.firewallEvents),
    spoliationCheck: join(outDir, outputNames.spoliationCheck),
    evidenceManifest: join(outDir, outputNames.evidenceManifest),
    accuracyReport: join(outDir, outputNames.accuracyReport),
    auditBundle: join(outDir, outputNames.auditBundle)
  };
}

async function runAgent(
  options: NormalizedSentinelOptions,
  agentExecutionPath: string,
  runId: string
): Promise<AgentExecution> {
  if (options.tracePath) {
    return readTraceExecution(options.tracePath);
  }
  if (!options.siftCommand) {
    throw new Error("Sentinel live mode requires siftCommand.");
  }
  const summary = await runProtocolSift({
    command: options.siftCommand,
    agentExecutionPath,
    maxRuntimeSeconds: options.maxRuntimeSeconds,
    runId
  });
  return {
    rawEvents: summary.rawEvents,
    finalReport: summary.finalReport,
    traceClaims: summary.traceClaims
  };
}

async function readTraceExecution(tracePath: string): Promise<AgentExecution> {
  const rawEvents = parseJsonl(await readFile(tracePath, "utf8"), tracePath);
  const finalReport = rawEvents
    .map((event) => stringValue(event.content))
    .filter((content, index, values) => eventName(rawEvents[index]) === "final_report" && values)
    .at(-1);
  return {
    rawEvents,
    finalReport: finalReport ?? "",
    traceClaims: rawEvents
      .filter((event) => eventName(event) === "claim_extracted" && isJsonRecord(event.claim))
      .map((event) => event.claim as JsonRecord)
  };
}

async function extractEvidenceTaintLedger(
  evidenceRoot: string,
  hashes: readonly EvidenceFileHash[],
  runId: string
): Promise<TaintLedgerEntry[]> {
  const hashesByPath = new Map(hashes.map((entry) => [entry.path, entry]));
  const entries: TaintLedgerEntry[] = [];
  for (const hash of hashes) {
    const absolutePath = join(evidenceRoot, ...hash.path.split("/"));
    const content = await readUtf8IfText(absolutePath);
    entries.push(
      ...extractTaintSpans({
        path: hash.path,
        sha256: hashesByPath.get(hash.path)?.sha256 ?? hash.sha256,
        content,
        runId
      })
    );
  }
  return entries;
}

async function normalizeAgentExecution(input: {
  readonly runId: string;
  readonly rawEvents: readonly JsonRecord[];
  readonly taintLedger: readonly TaintLedgerEntry[];
  readonly firewallEnabled: boolean;
  readonly firewallEvents: FirewallEvent[];
}): Promise<NormalizedAgentRow[]> {
  const rows: NormalizedAgentRow[] = [];
  const callTools = new Map<string, string>();
  for (const rawEvent of input.rawEvents) {
    const hookInput = traceEventToHookInput(rawEvent, input.runId, callTools);
    const normalized = normalizeClaudeCodeHook(hookInput, { sourceAgent: "claude-code" });
    const row: NormalizedAgentRow = {
      ...normalized,
      rawEvent: coerceJsonValue(rawEvent)
    };
    rows.push(row);
    if (!input.firewallEnabled || !isPreToolUse(normalized.hookEvent)) {
      continue;
    }
    const decision = classifyToolCall(normalized.args, input.taintLedger);
    if (decision.decision !== "block") {
      continue;
    }
    const firewallEvent = firewallEventFromDecision({
      runId: input.runId,
      tool: normalized.toolName,
      args: normalized.args,
      decision
    });
    input.firewallEvents.push(firewallEvent);
    rows.push({
      ...normalizeClaudeCodeHook(
        {
          session_id: input.runId,
          hook_event_name: "UserPromptSubmit",
          tool_name: "safe_reanalysis",
          tool_input: {
            firewallEventId: firewallEvent.id,
            prompt: firewallEvent.correctionTask.prompt
          }
        },
        { sourceAgent: "claude-code" }
      ),
      event: "safe_reanalysis_injected",
      firewallEventId: firewallEvent.id
    });
  }
  return rows;
}

async function extractTraceBackedClaims(input: {
  readonly runId: string;
  readonly outDir: string;
  readonly finalReport: string;
  readonly traceClaims: readonly JsonRecord[];
}): Promise<ClaimLedger> {
  if (input.traceClaims.length > 0) {
    const ledger = claimLedgerSchema.parse({
      id: `claim-ledger-${input.runId}-baseline`,
      runId: input.runId,
      generatedAt: new Date().toISOString(),
      claims: input.traceClaims.map((claim, index) => normalizeTraceClaim(claim, index))
    });
    return extractClaims(input.finalReport || stableJsonStringify(ledger as unknown as JsonValue), {
      cacheDir: join(input.outDir, ".extractor-cache"),
      committeeVotePath: join(input.outDir, outputNames.committeeVotes),
      maxRetries: 0,
      complete: async () => ledger
    });
  }
  if (input.finalReport.trim().length === 0) {
    throw new Error("Sentinel cannot extract claims without a final report.");
  }
  return extractClaims(input.finalReport, {
    cacheDir: join(input.outDir, ".extractor-cache"),
    committeeVotePath: join(input.outDir, outputNames.committeeVotes)
  });
}

function normalizeTraceClaim(raw: JsonRecord, index: number): Claim {
  const type = claimType(raw.type);
  const evidenceRefs = Array.isArray(raw.evidenceRefs)
    ? raw.evidenceRefs.flatMap((entry): EvidenceRef[] =>
        isEvidenceRef(entry) ? [entry as EvidenceRef] : []
      )
    : [];
  return claimSchema.parse({
    id: stringValue(raw.id) ?? `claim-${String(index + 1).padStart(4, "0")}`,
    text: stringValue(raw.text) ?? "Protocol SIFT claim without text.",
    type,
    severity: claimSeverity(raw.severity),
    status: "unverifiable",
    confidence: numberValue(raw.confidence) ?? 0.5,
    evidenceRefs,
    missingEvidence:
      type === "program_execution" && evidenceRefs.length === 0
        ? directProgramExecutionEvidence
        : [],
    ...(stringValue(raw.sourceLocator) ? { sourceLocator: stringValue(raw.sourceLocator) } : {})
  });
}

function verifyLedger(ledger: ClaimLedger): ClaimLedger {
  return claimLedgerSchema.parse({
    ...ledger,
    claims: ledger.claims.map((claim) =>
      claimSchema.parse({
        ...claim,
        status: verifyClaim(claim)
      })
    )
  });
}

function linkLedger(ledger: ClaimLedger, evidenceRoot: string): ClaimLedger {
  return claimLedgerSchema.parse({
    ...ledger,
    claims: ledger.claims.map((claim) => linkEvidence(claim, evidenceRoot))
  });
}

function evidenceBackedRepairRunner(linkedLedger: ClaimLedger): RepairAgentRunner {
  const linkedById = new Map(linkedLedger.claims.map((claim) => [claim.id, claim]));
  return async ({ claim, iteration, prompt, targetTools }) => {
    const repaired = linkedById.get(claim.id);
    return {
      ...(repaired ? { claim: repaired } : {}),
      output: repaired
        ? `iteration ${iteration}: repaired with ${repaired.evidenceRefs.length} linked evidence refs`
        : `iteration ${iteration}: no additional linked evidence found`,
      events: [
        {
          session_id: `kelpclaw.findevil.sentinel.repair.${claim.id}.${iteration}`,
          hook_event_name: "PostToolUse",
          tool_name: "ProtocolSIFT",
          tool_input: { prompt, targetTools },
          tool_response: {
            claimId: claim.id,
            repairedStatus: repaired?.status ?? claim.status,
            evidenceRefs: repaired?.evidenceRefs.length ?? claim.evidenceRefs.length
          }
        }
      ]
    };
  };
}

function evidenceManifest(
  caseMetadata: Readonly<Record<string, string>>,
  options: NormalizedSentinelOptions,
  hashes: readonly EvidenceFileHash[]
): Record<string, unknown> {
  return {
    id: `evidence-manifest-${caseMetadata.id ?? "findevil-sentinel"}`,
    root: relative(process.cwd(), options.evidenceRoot).split(sep).join("/") || ".",
    casePath: relative(process.cwd(), options.casePath).split(sep).join("/") || options.casePath,
    generatedAt: new Date().toISOString(),
    files: hashes
  };
}

async function writeAuditBundle(input: {
  readonly runId: string;
  readonly outDir: string;
  readonly outputs: SentinelOutputPathsWithCommittee;
  readonly evidenceRoot: string;
  readonly ok: boolean;
  readonly mode: SentinelMode;
  readonly policyDenials: number;
  readonly uncorrectedPolicyDenials: number;
}): Promise<void> {
  const bundleDir = input.outputs.auditBundle;
  await mkdir(bundleDir, { recursive: true });
  const sentinelFiles = await copySentinelArtifacts(input.outDir, bundleDir);
  const evidencePreviewFiles = await copyEvidencePreviewArtifacts(input.evidenceRoot, bundleDir);
  const copied = [...sentinelFiles, ...evidencePreviewFiles].sort((left, right) =>
    left.localeCompare(right)
  );
  await writeJson(join(bundleDir, "result.json"), {
    ok: input.ok,
    runId: input.runId,
    status: input.ok ? "succeeded" : "policy_denied",
    mode: input.mode,
    policyDenials: input.policyDenials,
    uncorrectedPolicyDenials: input.uncorrectedPolicyDenials
  });
  await writeJson(join(bundleDir, "compatibility.json"), {
    runnable: true,
    toolsDetected: ["@kelpclaw/agent-hooks", "@kelpclaw/findevil"],
    requiredSecrets: [],
    network: "none",
    sandboxProfile: "workspace-write",
    policyFindings: []
  });
  await writeJson(join(bundleDir, "policy-decisions.json"), {
    policyPack: "findevil-sentinel",
    decisions: {
      denied: input.policyDenials,
      uncorrected: input.uncorrectedPolicyDenials
    }
  });
  await writeJson(join(bundleDir, "redaction-report.json"), {
    redacted: false,
    files: copied
  });
  await writeReviewerIndex(bundleDir);
  const files = [
    ...copied,
    "result.json",
    "compatibility.json",
    "policy-decisions.json",
    "redaction-report.json",
    "index.html"
  ].sort((left, right) => left.localeCompare(right));
  const key = createAuditKey();
  await signAuditBundle(bundleDir, input.runId, files, key);
  await writeAuditAttestation(bundleDir, input.runId, files, key);
}

async function writeReviewerIndex(bundleDir: string): Promise<void> {
  const [ledger, repairTrace, firewallEvents, spoliation, manifest] = await Promise.all([
    readJson<ClaimLedger>(join(bundleDir, outputNames.claimLedger)),
    readJsonl<RepairTraceRow>(join(bundleDir, outputNames.repairTrace)),
    readJsonl<FirewallEvent>(join(bundleDir, outputNames.firewallEvents)),
    readOptionalJson<SpoliationCheck>(join(bundleDir, outputNames.spoliationCheck)),
    readJson<JsonRecord>(join(bundleDir, outputNames.evidenceManifest))
  ]);
  await writeFile(
    join(bundleDir, "index.html"),
    buildReviewerHtml(ledger, repairTrace, firewallEvents, spoliation, manifest),
    "utf8"
  );
}

async function copySentinelArtifacts(outDir: string, bundleDir: string): Promise<string[]> {
  const files = [
    outputNames.agentExecution,
    outputNames.claimLedger,
    outputNames.committeeVotes,
    outputNames.repairTrace,
    outputNames.taintLedger,
    outputNames.firewallEvents,
    outputNames.spoliationCheck,
    outputNames.evidenceManifest,
    outputNames.accuracyReport
  ];
  const copied: string[] = [];
  for (const file of files) {
    const source = join(outDir, file);
    try {
      if ((await stat(source)).isFile()) {
        await copyFile(source, join(bundleDir, file));
        copied.push(file);
      }
    } catch (error) {
      if (!isNotFound(error)) {
        throw error;
      }
    }
  }
  return copied;
}

async function copyEvidencePreviewArtifacts(
  evidenceRoot: string,
  bundleDir: string
): Promise<string[]> {
  const ledger = await readJson<ClaimLedger>(join(bundleDir, outputNames.claimLedger));
  const artifacts = [
    ...new Set(
      ledger.claims
        .flatMap((claim) => claim.evidenceRefs.map((ref) => safeBundlePath(ref.artifact)))
        .filter((artifact): artifact is string => artifact !== undefined)
    )
  ].sort((left, right) => left.localeCompare(right));
  const copied: string[] = [];
  for (const artifact of artifacts) {
    const source = join(evidenceRoot, ...artifact.split("/"));
    try {
      if (!(await stat(source)).isFile()) {
        continue;
      }
      const destination = join(bundleDir, ...artifact.split("/"));
      await mkdir(dirname(destination), { recursive: true });
      await copyFile(source, destination);
      copied.push(artifact);
    } catch (error) {
      if (!isNotFound(error)) {
        throw error;
      }
    }
  }
  return copied;
}

async function signAuditBundle(
  bundleDir: string,
  runId: string,
  files: readonly string[],
  key: AuditKeyFile
): Promise<void> {
  const manifest = {
    schemaVersion: "1.0.0",
    runId,
    generatedAt: new Date().toISOString(),
    algorithm: "ed25519",
    publicKeyId: key.keyId,
    files: await Promise.all(files.map((file) => auditManifestFile(bundleDir, file)))
  };
  const payload = stableJsonStringify(manifest as unknown as JsonValue);
  const signature = signBytes(
    null,
    Buffer.from(payload, "utf8"),
    createPrivateKey(key.privateKeyPem)
  ).toString("base64");
  await writeJson(join(bundleDir, "manifest.json"), manifest);
  await writeFile(join(bundleDir, "manifest.sig"), `${signature}\n`, "utf8");
  await writeJson(join(bundleDir, "manifest.pub.json"), {
    keyId: key.keyId,
    algorithm: key.algorithm,
    publicKeyPem: key.publicKeyPem
  });
}

async function writeAuditAttestation(
  bundleDir: string,
  runId: string,
  files: readonly string[],
  key: AuditKeyFile
): Promise<void> {
  const manifestHash = await sha256File(join(bundleDir, "manifest.json"));
  const attestation = {
    schemaVersion: "1.0.0",
    runId,
    generatedAt: new Date().toISOString(),
    policyPack: "findevil-sentinel",
    signer: {
      keyId: key.keyId,
      algorithm: key.algorithm
    },
    manifest: {
      path: "manifest.json",
      sha256: manifestHash,
      signaturePath: "manifest.sig",
      publicKeyPath: "manifest.pub.json"
    },
    files: files.slice().sort((left, right) => left.localeCompare(right)),
    evidence: {
      governanceReport: false,
      controls: false,
      sarif: false,
      webEvidence: false,
      evidenceWorkspace: false,
      hookEvents: true,
      agentRun: true
    }
  };
  const payload = stableJsonStringify(attestation as unknown as JsonValue);
  const signature = signBytes(
    null,
    Buffer.from(payload, "utf8"),
    createPrivateKey(key.privateKeyPem)
  ).toString("base64");
  await writeJson(join(bundleDir, "attestation.json"), attestation);
  await writeFile(join(bundleDir, "attestation.sig"), `${signature}\n`, "utf8");
}

async function auditManifestFile(bundleDir: string, file: string): Promise<JsonRecord> {
  const absolute = join(bundleDir, file);
  const [fileStat, content] = await Promise.all([stat(absolute), readFile(absolute)]);
  return {
    path: file,
    size: fileStat.size,
    sha256: createHash("sha256").update(content).digest("hex")
  };
}

function createAuditKey(): AuditKeyFile {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" }
  });
  return {
    algorithm: "ed25519",
    keyId: `sha256:${createHash("sha256").update(publicKey, "utf8").digest("hex")}`,
    publicKeyPem: publicKey,
    privateKeyPem: privateKey
  };
}

function traceEventToHookInput(
  rawEvent: JsonRecord,
  runId: string,
  callTools: Map<string, string>
): Parameters<typeof normalizeClaudeCodeHook>[0] {
  if ("hook_event_name" in rawEvent || "tool_name" in rawEvent) {
    return rawEvent;
  }
  const event = eventName(rawEvent);
  const sessionId = stringValue(rawEvent.runId) ?? runId;
  const callId = stringValue(rawEvent.callId);
  if (event === "tool_call") {
    const toolName = stringValue(rawEvent.tool) ?? "ProtocolSIFT";
    if (callId) {
      callTools.set(callId, toolName);
    }
    return {
      session_id: sessionId,
      hook_event_name: "PreToolUse",
      tool_name: toolName,
      ...(callId ? { tool_use_id: callId } : {}),
      tool_input: isJsonRecord(rawEvent.arguments) ? rawEvent.arguments : {}
    };
  }
  if (event === "tool_result") {
    return {
      session_id: sessionId,
      hook_event_name: "PostToolUse",
      tool_name: callId ? (callTools.get(callId) ?? "ProtocolSIFT") : "ProtocolSIFT",
      ...(callId ? { tool_use_id: callId } : {}),
      tool_input: callId ? { callId } : {},
      tool_response: coerceJsonValue(rawEvent)
    };
  }
  if (event === "final_report") {
    return {
      session_id: sessionId,
      hook_event_name: "Stop",
      tool_name: "ProtocolSIFT",
      tool_input: { finalReport: stringValue(rawEvent.content) ?? "" }
    };
  }
  return {
    session_id: sessionId,
    hook_event_name: "Notification",
    tool_name: "ProtocolSIFT",
    tool_input: { traceEvent: coerceJsonValue(rawEvent) }
  };
}

function parseJsonl(input: string, path: string): JsonRecord[] {
  return input
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => {
      const parsed = JSON.parse(line) as unknown;
      if (!isJsonRecord(parsed)) {
        throw new Error(`${path}:${index + 1} must contain a JSON object.`);
      }
      return parsed;
    });
}

async function readCaseMetadata(casePath: string): Promise<Readonly<Record<string, string>>> {
  const content = await readFile(casePath, "utf8");
  const fields: Record<string, string> = {};
  for (const line of content.split(/\r?\n/u)) {
    const match = /^([A-Za-z][A-Za-z0-9_-]*)\s*:\s*(.+?)\s*$/u.exec(line);
    if (match?.[1] && match[2]) {
      fields[match[1]] = match[2].replace(/^["']|["']$/gu, "");
    }
  }
  return fields;
}

async function readUtf8IfText(path: string): Promise<string> {
  const buffer = await readFile(path);
  const text = buffer.toString("utf8");
  return Buffer.from(text, "utf8").equals(buffer) ? text : "";
}

async function writeJson(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function readJson<T>(path: string): Promise<T> {
  return JSON.parse(await readFile(path, "utf8")) as T;
}

async function readOptionalJson<T>(path: string): Promise<T | undefined> {
  try {
    return await readJson<T>(path);
  } catch (error) {
    if (isNotFound(error)) {
      return undefined;
    }
    throw error;
  }
}

async function readJsonl<T>(path: string): Promise<T[]> {
  return parseJsonl(await readFile(path, "utf8"), path).map((row) => row as T);
}

async function writeJsonl(path: string, rows: readonly unknown[]): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    rows.length > 0 ? `${rows.map((row) => JSON.stringify(row)).join("\n")}\n` : "",
    "utf8"
  );
}

async function initializeJsonl(path: string): Promise<void> {
  await writeJsonl(path, []);
}

async function sha256File(path: string): Promise<string> {
  return createHash("sha256")
    .update(await readFile(path))
    .digest("hex");
}

function emptyLedger(runId: string): ClaimLedger {
  return claimLedgerSchema.parse({
    id: `claim-ledger-${runId}-skipped`,
    runId,
    generatedAt: new Date().toISOString(),
    claims: []
  });
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

function isEvidenceRef(input: unknown): input is EvidenceRef {
  return (
    isJsonRecord(input) &&
    isNonEmptyString(input.artifact) &&
    isNonEmptyString(input.locator) &&
    isNonEmptyString(input.supports) &&
    typeof input.hash === "string" &&
    /^sha256:[a-f0-9]{64}$/u.test(input.hash)
  );
}

function eventName(event: JsonRecord | undefined): string {
  return typeof event?.event === "string" ? event.event : "";
}

function isPreToolUse(hookEvent: string): boolean {
  return hookEvent === "PreToolUse" || hookEvent === "PermissionRequest";
}

function numberValue(input: unknown): number | undefined {
  return typeof input === "number" && Number.isFinite(input) ? input : undefined;
}

function stringValue(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function isNonEmptyString(input: unknown): input is string {
  return typeof input === "string" && input.trim().length > 0;
}

function safeBundlePath(input: string): string | undefined {
  const normalized = input.replace(/\\/gu, "/");
  if (
    normalized.length === 0 ||
    normalized.startsWith("/") ||
    normalized.includes("\0") ||
    normalized.split("/").some((part) => part.length === 0 || part === "..")
  ) {
    return undefined;
  }
  return normalized;
}

function isJsonRecord(input: unknown): input is JsonRecord {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function coerceJsonValue(value: unknown): JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => coerceJsonValue(item));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, coerceJsonValue(item)])
    );
  }
  return null;
}

function isNotFound(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT";
}
