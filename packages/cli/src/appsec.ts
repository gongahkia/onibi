import { spawn } from "node:child_process";
import { createHash, createPrivateKey, generateKeyPairSync, sign as signBytes } from "node:crypto";
import { copyFile, mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import {
  compareEvidenceWorkspaces,
  createEvidenceWorkspace,
  importBurpEvidence,
  importNessusEvidence,
  importNmapEvidence,
  importNucleiEvidence,
  qaEvidenceWorkspace,
  renderEvidenceRetestMarkdown,
  importSarifEvidence,
  importZapEvidence,
  loadEvidenceWorkspace,
  signEvidenceWorkspace,
  type EvidenceQaIssue,
  type EvidenceImportResult,
  type NormalizedEvidenceFinding
} from "@kelpclaw/evidence";
import { evaluatePolicy, requirePolicyPack, type PolicyDecision } from "@kelpclaw/policy";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";

type AppsecStatus = "succeeded" | "failed" | "blocked";
type AppsecQaFailThreshold = "none" | "warning" | "error";
type AppsecCorrelationStatus = "linked" | "missing-evidence" | "no-evidence";

interface CommandResult {
  readonly command: readonly string[];
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
}

interface AppsecAgentFinding {
  readonly id: string;
  readonly title: string;
  readonly severity: "info" | "low" | "medium" | "high" | "critical";
  readonly confidence: "low" | "medium" | "high" | "confirmed";
  readonly evidenceIds: readonly string[];
  readonly rationale: string;
  readonly recommendedAction: string;
}

interface AppsecTriageOutput {
  readonly summary: string;
  readonly triageFindings: readonly AppsecAgentFinding[];
  readonly recommendedNextSteps: readonly string[];
  readonly limitations: readonly string[];
}

interface AppsecPolicyRecord {
  readonly subject: string;
  readonly tool: string;
  readonly args: JsonRecord;
  readonly decision: PolicyDecision;
}

interface AppsecQaResult {
  readonly schemaVersion: "kelpclaw.appsec.qa.v1";
  readonly valid: boolean;
  readonly failed: boolean;
  readonly failThreshold: AppsecQaFailThreshold;
  readonly errorCount: number;
  readonly warningCount: number;
  readonly issues: readonly EvidenceQaIssue[];
}

interface AppsecCorrelationRecord {
  readonly triageFindingId: string;
  readonly status: AppsecCorrelationStatus;
  readonly evidenceIds: readonly string[];
  readonly linkedEvidenceIds: readonly string[];
  readonly missingEvidenceIds: readonly string[];
}

interface AppsecAuditBundleManifest {
  readonly schemaVersion: "1.0.0";
  readonly runId: string;
  readonly generatedAt: string;
  readonly algorithm: "ed25519";
  readonly publicKeyId: string;
  readonly files: readonly AppsecAuditBundleManifestFile[];
}

interface AppsecAuditBundleManifestFile {
  readonly path: string;
  readonly size: number;
  readonly sha256: string;
}

interface AppsecAuditKeyFile {
  readonly schemaVersion: "1.0.0";
  readonly algorithm: "ed25519";
  readonly keyId: string;
  readonly publicKeyPem: string;
  readonly privateKeyPem: string;
}

export interface AppsecAuditOutput {
  readonly ok: boolean;
  readonly runId: string;
  readonly status: AppsecStatus;
  readonly outDir: string;
  readonly bundleDir: string;
  readonly evidenceWorkspace: string;
  readonly importedFindings: number;
  readonly docker: {
    readonly built: boolean;
    readonly exitCode: number | null;
    readonly imageTag: string;
    readonly imageId?: string | undefined;
  };
  readonly agent: {
    readonly ran: boolean;
    readonly exitCode: number | null;
  };
  readonly labMode: boolean;
}

export async function runAppsecCommand(args: readonly string[]): Promise<void> {
  const [command, ...commandArgs] = args;
  if (command === "audit") {
    printJson(await appsecAudit(commandArgs));
    return;
  }
  if (command === "diff") {
    printJson(await appsecDiff(commandArgs));
    return;
  }
  throw new Error("Usage: kelp-claw appsec <audit|diff> [options]");
}

export async function appsecAudit(args: readonly string[]): Promise<AppsecAuditOutput> {
  const runId = option(args, "--run-id") ?? `appsec-run.${Date.now()}`;
  const outDir = resolve(option(args, "--out") ?? join(".kelpclaw", "appsec", runId));
  const contextDir = resolve(requiredOption(args, "--context"));
  const dockerfile = resolvePath(contextDir, requiredOption(args, "--dockerfile"));
  const dockerBin = option(args, "--docker-bin") ?? "docker";
  const imageTag = option(args, "--image-tag") ?? `kelpclaw-appsec:${safeTag(runId)}`;
  const policyPackName = option(args, "--policy") ?? "appsec-agent-baseline";
  const policyPack = requirePolicyPack(policyPackName);
  const agentCommand = requiredOption(args, "--agent-command");
  const labMode = hasFlag(args, "--lab-mode");
  const evidenceWorkspace = join(outDir, "evidence-workspace");
  const bundleDir = join(outDir, "audit-bundle");
  await mkdir(outDir, { recursive: true });
  await mkdir(bundleDir, { recursive: true });
  await createEvidenceWorkspace(evidenceWorkspace, {
    project: "KelpClaw AppSec Harness",
    scope: [contextDir]
  });

  const contextDigest = await hashDirectory(contextDir);
  const dockerfileSha256 = await sha256File(dockerfile);
  const dockerBuildCommand = [dockerBin, "build", "-f", dockerfile, "-t", imageTag, contextDir];
  const policyDecisions: AppsecPolicyRecord[] = [
    policyRecord(
      "docker-build",
      "Bash",
      { command: dockerBuildCommand.join(" ") },
      policyPack.ruleset
    ),
    policyRecord("agent-command", "Bash", { command: agentCommand }, policyPack.ruleset)
  ];
  const buildBlocked = policyDecisions.some((record) => policyBlocks(record.decision));
  const build =
    buildBlocked || hasFlag(args, "--skip-docker-build")
      ? skippedCommand(dockerBuildCommand)
      : await runCommand(dockerBuildCommand, outDir);
  await writeFile(join(outDir, "docker-build.stdout.log"), build.stdout, "utf8");
  await writeFile(join(outDir, "docker-build.stderr.log"), build.stderr, "utf8");
  const imageId =
    build.exitCode === 0 && !hasFlag(args, "--skip-docker-build")
      ? await inspectImageId(dockerBin, imageTag, outDir)
      : undefined;

  const imports = await importScannerEvidence(evidenceWorkspace, args);
  const evidenceState = await loadEvidenceWorkspace(evidenceWorkspace);
  const evidenceSignature = await signEvidenceWorkspace(evidenceWorkspace, {
    ...(option(args, "--key-dir") ? { keyDir: option(args, "--key-dir") } : {})
  });
  const triageInput = {
    schemaVersion: "kelpclaw.appsec.input.v1",
    runId,
    generatedAt: new Date().toISOString(),
    target: {
      contextDir,
      dockerfile,
      dockerfileSha256: `sha256:${dockerfileSha256}`,
      contextDigest,
      imageTag,
      ...(imageId ? { imageId } : {})
    },
    safety: {
      labMode,
      exploitExecution: "forbidden",
      role: "triage-assistant",
      instructions: [
        "Correlate supplied evidence only.",
        "Do not execute exploits, persistence, lateral movement, or internet-wide scanning.",
        "Recommend validation steps separately from confirmed findings."
      ]
    },
    scannerImports: imports,
    findings: evidenceState.findings.findings.map(appsecFindingSummary),
    policy: {
      pack: policyPack.name,
      decisions: policyDecisions
    },
    dockerBuild: {
      command: build.command,
      exitCode: build.exitCode,
      stdoutPath: "docker-build.stdout.log",
      stderrPath: "docker-build.stderr.log"
    }
  };
  const triageInputPath = join(outDir, "appsec-input.json");
  const triageOutputPath = join(outDir, "appsec-triage.json");
  await writeJson(triageInputPath, triageInput);

  const agentDecision = policyDecisions.find((record) => record.subject === "agent-command");
  const agentBlocked = agentDecision ? policyBlocks(agentDecision.decision) : false;
  const agent =
    agentCommand && !agentBlocked
      ? await runCommand([agentCommand, ...options(args, "--agent-arg")], outDir, {
          KELPCLAW_APPSEC_INPUT: triageInputPath,
          KELPCLAW_APPSEC_OUTPUT: triageOutputPath,
          KELPCLAW_EVIDENCE_WORKSPACE: evidenceWorkspace
        })
      : skippedCommand(agentCommand ? [agentCommand] : []);
  await writeFile(join(outDir, "agent.stdout.log"), agent.stdout, "utf8");
  await writeFile(join(outDir, "agent.stderr.log"), agent.stderr, "utf8");

  const triage = await readTriageOutput(triageOutputPath, !agentBlocked);
  const correlations = correlateAppsecFindings(triage, evidenceState.findings.findings);
  const baseStatus: AppsecStatus =
    buildBlocked || agentBlocked
      ? "blocked"
      : build.exitCode !== 0 || agent.exitCode !== 0 || !triage.ok
        ? "failed"
        : "succeeded";
  const sarif = appsecSarif({
    runId,
    evidenceFindings: evidenceState.findings.findings,
    triage: triage.ok ? triage.output : undefined,
    correlations
  });
  await writeJson(join(outDir, "findings.sarif"), sarif);
  const qa = await appsecQa({
    evidenceWorkspace,
    scannerImports: imports,
    triage,
    correlations,
    sarifPath: join(outDir, "findings.sarif"),
    signed: !hasFlag(args, "--no-sign"),
    failThreshold: appsecQaFailThreshold(args)
  });
  const status: AppsecStatus = baseStatus === "succeeded" && qa.failed ? "failed" : baseStatus;
  const appsecRun = {
    schemaVersion: "kelpclaw.appsec.run.v1",
    runId,
    status,
    ok: status === "succeeded",
    outDir,
    policyPack: policyPack.name,
    labMode,
    target: triageInput.target,
    docker: {
      built: !hasFlag(args, "--skip-docker-build") && !buildBlocked,
      exitCode: build.exitCode,
      command: build.command,
      ...(imageId ? { imageId } : {})
    },
    agent: {
      ran: !agentBlocked,
      exitCode: agent.exitCode,
      command: agent.command
    },
    scannerImports: imports,
    correlation: correlations,
    qa,
    evidence: {
      workspace: evidenceWorkspace,
      importedFindings: evidenceState.findings.findings.length,
      manifestPath: evidenceSignature.manifestPath,
      signaturePath: evidenceSignature.signaturePath
    },
    triage: triage.ok ? triage.output : { error: triage.error }
  };
  await writeJson(join(outDir, "appsec-run.json"), appsecRun);
  await writeJson(join(outDir, "appsec-qa.json"), qa);
  await writeJson(join(outDir, "policy-decisions.json"), {
    policyPack: policyPack.name,
    policyPackDescription: policyPack.description,
    policyPackMetadata: policyPack.metadata,
    ruleset: policyPack.ruleset,
    decisions: policyDecisions
  });
  await writeJson(join(outDir, "result.json"), {
    ok: status === "succeeded",
    runId,
    status,
    outDir,
    policyPack: policyPack.name,
    labMode,
    qa: {
      valid: qa.valid,
      failed: qa.failed,
      failThreshold: qa.failThreshold,
      errorCount: qa.errorCount,
      warningCount: qa.warningCount
    }
  });
  await writeAuditBundle({
    outDir,
    bundleDir,
    runId,
    qa,
    correlations,
    keyDir: resolve(option(args, "--key-dir") ?? ".kelpclaw/keys"),
    signed: !hasFlag(args, "--no-sign")
  });
  if (status !== "succeeded") {
    process.exitCode = 1;
  }
  return {
    ok: status === "succeeded",
    runId,
    status,
    outDir,
    bundleDir,
    evidenceWorkspace,
    importedFindings: evidenceState.findings.findings.length,
    docker: {
      built: !hasFlag(args, "--skip-docker-build") && !buildBlocked,
      exitCode: build.exitCode,
      imageTag,
      ...(imageId ? { imageId } : {})
    },
    agent: {
      ran: !agentBlocked,
      exitCode: agent.exitCode
    },
    labMode
  };
}

export async function appsecDiff(args: readonly string[]): Promise<JsonRecord> {
  const baselineInput = resolve(requiredOption(args, "--baseline"));
  const currentInput = resolve(requiredOption(args, "--current"));
  const baselineWorkspace = await resolveAppsecEvidenceWorkspace(baselineInput);
  const currentWorkspace = await resolveAppsecEvidenceWorkspace(currentInput);
  const result = await compareEvidenceWorkspaces(baselineWorkspace, currentWorkspace);
  const failOn = option(args, "--fail-on") ?? "none";
  const failed = appsecDiffFailed(result.summary, failOn);
  const format = option(args, "--format") ?? "json";
  if (format !== "json" && format !== "markdown") {
    throw new Error("--format must be json or markdown.");
  }
  const markdown = format === "markdown" ? renderEvidenceRetestMarkdown(result) : undefined;
  const output = {
    ok: !failed,
    failOn,
    baselineInput,
    currentInput,
    ...(markdown ? { markdown } : {}),
    ...result
  };
  const out = option(args, "--out");
  if (out) {
    await writeTextWithParents(resolve(out), markdown ?? `${JSON.stringify(output, null, 2)}\n`);
  }
  if (failed) {
    process.exitCode = 1;
  }
  return {
    ...(out ? { out: resolve(out) } : {}),
    ...output
  } as unknown as JsonRecord;
}

async function resolveAppsecEvidenceWorkspace(input: string): Promise<string> {
  const appsecWorkspace = join(input, "evidence-workspace");
  if (await fileExists(join(appsecWorkspace, "workspace.json"))) return appsecWorkspace;
  if (await fileExists(join(input, "workspace.json"))) return input;
  throw new Error(`${input} is not an AppSec output directory or evidence workspace.`);
}

function appsecDiffFailed(summary: Readonly<Record<string, number>>, failOn: string): boolean {
  const statusesByThreshold: Readonly<Record<string, readonly string[]>> = {
    none: [],
    any: ["new", "closed", "changed", "regressed", "ambiguous"],
    new: ["new"],
    closed: ["closed"],
    changed: ["changed", "regressed"],
    regressed: ["regressed"],
    ambiguous: ["ambiguous"]
  };
  const statuses = statusesByThreshold[failOn];
  if (!statuses) {
    throw new Error(
      "--fail-on must be one of none, any, new, closed, changed, regressed, or ambiguous."
    );
  }
  return statuses.some((status) => (summary[status] ?? 0) > 0);
}

async function importScannerEvidence(
  workspace: string,
  args: readonly string[]
): Promise<readonly EvidenceImportResult[]> {
  const imports: EvidenceImportResult[] = [];
  for (const input of options(args, "--sarif")) {
    imports.push(await importSarifEvidence(workspace, input));
  }
  for (const input of options(args, "--nuclei-jsonl")) {
    imports.push(await importNucleiEvidence(workspace, input));
  }
  for (const input of options(args, "--zap-json")) {
    imports.push(await importZapEvidence(workspace, input));
  }
  for (const input of options(args, "--nmap-xml")) {
    imports.push(await importNmapEvidence(workspace, input));
  }
  for (const input of options(args, "--burp-xml")) {
    imports.push(await importBurpEvidence(workspace, input));
  }
  for (const input of options(args, "--nessus-xml")) {
    imports.push(await importNessusEvidence(workspace, input));
  }
  return imports;
}

function appsecFindingSummary(finding: NormalizedEvidenceFinding): JsonRecord {
  const sourceReferences = finding.sourceReferences.map((source) => ({
    tool: source.tool,
    rawPath: source.rawPath,
    ...(source.locator ? { locator: source.locator } : {})
  }));
  return {
    id: finding.id,
    title: finding.title,
    severity: finding.severity,
    confidence: finding.confidence,
    status: finding.status,
    weaknessIds: [...finding.weaknessIds],
    sourceReferences
  };
}

async function readTriageOutput(
  outputPath: string,
  required: boolean
): Promise<
  | { readonly ok: true; readonly output: AppsecTriageOutput }
  | { readonly ok: false; readonly error: string }
> {
  if (!required) {
    return {
      ok: true,
      output: {
        summary: "No AppSec triage agent was configured.",
        triageFindings: [],
        recommendedNextSteps: [],
        limitations: ["No --agent-command was provided."]
      }
    };
  }
  try {
    const parsed = JSON.parse(await readFile(outputPath, "utf8")) as unknown;
    const validation = validateTriageOutput(parsed);
    return validation.ok ? { ok: true, output: validation.output } : validation;
  } catch (error) {
    return {
      ok: false,
      error: `Unable to read AppSec triage output: ${errorMessage(error)}`
    };
  }
}

function validateTriageOutput(
  value: unknown
):
  | { readonly ok: true; readonly output: AppsecTriageOutput }
  | { readonly ok: false; readonly error: string } {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return { ok: false, error: "AppSec triage output must be a JSON object." };
  }
  const record = value as JsonRecord;
  if (typeof record.summary !== "string") {
    return { ok: false, error: "AppSec triage output requires string summary." };
  }
  if (!Array.isArray(record.triageFindings)) {
    return { ok: false, error: "AppSec triage output requires triageFindings array." };
  }
  if (!Array.isArray(record.recommendedNextSteps)) {
    return { ok: false, error: "AppSec triage output requires recommendedNextSteps array." };
  }
  if (!Array.isArray(record.limitations)) {
    return { ok: false, error: "AppSec triage output requires limitations array." };
  }
  try {
    return {
      ok: true,
      output: {
        summary: record.summary,
        triageFindings: record.triageFindings.map((finding, index) =>
          appsecAgentFinding(finding, index)
        ),
        recommendedNextSteps: record.recommendedNextSteps.map(String),
        limitations: record.limitations.map(String)
      }
    };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

function appsecAgentFinding(value: unknown, index: number): AppsecAgentFinding {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`triageFindings[${index}] must be an object.`);
  }
  const record = value as JsonRecord;
  return {
    id: stringValue(record.id, `agent-finding-${index + 1}`),
    title: stringValue(record.title, "Untitled AppSec triage finding"),
    severity: severityValue(record.severity),
    confidence: confidenceValue(record.confidence),
    evidenceIds: Array.isArray(record.evidenceIds) ? record.evidenceIds.map(String) : [],
    rationale: stringValue(record.rationale, ""),
    recommendedAction: stringValue(record.recommendedAction, "")
  };
}

function correlateAppsecFindings(
  triage: Awaited<ReturnType<typeof readTriageOutput>>,
  evidenceFindings: readonly NormalizedEvidenceFinding[]
): readonly AppsecCorrelationRecord[] {
  if (!triage.ok) return [];
  const evidenceIds = new Set(evidenceFindings.map((finding) => finding.id));
  return triage.output.triageFindings.map((finding) => {
    const linkedEvidenceIds = finding.evidenceIds.filter((evidenceId) =>
      evidenceIds.has(evidenceId)
    );
    const missingEvidenceIds = finding.evidenceIds.filter(
      (evidenceId) => !evidenceIds.has(evidenceId)
    );
    const status: AppsecCorrelationStatus =
      finding.evidenceIds.length === 0
        ? "no-evidence"
        : missingEvidenceIds.length > 0
          ? "missing-evidence"
          : "linked";
    return {
      triageFindingId: finding.id,
      status,
      evidenceIds: [...finding.evidenceIds],
      linkedEvidenceIds,
      missingEvidenceIds
    };
  });
}

function appsecSarif(input: {
  readonly runId: string;
  readonly evidenceFindings: readonly NormalizedEvidenceFinding[];
  readonly triage?: AppsecTriageOutput | undefined;
  readonly correlations: readonly AppsecCorrelationRecord[];
}): JsonRecord {
  const correlationByFindingId = new Map(
    input.correlations.map((correlation) => [correlation.triageFindingId, correlation])
  );
  const evidenceResults = input.evidenceFindings.map((finding) => ({
    ruleId: `kelp.appsec.evidence.${safeRuleId(finding.id)}`,
    level: sarifLevel(finding.severity),
    message: { text: finding.title },
    properties: {
      title: finding.title,
      source: "evidence-workspace",
      severity: finding.severity,
      confidence: finding.confidence,
      status: finding.status,
      weaknessIds: [...finding.weaknessIds]
    }
  }));
  const triageResults = (input.triage?.triageFindings ?? []).map((finding) => ({
    ruleId: `kelp.appsec.triage.${safeRuleId(finding.id)}`,
    level: sarifLevel(finding.severity),
    message: { text: finding.title },
    properties: {
      title: finding.title,
      source: "appsec-agent",
      severity: finding.severity,
      confidence: finding.confidence,
      evidenceIds: [...finding.evidenceIds],
      correlationStatus: correlationByFindingId.get(finding.id)?.status ?? "no-evidence",
      linkedEvidenceIds: correlationByFindingId.get(finding.id)?.linkedEvidenceIds ?? [],
      missingEvidenceIds: correlationByFindingId.get(finding.id)?.missingEvidenceIds ?? [],
      rationale: finding.rationale,
      recommendedAction: finding.recommendedAction
    }
  }));
  return {
    version: "2.1.0",
    $schema: "https://json.schemastore.org/sarif-2.1.0.json",
    runs: [
      {
        tool: {
          driver: {
            name: "KelpClaw AppSec Harness",
            informationUri: "https://github.com/gongahkia/kelp",
            rules: []
          }
        },
        automationDetails: { id: input.runId },
        results: [...evidenceResults, ...triageResults]
      }
    ]
  } as unknown as JsonRecord;
}

async function appsecQa(input: {
  readonly evidenceWorkspace: string;
  readonly scannerImports: readonly EvidenceImportResult[];
  readonly triage: Awaited<ReturnType<typeof readTriageOutput>>;
  readonly correlations: readonly AppsecCorrelationRecord[];
  readonly sarifPath: string;
  readonly signed: boolean;
  readonly failThreshold: AppsecQaFailThreshold;
}): Promise<AppsecQaResult> {
  const evidenceQa = await qaEvidenceWorkspace(input.evidenceWorkspace);
  const issues: EvidenceQaIssue[] = [...evidenceQa.issues];
  if (input.scannerImports.length === 0) {
    issues.push({
      level: "warning",
      code: "appsec-empty-scanner-imports",
      message: "AppSec audit has no passive scanner imports."
    });
  }
  if (!(await fileExists(input.sarifPath))) {
    issues.push({
      level: "error",
      code: "appsec-sarif-missing",
      message: "AppSec SARIF output is missing.",
      path: input.sarifPath
    });
  }
  if (!input.signed) {
    issues.push({
      level: "warning",
      code: "appsec-audit-bundle-unsigned",
      message: "AppSec audit bundle was generated without a manifest signature."
    });
  }
  if (!input.triage.ok) {
    issues.push({
      level: "error",
      code: "appsec-triage-invalid",
      message: input.triage.error
    });
  } else {
    for (const correlation of input.correlations) {
      if (correlation.status === "no-evidence") {
        issues.push({
          level: "warning",
          code: "appsec-agent-finding-uncorrelated",
          message: "Agent triage finding does not cite scanner evidence IDs.",
          subject: correlation.triageFindingId
        });
      }
      if (correlation.status === "missing-evidence") {
        issues.push({
          level: "error",
          code: "appsec-agent-finding-invalid-evidence-id",
          message: `Agent triage finding cites unknown evidence IDs: ${correlation.missingEvidenceIds.join(", ")}.`,
          subject: correlation.triageFindingId
        });
      }
    }
  }
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.filter((issue) => issue.level === "warning").length;
  return {
    schemaVersion: "kelpclaw.appsec.qa.v1",
    valid: errorCount === 0,
    failed: appsecQaFails(input.failThreshold, errorCount, warningCount),
    failThreshold: input.failThreshold,
    errorCount,
    warningCount,
    issues: issues.sort(
      (left, right) =>
        left.level.localeCompare(right.level) ||
        left.code.localeCompare(right.code) ||
        (left.subject ?? "").localeCompare(right.subject ?? "")
    )
  };
}

function appsecQaFails(
  threshold: AppsecQaFailThreshold,
  errorCount: number,
  warningCount: number
): boolean {
  if (threshold === "none") return false;
  if (threshold === "error") return errorCount > 0;
  return errorCount > 0 || warningCount > 0;
}

async function writeAuditBundle(input: {
  readonly outDir: string;
  readonly bundleDir: string;
  readonly runId: string;
  readonly qa: AppsecQaResult;
  readonly correlations: readonly AppsecCorrelationRecord[];
  readonly keyDir: string;
  readonly signed: boolean;
}): Promise<void> {
  const files = [
    "appsec-run.json",
    "appsec-input.json",
    "appsec-triage.json",
    "result.json",
    "appsec-qa.json",
    "policy-decisions.json",
    "findings.sarif",
    "docker-build.stdout.log",
    "docker-build.stderr.log",
    "agent.stdout.log",
    "agent.stderr.log"
  ];
  const copied: string[] = [];
  for (const file of files) {
    if (await fileExists(join(input.outDir, file))) {
      await copyFile(join(input.outDir, file), join(input.bundleDir, file));
      copied.push(file);
    }
  }
  await writeFile(
    join(input.bundleDir, "index.html"),
    appsecIndexHtml(input.runId, copied, input.qa, input.correlations),
    "utf8"
  );
  copied.push("index.html");
  if (!input.signed) {
    return;
  }
  const key = await ensureAuditSigningKey(input.keyDir);
  await signAuditBundle({
    bundleDir: input.bundleDir,
    runId: input.runId,
    files: copied,
    key
  });
  copied.push("manifest.json", "manifest.sig", "manifest.pub.json");
  await writeAuditAttestation({
    bundleDir: input.bundleDir,
    runId: input.runId,
    files: copied.filter(
      (file) => !["manifest.json", "manifest.sig", "manifest.pub.json"].includes(file)
    ),
    key
  });
}

function appsecIndexHtml(
  runId: string,
  files: readonly string[],
  qa: AppsecQaResult,
  correlations: readonly AppsecCorrelationRecord[]
): string {
  const qaRows = qa.issues
    .map(
      (issue) =>
        `<tr><td>${escapeHtml(issue.level)}</td><td>${escapeHtml(issue.code)}</td><td>${escapeHtml(issue.message)}</td><td>${escapeHtml(issue.subject ?? "")}</td></tr>`
    )
    .join("");
  const correlationRows = correlations
    .map(
      (correlation) =>
        `<tr><td>${escapeHtml(correlation.triageFindingId)}</td><td>${escapeHtml(correlation.status)}</td><td>${escapeHtml(correlation.linkedEvidenceIds.join(", "))}</td><td>${escapeHtml(correlation.missingEvidenceIds.join(", "))}</td></tr>`
    )
    .join("");
  return `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>KelpClaw AppSec Audit Bundle</title></head>
<body>
<h1>KelpClaw AppSec Audit Bundle</h1>
<p>Run: ${escapeHtml(runId)}</p>
<h2>QA</h2>
<p>Status: ${qa.valid ? "valid" : "invalid"}; threshold: ${escapeHtml(qa.failThreshold)}; errors: ${qa.errorCount}; warnings: ${qa.warningCount}</p>
<table><thead><tr><th>Level</th><th>Code</th><th>Message</th><th>Subject</th></tr></thead><tbody>${qaRows || '<tr><td colspan="4">No QA issues.</td></tr>'}</tbody></table>
<h2>Scanner Correlation</h2>
<table><thead><tr><th>Triage Finding</th><th>Status</th><th>Linked Evidence</th><th>Missing Evidence</th></tr></thead><tbody>${correlationRows || '<tr><td colspan="4">No triage findings.</td></tr>'}</tbody></table>
<h2>Files</h2>
<ul>${files.map((file) => `<li>${escapeHtml(file)}</li>`).join("")}</ul>
</body>
</html>
`;
}

async function signAuditBundle(input: {
  readonly bundleDir: string;
  readonly runId: string;
  readonly files: readonly string[];
  readonly key: AppsecAuditKeyFile;
}): Promise<void> {
  const manifest: AppsecAuditBundleManifest = {
    schemaVersion: "1.0.0",
    runId: input.runId,
    generatedAt: new Date().toISOString(),
    algorithm: "ed25519",
    publicKeyId: input.key.keyId,
    files: await Promise.all(
      input.files
        .slice()
        .sort((left, right) => left.localeCompare(right))
        .map((file) => auditManifestFile(input.bundleDir, file))
    )
  };
  const payload = stableJsonStringify(manifest as unknown as JsonValue);
  const signature = signBytes(
    null,
    Buffer.from(payload, "utf8"),
    createPrivateKey(input.key.privateKeyPem)
  ).toString("base64");
  await writeJson(join(input.bundleDir, "manifest.json"), manifest);
  await writeFile(join(input.bundleDir, "manifest.sig"), `${signature}\n`, "utf8");
  await writeJson(join(input.bundleDir, "manifest.pub.json"), {
    keyId: input.key.keyId,
    algorithm: input.key.algorithm,
    publicKeyPem: input.key.publicKeyPem
  });
}

async function writeAuditAttestation(input: {
  readonly bundleDir: string;
  readonly runId: string;
  readonly files: readonly string[];
  readonly key: AppsecAuditKeyFile;
}): Promise<void> {
  const attestation = {
    schemaVersion: "1.0.0",
    runId: input.runId,
    generatedAt: new Date().toISOString(),
    signer: {
      keyId: input.key.keyId,
      algorithm: input.key.algorithm
    },
    manifest: {
      path: "manifest.json",
      sha256: await sha256File(join(input.bundleDir, "manifest.json")),
      signaturePath: "manifest.sig",
      publicKeyPath: "manifest.pub.json"
    },
    files: input.files.slice().sort((left, right) => left.localeCompare(right)),
    evidence: {
      governanceReport: false,
      controls: false,
      sarif: input.files.includes("findings.sarif"),
      webEvidence: false,
      evidenceWorkspace: false,
      hookEvents: false,
      agentRun: input.files.includes("appsec-run.json")
    }
  };
  const payload = stableJsonStringify(attestation as unknown as JsonValue);
  const signature = signBytes(
    null,
    Buffer.from(payload, "utf8"),
    createPrivateKey(input.key.privateKeyPem)
  ).toString("base64");
  await writeJson(join(input.bundleDir, "attestation.json"), attestation);
  await writeFile(join(input.bundleDir, "attestation.sig"), `${signature}\n`, "utf8");
}

async function ensureAuditSigningKey(keyDir: string): Promise<AppsecAuditKeyFile> {
  await mkdir(keyDir, { recursive: true });
  const keyPath = join(keyDir, "audit-ed25519.json");
  if (await fileExists(keyPath)) {
    const existing = JSON.parse(await readFile(keyPath, "utf8")) as AppsecAuditKeyFile;
    if (existing.algorithm !== "ed25519" || !existing.privateKeyPem || !existing.publicKeyPem) {
      throw new Error(`${keyPath} is not a valid KelpClaw Ed25519 audit key.`);
    }
    return existing;
  }
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" }
  });
  const key: AppsecAuditKeyFile = {
    schemaVersion: "1.0.0",
    algorithm: "ed25519",
    keyId: `sha256:${createHash("sha256").update(publicKey, "utf8").digest("hex")}`,
    publicKeyPem: publicKey,
    privateKeyPem: privateKey
  };
  await writeJson(keyPath, key);
  return key;
}

async function auditManifestFile(
  root: string,
  path: string
): Promise<AppsecAuditBundleManifestFile> {
  const absolutePath = join(root, path);
  const info = await stat(absolutePath);
  return {
    path,
    size: info.size,
    sha256: await sha256File(absolutePath)
  };
}

function policyRecord(
  subject: string,
  tool: string,
  args: JsonRecord,
  ruleset: Parameters<typeof evaluatePolicy>[1]
): AppsecPolicyRecord {
  return {
    subject,
    tool,
    args,
    decision: evaluatePolicy({ tool, args }, ruleset)
  };
}

function policyBlocks(decision: PolicyDecision): boolean {
  return decision.action === "deny" || decision.action === "require-approval";
}

function runCommand(
  command: readonly string[],
  cwd: string,
  extraEnv: Readonly<Record<string, string>> = {}
): Promise<CommandResult> {
  const [executable, ...args] = command;
  if (!executable) {
    return Promise.resolve(skippedCommand(command));
  }
  return new Promise((resolveCommand, rejectCommand) => {
    const child = spawn(executable, args, {
      cwd,
      env: { ...process.env, ...extraEnv },
      stdio: ["ignore", "pipe", "pipe"]
    });
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    child.stdout.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
    child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
    child.on("error", rejectCommand);
    child.on("close", (code) =>
      resolveCommand({
        command,
        exitCode: code ?? 1,
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8")
      })
    );
  });
}

function skippedCommand(command: readonly string[]): CommandResult {
  return {
    command,
    exitCode: 0,
    stdout: "",
    stderr: ""
  };
}

async function inspectImageId(
  dockerBin: string,
  imageTag: string,
  cwd: string
): Promise<string | undefined> {
  const result = await runCommand(
    [dockerBin, "image", "inspect", imageTag, "--format", "{{.Id}}"],
    cwd
  ).catch(() => undefined);
  return result && result.exitCode === 0 ? result.stdout.trim() || undefined : undefined;
}

async function hashDirectory(root: string): Promise<JsonRecord> {
  const files = await listHashableFiles(root, root);
  const hash = createHash("sha256");
  for (const file of files) {
    const absolutePath = join(root, file);
    hash.update(file);
    hash.update("\0");
    hash.update(await sha256File(absolutePath));
    hash.update("\0");
  }
  return {
    algorithm: "sha256",
    fileCount: files.length,
    sha256: `sha256:${hash.digest("hex")}`
  };
}

async function listHashableFiles(root: string, current: string): Promise<readonly string[]> {
  const entries = await readdir(current, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    if (excludedContextEntries.has(entry.name)) {
      continue;
    }
    const absolutePath = join(current, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listHashableFiles(root, absolutePath)));
    } else if (entry.isFile()) {
      files.push(relative(root, absolutePath));
    }
  }
  return files.sort((left, right) => left.localeCompare(right));
}

const excludedContextEntries = new Set([
  ".git",
  ".kelpclaw",
  "node_modules",
  "dist",
  "coverage",
  ".next",
  "target"
]);

async function sha256File(path: string): Promise<string> {
  return createHash("sha256")
    .update(await readFile(path))
    .digest("hex");
}

function resolvePath(base: string, value: string): string {
  return value.startsWith("/") ? value : resolve(base, value);
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : undefined;
}

function options(args: readonly string[], name: string): readonly string[] {
  const values: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === name && args[index + 1]) {
      values.push(args[index + 1] as string);
      index += 1;
    }
  }
  return values;
}

function hasFlag(args: readonly string[], name: string): boolean {
  return args.includes(name);
}

function appsecQaFailThreshold(args: readonly string[]): AppsecQaFailThreshold {
  const value = option(args, "--fail-on-qa");
  if (!value && hasFlag(args, "--fail-on-qa")) return "error";
  if (!value || value === "false" || value === "none") return "none";
  if (value === "true" || value === "error") return "error";
  if (value === "warning") return "warning";
  throw new Error("--fail-on-qa must be one of none, warning, or error.");
}

function requiredOption(args: readonly string[], name: string): string {
  const value = option(args, name);
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

function severityValue(value: unknown): AppsecAgentFinding["severity"] {
  return value === "low" ||
    value === "medium" ||
    value === "high" ||
    value === "critical" ||
    value === "info"
    ? value
    : "info";
}

function confidenceValue(value: unknown): AppsecAgentFinding["confidence"] {
  return value === "low" || value === "medium" || value === "high" || value === "confirmed"
    ? value
    : "low";
}

function sarifLevel(severity: string): string {
  if (severity === "critical" || severity === "high") {
    return "error";
  }
  if (severity === "medium") {
    return "warning";
  }
  return "note";
}

function safeRuleId(value: string): string {
  return value.replace(/[^a-z0-9_.-]/giu, "-").toLowerCase();
}

function safeTag(value: string): string {
  return value.replace(/[^a-z0-9_.-]/giu, "-").toLowerCase();
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/gu, "&amp;")
    .replace(/</gu, "&lt;")
    .replace(/>/gu, "&gt;")
    .replace(/"/gu, "&quot;");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function fileExists(path: string): Promise<boolean> {
  return stat(path)
    .then(() => true)
    .catch(() => false);
}

async function writeJson(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${stableJsonStringify(value as JsonValue)}\n`, "utf8");
}

async function writeTextWithParents(path: string, value: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, value, "utf8");
}

function printJson(value: unknown): void {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}
