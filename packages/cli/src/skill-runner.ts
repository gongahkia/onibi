import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  randomUUID,
  sign as signBytes,
  verify as verifyBytes
} from "node:crypto";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readdir, readFile, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join, relative, resolve } from "node:path";
import {
  evaluatePolicy,
  policyPackToYaml,
  requirePolicyPack,
  type PolicyDecision,
  type PolicyRuleSet
} from "@kelpclaw/policy";
import {
  copyEvidenceWorkspaceBundle,
  evidenceWorkspaceSummary,
  type EvidenceWorkspaceSummary
} from "@kelpclaw/evidence";
import {
  readWebEvidenceBundle,
  writeWebEvidenceFiles,
  type WebEvidenceBundle
} from "@kelpclaw/web-intel";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";

export type SkillNetworkMode = "none" | "declared";
export type SkillSandboxProfile = "safe-local" | "networked" | "destructive-risk";

export interface SkillPolicyFinding {
  readonly tool: string;
  readonly action: PolicyDecision["action"];
  readonly matchedRuleIds: readonly string[];
  readonly reason: string;
  readonly approverRole?: string | undefined;
}

export interface SkillCompatibilityReport {
  readonly runnable: boolean;
  readonly toolsDetected: readonly string[];
  readonly requiredSecrets: readonly string[];
  readonly network: SkillNetworkMode;
  readonly sandboxProfile: SkillSandboxProfile;
  readonly policyFindings: readonly SkillPolicyFinding[];
}

export interface SkillRunOutput {
  readonly ok: boolean;
  readonly runId: string;
  readonly status: "succeeded" | "blocked" | "failed";
  readonly runDir: string;
  readonly compatibility: SkillCompatibilityReport;
  readonly policyPack: string;
  readonly mode: "audit" | "live";
  readonly agent?: string | undefined;
  readonly wrapper?: boolean | undefined;
}

export interface AuditBundleOutput {
  readonly ok: true;
  readonly runId: string;
  readonly bundleDir: string;
  readonly files: readonly string[];
  readonly signed: boolean;
  readonly manifest?: string | undefined;
}

export interface AuditBundleVerificationOutput {
  readonly ok: boolean;
  readonly bundleDir: string;
  readonly runId?: string | undefined;
  readonly strict: boolean;
  readonly signature: {
    readonly valid: boolean;
    readonly keyId?: string | undefined;
    readonly algorithm?: string | undefined;
  };
  readonly attestation?:
    | {
        readonly valid: boolean;
        readonly signed: boolean;
        readonly manifestHash?: string | undefined;
        readonly referencedFiles: readonly string[];
      }
    | undefined;
  readonly files: {
    readonly checked: number;
    readonly failed: readonly string[];
  };
  readonly failures: readonly string[];
}

export interface AuditKeyOutput {
  readonly ok: true;
  readonly keyDir: string;
  readonly keyPath: string;
  readonly keyId: string;
  readonly algorithm: "ed25519";
  readonly publicKeyPem: string;
}

export interface PolicyExplainOutput {
  readonly ok: boolean;
  readonly skill: {
    readonly ref: string;
    readonly name: string;
    readonly contentHash: string;
  };
  readonly policyPack: string;
  readonly compatibility: SkillCompatibilityReport;
  readonly plannedSteps: readonly PolicyExplainStep[];
  readonly summary: {
    readonly totalSteps: number;
    readonly allowed: number;
    readonly logOnly: number;
    readonly requireApproval: number;
    readonly denied: number;
  };
}

export interface SarifExportOutput {
  readonly ok: boolean;
  readonly out?: string | undefined;
  readonly resultCount: number;
  readonly sarif: JsonRecord;
}

export interface GovernanceControlsOutput {
  readonly ok: boolean;
  readonly out?: string | undefined;
  readonly controlCount: number;
  readonly markdown: string;
}

export type InventoryCoverageSeverity = "info" | "moderate" | "high";

export interface InventoryScanOutput {
  readonly ok: true;
  readonly schemaVersion: "1.0.0";
  readonly generatedAt: string;
  readonly root: string;
  readonly policyPack: string;
  readonly skills: readonly InventorySkillRecord[];
  readonly runs: readonly InventoryRunRecord[];
  readonly bundles: readonly InventoryBundleRecord[];
  readonly webEvidence: readonly InventoryWebEvidenceRecord[];
  readonly evidenceWorkspaces: readonly InventoryEvidenceWorkspaceRecord[];
  readonly githubActions: readonly InventoryGitHubActionRecord[];
  readonly mcpGateways: readonly InventoryMcpGatewayRecord[];
  readonly permissionEdges: readonly InventoryPermissionEdge[];
  readonly coverageFindings: readonly InventoryCoverageFinding[];
}

export interface InventoryGraphOutput {
  readonly ok: true;
  readonly format: "markdown" | "mermaid";
  readonly edgeCount: number;
  readonly out?: string | undefined;
  readonly content: string;
}

export interface InventoryCoverageOutput {
  readonly ok: boolean;
  readonly format: "json" | "markdown";
  readonly findingCount: number;
  readonly summary: {
    readonly high: number;
    readonly moderate: number;
    readonly info: number;
  };
  readonly findings: readonly InventoryCoverageFinding[];
  readonly out?: string | undefined;
  readonly markdown?: string | undefined;
}

export type GovernanceAutonomyTier = "low" | "moderate" | "high";
export type GovernanceFindingSeverity = "info" | "moderate" | "high";
export type GovernanceSubjectKind = "skill" | "run";

export interface GovernanceReportOutput {
  readonly ok: boolean;
  readonly schemaVersion: "1.0.0";
  readonly region: string;
  readonly framework: string;
  readonly generatedAt: string;
  readonly subject: {
    readonly kind: GovernanceSubjectKind;
    readonly ref?: string | undefined;
    readonly runId?: string | undefined;
    readonly name?: string | undefined;
    readonly contentHash?: string | undefined;
  };
  readonly policyPack: string;
  readonly runnable: boolean;
  readonly autonomyTier: GovernanceAutonomyTier;
  readonly riskSummary: {
    readonly toolRisk: GovernanceAutonomyTier;
    readonly dataRisk: GovernanceAutonomyTier;
    readonly networkRisk: GovernanceAutonomyTier;
    readonly reversibilityRisk: GovernanceAutonomyTier;
  };
  readonly controls: {
    readonly policyPack: string;
    readonly sandboxProfile: SkillSandboxProfile;
    readonly approvalRequired: boolean;
    readonly denied: boolean;
    readonly auditTrail: boolean;
    readonly replayEvidence: boolean;
    readonly signedBundle: boolean;
    readonly hookEvents: boolean;
    readonly failClosed: boolean;
    readonly webEvidence: boolean;
    readonly evidenceWorkspace: boolean;
  };
  readonly webEvidence?:
    | {
        readonly sourceCount: number;
        readonly providers: readonly string[];
        readonly domains: readonly string[];
        readonly storedFullContent: boolean;
        readonly redacted: boolean;
        readonly errorCount: number;
      }
    | undefined;
  readonly evidenceWorkspace?:
    | {
        readonly evidenceCount: number;
        readonly findingCount: number;
        readonly signed: boolean;
        readonly verified: boolean;
        readonly highOrCriticalFindings: number;
        readonly sourceReferenceGaps: number;
      }
    | undefined;
  readonly findings: readonly GovernanceFinding[];
  readonly frameworkMapping: readonly GovernanceFrameworkMapping[];
  readonly residualRisks: readonly string[];
  readonly files?: readonly string[] | undefined;
}

interface GovernanceFinding {
  readonly severity: GovernanceFindingSeverity;
  readonly category:
    | "policy"
    | "tool-risk"
    | "data-risk"
    | "network-risk"
    | "reversibility"
    | "auditability";
  readonly title: string;
  readonly evidence: string;
  readonly recommendation: string;
}

interface GovernanceFrameworkMapping {
  readonly controlArea: string;
  readonly evidence: readonly string[];
  readonly status: "covered" | "partial" | "gap";
}

interface InventorySkillRecord {
  readonly path: string;
  readonly name: string;
  readonly toolsDetected: readonly string[];
  readonly requiredSecrets: readonly string[];
  readonly network: SkillNetworkMode;
  readonly sandboxProfile: SkillSandboxProfile;
  readonly runnable: boolean;
  readonly policyFindings: readonly SkillPolicyFinding[];
  readonly error?: string | undefined;
}

interface InventoryRunRecord {
  readonly runId: string;
  readonly runDir: string;
  readonly status?: string | undefined;
  readonly skillRef?: string | undefined;
  readonly policyPack?: string | undefined;
  readonly hasAuditJsonl: boolean;
  readonly hasHookEvents: boolean;
}

interface InventoryBundleRecord {
  readonly bundleDir: string;
  readonly runId?: string | undefined;
  readonly hasManifest: boolean;
  readonly hasSignature: boolean;
  readonly hasPublicKey: boolean;
  readonly hasAttestation: boolean;
  readonly hasSarif: boolean;
  readonly hasControls: boolean;
  readonly hasGovernanceReport: boolean;
}

interface InventoryWebEvidenceRecord {
  readonly path: string;
  readonly provider: string;
  readonly sourceCount: number;
  readonly storedFullContent: boolean;
  readonly redacted: boolean;
}

interface InventoryEvidenceWorkspaceRecord {
  readonly path: string;
  readonly evidenceCount: number;
  readonly findingCount: number;
  readonly signed: boolean;
  readonly verified: boolean;
  readonly highOrCriticalFindings: number;
  readonly sourceReferenceGaps: number;
  readonly latestManifest?: string | undefined;
  readonly verificationFailures: readonly string[];
}

interface InventoryGitHubActionRecord {
  readonly path: string;
  readonly usesAuditSkill: boolean;
  readonly uploadsSarif: boolean;
  readonly hasInventoryMode: boolean;
}

interface InventoryMcpGatewayRecord {
  readonly path: string;
  readonly command: string;
  readonly policy?: string | undefined;
  readonly allowsBrowserTools: boolean;
}

interface InventoryPermissionEdge {
  readonly source: string;
  readonly target: string;
  readonly kind:
    | "uses-tool"
    | "requires-secret"
    | "declares-network"
    | "protected-by"
    | "exported-as"
    | "signed-by"
    | "has-web-evidence"
    | "has-evidence-workspace"
    | "configured-in";
}

interface InventoryCoverageFinding {
  readonly severity: InventoryCoverageSeverity;
  readonly category:
    | "policy"
    | "runtime-evidence"
    | "bundle-evidence"
    | "network-evidence"
    | "evidence"
    | "automation"
    | "coverage";
  readonly title: string;
  readonly evidence: string;
  readonly recommendation: string;
}

export interface ReplayDiffOutput {
  readonly ok: boolean;
  readonly skill: {
    readonly ref: string;
    readonly contentHash: string;
  };
  readonly agents: readonly string[];
  readonly same: {
    readonly toolSequence: boolean;
    readonly normalizedHashes: boolean;
    readonly outputs: boolean;
    readonly policyDecisions: boolean;
  };
  readonly runs: readonly AgentReplaySummary[];
  readonly differences: readonly string[];
}

interface AgentReplaySummary {
  readonly agent: string;
  readonly tools: readonly string[];
  readonly normalizedHashes: readonly string[];
  readonly outputHashes: readonly string[];
  readonly policyDecisionActions: readonly string[];
  readonly runId?: string | undefined;
  readonly runDir?: string | undefined;
  readonly exitCode?: number | undefined;
}

interface SkillDocument {
  readonly ref: string;
  readonly source: "local" | "github";
  readonly content: string;
  readonly frontmatter: Readonly<Record<string, unknown>>;
  readonly contentHash: string;
}

interface PlannedToolStep {
  readonly tool: string;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
}

interface SkillAnalysis {
  readonly document: SkillDocument;
  readonly name: string;
  readonly description: string;
  readonly tags: readonly string[];
  readonly toolsDetected: readonly string[];
  readonly requiredSecrets: readonly string[];
  readonly network: SkillNetworkMode;
  readonly sandboxProfile: SkillSandboxProfile;
  readonly plannedSteps: readonly PlannedToolStep[];
}

interface PolicyDecisionRecord {
  readonly tool: string;
  readonly args: JsonRecord;
  readonly decision: PolicyDecision;
}

interface PolicyExplainStep extends PolicyDecisionRecord {
  readonly index: number;
}

interface AgentRunRecord {
  readonly agent: string;
  readonly command: readonly string[];
  readonly hookCommand: string;
  readonly hookEventsPath: string;
  readonly workspaceDir: string;
  readonly artifactsDir: string;
  readonly stdoutPath: string;
  readonly stderrPath: string;
  readonly lastMessagePath?: string | undefined;
  readonly wrapper: boolean;
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly hookEvents: readonly LocalHookEvent[];
  readonly wrapperEvents: readonly LocalHookEvent[];
  readonly enforcement: LiveRunEnforcement;
  readonly observedSteps: readonly PlannedToolStep[];
  readonly generatedArtifacts: readonly string[];
  readonly workspaceFiles: readonly string[];
  readonly startedAt: string;
  readonly finishedAt: string;
}

interface LocalHookEvent {
  readonly id: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly decision: PolicyDecision;
  readonly status: "allowed" | "denied" | "approval-required";
  readonly recordedAt: string;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
}

interface LiveRunEnforcement {
  readonly enabled: boolean;
  readonly plannedBlocked: boolean;
  readonly hookBlocked: boolean;
  readonly wrapperBlocked: boolean;
  readonly unclassifiedBlocked: boolean;
  readonly observedBlocked: boolean;
  readonly source:
    | "planned-policy"
    | "hook-pretool"
    | "wrapper-observed"
    | "unclassified-event"
    | "observed-policy"
    | "none";
  readonly terminatedByPolicy?: boolean | undefined;
}

interface RunCommandResult {
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly terminatedByPolicy: boolean;
}

interface WrapperObservation {
  readonly event?: LocalHookEvent | undefined;
  readonly blocked: boolean;
  readonly unclassified: boolean;
}

interface AuditKeyFile {
  readonly schemaVersion: "1.0.0";
  readonly algorithm: "ed25519";
  readonly keyId: string;
  readonly publicKeyPem: string;
  readonly privateKeyPem: string;
}

interface AuditBundleManifest {
  readonly schemaVersion: "1.0.0";
  readonly runId: string;
  readonly generatedAt: string;
  readonly algorithm: "ed25519";
  readonly publicKeyId: string;
  readonly files: readonly AuditBundleManifestFile[];
}

interface AuditBundleManifestFile {
  readonly path: string;
  readonly size: number;
  readonly sha256: string;
}

interface AuditBundleAttestation {
  readonly schemaVersion: "1.0.0";
  readonly runId: string;
  readonly generatedAt: string;
  readonly skillHash?: string | undefined;
  readonly policyPack?: string | undefined;
  readonly signer: {
    readonly keyId: string;
    readonly algorithm: "ed25519";
  };
  readonly manifest: {
    readonly path: "manifest.json";
    readonly sha256: string;
    readonly signaturePath: "manifest.sig";
    readonly publicKeyPath: "manifest.pub.json";
  };
  readonly files: readonly string[];
  readonly evidence: {
    readonly governanceReport: boolean;
    readonly controls: boolean;
    readonly sarif: boolean;
    readonly webEvidence: boolean;
    readonly evidenceWorkspace: boolean;
    readonly hookEvents: boolean;
    readonly agentRun: boolean;
  };
}

interface SkillRunInternalResult extends SkillRunOutput {
  readonly analysis: SkillAnalysis;
  readonly plannedDecisions: readonly PolicyDecisionRecord[];
  readonly observedDecisions: readonly PolicyDecisionRecord[];
  readonly agentRun?: AgentRunRecord | undefined;
}

const knownTools = [
  "Bash",
  "Read",
  "Write",
  "Edit",
  "MultiEdit",
  "Glob",
  "Grep",
  "LS",
  "WebFetch",
  "WebSearch",
  "Task",
  "TodoWrite"
] as const;

const auditBundleFiles = [
  "skill.json",
  "workflow.json",
  "bom.json",
  "audit.jsonl",
  "policy-decisions.json"
] as const;

export async function compatibilityReport(
  args: readonly string[]
): Promise<SkillCompatibilityReport> {
  const skillRef = requiredPositional(args, 0);
  const policyPackName = option(args, "--policy") ?? "baseline";
  const analysis = await analyzeSkillReference(skillRef, requirePolicyPack(policyPackName).ruleset);
  return compatibilityFromAnalysis(analysis, requirePolicyPack(policyPackName).ruleset);
}

export async function runSkill(args: readonly string[]): Promise<SkillRunOutput> {
  const result = await runSkillInternal(args);
  if (result.status !== "succeeded") {
    process.exitCode = 1;
  }
  return {
    ok: result.ok,
    runId: result.runId,
    status: result.status,
    runDir: result.runDir,
    compatibility: result.compatibility,
    policyPack: result.policyPack,
    mode: result.mode,
    ...(result.agent ? { agent: result.agent } : {}),
    ...(result.wrapper !== undefined ? { wrapper: result.wrapper } : {})
  };
}

async function runSkillInternal(args: readonly string[]): Promise<SkillRunInternalResult> {
  const skillRef = requiredPositional(args, 0);
  const inputPath = requiredOption(args, "--input");
  const policyPackName = option(args, "--policy") ?? "baseline";
  const agent = option(args, "--agent");
  const wrapper = hasFlag(args, "--wrapper");
  const runId = option(args, "--run-id") ?? `skill-run.${Date.now()}.${randomUUID()}`;
  const runsRoot = resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs");
  const runDir = join(runsRoot, runId);
  const policyPack = requirePolicyPack(policyPackName);
  const input = jsonRecord(JSON.parse(await readFile(inputPath, "utf8")) as unknown, inputPath);
  const analysis = await analyzeSkillReference(skillRef, policyPack.ruleset);
  const compatibility = compatibilityFromAnalysis(analysis, policyPack.ruleset);
  const plannedDecisions = evaluatePlannedSteps(analysis, policyPack.ruleset);
  const enforcePolicy = hasFlag(args, "--enforce-policy") || agent !== undefined;
  const plannedBlocked =
    !compatibility.runnable || plannedDecisions.some((record) => record.decision.action === "deny");
  const createdAt = new Date().toISOString();

  await mkdir(runDir, { recursive: true });
  let agentRun: AgentRunRecord | undefined;
  let observedDecisions: readonly PolicyDecisionRecord[] = [];
  let status: SkillRunOutput["status"] = plannedBlocked ? "blocked" : "succeeded";
  if (!plannedBlocked && agent) {
    agentRun = await runLiveAgent({
      args,
      agent,
      analysis,
      input,
      runDir,
      policyPackName: policyPack.name,
      ruleset: policyPack.ruleset,
      enforcePolicy
    });
    observedDecisions = evaluatePlannedSteps(
      { ...analysis, plannedSteps: agentRun.observedSteps },
      policyPack.ruleset
    );
    const observedBlocked = observedDecisions.some(
      (record) => record.decision.action === "deny" || record.decision.action === "require-approval"
    );
    status =
      agentRun.enforcement.hookBlocked ||
      agentRun.enforcement.wrapperBlocked ||
      agentRun.enforcement.unclassifiedBlocked ||
      observedBlocked
        ? "blocked"
        : agentRun.exitCode === 0
          ? "succeeded"
          : "failed";
    await writeJson(join(runDir, "agent-run.json"), agentRun);
    await writeFile(join(runDir, "stdout.log"), agentRun.stdout, "utf8");
    await writeFile(join(runDir, "stderr.log"), agentRun.stderr, "utf8");
  }

  const policyDecisions = observedDecisions.length > 0 ? observedDecisions : plannedDecisions;
  await writeJson(join(runDir, "skill.json"), skillJson(analysis));
  await writeJson(join(runDir, "input.json"), input);
  await writeJson(join(runDir, "compatibility.json"), compatibility);
  await writeJson(join(runDir, "policy-decisions.json"), {
    policyPack: policyPack.name,
    policyPackDescription: policyPack.description,
    ruleset: policyPack.ruleset,
    plannedDecisions,
    observedDecisions,
    decisions: policyDecisions
  });
  await writeJson(join(runDir, "workflow.json"), workflowJson(analysis, runId, createdAt));
  await writeJson(
    join(runDir, "bom.json"),
    bomJson(analysis, runId, policyPack.name, createdAt, agentRun)
  );
  await writeFile(
    join(runDir, "audit.jsonl"),
    auditJsonl({
      analysis,
      compatibility,
      plannedDecisions,
      observedDecisions,
      runId,
      createdAt,
      status,
      agentRun
    }),
    "utf8"
  );
  await writeJson(join(runDir, "result.json"), {
    ok: status === "succeeded",
    runId,
    status,
    runDir,
    compatibility,
    policyPack: policyPack.name,
    mode: agent ? "live" : "audit",
    ...(agent ? { agent } : {}),
    ...(agent ? { wrapper } : {}),
    ...(agentRun ? { exitCode: agentRun.exitCode } : {})
  });

  return {
    ok: status === "succeeded",
    runId,
    status,
    runDir,
    compatibility,
    policyPack: policyPack.name,
    mode: agent ? "live" : "audit",
    ...(agent ? { agent } : {}),
    ...(agent ? { wrapper } : {}),
    analysis,
    plannedDecisions,
    observedDecisions,
    ...(agentRun ? { agentRun } : {})
  };
}

export async function exportAuditBundle(args: readonly string[]): Promise<AuditBundleOutput> {
  const runId = requiredPositional(args, 0);
  const runsRoot = resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs");
  const runDir = join(runsRoot, runId);
  const bundleDir = resolve(option(args, "--out") ?? join(".kelpclaw/audit-bundles", runId));
  await mkdir(bundleDir, { recursive: true });

  const copied: string[] = [];
  for (const file of auditBundleFiles) {
    await copyFile(join(runDir, file), join(bundleDir, file));
    copied.push(file);
  }
  for (const file of [
    "compatibility.json",
    "result.json",
    "agent-run.json",
    "hook-events.jsonl",
    "wrapper-events.jsonl",
    "stdout.log",
    "stderr.log"
  ]) {
    if (await fileExists(join(runDir, file))) {
      await copyFile(join(runDir, file), join(bundleDir, file));
      copied.push(file);
    }
  }
  await writeFile(join(bundleDir, "index.html"), await auditIndexHtml(runDir), "utf8");
  copied.push("index.html");
  const signed = !hasFlag(args, "--no-sign");
  let webEvidence: WebEvidenceBundle | undefined;
  if (hasFlag(args, "--include-web-evidence")) {
    const evidencePath = option(args, "--include-web-evidence");
    if (!evidencePath) {
      throw new Error(
        "Option --include-web-evidence requires a web-evidence.json file or directory."
      );
    }
    webEvidence = await readWebEvidenceBundle(resolve(evidencePath));
    const webFiles = await writeWebEvidenceFiles(bundleDir, webEvidence);
    copied.push(...webFiles);
  }
  let evidenceSummary: EvidenceWorkspaceSummary | undefined;
  if (hasFlag(args, "--include-evidence")) {
    const evidencePath = option(args, "--include-evidence");
    if (!evidencePath) {
      throw new Error("Option --include-evidence requires an evidence workspace directory.");
    }
    const evidenceRoot = resolve(evidencePath);
    const evidenceCopy = await copyEvidenceWorkspaceBundle(
      evidenceRoot,
      join(bundleDir, "evidence-workspace")
    );
    copied.push(...evidenceCopy.files.map((file) => `evidence-workspace/${file}`));
    evidenceSummary = await evidenceWorkspaceSummary(evidenceRoot);
    await writeJson(join(bundleDir, "evidence-summary.json"), evidenceSummary);
    copied.push("evidence-summary.json");
  }
  let governance: GovernanceReportOutput | undefined;
  if (
    hasFlag(args, "--include-governance") ||
    hasFlag(args, "--include-controls") ||
    hasFlag(args, "--include-sarif")
  ) {
    governance = await governanceReportForRun({
      runId,
      runsRoot,
      region: option(args, "--region") ?? "sg",
      framework: option(args, "--framework") ?? "agentic-ai",
      webEvidence,
      evidenceSummary,
      signedBundle: signed
    });
  }
  if (hasFlag(args, "--include-governance") && governance) {
    const governanceFiles = await writeGovernanceReportFiles(bundleDir, governance);
    copied.push(...governanceFiles);
  }
  if (hasFlag(args, "--include-controls") && governance) {
    await writeFile(join(bundleDir, "controls.md"), governanceControlsMarkdown(governance), "utf8");
    copied.push("controls.md");
  }
  if (hasFlag(args, "--include-sarif")) {
    const sarif = await sarifForRun({
      runId,
      runsRoot,
      region: option(args, "--region") ?? "sg",
      framework: option(args, "--framework") ?? "agentic-ai",
      bundleDir,
      webEvidence,
      governance
    });
    await writeJson(join(bundleDir, "findings.sarif"), sarif);
    copied.push("findings.sarif");
  }
  let manifest: string | undefined;
  if (signed) {
    const key = await ensureAuditSigningKey(resolve(option(args, "--key-dir") ?? ".kelpclaw/keys"));
    const attestedFiles = [...copied];
    manifest = await signAuditBundle({
      bundleDir,
      runId,
      files: copied,
      key
    });
    copied.push("manifest.json", "manifest.sig", "manifest.pub.json");
    const attestationFiles = await writeAuditAttestation({
      bundleDir,
      runId,
      files: attestedFiles,
      key
    });
    copied.push(...attestationFiles);
  }

  return {
    ok: true,
    runId,
    bundleDir,
    files: copied,
    signed,
    ...(manifest ? { manifest } : {})
  };
}

export async function initAuditKey(args: readonly string[]): Promise<AuditKeyOutput> {
  const keyDir = resolve(option(args, "--key-dir") ?? ".kelpclaw/keys");
  const key = await ensureAuditSigningKey(keyDir);
  return {
    ok: true,
    keyDir,
    keyPath: join(keyDir, "audit-ed25519.json"),
    keyId: key.keyId,
    algorithm: key.algorithm,
    publicKeyPem: key.publicKeyPem
  };
}

export async function verifyAuditBundle(
  args: readonly string[]
): Promise<AuditBundleVerificationOutput> {
  const bundleDir = resolve(requiredPositional(args, 0));
  const strict = hasFlag(args, "--strict");
  const failures: string[] = [];
  let manifest: Partial<AuditBundleManifest>;
  let signatureValid = false;
  let manifestPublicKeyPem: string | undefined;
  try {
    manifest = JSON.parse(
      await readFile(join(bundleDir, "manifest.json"), "utf8")
    ) as Partial<AuditBundleManifest>;
  } catch (error) {
    process.exitCode = 1;
    return {
      ok: false,
      bundleDir,
      strict,
      signature: { valid: false },
      files: { checked: 0, failed: [] },
      failures: [`unable to read manifest.json: ${errorMessage(error)}`]
    };
  }
  try {
    const signature = Buffer.from(
      (await readFile(join(bundleDir, "manifest.sig"), "utf8")).trim(),
      "base64"
    );
    const publicKey = JSON.parse(await readFile(join(bundleDir, "manifest.pub.json"), "utf8")) as {
      readonly publicKeyPem?: string | undefined;
    };
    if (!publicKey.publicKeyPem) {
      failures.push("manifest.pub.json does not contain publicKeyPem.");
    } else {
      manifestPublicKeyPem = publicKey.publicKeyPem;
      signatureValid = verifyBytes(
        null,
        Buffer.from(stableJsonStringify(manifest as JsonValue), "utf8"),
        createPublicKey(publicKey.publicKeyPem),
        signature
      );
      if (!signatureValid) {
        failures.push("manifest signature is invalid.");
      }
    }
  } catch (error) {
    failures.push(`unable to verify manifest signature: ${errorMessage(error)}`);
  }
  const fileFailures: string[] = [];
  for (const entry of manifest.files ?? []) {
    if (!isSafeBundlePath(entry.path)) {
      fileFailures.push(entry.path);
      failures.push(`unsafe bundle path in manifest: ${entry.path}`);
      continue;
    }
    try {
      const actualHash = createHash("sha256")
        .update(await readFile(join(bundleDir, entry.path)))
        .digest("hex");
      if (actualHash !== entry.sha256) {
        fileFailures.push(entry.path);
        failures.push(`hash mismatch for ${entry.path}.`);
      }
    } catch (error) {
      fileFailures.push(entry.path);
      failures.push(`unable to read ${entry.path}: ${errorMessage(error)}`);
    }
  }
  const attestation = strict
    ? await verifyAuditAttestation({
        bundleDir,
        manifest,
        publicKeyPem: manifestPublicKeyPem
      })
    : undefined;
  if (attestation) {
    failures.push(...attestation.failures);
  }
  const ok = signatureValid && failures.length === 0;
  if (!ok) {
    process.exitCode = 1;
  }
  return {
    ok,
    bundleDir,
    ...(manifest.runId ? { runId: manifest.runId } : {}),
    strict,
    signature: {
      valid: signatureValid,
      ...(manifest.publicKeyId ? { keyId: manifest.publicKeyId } : {}),
      ...(manifest.algorithm ? { algorithm: manifest.algorithm } : {})
    },
    ...(attestation
      ? {
          attestation: {
            valid: attestation.valid,
            signed: attestation.signed,
            ...(attestation.manifestHash ? { manifestHash: attestation.manifestHash } : {}),
            referencedFiles: attestation.referencedFiles
          }
        }
      : {}),
    files: {
      checked: manifest.files?.length ?? 0,
      failed: fileFailures
    },
    failures
  };
}

export async function replayDiff(args: readonly string[]): Promise<ReplayDiffOutput> {
  const skillRef = requiredOption(args, "--skill");
  const agents = (option(args, "--agents") ?? "claude-code,codex-cli,goose")
    .split(",")
    .map((agent) => agent.trim())
    .filter(Boolean);
  if (agents.length === 0) {
    throw new Error("replay-diff requires at least one agent.");
  }
  if (hasFlag(args, "--recorded") || option(args, "--input")) {
    return recordedReplayDiff(args, skillRef, agents);
  }
  const policyPack = requirePolicyPack(option(args, "--policy") ?? "baseline");
  const analysis = await analyzeSkillReference(skillRef, policyPack.ruleset);
  const steps =
    analysis.plannedSteps.length > 0
      ? analysis.plannedSteps
      : [{ tool: "Read", args: { filePath: analysis.document.ref } }];
  const decisionRecords = evaluatePlannedSteps(
    { ...analysis, plannedSteps: steps },
    policyPack.ruleset
  );
  const runs = agents.map((agent) => replaySummaryForAgent(agent, steps, decisionRecords));
  const differences = replayDifferences(runs);
  return {
    ok: differences.length === 0,
    skill: {
      ref: analysis.document.ref,
      contentHash: analysis.document.contentHash
    },
    agents,
    same: {
      toolSequence: uniqueCount(runs.map((run) => run.tools.join("\n"))) === 1,
      normalizedHashes: uniqueCount(runs.map((run) => run.normalizedHashes.join("\n"))) === 1,
      outputs: uniqueCount(runs.map((run) => run.outputHashes.join("\n"))) === 1,
      policyDecisions: uniqueCount(runs.map((run) => run.policyDecisionActions.join("\n"))) === 1
    },
    runs,
    differences
  };
}

async function recordedReplayDiff(
  args: readonly string[],
  skillRef: string,
  agents: readonly string[]
): Promise<ReplayDiffOutput> {
  const inputPath = requiredOption(args, "--input");
  const policyPack = requirePolicyPack(option(args, "--policy") ?? "baseline");
  const runsRoot = resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs");
  const replayId = option(args, "--run-id") ?? `replay-diff.${Date.now()}.${randomUUID()}`;
  const runResults: SkillRunInternalResult[] = [];
  for (const [index, agent] of agents.entries()) {
    runResults.push(
      await runSkillInternal([
        skillRef,
        "--input",
        inputPath,
        "--policy",
        policyPack.name,
        "--agent",
        agent,
        "--run-id",
        `${replayId}.${index}.${agent.replace(/[^a-z0-9_.-]/giu, "-")}`,
        "--runs-dir",
        runsRoot,
        ...forwardedAgentArgs(args)
      ])
    );
  }
  const analysis =
    runResults[0]?.analysis ?? (await analyzeSkillReference(skillRef, policyPack.ruleset));
  const runs = runResults.map((result) =>
    recordedReplaySummary(
      result.agent ?? "unknown",
      result.agentRun?.observedSteps.length
        ? result.agentRun.observedSteps
        : result.analysis.plannedSteps,
      result.observedDecisions.length ? result.observedDecisions : result.plannedDecisions,
      result
    )
  );
  const differences = replayDifferences(runs);
  return {
    ok: differences.length === 0 && runResults.every((result) => result.ok),
    skill: {
      ref: analysis.document.ref,
      contentHash: analysis.document.contentHash
    },
    agents,
    same: {
      toolSequence: uniqueCount(runs.map((run) => run.tools.join("\n"))) === 1,
      normalizedHashes: uniqueCount(runs.map((run) => run.normalizedHashes.join("\n"))) === 1,
      outputs: uniqueCount(runs.map((run) => run.outputHashes.join("\n"))) === 1,
      policyDecisions: uniqueCount(runs.map((run) => run.policyDecisionActions.join("\n"))) === 1
    },
    runs,
    differences
  };
}

export function policyPackCliOutput(name: string) {
  const pack = requirePolicyPack(name);
  return {
    ok: true,
    pack: pack.name,
    description: pack.description,
    ruleset: pack.ruleset,
    yaml: policyPackToYaml(pack)
  };
}

export async function policyExplain(args: readonly string[]): Promise<PolicyExplainOutput> {
  const skillRef = requiredPositional(args, 0);
  const policyPack = requirePolicyPack(option(args, "--policy") ?? "baseline");
  const analysis = await analyzeSkillReference(skillRef, policyPack.ruleset);
  const compatibility = compatibilityFromAnalysis(analysis, policyPack.ruleset);
  const decisions = evaluatePlannedSteps(analysis, policyPack.ruleset);
  const plannedSteps = analysis.plannedSteps.map((step, index) => ({
    index,
    tool: step.tool,
    args: step.args,
    decision: decisions[index]?.decision ?? {
      action: "allow" as const,
      matchedRuleIds: [],
      reason: "no policy rules matched"
    }
  }));
  return {
    ok: compatibility.runnable,
    skill: {
      ref: analysis.document.ref,
      name: analysis.name,
      contentHash: analysis.document.contentHash
    },
    policyPack: policyPack.name,
    compatibility,
    plannedSteps,
    summary: {
      totalSteps: plannedSteps.length,
      allowed: plannedSteps.filter((step) => step.decision.action === "allow").length,
      logOnly: plannedSteps.filter((step) => step.decision.action === "log-only").length,
      requireApproval: plannedSteps.filter((step) => step.decision.action === "require-approval")
        .length,
      denied: plannedSteps.filter((step) => step.decision.action === "deny").length
    }
  };
}

export async function inventoryScan(args: readonly string[]): Promise<InventoryScanOutput> {
  const inventory = await buildInventory(args);
  const out = option(args, "--out");
  if (out) {
    await writeJsonWithParents(resolve(out), inventory);
  }
  return inventory;
}

export async function inventoryGraph(args: readonly string[]): Promise<InventoryGraphOutput> {
  const inventory = await buildInventory(args);
  const format = inventoryFormat(args);
  const content =
    format === "mermaid" ? inventoryMermaid(inventory) : inventoryGraphMarkdown(inventory);
  const out = option(args, "--out");
  if (out) {
    await writeTextWithParents(resolve(out), content);
  }
  return {
    ok: true,
    format,
    edgeCount: inventory.permissionEdges.length,
    ...(out ? { out: resolve(out) } : {}),
    content
  };
}

export async function inventoryCoverage(args: readonly string[]): Promise<InventoryCoverageOutput> {
  const inventory = await buildInventory(args);
  const format = coverageFormat(args);
  const findings = inventory.coverageFindings;
  const summary = coverageSummary(findings);
  const markdown = format === "markdown" ? inventoryCoverageMarkdown(inventory) : undefined;
  const out = option(args, "--out");
  if (out) {
    if (format === "markdown") {
      await writeTextWithParents(resolve(out), markdown ?? inventoryCoverageMarkdown(inventory));
    } else {
      await writeJsonWithParents(resolve(out), {
        ok: findings.length === 0,
        format,
        findingCount: findings.length,
        summary,
        findings
      });
    }
  }
  const failOn = option(args, "--fail-on") ?? "none";
  if (coverageShouldFail(findings, failOn)) {
    process.exitCode = 1;
  }
  return {
    ok: !coverageShouldFail(findings, failOn),
    format,
    findingCount: findings.length,
    summary,
    findings,
    ...(out ? { out: resolve(out) } : {}),
    ...(markdown ? { markdown } : {})
  };
}

export async function governanceReport(args: readonly string[]): Promise<GovernanceReportOutput> {
  const subject = requiredPositional(args, 0);
  const region = option(args, "--region") ?? "sg";
  const framework = option(args, "--framework") ?? "agentic-ai";
  const webEvidence = hasFlag(args, "--include-web-evidence")
    ? await readWebEvidenceArgument(args)
    : undefined;
  const evidenceSummary = hasFlag(args, "--include-evidence")
    ? await readEvidenceSummaryArgument(args)
    : undefined;
  const report = (await isSkillSubject(subject))
    ? await governanceReportForSkill({
        skillRef: subject,
        policyPackName: option(args, "--policy") ?? "baseline",
        region,
        framework,
        webEvidence,
        evidenceSummary
      })
    : await governanceReportForRun({
        runId: subject,
        runsRoot: resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs"),
        region,
        framework,
        bundleDir: option(args, "--bundle-dir"),
        webEvidence,
        evidenceSummary
      });
  const outDir = option(args, "--out");
  if (!outDir) {
    return report;
  }
  const files = await writeGovernanceReportFiles(resolve(outDir), report);
  return {
    ...report,
    files
  };
}

export async function governanceControls(
  args: readonly string[]
): Promise<GovernanceControlsOutput> {
  const out = option(args, "--out");
  const report = await governanceReport(stripOption(args, "--out"));
  const markdown = governanceControlsMarkdown(report);
  if (out) {
    const resolved = resolve(out);
    await mkdir(dirname(resolved), { recursive: true });
    await writeFile(resolved, markdown, "utf8");
    return {
      ok: report.ok,
      out: resolved,
      controlCount: report.frameworkMapping.length,
      markdown
    };
  }
  return {
    ok: report.ok,
    controlCount: report.frameworkMapping.length,
    markdown
  };
}

export async function exportSarif(args: readonly string[]): Promise<SarifExportOutput> {
  const subject = requiredPositional(args, 0);
  const out = option(args, "--out");
  const webEvidence = hasFlag(args, "--include-web-evidence")
    ? await readWebEvidenceArgument(args)
    : undefined;
  const sarif = (await isSkillSubject(subject))
    ? await sarifForSkill({
        skillRef: subject,
        policyPackName: option(args, "--policy") ?? "baseline",
        region: option(args, "--region") ?? "sg",
        framework: option(args, "--framework") ?? "agentic-ai",
        webEvidence
      })
    : await sarifForRun({
        runId: subject,
        runsRoot: resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs"),
        region: option(args, "--region") ?? "sg",
        framework: option(args, "--framework") ?? "agentic-ai",
        bundleDir: option(args, "--bundle-dir"),
        webEvidence
      });
  if (out) {
    const resolved = resolve(out);
    await mkdir(dirname(resolved), { recursive: true });
    await writeJson(resolved, sarif);
    return {
      ok: true,
      out: resolved,
      resultCount: sarifResultCount(sarif),
      sarif
    };
  }
  return {
    ok: true,
    resultCount: sarifResultCount(sarif),
    sarif
  };
}

async function governanceReportForSkill(input: {
  readonly skillRef: string;
  readonly policyPackName: string;
  readonly region: string;
  readonly framework: string;
  readonly webEvidence?: WebEvidenceBundle | undefined;
  readonly evidenceSummary?: EvidenceWorkspaceSummary | undefined;
}): Promise<GovernanceReportOutput> {
  const policyPack = requirePolicyPack(input.policyPackName);
  const analysis = await analyzeSkillReference(input.skillRef, policyPack.ruleset);
  const compatibility = compatibilityFromAnalysis(analysis, policyPack.ruleset);
  const decisions = evaluatePlannedSteps(analysis, policyPack.ruleset);
  return buildGovernanceReport({
    region: input.region,
    framework: input.framework,
    subject: {
      kind: "skill",
      ref: analysis.document.ref,
      name: analysis.name,
      contentHash: analysis.document.contentHash
    },
    compatibility,
    policyPackName: policyPack.name,
    decisions,
    auditTrail: false,
    replayEvidence: false,
    signedBundle: false,
    hookEvents: false,
    failClosed: false,
    webEvidence: input.webEvidence,
    evidenceSummary: input.evidenceSummary,
    sourceText: analysis.document.content
  });
}

async function governanceReportForRun(input: {
  readonly runId: string;
  readonly runsRoot: string;
  readonly region: string;
  readonly framework: string;
  readonly bundleDir?: string | undefined;
  readonly webEvidence?: WebEvidenceBundle | undefined;
  readonly evidenceSummary?: EvidenceWorkspaceSummary | undefined;
  readonly signedBundle?: boolean | undefined;
}): Promise<GovernanceReportOutput> {
  const runDir = join(input.runsRoot, input.runId);
  const skill = jsonRecord(
    JSON.parse(await readFile(join(runDir, "skill.json"), "utf8")),
    "skill.json"
  );
  const compatibility = JSON.parse(
    await readFile(join(runDir, "compatibility.json"), "utf8")
  ) as SkillCompatibilityReport;
  const policy = jsonRecord(
    JSON.parse(await readFile(join(runDir, "policy-decisions.json"), "utf8")),
    "policy-decisions.json"
  );
  const result = (await readJsonIfExists(join(runDir, "result.json"))) as JsonRecord | undefined;
  const agentRun = (await readJsonIfExists(join(runDir, "agent-run.json"))) as
    | AgentRunRecord
    | undefined;
  const policyPackName =
    stringField(policy, "policyPack") ?? stringField(result ?? {}, "policyPack") ?? "unknown";
  const decisions = policyDecisionRecords(policy.decisions);
  const defaultBundleDir = join(resolve(input.runsRoot), "..", "audit-bundles", input.runId);
  const bundleDir = input.bundleDir ? resolve(input.bundleDir) : defaultBundleDir;
  return buildGovernanceReport({
    region: input.region,
    framework: input.framework,
    subject: {
      kind: "run",
      runId: input.runId,
      ...(stringField(skill, "ref") ? { ref: stringField(skill, "ref") } : {}),
      ...(stringField(skill, "name") ? { name: stringField(skill, "name") } : {}),
      ...(stringField(skill, "contentHash")
        ? { contentHash: stringField(skill, "contentHash") }
        : {})
    },
    compatibility,
    policyPackName,
    decisions,
    auditTrail: await fileExists(join(runDir, "audit.jsonl")),
    replayEvidence: Boolean(
      agentRun?.observedSteps.length ||
      agentRun?.hookEvents.length ||
      (await fileExists(join(runDir, "hook-events.jsonl")))
    ),
    signedBundle: input.signedBundle ?? (await fileExists(join(bundleDir, "manifest.json"))),
    hookEvents: Boolean(
      agentRun?.hookEvents.length ||
      agentRun?.wrapperEvents.length ||
      (await fileExists(join(runDir, "hook-events.jsonl")))
    ),
    failClosed: Boolean(
      agentRun?.enforcement.enabled &&
      (agentRun.enforcement.hookBlocked ||
        agentRun.enforcement.wrapperBlocked ||
        agentRun.enforcement.unclassifiedBlocked ||
        agentRun.enforcement.source === "unclassified-event")
    ),
    webEvidence: input.webEvidence,
    evidenceSummary: input.evidenceSummary
  });
}

async function readWebEvidenceArgument(args: readonly string[]): Promise<WebEvidenceBundle> {
  const evidencePath = option(args, "--include-web-evidence");
  if (!evidencePath) {
    throw new Error(
      "Option --include-web-evidence requires a web-evidence.json file or directory."
    );
  }
  return readWebEvidenceBundle(resolve(evidencePath));
}

async function readEvidenceSummaryArgument(
  args: readonly string[]
): Promise<EvidenceWorkspaceSummary> {
  const evidencePath = option(args, "--include-evidence");
  if (!evidencePath) {
    throw new Error("Option --include-evidence requires an evidence workspace directory.");
  }
  return evidenceWorkspaceSummary(resolve(evidencePath));
}

async function buildInventory(args: readonly string[]): Promise<InventoryScanOutput> {
  const root = resolve(option(args, "--root") ?? ".");
  const policyPack = requirePolicyPack(option(args, "--policy") ?? "sg-agentic-ai-baseline");
  const runsRoot = resolvePath(root, option(args, "--runs-dir") ?? ".kelpclaw/runs");
  const bundlesRoot = resolvePath(root, option(args, "--bundles-dir") ?? ".kelpclaw/audit-bundles");
  const webEvidenceRoot = resolvePath(
    root,
    option(args, "--web-evidence-dir") ?? ".kelpclaw/web-evidence"
  );
  const evidenceRoot = resolvePath(root, option(args, "--evidence-dir") ?? ".kelpclaw/evidence");

  const [skills, runs, bundles, webEvidence, evidenceWorkspaces, githubActions, mcpGateways] =
    await Promise.all([
      inventorySkills(root, policyPack.ruleset),
      inventoryRuns(root, runsRoot),
      inventoryBundles(root, bundlesRoot),
      inventoryWebEvidence(root, webEvidenceRoot),
      inventoryEvidenceWorkspaces(root, evidenceRoot),
      inventoryGitHubActions(root),
      inventoryMcpGateways(root)
    ]);
  const permissionEdges = inventoryPermissionEdges({
    policyPack: policyPack.name,
    skills,
    runs,
    bundles,
    webEvidence,
    evidenceWorkspaces,
    githubActions,
    mcpGateways
  });
  const coverageFindings = inventoryCoverageFindings({
    policyPack: policyPack.name,
    skills,
    runs,
    bundles,
    webEvidence,
    evidenceWorkspaces,
    githubActions,
    mcpGateways
  });
  return {
    ok: true,
    schemaVersion: "1.0.0",
    generatedAt: new Date().toISOString(),
    root,
    policyPack: policyPack.name,
    skills,
    runs,
    bundles,
    webEvidence,
    evidenceWorkspaces,
    githubActions,
    mcpGateways,
    permissionEdges,
    coverageFindings
  };
}

async function inventorySkills(
  root: string,
  ruleset: PolicyRuleSet
): Promise<readonly InventorySkillRecord[]> {
  const skillPaths = await scanFiles(root, (file) => basename(file) === "SKILL.md");
  const skills = await Promise.all(
    skillPaths.map(async (skillPath) => {
      try {
        const analysis = await analyzeSkillReference(skillPath, ruleset);
        const compatibility = compatibilityFromAnalysis(analysis, ruleset);
        return {
          path: relativePath(root, skillPath),
          name: analysis.name,
          toolsDetected: compatibility.toolsDetected,
          requiredSecrets: compatibility.requiredSecrets,
          network: compatibility.network,
          sandboxProfile: compatibility.sandboxProfile,
          runnable: compatibility.runnable,
          policyFindings: compatibility.policyFindings
        };
      } catch (error) {
        return {
          path: relativePath(root, skillPath),
          name: basename(dirname(skillPath)) || "unknown-skill",
          toolsDetected: [],
          requiredSecrets: [],
          network: "none" as const,
          sandboxProfile: "safe-local" as const,
          runnable: false,
          policyFindings: [],
          error: errorMessage(error)
        };
      }
    })
  );
  return skills.sort((left, right) => left.path.localeCompare(right.path));
}

async function inventoryRuns(
  root: string,
  runsRoot: string
): Promise<readonly InventoryRunRecord[]> {
  const runDirs = await childDirectories(runsRoot);
  const runs = await Promise.all(
    runDirs.map(async (runDir) => {
      const result = (await readJsonIfExists(join(runDir, "result.json"))) as
        | JsonRecord
        | undefined;
      const skill = (await readJsonIfExists(join(runDir, "skill.json"))) as JsonRecord | undefined;
      const policy = (await readJsonIfExists(join(runDir, "policy-decisions.json"))) as
        | JsonRecord
        | undefined;
      const agentRun = (await readJsonIfExists(join(runDir, "agent-run.json"))) as
        | AgentRunRecord
        | undefined;
      const runId = stringField(result ?? {}, "runId") ?? basename(runDir);
      return {
        runId,
        runDir: relativePath(root, runDir),
        ...(stringField(result ?? {}, "status")
          ? { status: stringField(result ?? {}, "status") }
          : {}),
        ...(stringField(skill ?? {}, "ref") ? { skillRef: stringField(skill ?? {}, "ref") } : {}),
        ...(stringField(policy ?? {}, "policyPack") || stringField(result ?? {}, "policyPack")
          ? {
              policyPack:
                stringField(policy ?? {}, "policyPack") ?? stringField(result ?? {}, "policyPack")
            }
          : {}),
        hasAuditJsonl: await fileExists(join(runDir, "audit.jsonl")),
        hasHookEvents: Boolean(
          agentRun?.hookEvents.length ||
          agentRun?.wrapperEvents.length ||
          (await fileExists(join(runDir, "hook-events.jsonl")))
        )
      };
    })
  );
  return runs.sort((left, right) => left.runId.localeCompare(right.runId));
}

async function inventoryBundles(
  root: string,
  bundlesRoot: string
): Promise<readonly InventoryBundleRecord[]> {
  const bundleDirs = await childDirectories(bundlesRoot);
  const bundles = await Promise.all(
    bundleDirs.map(async (bundleDir) => {
      const manifest = (await readJsonIfExists(join(bundleDir, "manifest.json"))) as
        | Partial<AuditBundleManifest>
        | undefined;
      return {
        bundleDir: relativePath(root, bundleDir),
        ...(manifest?.runId ? { runId: manifest.runId } : {}),
        hasManifest: Boolean(manifest),
        hasSignature: await fileExists(join(bundleDir, "manifest.sig")),
        hasPublicKey: await fileExists(join(bundleDir, "manifest.pub.json")),
        hasAttestation: Boolean(
          (await fileExists(join(bundleDir, "attestation.json"))) &&
          (await fileExists(join(bundleDir, "attestation.sig")))
        ),
        hasSarif: await fileExists(join(bundleDir, "findings.sarif")),
        hasControls: await fileExists(join(bundleDir, "controls.md")),
        hasGovernanceReport: await fileExists(join(bundleDir, "governance-report.json"))
      };
    })
  );
  return bundles.sort((left, right) => left.bundleDir.localeCompare(right.bundleDir));
}

async function inventoryWebEvidence(
  root: string,
  webEvidenceRoot: string
): Promise<readonly InventoryWebEvidenceRecord[]> {
  const evidenceFiles = await scanFiles(
    webEvidenceRoot,
    (file) => basename(file) === "web-evidence.json"
  );
  const evidence = await Promise.all(
    evidenceFiles.map(async (evidenceFile) => {
      const bundle = await readWebEvidenceBundle(evidenceFile);
      return {
        path: relativePath(root, evidenceFile),
        provider: bundle.selectedProvider,
        sourceCount: bundle.summary.sourceCount,
        storedFullContent: bundle.summary.storedFullContent,
        redacted: bundle.summary.redacted
      };
    })
  );
  return evidence.sort((left, right) => left.path.localeCompare(right.path));
}

async function inventoryEvidenceWorkspaces(
  root: string,
  evidenceRoot: string
): Promise<readonly InventoryEvidenceWorkspaceRecord[]> {
  const workspaceFiles = await scanFiles(
    evidenceRoot,
    (file) => basename(file) === "workspace.json"
  );
  const workspaces = await Promise.all(
    workspaceFiles.map(async (workspaceFile) => {
      const summary = await evidenceWorkspaceSummary(dirname(workspaceFile));
      return {
        path: relativePath(root, dirname(workspaceFile)),
        evidenceCount: summary.evidenceCount,
        findingCount: summary.findingCount,
        signed: summary.signed,
        verified: summary.verified,
        highOrCriticalFindings: summary.highOrCriticalFindings,
        sourceReferenceGaps: summary.sourceReferenceGaps,
        ...(summary.latestManifest
          ? { latestManifest: relativePath(root, summary.latestManifest) }
          : {}),
        verificationFailures: summary.verificationFailures
      };
    })
  );
  return workspaces.sort((left, right) => left.path.localeCompare(right.path));
}

async function inventoryGitHubActions(
  root: string
): Promise<readonly InventoryGitHubActionRecord[]> {
  const githubRoot = join(root, ".github");
  const actionFiles = await scanFiles(githubRoot, (file) => /\.(?:ya?ml)$/iu.test(file));
  const actions = await Promise.all(
    actionFiles.map(async (actionFile) => {
      const content = await readFile(actionFile, "utf8");
      return {
        path: relativePath(root, actionFile),
        usesAuditSkill: /audit-skill/iu.test(content),
        uploadsSarif: /upload-sarif|findings\.sarif|upload-sarif@/iu.test(content),
        hasInventoryMode: /mode:\s*(?:audit\|inventory|inventory)|inventory\s+scan/iu.test(content)
      };
    })
  );
  return actions
    .filter((action) => action.usesAuditSkill || action.hasInventoryMode)
    .sort((left, right) => left.path.localeCompare(right.path));
}

async function inventoryMcpGateways(root: string): Promise<readonly InventoryMcpGatewayRecord[]> {
  const candidates = await scanFiles(root, (file) => likelyTextFile(file));
  const gateways: InventoryMcpGatewayRecord[] = [];
  for (const file of candidates) {
    const content = await readFile(file, "utf8").catch(() => "");
    if (!/mcp\s+web-gateway/iu.test(content)) {
      continue;
    }
    for (const match of content.matchAll(/kelp-claw\s+mcp\s+web-gateway[^\n`"']*/giu)) {
      const command = match[0].trim();
      gateways.push({
        path: relativePath(root, file),
        command,
        ...(policyFromCommand(command) ? { policy: policyFromCommand(command) } : {}),
        allowsBrowserTools: /--allow-browser-tools/u.test(command)
      });
    }
  }
  return gateways.sort(
    (left, right) =>
      left.path.localeCompare(right.path) || left.command.localeCompare(right.command)
  );
}

function inventoryPermissionEdges(input: {
  readonly policyPack: string;
  readonly skills: readonly InventorySkillRecord[];
  readonly runs: readonly InventoryRunRecord[];
  readonly bundles: readonly InventoryBundleRecord[];
  readonly webEvidence: readonly InventoryWebEvidenceRecord[];
  readonly evidenceWorkspaces: readonly InventoryEvidenceWorkspaceRecord[];
  readonly githubActions: readonly InventoryGitHubActionRecord[];
  readonly mcpGateways: readonly InventoryMcpGatewayRecord[];
}): readonly InventoryPermissionEdge[] {
  const edges: InventoryPermissionEdge[] = [];
  for (const skill of input.skills) {
    const skillNode = `skill:${skill.path}`;
    edges.push({ source: skillNode, target: `policy:${input.policyPack}`, kind: "protected-by" });
    for (const tool of skill.toolsDetected) {
      edges.push({ source: skillNode, target: `tool:${tool}`, kind: "uses-tool" });
    }
    for (const secret of skill.requiredSecrets) {
      edges.push({ source: skillNode, target: `secret:${secret}`, kind: "requires-secret" });
    }
    if (skill.network === "declared") {
      edges.push({ source: skillNode, target: "network:declared", kind: "declares-network" });
      for (const evidence of input.webEvidence) {
        edges.push({
          source: skillNode,
          target: `web-evidence:${evidence.path}`,
          kind: "has-web-evidence"
        });
      }
    }
  }
  for (const run of input.runs) {
    const bundle = input.bundles.find((candidate) => candidate.runId === run.runId);
    if (bundle) {
      edges.push({
        source: `run:${run.runId}`,
        target: `bundle:${bundle.bundleDir}`,
        kind: "exported-as"
      });
    }
  }
  for (const bundle of input.bundles) {
    if (bundle.hasAttestation) {
      edges.push({
        source: `bundle:${bundle.bundleDir}`,
        target: "attestation:ed25519",
        kind: "signed-by"
      });
    }
  }
  for (const workspace of input.evidenceWorkspaces) {
    edges.push({
      source: `evidence-workspace:${workspace.path}`,
      target: `policy:${input.policyPack}`,
      kind: "protected-by"
    });
    if (workspace.verified) {
      edges.push({
        source: `evidence-workspace:${workspace.path}`,
        target: "evidence-manifest:sha256",
        kind: "signed-by"
      });
    }
    for (const skill of input.skills) {
      edges.push({
        source: `skill:${skill.path}`,
        target: `evidence-workspace:${workspace.path}`,
        kind: "has-evidence-workspace"
      });
    }
  }
  for (const action of input.githubActions) {
    edges.push({
      source: `github-action:${action.path}`,
      target: "kelpclaw:audit-skill",
      kind: "configured-in"
    });
  }
  for (const gateway of input.mcpGateways) {
    edges.push({
      source: `mcp-gateway:${gateway.path}`,
      target: gateway.policy ? `policy:${gateway.policy}` : `policy:${input.policyPack}`,
      kind: "protected-by"
    });
  }
  return dedupeInventoryEdges(edges);
}

function inventoryCoverageFindings(input: {
  readonly policyPack: string;
  readonly skills: readonly InventorySkillRecord[];
  readonly runs: readonly InventoryRunRecord[];
  readonly bundles: readonly InventoryBundleRecord[];
  readonly webEvidence: readonly InventoryWebEvidenceRecord[];
  readonly evidenceWorkspaces: readonly InventoryEvidenceWorkspaceRecord[];
  readonly githubActions: readonly InventoryGitHubActionRecord[];
  readonly mcpGateways: readonly InventoryMcpGatewayRecord[];
}): readonly InventoryCoverageFinding[] {
  const findings: InventoryCoverageFinding[] = [];
  for (const skill of input.skills) {
    if (!skill.runnable || skill.error) {
      findings.push({
        severity: "high",
        category: "policy",
        title: "Skill is not runnable",
        evidence: skill.error ?? `${skill.path} has deny-level policy findings.`,
        recommendation: "Fix the skill or select a stricter review workflow before production use."
      });
    }
    for (const finding of skill.policyFindings.filter((finding) => finding.action === "deny")) {
      findings.push({
        severity: "high",
        category: "policy",
        title: `Denied policy finding for ${skill.path}`,
        evidence: `${finding.tool}: ${finding.reason}`,
        recommendation: "Keep fail-closed behavior enabled and revise the skill."
      });
    }
    if (skill.toolsDetected.includes("Task") && input.policyPack !== "browser-automation-strict") {
      findings.push({
        severity: "high",
        category: "automation",
        title: "Agentic task capability without strict browser policy",
        evidence: `${skill.path} declares Task while inventory policy is ${input.policyPack}.`,
        recommendation: "Use browser-automation-strict for browser/web-agent automation."
      });
    }
    if (skill.network === "declared" && input.webEvidence.length === 0) {
      findings.push({
        severity: "moderate",
        category: "network-evidence",
        title: "Networked skill has no web evidence",
        evidence: `${skill.path} declares network access but no web-evidence.json files were found.`,
        recommendation: "Attach governed web evidence or document why none is required."
      });
    }
    if (input.policyPack === "baseline") {
      findings.push({
        severity: "moderate",
        category: "policy",
        title: "Baseline policy used for inventory",
        evidence: `${skill.path} was assessed with baseline policy.`,
        recommendation:
          "Use sg-agentic-ai-baseline or another SG/APAC policy pack for governance inventory."
      });
    }
  }
  for (const run of input.runs) {
    const bundle = input.bundles.find((candidate) => candidate.runId === run.runId);
    if (!bundle?.hasManifest || !bundle.hasSignature) {
      findings.push({
        severity: "high",
        category: "runtime-evidence",
        title: "Run has no signed audit bundle",
        evidence: `${run.runId} has no matching signed bundle.`,
        recommendation: "Export and strict-verify an audit bundle for this run."
      });
    }
  }
  for (const bundle of input.bundles) {
    if (bundle.hasManifest && !bundle.hasAttestation) {
      findings.push({
        severity: "moderate",
        category: "bundle-evidence",
        title: "Bundle lacks strict attestation",
        evidence: `${bundle.bundleDir} has a manifest but no attestation.json/attestation.sig pair.`,
        recommendation: "Re-export the bundle with current KelpClaw signing."
      });
    }
    if (bundle.hasManifest && !bundle.hasControls) {
      findings.push({
        severity: "info",
        category: "coverage",
        title: "Bundle has no controls matrix",
        evidence: `${bundle.bundleDir} does not include controls.md.`,
        recommendation: "Export with --include-controls for reviewer handoff."
      });
    }
  }
  for (const workspace of input.evidenceWorkspaces) {
    if (!workspace.signed || !workspace.verified) {
      findings.push({
        severity: "moderate",
        category: "evidence",
        title: "Evidence workspace is not verified",
        evidence: `${workspace.path} signed=${workspace.signed} verified=${workspace.verified}.`,
        recommendation: "Run kelp-claw evidence sign and kelp-claw evidence verify before handoff."
      });
    }
    if (workspace.sourceReferenceGaps > 0) {
      findings.push({
        severity: "moderate",
        category: "evidence",
        title: "Evidence findings lack source references",
        evidence: `${workspace.path} has ${workspace.sourceReferenceGaps} findings without source references.`,
        recommendation: "Import findings through governed adapters or attach source references."
      });
    }
    if (workspace.highOrCriticalFindings > 0) {
      findings.push({
        severity: "info",
        category: "evidence",
        title: "High-risk evidence findings present",
        evidence: `${workspace.path} has ${workspace.highOrCriticalFindings} high or critical findings.`,
        recommendation: "Route these findings through reviewer triage and remediation tracking."
      });
    }
  }
  for (const action of input.githubActions) {
    if (action.usesAuditSkill && !action.uploadsSarif) {
      findings.push({
        severity: "moderate",
        category: "coverage",
        title: "GitHub Action does not upload SARIF",
        evidence: `${action.path} uses audit-skill without SARIF upload evidence.`,
        recommendation: "Enable upload-sarif in the KelpClaw GitHub Action."
      });
    }
  }
  if (input.skills.length > 0 && input.runs.length === 0) {
    findings.push({
      severity: "info",
      category: "runtime-evidence",
      title: "No skill runs found",
      evidence: "Inventory did not find .kelpclaw/runs entries.",
      recommendation: "Run representative skills to collect runtime evidence."
    });
  }
  if (input.skills.length > 0 && input.bundles.length === 0) {
    findings.push({
      severity: "info",
      category: "bundle-evidence",
      title: "No audit bundles found",
      evidence: "Inventory did not find .kelpclaw/audit-bundles entries.",
      recommendation: "Export signed audit bundles for externally reviewable evidence."
    });
  }
  return dedupeInventoryFindings(findings);
}

function buildGovernanceReport(input: {
  readonly region: string;
  readonly framework: string;
  readonly subject: GovernanceReportOutput["subject"];
  readonly compatibility: SkillCompatibilityReport;
  readonly policyPackName: string;
  readonly decisions: readonly PolicyDecisionRecord[];
  readonly auditTrail: boolean;
  readonly replayEvidence: boolean;
  readonly signedBundle: boolean;
  readonly hookEvents: boolean;
  readonly failClosed: boolean;
  readonly webEvidence?: WebEvidenceBundle | undefined;
  readonly evidenceSummary?: EvidenceWorkspaceSummary | undefined;
  readonly sourceText?: string | undefined;
}): GovernanceReportOutput {
  const denied = hasAction(input.compatibility, input.decisions, "deny");
  const approvalRequired = hasAction(input.compatibility, input.decisions, "require-approval");
  const tools = input.compatibility.toolsDetected;
  const serializedEvidence = stableJsonStringify({
    compatibility: input.compatibility,
    decisions: input.decisions.map((decision) => ({
      tool: decision.tool,
      args: decision.args,
      action: decision.decision.action
    })),
    sourceText: input.sourceText ?? ""
  } as unknown as JsonValue).toLowerCase();
  const hasMutatingTool = tools.some((tool) => ["Write", "Edit", "MultiEdit"].includes(tool));
  const hasNetwork = input.compatibility.network === "declared";
  const hasWebEvidence = Boolean(input.webEvidence);
  const hasEvidenceWorkspace = Boolean(input.evidenceSummary);
  const hasSecrets = input.compatibility.requiredSecrets.length > 0;
  const hasPii = /(email|phone|passport|nric|ssn|dob|address|customer|user|personal data)/iu.test(
    serializedEvidence
  );
  const hasDestructive = input.compatibility.sandboxProfile === "destructive-risk";
  const mutatingShell = input.decisions.some(
    (decision) => decision.tool === "Bash" && mutationPattern().test(jsonText(decision.args))
  );
  const riskSummary = {
    toolRisk: tier([
      hasDestructive || denied,
      tools.includes("Bash") || hasMutatingTool || mutatingShell || tools.includes("Task")
    ]),
    dataRisk: tier([hasNetwork && hasSecrets, hasSecrets || hasPii || hasMutatingTool]),
    networkRisk: tier([hasNetwork && hasSecrets, hasNetwork || hasWebEvidence]),
    reversibilityRisk: tier([hasDestructive || denied, hasMutatingTool || mutatingShell])
  };
  const autonomyTier = maxTier([
    riskSummary.toolRisk,
    riskSummary.dataRisk,
    riskSummary.networkRisk,
    riskSummary.reversibilityRisk,
    approvalRequired ? "moderate" : "low",
    input.failClosed ? "high" : "low"
  ]);
  const controls = {
    policyPack: input.policyPackName,
    sandboxProfile: input.compatibility.sandboxProfile,
    approvalRequired,
    denied,
    auditTrail: input.auditTrail,
    replayEvidence: input.replayEvidence,
    signedBundle: input.signedBundle,
    hookEvents: input.hookEvents,
    failClosed: input.failClosed,
    webEvidence: hasWebEvidence,
    evidenceWorkspace: hasEvidenceWorkspace
  };
  const webEvidenceSummary = input.webEvidence
    ? webEvidenceGovernanceSummary(input.webEvidence)
    : undefined;
  const evidenceWorkspaceSummary = input.evidenceSummary
    ? evidenceGovernanceSummary(input.evidenceSummary)
    : undefined;
  const findings = governanceFindings({
    compatibility: input.compatibility,
    decisions: input.decisions,
    denied,
    approvalRequired,
    hasMutatingTool,
    hasNetwork,
    hasSecrets,
    hasPii,
    hasDestructive,
    mutatingShell,
    auditTrail: input.auditTrail,
    hookEvents: input.hookEvents,
    failClosed: input.failClosed,
    webEvidence: input.webEvidence,
    evidenceSummary: input.evidenceSummary
  });
  return {
    ok: input.compatibility.runnable && !denied,
    schemaVersion: "1.0.0",
    region: input.region,
    framework: input.framework,
    generatedAt: new Date().toISOString(),
    subject: input.subject,
    policyPack: input.policyPackName,
    runnable: input.compatibility.runnable,
    autonomyTier,
    riskSummary,
    controls,
    ...(webEvidenceSummary ? { webEvidence: webEvidenceSummary } : {}),
    ...(evidenceWorkspaceSummary ? { evidenceWorkspace: evidenceWorkspaceSummary } : {}),
    findings,
    frameworkMapping: governanceFrameworkMapping(controls, input.compatibility, findings),
    residualRisks: governanceResidualRisks(input.subject.kind, controls)
  };
}

function webEvidenceGovernanceSummary(
  bundle: WebEvidenceBundle
): NonNullable<GovernanceReportOutput["webEvidence"]> {
  const providers = uniqueSorted([
    bundle.selectedProvider,
    ...bundle.events.map((event) => event.provider),
    ...bundle.sources.map((source) => source.provider)
  ]);
  const domains = uniqueSorted(
    bundle.sources
      .map((source) => source.url)
      .filter((url): url is string => Boolean(url))
      .map((url) => hostnameFromUrl(url))
      .filter((hostname): hostname is string => Boolean(hostname))
  );
  return {
    sourceCount: bundle.summary.sourceCount,
    providers,
    domains,
    storedFullContent: bundle.summary.storedFullContent,
    redacted: bundle.summary.redacted,
    errorCount: bundle.summary.errorCount
  };
}

function evidenceGovernanceSummary(
  summary: EvidenceWorkspaceSummary
): NonNullable<GovernanceReportOutput["evidenceWorkspace"]> {
  return {
    evidenceCount: summary.evidenceCount,
    findingCount: summary.findingCount,
    signed: summary.signed,
    verified: summary.verified,
    highOrCriticalFindings: summary.highOrCriticalFindings,
    sourceReferenceGaps: summary.sourceReferenceGaps
  };
}

function governanceControlsMarkdown(report: GovernanceReportOutput): string {
  const subject =
    report.subject.kind === "run"
      ? `run ${report.subject.runId ?? "unknown"}`
      : `skill ${report.subject.name ?? report.subject.ref ?? "unknown"}`;
  const rows = report.frameworkMapping
    .map((mapping) =>
      [
        mapping.controlArea,
        mapping.status,
        controlEvidenceFiles(mapping.controlArea, report).join(", ") || "governance-report.json",
        report.residualRisks.join("<br>"),
        reviewerAction(mapping.status)
      ]
        .map(markdownCell)
        .join(" | ")
    )
    .map((row) => `| ${row} |`)
    .join("\n");
  return `# KelpClaw Governance Controls

Subject: ${subject}

Region: ${report.region}

Framework: ${report.framework}

Autonomy tier: ${report.autonomyTier}

| Control Area | Status | Evidence Files | Residual Risk | Reviewer Action |
| --- | --- | --- | --- | --- |
${rows}
`;
}

function controlEvidenceFiles(
  controlArea: string,
  report: GovernanceReportOutput
): readonly string[] {
  if (/accountability|approval/iu.test(controlArea)) {
    return ["policy-decisions.json", "governance-report.json"];
  }
  if (/autonomy|technical/iu.test(controlArea)) {
    return ["compatibility.json", "policy-decisions.json", "result.json"];
  }
  if (/traceability|audit/iu.test(controlArea)) {
    return [
      "audit.jsonl",
      ...(report.controls.hookEvents ? ["hook-events.jsonl"] : []),
      ...(report.controls.signedBundle ? ["manifest.json", "manifest.sig"] : []),
      ...(report.controls.webEvidence ? ["web-evidence.json", "web-events.jsonl"] : []),
      ...(report.controls.evidenceWorkspace
        ? ["evidence-summary.json", "evidence-workspace/audit-log.jsonl"]
        : [])
    ];
  }
  if (/data|third-party/iu.test(controlArea)) {
    return [
      "compatibility.json",
      "bom.json",
      ...(report.controls.webEvidence ? ["web-bom.json", "web-evidence.json"] : []),
      ...(report.controls.evidenceWorkspace
        ? ["evidence-workspace/evidence/index.json", "evidence-workspace/normalized/findings.json"]
        : [])
    ];
  }
  return ["governance-report.json", "controls.md"];
}

function reviewerAction(status: GovernanceFrameworkMapping["status"]): string {
  if (status === "covered") {
    return "Verify evidence and sign off.";
  }
  if (status === "gap") {
    return "Block or document an explicit exception.";
  }
  return "Collect missing evidence or reviewer approval.";
}

function markdownCell(value: string): string {
  return value.replace(/\|/gu, "\\|").replace(/\n/gu, "<br>");
}

async function writeGovernanceReportFiles(
  outDir: string,
  report: GovernanceReportOutput
): Promise<readonly string[]> {
  await mkdir(outDir, { recursive: true });
  await writeJson(join(outDir, "governance-report.json"), report);
  await writeFile(join(outDir, "governance-report.html"), governanceReportHtml(report), "utf8");
  return ["governance-report.json", "governance-report.html"];
}

function governanceReportHtml(report: GovernanceReportOutput): string {
  const findingsRows = report.findings
    .map(
      (finding) =>
        `<tr><td>${escapeHtml(finding.severity)}</td><td>${escapeHtml(finding.category)}</td><td>${escapeHtml(finding.title)}</td><td>${escapeHtml(finding.recommendation)}</td></tr>`
    )
    .join("\n");
  const mappingRows = report.frameworkMapping
    .map(
      (mapping) =>
        `<tr><td>${escapeHtml(mapping.controlArea)}</td><td>${escapeHtml(mapping.status)}</td><td>${escapeHtml(mapping.evidence.join("; "))}</td></tr>`
    )
    .join("\n");
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>KelpClaw Governance Report</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #1f2937; }
    h1 { font-size: 24px; }
    h2 { font-size: 16px; margin-top: 24px; }
    table { border-collapse: collapse; width: 100%; margin-top: 8px; }
    th, td { border: 1px solid #d9e2ec; padding: 8px; text-align: left; vertical-align: top; }
    th { background: #f8fafc; }
    pre { background: #f8fafc; border: 1px solid #d9e2ec; border-radius: 6px; padding: 12px; overflow: auto; }
  </style>
</head>
<body>
  <h1>KelpClaw Governance Report</h1>
  <p><strong>Region:</strong> ${escapeHtml(report.region)} &middot; <strong>Framework:</strong> ${escapeHtml(report.framework)} &middot; <strong>Autonomy tier:</strong> ${escapeHtml(report.autonomyTier)}</p>
  <h2>Controls</h2>
  <pre>${escapeHtml(stableJsonStringify(report.controls as unknown as JsonValue))}</pre>
  <h2>Findings</h2>
  <table><thead><tr><th>Severity</th><th>Category</th><th>Finding</th><th>Recommendation</th></tr></thead><tbody>${findingsRows}</tbody></table>
  <h2>Framework Mapping</h2>
  <table><thead><tr><th>Control Area</th><th>Status</th><th>Evidence</th></tr></thead><tbody>${mappingRows}</tbody></table>
  <h2>Residual Risks</h2>
  <pre>${escapeHtml(report.residualRisks.join("\n"))}</pre>
</body>
</html>
`;
}

async function sarifForSkill(input: {
  readonly skillRef: string;
  readonly policyPackName: string;
  readonly region: string;
  readonly framework: string;
  readonly webEvidence?: WebEvidenceBundle | undefined;
}): Promise<JsonRecord> {
  const policyPack = requirePolicyPack(input.policyPackName);
  const analysis = await analyzeSkillReference(input.skillRef, policyPack.ruleset);
  const compatibility = compatibilityFromAnalysis(analysis, policyPack.ruleset);
  const decisions = evaluatePlannedSteps(analysis, policyPack.ruleset);
  const governance = buildGovernanceReport({
    region: input.region,
    framework: input.framework,
    subject: {
      kind: "skill",
      ref: analysis.document.ref,
      name: analysis.name,
      contentHash: analysis.document.contentHash
    },
    compatibility,
    policyPackName: policyPack.name,
    decisions,
    auditTrail: false,
    replayEvidence: false,
    signedBundle: false,
    hookEvents: false,
    failClosed: false,
    webEvidence: input.webEvidence,
    sourceText: analysis.document.content
  });
  return buildSarif({
    subjectKind: "skill",
    locationUri: analysis.document.ref,
    compatibility,
    decisions,
    governance,
    webEvidence: input.webEvidence
  });
}

async function sarifForRun(input: {
  readonly runId: string;
  readonly runsRoot: string;
  readonly region: string;
  readonly framework: string;
  readonly bundleDir?: string | undefined;
  readonly webEvidence?: WebEvidenceBundle | undefined;
  readonly governance?: GovernanceReportOutput | undefined;
}): Promise<JsonRecord> {
  const runDir = join(input.runsRoot, input.runId);
  const compatibility = JSON.parse(
    await readFile(join(runDir, "compatibility.json"), "utf8")
  ) as SkillCompatibilityReport;
  const policy = jsonRecord(
    JSON.parse(await readFile(join(runDir, "policy-decisions.json"), "utf8")),
    "policy-decisions.json"
  );
  const decisions = policyDecisionRecords(policy.decisions);
  const governance =
    input.governance ??
    (await governanceReportForRun({
      runId: input.runId,
      runsRoot: input.runsRoot,
      region: input.region,
      framework: input.framework,
      bundleDir: input.bundleDir,
      webEvidence: input.webEvidence
    }));
  return buildSarif({
    subjectKind: "run",
    locationUri: runDir,
    compatibility,
    decisions,
    governance,
    webEvidence: input.webEvidence
  });
}

function buildSarif(input: {
  readonly subjectKind: GovernanceSubjectKind;
  readonly locationUri: string;
  readonly compatibility: SkillCompatibilityReport;
  readonly decisions: readonly PolicyDecisionRecord[];
  readonly governance: GovernanceReportOutput;
  readonly webEvidence?: WebEvidenceBundle | undefined;
}): JsonRecord {
  const results = [
    ...input.compatibility.policyFindings.map((finding) =>
      sarifResult({
        ruleId: sarifPolicyRuleId(finding),
        level: policyActionLevel(finding.action),
        title: `${finding.action} policy finding for ${finding.tool}`,
        message: `${finding.reason}; rules=${finding.matchedRuleIds.join(",") || "none"}`,
        locationUri: input.locationUri,
        properties: {
          source: "compatibility",
          subjectKind: input.subjectKind,
          tool: finding.tool,
          action: finding.action,
          matchedRuleIds: [...finding.matchedRuleIds]
        }
      })
    ),
    ...input.decisions
      .filter(
        (record) =>
          record.decision.action === "deny" || record.decision.action === "require-approval"
      )
      .map((record) =>
        sarifResult({
          ruleId: sarifDecisionRuleId(record),
          level: policyActionLevel(record.decision.action),
          title: `${record.decision.action} decision for ${record.tool}`,
          message: `${record.decision.reason}; args=${jsonText(record.args)}`,
          locationUri: input.locationUri,
          properties: {
            source: "policy-decision",
            subjectKind: input.subjectKind,
            tool: record.tool,
            action: record.decision.action,
            matchedRuleIds: [...record.decision.matchedRuleIds]
          }
        })
      ),
    ...input.governance.findings.map((finding) =>
      sarifResult({
        ruleId: `kelp.governance.${finding.category}`,
        level: governanceSeverityLevel(finding.severity),
        title: finding.title,
        message: `${finding.evidence} Recommendation: ${finding.recommendation}`,
        locationUri: input.locationUri,
        properties: {
          source: "governance",
          subjectKind: input.subjectKind,
          category: finding.category,
          severity: finding.severity
        }
      })
    ),
    ...(input.webEvidence
      ? webEvidenceSarifResults(input.webEvidence, input.locationUri, input.subjectKind)
      : [])
  ];
  const rules = uniqueSarifRules(results);
  return {
    version: "2.1.0",
    $schema: "https://json.schemastore.org/sarif-2.1.0.json",
    runs: [
      {
        tool: {
          driver: {
            name: "KelpClaw",
            informationUri: "https://github.com/gongahkia/kelp-claw",
            semanticVersion: "0.1.0",
            rules
          }
        },
        results
      }
    ]
  } as unknown as JsonRecord;
}

function webEvidenceSarifResults(
  evidence: WebEvidenceBundle,
  locationUri: string,
  subjectKind: GovernanceSubjectKind
): readonly JsonRecord[] {
  const results: JsonRecord[] = [
    sarifResult({
      ruleId: "kelp.web.evidence",
      level: "note",
      title: "Web evidence attached",
      message: `Web evidence contains ${evidence.summary.sourceCount} source(s) from ${evidence.selectedProvider}.`,
      locationUri,
      properties: {
        source: "web-evidence",
        subjectKind,
        provider: evidence.selectedProvider,
        sourceCount: evidence.summary.sourceCount
      }
    })
  ];
  if (evidence.summary.storedFullContent) {
    results.push(
      sarifResult({
        ruleId: "kelp.web.full-content-stored",
        level: "warning",
        title: "Full web content stored",
        message: "Web evidence stores full source content; confirm retention and sharing controls.",
        locationUri,
        properties: { source: "web-evidence", subjectKind }
      })
    );
  }
  if (evidence.summary.redacted) {
    results.push(
      sarifResult({
        ruleId: "kelp.web.redacted-content",
        level: "note",
        title: "Web evidence was redacted",
        message: "KelpClaw redacted secret-like or personal-data-like content from web evidence.",
        locationUri,
        properties: { source: "web-evidence", subjectKind }
      })
    );
  }
  if (evidence.summary.errorCount > 0) {
    results.push(
      sarifResult({
        ruleId: "kelp.web.provider-errors",
        level: "warning",
        title: "Web provider errors recorded",
        message: `Web evidence recorded ${evidence.summary.errorCount} provider error(s).`,
        locationUri,
        properties: { source: "web-evidence", subjectKind }
      })
    );
  }
  return results;
}

function sarifResult(input: {
  readonly ruleId: string;
  readonly level: "error" | "warning" | "note";
  readonly title: string;
  readonly message: string;
  readonly locationUri: string;
  readonly properties: JsonRecord;
}): JsonRecord {
  return {
    ruleId: input.ruleId,
    level: input.level,
    message: { text: input.message },
    locations: [
      {
        physicalLocation: {
          artifactLocation: { uri: input.locationUri },
          region: { startLine: 1 }
        }
      }
    ],
    properties: {
      title: input.title,
      ...input.properties
    }
  };
}

function uniqueSarifRules(results: readonly JsonRecord[]): readonly JsonRecord[] {
  const ruleIds = uniqueSorted(
    results
      .map((result) => stringField(result, "ruleId"))
      .filter((ruleId): ruleId is string => Boolean(ruleId))
  );
  return ruleIds.map((id) => ({
    id,
    name: id,
    shortDescription: { text: sarifRuleTitle(id) },
    help: { text: "Generated by KelpClaw policy, governance, and audit evidence." }
  }));
}

function sarifRuleTitle(ruleId: string): string {
  return ruleId
    .replace(/^kelp\./u, "")
    .replace(/[._-]+/gu, " ")
    .replace(/\b\w/gu, (match) => match.toUpperCase());
}

function sarifPolicyRuleId(finding: SkillPolicyFinding): string {
  return finding.matchedRuleIds[0]
    ? `kelp.policy.${finding.matchedRuleIds[0]}`
    : `kelp.policy.${finding.action}`;
}

function sarifDecisionRuleId(record: PolicyDecisionRecord): string {
  return record.decision.matchedRuleIds[0]
    ? `kelp.policy.${record.decision.matchedRuleIds[0]}`
    : `kelp.policy.${record.decision.action}`;
}

function policyActionLevel(action: PolicyDecision["action"]): "error" | "warning" | "note" {
  if (action === "deny") {
    return "error";
  }
  if (action === "require-approval") {
    return "warning";
  }
  return "note";
}

function governanceSeverityLevel(
  severity: GovernanceFindingSeverity
): "error" | "warning" | "note" {
  if (severity === "high") {
    return "error";
  }
  if (severity === "moderate") {
    return "warning";
  }
  return "note";
}

function sarifResultCount(sarif: JsonRecord): number {
  const runs = Array.isArray(sarif.runs) ? sarif.runs : [];
  const firstRun = runs[0] as JsonRecord | undefined;
  return Array.isArray(firstRun?.results) ? firstRun.results.length : 0;
}

function governanceFindings(input: {
  readonly compatibility: SkillCompatibilityReport;
  readonly decisions: readonly PolicyDecisionRecord[];
  readonly denied: boolean;
  readonly approvalRequired: boolean;
  readonly hasMutatingTool: boolean;
  readonly hasNetwork: boolean;
  readonly hasSecrets: boolean;
  readonly hasPii: boolean;
  readonly hasDestructive: boolean;
  readonly mutatingShell: boolean;
  readonly auditTrail: boolean;
  readonly hookEvents: boolean;
  readonly failClosed: boolean;
  readonly webEvidence?: WebEvidenceBundle | undefined;
  readonly evidenceSummary?: EvidenceWorkspaceSummary | undefined;
}): readonly GovernanceFinding[] {
  const findings: GovernanceFinding[] = [];
  for (const finding of input.compatibility.policyFindings) {
    findings.push({
      severity: finding.action === "deny" ? "high" : "moderate",
      category: "policy",
      title: `${finding.action} policy finding for ${finding.tool}`,
      evidence: `${finding.reason}; rules=${finding.matchedRuleIds.join(",") || "none"}`,
      recommendation:
        finding.action === "deny"
          ? "Do not run until the skill or policy exception is reviewed."
          : "Route this step through an accountable reviewer before execution."
    });
  }
  for (const decision of input.decisions) {
    if (decision.decision.action !== "deny" && decision.decision.action !== "require-approval") {
      continue;
    }
    findings.push({
      severity: decision.decision.action === "deny" ? "high" : "moderate",
      category: "policy",
      title: `${decision.decision.action} decision for ${decision.tool}`,
      evidence: `${decision.decision.reason}; args=${jsonText(decision.args)}`,
      recommendation:
        decision.decision.action === "deny"
          ? "Keep fail-closed behavior enabled and revise the skill before production use."
          : "Capture reviewer approval and rationale before executing this action."
    });
  }
  if (input.hasDestructive) {
    findings.push({
      severity: "high",
      category: "reversibility",
      title: "Destructive shell behavior detected",
      evidence: "The sandbox profile is destructive-risk.",
      recommendation: "Block by default or require a break-glass workflow with separate approval."
    });
  }
  if (input.hasMutatingTool || input.mutatingShell) {
    findings.push({
      severity: "moderate",
      category: "tool-risk",
      title: "Mutating tool capability detected",
      evidence: "The skill can write files, edit files, or mutate external developer systems.",
      recommendation: "Require approval for irreversible or externally visible actions."
    });
  }
  if (input.hasNetwork) {
    findings.push({
      severity: input.hasSecrets ? "high" : "moderate",
      category: "network-risk",
      title: "External network access declared",
      evidence: `Required secrets: ${input.compatibility.requiredSecrets.join(", ") || "none"}.`,
      recommendation:
        "Restrict outbound destinations and include network evidence in the audit bundle."
    });
  }
  if (input.webEvidence) {
    findings.push({
      severity: input.webEvidence.summary.storedFullContent ? "moderate" : "info",
      category: "network-risk",
      title: "Web evidence attached",
      evidence: `Sources: ${input.webEvidence.summary.sourceCount}; provider: ${input.webEvidence.selectedProvider}; stored full content: ${input.webEvidence.summary.storedFullContent}.`,
      recommendation:
        "Review source domains, provider terms, and retention before forwarding externally."
    });
  }
  if (input.evidenceSummary) {
    findings.push({
      severity: input.evidenceSummary.verified ? "info" : "moderate",
      category: "auditability",
      title: "Evidence workspace attached",
      evidence: `Evidence records: ${input.evidenceSummary.evidenceCount}; findings: ${input.evidenceSummary.findingCount}; verified: ${input.evidenceSummary.verified}.`,
      recommendation:
        "Review normalized findings, raw evidence digests, and chain-of-custody status before handoff."
    });
    if (input.evidenceSummary.sourceReferenceGaps > 0) {
      findings.push({
        severity: "moderate",
        category: "auditability",
        title: "Evidence findings without source references",
        evidence: `${input.evidenceSummary.sourceReferenceGaps} normalized findings lack source references.`,
        recommendation: "Attach adapter provenance or reviewer notes before relying on these findings."
      });
    }
  }
  if (input.hasPii || input.hasSecrets) {
    findings.push({
      severity: input.hasNetwork && input.hasSecrets ? "high" : "moderate",
      category: "data-risk",
      title: "Personal data or secret-like material may be handled",
      evidence: `PII terms detected: ${input.hasPii}; required secrets: ${input.compatibility.requiredSecrets.join(", ") || "none"}.`,
      recommendation:
        "Use the SG PDPA strict pack and avoid storing raw personal data in audit outputs."
    });
  }
  if (!input.auditTrail) {
    findings.push({
      severity: "info",
      category: "auditability",
      title: "Runtime audit trail not present",
      evidence: "This report is static or audit.jsonl was not found.",
      recommendation: "Run the skill and export a signed audit bundle for production evidence."
    });
  }
  if (input.failClosed) {
    findings.push({
      severity: "high",
      category: "tool-risk",
      title: "Fail-closed enforcement was triggered",
      evidence: "A hook, wrapper, or unclassified tool event caused the run to block.",
      recommendation: "Inspect wrapper-events.jsonl and policy-decisions.json before retrying."
    });
  }
  return dedupeFindings(findings);
}

function governanceFrameworkMapping(
  controls: GovernanceReportOutput["controls"],
  compatibility: SkillCompatibilityReport,
  findings: readonly GovernanceFinding[]
): readonly GovernanceFrameworkMapping[] {
  return [
    {
      controlArea: "Human accountability and approval",
      status: controls.approvalRequired ? "covered" : "partial",
      evidence: controls.approvalRequired
        ? ["Policy requires approval for at least one action."]
        : ["No approval-required action was detected for this skill/run."]
    },
    {
      controlArea: "Bounded autonomy and technical controls",
      status: controls.denied || controls.failClosed || controls.policyPack ? "covered" : "partial",
      evidence: [
        `Policy pack: ${controls.policyPack}`,
        `Sandbox profile: ${controls.sandboxProfile}`,
        `Fail-closed triggered: ${controls.failClosed}`
      ]
    },
    {
      controlArea: "Traceability and audit evidence",
      status: controls.auditTrail && controls.signedBundle ? "covered" : "partial",
      evidence: [
        `Audit trail: ${controls.auditTrail}`,
        `Hook events: ${controls.hookEvents}`,
        `Web evidence: ${controls.webEvidence}`,
        `Evidence workspace: ${controls.evidenceWorkspace}`,
        `Signed bundle: ${controls.signedBundle}`
      ]
    },
    {
      controlArea: "Data and third-party risk",
      status:
        compatibility.network === "none" &&
        compatibility.requiredSecrets.length === 0 &&
        !controls.webEvidence
          ? "covered"
          : "partial",
      evidence: [
        `Network: ${compatibility.network}`,
        `Required secrets: ${compatibility.requiredSecrets.join(", ") || "none"}`,
        `Web evidence attached: ${controls.webEvidence}`,
        `Evidence workspace attached: ${controls.evidenceWorkspace}`
      ]
    },
    {
      controlArea: "Residual risk review",
      status: findings.some((finding) => finding.severity === "high") ? "gap" : "partial",
      evidence: findings.length
        ? findings.map((finding) => `${finding.severity}: ${finding.title}`)
        : ["No governance findings were emitted."]
    }
  ];
}

function governanceResidualRisks(
  subjectKind: GovernanceSubjectKind,
  controls: GovernanceReportOutput["controls"]
): readonly string[] {
  return [
    "KelpClaw assembles evidence for governance review; it does not certify legal or regulatory compliance.",
    ...(subjectKind === "skill"
      ? [
          "Static SKILL.md analysis cannot prove runtime behavior; run the skill to collect audit evidence."
        ]
      : []),
    ...(controls.hookEvents
      ? []
      : ["Tool-level enforcement depends on agent hook or JSONL event coverage."]),
    ...(controls.signedBundle
      ? []
      : [
          "Forwardable evidence should be exported as a signed audit bundle before external review."
        ]),
    ...(controls.webEvidence
      ? [
          "Web search and browser evidence may include third-party content, crawler gaps, and provider-specific ranking bias."
        ]
      : []),
    ...(controls.evidenceWorkspace
      ? [
          "Evidence workspace custody proves local file integrity, not independent legal admissibility or external timestamping."
        ]
      : []),
    "External WORM storage, retention policy, and independent reviewer workflow remain deployment responsibilities."
  ];
}

function hasAction(
  compatibility: SkillCompatibilityReport,
  decisions: readonly PolicyDecisionRecord[],
  action: PolicyDecision["action"]
): boolean {
  return (
    compatibility.policyFindings.some((finding) => finding.action === action) ||
    decisions.some((decision) => decision.decision.action === action)
  );
}

function tier([high, moderate]: readonly [boolean, boolean]): GovernanceAutonomyTier {
  if (high) {
    return "high";
  }
  return moderate ? "moderate" : "low";
}

function maxTier(tiers: readonly GovernanceAutonomyTier[]): GovernanceAutonomyTier {
  if (tiers.includes("high")) {
    return "high";
  }
  return tiers.includes("moderate") ? "moderate" : "low";
}

function mutationPattern(): RegExp {
  return /\b(gh\s+(issue|pr|label|release)\s+(create|edit|delete|reopen|comment|merge|close)|git\s+push|curl\s+(-X\s+)?(POST|PUT|PATCH|DELETE)|rm\s+-rf|write|create|delete|update)\b/iu;
}

function jsonText(value: unknown): string {
  return stableJsonStringify(value as JsonValue);
}

function dedupeFindings(findings: readonly GovernanceFinding[]): readonly GovernanceFinding[] {
  const seen = new Set<string>();
  return findings.filter((finding) => {
    const key = `${finding.severity}:${finding.category}:${finding.title}:${finding.evidence}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function policyDecisionRecords(value: unknown): readonly PolicyDecisionRecord[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter(isPolicyDecisionRecord);
}

function isPolicyDecisionRecord(value: unknown): value is PolicyDecisionRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return (
    typeof record.tool === "string" &&
    Boolean(record.args && typeof record.args === "object" && !Array.isArray(record.args)) &&
    Boolean(
      record.decision && typeof record.decision === "object" && !Array.isArray(record.decision)
    )
  );
}

async function readJsonIfExists(path: string): Promise<unknown | undefined> {
  if (!(await fileExists(path))) {
    return undefined;
  }
  return JSON.parse(await readFile(path, "utf8")) as unknown;
}

async function isSkillSubject(subject: string): Promise<boolean> {
  return (
    subject.startsWith("github:") || /\.md$/iu.test(subject) || (await fileExists(resolve(subject)))
  );
}

async function runLiveAgent(input: {
  readonly args: readonly string[];
  readonly agent: string;
  readonly analysis: SkillAnalysis;
  readonly input: JsonRecord;
  readonly runDir: string;
  readonly policyPackName: string;
  readonly ruleset: PolicyRuleSet;
  readonly enforcePolicy: boolean;
}): Promise<AgentRunRecord> {
  const workspaceDir = join(input.runDir, "workspace");
  const artifactsDir = join(workspaceDir, "artifacts");
  const hookDir = join(input.runDir, "hooks");
  const hookEventsPath = join(input.runDir, "hook-events.jsonl");
  const hookScriptPath = join(hookDir, "kelpclaw-skill-hook.mjs");
  const stdoutPath = join(input.runDir, "stdout.log");
  const stderrPath = join(input.runDir, "stderr.log");
  const wrapperEventsPath = join(input.runDir, "wrapper-events.jsonl");
  const lastMessagePath = join(input.runDir, "last-message.md");
  const promptPath = join(workspaceDir, "prompt.md");
  const wrapper = hasFlag(input.args, "--wrapper");
  await mkdir(artifactsDir, { recursive: true });
  await mkdir(hookDir, { recursive: true });
  await writeFile(join(workspaceDir, "SKILL.md"), input.analysis.document.content, "utf8");
  await writeJson(join(workspaceDir, "input.json"), input.input);
  await writeFile(hookScriptPath, localHookScript(), "utf8");
  const hookCommand = `${process.execPath} ${JSON.stringify(hookScriptPath)}`;
  const prompt = liveAgentPrompt(input.analysis, input.input, artifactsDir, hookCommand);
  await writeFile(promptPath, prompt, "utf8");
  const command = liveAgentCommand(input.args, input.agent, workspaceDir, lastMessagePath);
  const wrapperObserver = wrapper
    ? createCodexWrapperObserver({
        analysis: input.analysis,
        ruleset: input.ruleset,
        enforcePolicy: input.enforcePolicy
      })
    : undefined;
  const startedAt = new Date().toISOString();
  const result = await runCommand(
    command,
    prompt,
    workspaceDir,
    {
      KELPCLAW_SKILL_HOOK_COMMAND: hookCommand,
      KELPCLAW_SKILL_HOOK_EVENTS: hookEventsPath,
      KELPCLAW_SKILL_HOOK_POLICY: stableJsonStringify(input.ruleset as unknown as JsonValue),
      KELPCLAW_SKILL_HOOK_ENFORCE: input.enforcePolicy ? "1" : "0",
      KELPCLAW_SKILL_HOOK_POLICY_PACK: input.policyPackName,
      KELPCLAW_SKILL_ID: input.analysis.name,
      KELPCLAW_SKILL_TAGS: input.analysis.tags.join(",")
    },
    wrapperObserver?.handleLine
  );
  const finishedAt = new Date().toISOString();
  const wrapperEvents = wrapperObserver?.events ?? [];
  if (wrapperEvents.length > 0) {
    await writeFile(
      wrapperEventsPath,
      `${wrapperEvents.map((event) => stableJsonStringify(event as unknown as JsonValue)).join("\n")}\n`,
      "utf8"
    );
  }
  let hookEvents = await readHookEvents(hookEventsPath);
  if (hookEvents.length === 0 && wrapperEvents.length > 0) {
    await writeFile(
      hookEventsPath,
      `${wrapperEvents.map((event) => stableJsonStringify(event as unknown as JsonValue)).join("\n")}\n`,
      "utf8"
    );
    hookEvents = wrapperEvents;
  }
  const observedSteps =
    hookEvents.length > 0
      ? plannedStepsFromHookEvents(hookEvents)
      : parseObservedToolSteps(result.stdout, result.stderr);
  const generatedArtifacts = await listFilesIfPresent(artifactsDir);
  const workspaceFiles = await listFilesIfPresent(workspaceDir);
  const hookBlocked = hookEvents.some(
    (event) =>
      event.hookEvent === "PreToolUse" &&
      (event.status === "denied" || event.status === "approval-required")
  );
  const wrapperBlocked = wrapperEvents.some(
    (event) => event.status === "denied" || event.status === "approval-required"
  );
  const unclassifiedBlocked = wrapperEvents.some(
    (event) =>
      event.status === "denied" &&
      event.toolName === "Unknown" &&
      event.decision.reason.includes("unclassified")
  );
  const observedDecisions = evaluatePlannedSteps(
    { ...input.analysis, plannedSteps: observedSteps },
    input.ruleset
  );
  const observedBlocked = observedDecisions.some(
    (record) => record.decision.action === "deny" || record.decision.action === "require-approval"
  );
  return {
    agent: input.agent,
    command,
    hookCommand,
    hookEventsPath,
    workspaceDir,
    artifactsDir,
    stdoutPath,
    stderrPath,
    lastMessagePath,
    wrapper,
    exitCode: result.exitCode,
    stdout: result.stdout,
    stderr: result.stderr,
    hookEvents,
    wrapperEvents,
    enforcement: {
      enabled: input.enforcePolicy,
      plannedBlocked: false,
      hookBlocked,
      wrapperBlocked,
      unclassifiedBlocked,
      observedBlocked,
      source: hookBlocked
        ? "hook-pretool"
        : unclassifiedBlocked
          ? "unclassified-event"
          : wrapperBlocked
            ? "wrapper-observed"
            : observedBlocked
              ? "observed-policy"
              : "none",
      terminatedByPolicy: result.terminatedByPolicy
    },
    observedSteps,
    generatedArtifacts,
    workspaceFiles: workspaceFiles.filter(
      (file) => !["SKILL.md", "input.json", "prompt.md"].includes(file)
    ),
    startedAt,
    finishedAt
  };
}

function liveAgentCommand(
  args: readonly string[],
  agent: string,
  workspaceDir: string,
  lastMessagePath: string
): readonly string[] {
  const customCommand = option(args, "--agent-command") ?? process.env.KELPCLAW_AGENT_COMMAND;
  const customArgs = options(args, "--agent-arg");
  if (customCommand) {
    return [customCommand, ...customArgs];
  }
  if (agent !== "codex-cli") {
    throw new Error(
      `Live runner for '${agent}' requires --agent-command. Built-in live execution currently supports codex-cli.`
    );
  }
  const codexBin = option(args, "--codex-bin") ?? process.env.KELPCLAW_CODEX_BIN ?? "codex";
  return [
    codexBin,
    "exec",
    "--json",
    "--ephemeral",
    "--sandbox",
    "workspace-write",
    "--ask-for-approval",
    "never",
    "--skip-git-repo-check",
    "-C",
    workspaceDir,
    "-o",
    lastMessagePath,
    "-"
  ];
}

function liveAgentPrompt(
  analysis: SkillAnalysis,
  input: JsonRecord,
  artifactsDir: string,
  hookCommand: string
): string {
  return `You are running a KelpClaw SKILL.md in a temporary workspace.

Rules:
- Use only the current workspace.
- Treat SKILL.md as the authoritative skill instructions.
- Read input.json for the invocation input.
- Write generated files under ${artifactsDir}.
- Before each tool action, pipe a JSON object to this hook command and do not execute the action if it exits nonzero:
  ${hookCommand}
- After each tool action, pipe a PostToolUse JSON object to the same hook command.
- If your runtime emits JSONL tool events, include tool name and args so KelpClaw can normalize them for replay and policy.
- Print or return a concise final result.

## Skill Reference
${analysis.document.ref}

## SKILL.md
${analysis.document.content}

## input.json
${stableJsonStringify(input)}
`;
}

async function runCommand(
  command: readonly string[],
  stdin: string,
  cwd: string,
  extraEnv: Readonly<Record<string, string>> = {},
  onStdoutLine?: ((line: string) => "terminate" | undefined) | undefined
): Promise<RunCommandResult> {
  const [executable, ...args] = command;
  if (!executable) {
    throw new Error("Agent command is empty.");
  }
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, {
      cwd,
      env: {
        ...process.env,
        KELPCLAW_SKILL_WORKSPACE: cwd,
        ...extraEnv
      },
      stdio: ["pipe", "pipe", "pipe"]
    });
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    let pendingStdout = "";
    let terminatedByPolicy = false;
    child.stdout.on("data", (chunk: Buffer) => {
      stdoutChunks.push(chunk);
      if (!onStdoutLine) {
        return;
      }
      pendingStdout += chunk.toString("utf8");
      const lines = pendingStdout.split(/\r?\n/u);
      pendingStdout = lines.pop() ?? "";
      for (const line of lines) {
        if (onStdoutLine(line) === "terminate" && !child.killed) {
          terminatedByPolicy = true;
          child.kill("SIGTERM");
          break;
        }
      }
    });
    child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
    child.on("error", reject);
    child.on("close", (code) => {
      if (onStdoutLine && pendingStdout.trim()) {
        if (onStdoutLine(pendingStdout) === "terminate") {
          terminatedByPolicy = true;
        }
      }
      resolve({
        exitCode: code ?? 1,
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8"),
        terminatedByPolicy
      });
    });
    child.stdin.end(stdin);
  });
}

function createCodexWrapperObserver(input: {
  readonly analysis: SkillAnalysis;
  readonly ruleset: PolicyRuleSet;
  readonly enforcePolicy: boolean;
}): {
  readonly events: readonly LocalHookEvent[];
  readonly handleLine: (line: string) => "terminate" | undefined;
} {
  const events: LocalHookEvent[] = [];
  return {
    events,
    handleLine(line: string): "terminate" | undefined {
      const observation = codexWrapperObservation(line, input, events);
      if (!observation.event) {
        return undefined;
      }
      events.push(observation.event);
      return input.enforcePolicy && observation.blocked ? "terminate" : undefined;
    }
  };
}

function codexWrapperObservation(
  line: string,
  input: {
    readonly analysis: SkillAnalysis;
    readonly ruleset: PolicyRuleSet;
    readonly enforcePolicy: boolean;
  },
  previousEvents: readonly LocalHookEvent[]
): WrapperObservation {
  const trimmed = line.trim();
  if (!trimmed || !trimmed.startsWith("{")) {
    return { blocked: false, unclassified: false };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed) as unknown;
  } catch {
    return { blocked: false, unclassified: false };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { blocked: false, unclassified: false };
  }
  const record = parsed as JsonRecord;
  const step = wrapperToolStep(record);
  if (!step) {
    if (!isToolLikeJson(record)) {
      return { blocked: false, unclassified: false };
    }
    const decision: PolicyDecision = input.enforcePolicy
      ? {
          action: "deny",
          matchedRuleIds: [],
          reason: "unclassified Codex JSONL tool event under enforced policy"
        }
      : {
          action: "log-only",
          matchedRuleIds: [],
          reason: "unclassified Codex JSONL tool event observed in advisory mode"
        };
    const event = buildLocalHookEvent({
      hookEvent: "ObservedToolUse",
      toolName: "Unknown",
      args: { raw: record },
      decision,
      previousEvents
    });
    return {
      event,
      blocked: input.enforcePolicy,
      unclassified: true
    };
  }
  const decision = evaluatePolicy(
    {
      tool: step.tool,
      args: step.args,
      skill: {
        id: input.analysis.name,
        tags: input.analysis.tags
      }
    },
    input.ruleset
  );
  const event = buildLocalHookEvent({
    hookEvent: "ObservedToolUse",
    toolName: step.tool,
    args: step.args,
    ...(step.result !== undefined ? { result: step.result } : {}),
    decision,
    previousEvents
  });
  return {
    event,
    blocked: decision.action === "deny" || decision.action === "require-approval",
    unclassified: false
  };
}

function wrapperToolStep(record: JsonRecord): PlannedToolStep | undefined {
  const direct = directToolStep(record);
  if (direct) {
    return direct;
  }
  const steps: PlannedToolStep[] = [];
  for (const entry of Object.values(record)) {
    collectToolSteps(entry, steps);
  }
  return steps[0];
}

function buildLocalHookEvent(input: {
  readonly hookEvent: string;
  readonly toolName: string;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly decision: PolicyDecision;
  readonly previousEvents: readonly LocalHookEvent[];
}): LocalHookEvent {
  const status: LocalHookEvent["status"] =
    input.decision.action === "deny"
      ? "denied"
      : input.decision.action === "require-approval"
        ? "approval-required"
        : "allowed";
  const chainIndex = input.previousEvents.length;
  const base = {
    id: `hook-event.${randomUUID()}`,
    hookEvent: input.hookEvent,
    toolName: input.toolName,
    args: input.args,
    ...(input.result !== undefined ? { result: input.result } : {}),
    decision: input.decision,
    status,
    recordedAt: new Date().toISOString()
  };
  const contentHash = hashJson(base);
  return {
    ...base,
    contentHash,
    prevEventHash: hashJson({
      chainIndex,
      previousContentHash: input.previousEvents.at(-1)?.contentHash ?? `sha256:${"0".repeat(64)}`,
      contentHash
    }),
    chainIndex
  };
}

function isToolLikeJson(record: JsonRecord): boolean {
  const type = stringFromUnknown(record.type)?.toLowerCase();
  if (
    type &&
    (type.includes("tool") ||
      type.includes("call") ||
      type.includes("exec") ||
      type === "local_shell_call" ||
      type === "shell_command")
  ) {
    return true;
  }
  return ["toolName", "tool_name", "tool", "function_call", "tool_call"].some(
    (key) => key in record
  );
}

function parseObservedToolSteps(stdout: string, stderr: string): readonly PlannedToolStep[] {
  const steps: PlannedToolStep[] = [];
  for (const line of `${stdout}\n${stderr}`.split(/\r?\n/u)) {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith("{")) {
      continue;
    }
    try {
      collectToolSteps(JSON.parse(trimmed) as unknown, steps);
    } catch {
      // Agent stdout is not guaranteed to be pure JSONL.
    }
  }
  const seen = new Set<string>();
  return steps.filter((step) => {
    const key = hashJson(step);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

async function readHookEvents(path: string): Promise<readonly LocalHookEvent[]> {
  if (!(await fileExists(path))) {
    return [];
  }
  const content = await readFile(path, "utf8");
  return content
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line) as LocalHookEvent);
}

function plannedStepsFromHookEvents(events: readonly LocalHookEvent[]): readonly PlannedToolStep[] {
  const postEvents = events.filter((event) => event.hookEvent === "PostToolUse");
  const source = postEvents.length > 0 ? postEvents : events;
  return source.map((event) => ({
    tool: event.toolName,
    args: event.args,
    ...(event.result !== undefined ? { result: event.result } : {})
  }));
}

function localHookScript(): string {
  return `import { appendFileSync, readFileSync } from "node:fs";
import { createHash, randomUUID } from "node:crypto";

const eventPath = process.env.KELPCLAW_SKILL_HOOK_EVENTS;
const ruleset = JSON.parse(process.env.KELPCLAW_SKILL_HOOK_POLICY || '{"rules":[]}');
const enforce = process.env.KELPCLAW_SKILL_HOOK_ENFORCE === "1";
const skill = {
  id: process.env.KELPCLAW_SKILL_ID,
  tags: (process.env.KELPCLAW_SKILL_TAGS || "").split(",").map((tag) => tag.trim()).filter(Boolean)
};

let input = "";
process.stdin.on("data", (chunk) => { input += chunk; });
process.stdin.on("end", () => {
  const raw = input.trim() ? JSON.parse(input) : {};
  const hookEvent = stringValue(raw.hookEvent) || stringValue(raw.hook_event_name) || "PreToolUse";
  const toolName = stringValue(raw.toolName) || stringValue(raw.tool_name) || stringValue(raw.name) || "Unknown";
  const args = objectValue(raw.args) || objectValue(raw.tool_input) || objectValue(raw.input) || {};
  const result = jsonValue(raw.result) ?? jsonValue(raw.tool_response);
  const decision = evaluatePolicy({ tool: toolName, args, skill }, ruleset);
  const status = decision.action === "deny" ? "denied" : decision.action === "require-approval" ? "approval-required" : "allowed";
  const existingEvents = readExistingEvents(eventPath);
  const chainIndex = existingEvents.length;
  const base = {
    id: "hook-event." + randomUUID(),
    hookEvent,
    toolName,
    args,
    ...(result !== undefined ? { result } : {}),
    decision,
    status,
    recordedAt: new Date().toISOString()
  };
  const contentHash = hashJson(base);
  const event = {
    ...base,
    contentHash,
    prevEventHash: hashJson({
      chainIndex,
      previousContentHash: existingEvents.at(-1)?.contentHash || "sha256:" + "0".repeat(64),
      contentHash
    }),
    chainIndex
  };
  if (eventPath) {
    appendFileSync(eventPath, stableJson(event) + "\\n", "utf8");
  }
  if (hookEvent === "PreToolUse" && status !== "allowed" && enforce) {
    console.log(JSON.stringify({
      continue: false,
      permissionDecision: "deny",
      permissionDecisionReason: decision.reason,
      decision
    }));
    process.exit(status === "approval-required" ? 3 : 2);
  }
  console.log(JSON.stringify({ continue: true, decision }));
});

function evaluatePolicy(context, ruleset) {
  const matches = (ruleset.rules || []).filter((rule) => evaluateExpression(rule.when, context));
  if (matches.length === 0) {
    return { action: "allow", matchedRuleIds: [], reason: "no policy rules matched" };
  }
  const ranks = { allow: 0, "log-only": 1, "require-approval": 2, deny: 3 };
  const selected = [...matches].sort((left, right) => (ranks[right.action] - ranks[left.action]) || left.id.localeCompare(right.id))[0];
  return {
    action: selected.action,
    matchedRuleIds: matches.map((rule) => rule.id).sort(),
    reason: "matched policy rule '" + selected.id + "'",
    ...(selected.approverRole ? { approverRole: selected.approverRole } : {})
  };
}

function readExistingEvents(path) {
  if (!path) return [];
  try {
    return readFileSync(path, "utf8").split(/\\r?\\n/u).map((line) => line.trim()).filter(Boolean).map((line) => JSON.parse(line));
  } catch {
    return [];
  }
}

function hashJson(value) {
  return "sha256:" + createHash("sha256").update(stableJson(value), "utf8").digest("hex");
}

function evaluateExpression(expression, context) {
  const trimmed = String(expression || "").trim();
  const orParts = splitOperator(trimmed, "||");
  if (orParts.length > 1) return orParts.some((part) => evaluateExpression(part, context));
  const andParts = splitOperator(trimmed, "&&");
  if (andParts.length > 1) return andParts.every((part) => evaluateExpression(part, context));
  return evaluateAtom(trimmed, context);
}

function evaluateAtom(atom, context) {
  let match = /^tool\\s*==\\s*"([^"]+)"$/u.exec(atom);
  if (match) return context.tool === match[1];
  match = /^tool\\s+startsWith\\s+"([^"]+)"$/u.exec(atom);
  if (match) return context.tool.startsWith(match[1]);
  match = /^(skill(?:\\.[a-zA-Z0-9_-]+)+)\\s+includes\\s+"([^"]+)"$/u.exec(atom);
  if (match) {
    const value = readPath(match[1], context);
    return Array.isArray(value) && value.includes(match[2]);
  }
  match = /^(args(?:\\.[a-zA-Z0-9_-]+)+)\\s*=~\\s*"([^"]+)"$/u.exec(atom);
  if (match) {
    const value = readPath(match[1], context);
    return typeof value === "string" && new RegExp(match[2], "u").test(value);
  }
  match = /^((?:args|skill)(?:\\.[a-zA-Z0-9_-]+)+)\\s*==\\s*"([^"]*)"$/u.exec(atom);
  if (match) return readPath(match[1], context) === match[2];
  return false;
}

function splitOperator(expression, operator) {
  const parts = [];
  let quoteOpen = false;
  let start = 0;
  for (let index = 0; index < expression.length; index += 1) {
    const char = expression[index];
    if (char === '"' && expression[index - 1] !== "\\\\") quoteOpen = !quoteOpen;
    if (!quoteOpen && expression.slice(index, index + operator.length) === operator) {
      parts.push(expression.slice(start, index).trim());
      start = index + operator.length;
      index += operator.length - 1;
    }
  }
  if (parts.length === 0) return [expression];
  parts.push(expression.slice(start).trim());
  return parts;
}

function readPath(path, context) {
  const [root, ...segments] = path.split(".");
  let value = context[root];
  for (const segment of segments) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    value = value[segment];
  }
  return value;
}

function stableJson(value) {
  return JSON.stringify(sortJson(value));
}

function sortJson(value) {
  if (Array.isArray(value)) return value.map(sortJson);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).filter(([, entry]) => entry !== undefined).sort(([left], [right]) => left.localeCompare(right)).map(([key, entry]) => [key, sortJson(entry)]));
  }
  return value;
}

function stringValue(value) {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function objectValue(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : undefined;
}

function jsonValue(value) {
  return isJsonValue(value) ? value : undefined;
}

function isJsonValue(value) {
  if (value === null || ["string", "number", "boolean"].includes(typeof value)) return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  if (value && typeof value === "object") return Object.values(value).every(isJsonValue);
  return false;
}
`;
}

function collectToolSteps(value: unknown, steps: PlannedToolStep[]): void {
  if (!value || typeof value !== "object") {
    return;
  }
  if (Array.isArray(value)) {
    for (const entry of value) {
      collectToolSteps(entry, steps);
    }
    return;
  }
  const record = value as Record<string, unknown>;
  const direct = directToolStep(record);
  if (direct) {
    steps.push(direct);
  }
  for (const entry of Object.values(record)) {
    collectToolSteps(entry, steps);
  }
}

function directToolStep(record: Record<string, unknown>): PlannedToolStep | undefined {
  const type = stringFromUnknown(record.type)?.toLowerCase();
  const name =
    stringFromUnknown(record.toolName) ??
    stringFromUnknown(record.tool_name) ??
    stringFromUnknown(record.name);
  if (type === "local_shell_call" || type === "shell_command") {
    return {
      tool: "Bash",
      args: { command: commandFromRecord(record) ?? "observed shell command" },
      ...(jsonValueField(record, "result") ? { result: jsonValueField(record, "result") } : {})
    };
  }
  const command = commandFromRecord(record);
  if (type && (type.includes("exec") || type.includes("shell")) && command) {
    return {
      tool: "Bash",
      args: { command },
      ...(jsonValueField(record, "result") ? { result: jsonValueField(record, "result") } : {})
    };
  }
  if (name && knownTools.includes(name as (typeof knownTools)[number])) {
    return {
      tool: name,
      args: argsFromRecord(record),
      ...(jsonValueField(record, "result") ? { result: jsonValueField(record, "result") } : {})
    };
  }
  if ((type?.includes("tool") || type?.includes("call")) && name) {
    return {
      tool: normalizeObservedToolName(name),
      args: argsFromRecord(record),
      ...(jsonValueField(record, "result") ? { result: jsonValueField(record, "result") } : {})
    };
  }
  return undefined;
}

function normalizeObservedToolName(name: string): string {
  const normalized = name.toLowerCase();
  if (normalized.includes("shell") || normalized.includes("bash") || normalized.includes("exec")) {
    return "Bash";
  }
  if (normalized.includes("read")) {
    return "Read";
  }
  if (normalized.includes("write")) {
    return "Write";
  }
  if (normalized.includes("edit")) {
    return "Edit";
  }
  return name;
}

function argsFromRecord(record: Record<string, unknown>): JsonRecord {
  for (const key of ["args", "arguments", "input", "parameters", "tool_input", "toolInput"]) {
    const value = record[key];
    if (value && typeof value === "object" && !Array.isArray(value)) {
      return value as JsonRecord;
    }
    if (typeof value === "string") {
      try {
        const parsed = JSON.parse(value) as unknown;
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
          return parsed as JsonRecord;
        }
      } catch {
        return { value };
      }
    }
  }
  const command = commandFromRecord(record);
  return command ? { command } : {};
}

function commandFromRecord(record: Record<string, unknown>): string | undefined {
  for (const key of ["command", "cmd", "script"]) {
    const value = stringFromUnknown(record[key]);
    if (value) {
      return value;
    }
  }
  const args = record.args;
  if (args && typeof args === "object" && !Array.isArray(args)) {
    return commandFromRecord(args as Record<string, unknown>);
  }
  const input = record.input;
  if (input && typeof input === "object" && !Array.isArray(input)) {
    return commandFromRecord(input as Record<string, unknown>);
  }
  return undefined;
}

function jsonValueField(record: Record<string, unknown>, key: string): JsonValue | undefined {
  const value = record[key];
  return isJsonValue(value) ? value : undefined;
}

function stringFromUnknown(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : undefined;
}

function isJsonValue(value: unknown): value is JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return true;
  }
  if (Array.isArray(value)) {
    return value.every((entry) => isJsonValue(entry));
  }
  if (typeof value === "object") {
    return Object.values(value).every((entry) => isJsonValue(entry));
  }
  return false;
}

async function analyzeSkillReference(ref: string, ruleset: PolicyRuleSet): Promise<SkillAnalysis> {
  const document = await loadSkillDocument(ref);
  const name =
    stringField(document.frontmatter, "name") ??
    firstMarkdownHeading(document.content) ??
    basename(ref).replace(/\.md$/iu, "");
  const description =
    stringField(document.frontmatter, "description") ?? firstParagraph(document.content) ?? "";
  const tags = uniqueSorted([
    ...stringArrayField(document.frontmatter, "tags"),
    ...stringArrayField(document.frontmatter, "capabilities")
  ]);
  const toolsDetected = detectTools(document);
  const requiredSecrets = detectRequiredSecrets(document);
  const network = inferNetwork(document, toolsDetected);
  const sandboxProfile = inferSandboxProfile(document.content, network);
  const plannedSteps = plannedToolSteps(document.content, toolsDetected);
  const analysis = {
    document,
    name,
    description,
    tags,
    toolsDetected,
    requiredSecrets,
    network,
    sandboxProfile,
    plannedSteps
  };
  compatibilityFromAnalysis(analysis, ruleset);
  return analysis;
}

async function loadSkillDocument(ref: string): Promise<SkillDocument> {
  const content = ref.startsWith("github:")
    ? await fetchGithubSkill(ref)
    : await readFile(resolve(ref), "utf8");
  const frontmatter = parseFrontmatter(content);
  return {
    ref,
    source: ref.startsWith("github:") ? "github" : "local",
    content,
    frontmatter,
    contentHash: hashJson({ ref, content })
  };
}

async function fetchGithubSkill(ref: string): Promise<string> {
  const path = ref.slice("github:".length);
  const [owner, repo, ...segments] = path.split("/").filter(Boolean);
  if (!owner || !repo || segments.length === 0) {
    throw new Error("GitHub skill refs must use github:owner/repo/path/SKILL.md.");
  }
  const url = `https://raw.githubusercontent.com/${owner}/${repo}/main/${segments.join("/")}`;
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Unable to fetch '${ref}' from ${url}: HTTP ${response.status}.`);
  }
  return response.text();
}

function compatibilityFromAnalysis(
  analysis: SkillAnalysis,
  ruleset: PolicyRuleSet
): SkillCompatibilityReport {
  const findings = evaluatePlannedSteps(analysis, ruleset)
    .filter(
      (record) => record.decision.action === "deny" || record.decision.action === "require-approval"
    )
    .map((record) => policyFinding(record.tool, record.decision));
  return {
    runnable: !findings.some((finding) => finding.action === "deny"),
    toolsDetected: analysis.toolsDetected,
    requiredSecrets: analysis.requiredSecrets,
    network: analysis.network,
    sandboxProfile: analysis.sandboxProfile,
    policyFindings: findings
  };
}

function evaluatePlannedSteps(
  analysis: Pick<SkillAnalysis, "plannedSteps" | "name" | "tags">,
  ruleset: PolicyRuleSet
): readonly PolicyDecisionRecord[] {
  return analysis.plannedSteps.map((step) => ({
    tool: step.tool,
    args: step.args,
    decision: evaluatePolicy(
      {
        tool: step.tool,
        args: step.args,
        skill: {
          id: analysis.name,
          tags: analysis.tags
        }
      },
      ruleset
    )
  }));
}

function policyFinding(tool: string, decision: PolicyDecision): SkillPolicyFinding {
  return {
    tool,
    action: decision.action,
    matchedRuleIds: decision.matchedRuleIds,
    reason: decision.reason,
    ...(decision.approverRole ? { approverRole: decision.approverRole } : {})
  };
}

function detectTools(document: SkillDocument): readonly string[] {
  const declared = [
    ...stringArrayField(document.frontmatter, "tools"),
    ...stringArrayField(document.frontmatter, "allowedTools"),
    ...stringArrayField(document.frontmatter, "allowed-tools")
  ];
  const content = document.content;
  const detected = new Set<string>();
  for (const tool of declared) {
    if (knownTools.includes(tool as (typeof knownTools)[number])) {
      detected.add(tool);
    }
  }
  for (const tool of knownTools) {
    if (new RegExp(`(^|[^A-Za-z0-9_])${escapeRegex(tool)}([^A-Za-z0-9_]|$)`, "u").test(content)) {
      detected.add(tool);
    }
  }
  if (/```(?:bash|sh|shell)\b/iu.test(content) || /\b(shell|terminal) command\b/iu.test(content)) {
    detected.add("Bash");
  }
  if (/\b(read|open|inspect)\s+(?:a\s+)?file\b/iu.test(content)) {
    detected.add("Read");
  }
  if (/\b(write|create)\s+(?:a\s+)?file\b/iu.test(content)) {
    detected.add("Write");
  }
  if (/\b(modify|edit)\s+(?:a\s+)?file\b/iu.test(content)) {
    detected.add("Edit");
  }
  return knownTools.filter((tool) => detected.has(tool));
}

function detectRequiredSecrets(document: SkillDocument): readonly string[] {
  const declared = [
    ...stringArrayField(document.frontmatter, "requiredSecrets"),
    ...stringArrayField(document.frontmatter, "required-secrets"),
    ...stringArrayField(document.frontmatter, "secrets")
  ];
  const envStyle =
    document.content.match(
      /\b[A-Z][A-Z0-9_]*(?:API_KEY|TOKEN|SECRET|PASSWORD|PRIVATE_KEY|ACCESS_KEY)[A-Z0-9_]*\b/gu
    ) ?? [];
  const dotted =
    document.content.match(/\b[a-z][a-z0-9_.-]*(?:token|secret|password|apiKey|api_key)\b/gu) ?? [];
  return uniqueSorted(
    [...declared, ...envStyle, ...dotted].filter((secret) => !ignoredSecretWords.has(secret))
  );
}

function inferNetwork(document: SkillDocument, toolsDetected: readonly string[]): SkillNetworkMode {
  if (
    toolsDetected.includes("WebFetch") ||
    toolsDetected.includes("WebSearch") ||
    /\b(https?:\/\/|api\.|curl\s+https?:|wget\s+https?:|git\s+clone|network|internet)\b/iu.test(
      document.content
    )
  ) {
    return "declared";
  }
  return "none";
}

function inferSandboxProfile(content: string, network: SkillNetworkMode): SkillSandboxProfile {
  if (destructiveShellPattern().test(content)) {
    return "destructive-risk";
  }
  return network === "declared" ? "networked" : "safe-local";
}

function plannedToolSteps(
  content: string,
  toolsDetected: readonly string[]
): readonly PlannedToolStep[] {
  const steps: PlannedToolStep[] = [];
  for (const tool of toolsDetected) {
    if (tool === "Bash") {
      const commands = extractBashCommands(content);
      for (const command of commands.length > 0 ? commands : ["skill-directed shell command"]) {
        steps.push({ tool, args: { command } });
      }
      continue;
    }
    if (tool === "Read") {
      steps.push({ tool, args: { filePath: "declared by skill" } });
      continue;
    }
    if (tool === "Write" || tool === "Edit" || tool === "MultiEdit") {
      steps.push({ tool, args: { filePath: "declared by skill" } });
      continue;
    }
    if (tool === "WebFetch") {
      steps.push({ tool, args: { url: firstUrl(content) ?? "declared by skill" } });
      continue;
    }
    steps.push({ tool, args: {} });
  }
  return steps;
}

function extractBashCommands(content: string): readonly string[] {
  const commands: string[] = [];
  for (const match of content.matchAll(/```(?:bash|sh|shell)\s*\n([\s\S]*?)```/giu)) {
    const body = match[1] ?? "";
    commands.push(
      ...body
        .split(/\r?\n/u)
        .map((line) => line.trim().replace(/^\$\s*/u, ""))
        .filter((line) => line.length > 0 && !line.startsWith("#"))
    );
  }
  for (const match of content.matchAll(/\$\s+([^\n`]+)/gu)) {
    commands.push(match[1]?.trim() ?? "");
  }
  for (const match of content.matchAll(/Bash\((?:command:\s*)?"([^"]+)"\)/gu)) {
    commands.push(match[1] ?? "");
  }
  const destructive = content.match(destructiveShellPattern());
  if (destructive?.[0] && !commands.some((command) => command.includes(destructive[0] ?? ""))) {
    commands.push(destructive[0]);
  }
  return uniqueSorted(commands.filter(Boolean)).slice(0, 20);
}

function destructiveShellPattern(): RegExp {
  return /\b(rm\s+-rf|sudo\s+rm|mkfs|diskutil\s+erase|git\s+reset\s+--hard|git\s+clean\s+-fd)\b/iu;
}

function parseFrontmatter(content: string): Readonly<Record<string, unknown>> {
  if (!content.startsWith("---")) {
    return {};
  }
  const end = content.indexOf("\n---", 3);
  if (end < 0) {
    return {};
  }
  const raw = content.slice(3, end);
  const output: Record<string, unknown> = {};
  let currentArrayKey: string | undefined;
  for (const rawLine of raw.split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    if (line.startsWith("- ") && currentArrayKey) {
      const current = Array.isArray(output[currentArrayKey])
        ? (output[currentArrayKey] as readonly unknown[])
        : [];
      output[currentArrayKey] = [...current, stripQuotes(line.slice(2).trim())];
      continue;
    }
    const separator = line.indexOf(":");
    if (separator < 0) {
      continue;
    }
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    currentArrayKey = undefined;
    if (!value) {
      output[key] = [];
      currentArrayKey = key;
      continue;
    }
    output[key] = parseFrontmatterValue(value);
  }
  return output;
}

function parseFrontmatterValue(value: string): unknown {
  if (value.startsWith("[") && value.endsWith("]")) {
    return value
      .slice(1, -1)
      .split(",")
      .map((entry) => stripQuotes(entry.trim()))
      .filter(Boolean);
  }
  return stripQuotes(value);
}

function stripQuotes(value: string): string {
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    return value.slice(1, -1);
  }
  return value;
}

function skillJson(analysis: SkillAnalysis): unknown {
  return {
    ref: analysis.document.ref,
    source: analysis.document.source,
    name: analysis.name,
    description: analysis.description,
    tags: analysis.tags,
    contentHash: analysis.document.contentHash,
    bytes: Buffer.byteLength(analysis.document.content, "utf8"),
    frontmatter: analysis.document.frontmatter as JsonRecord
  };
}

function workflowJson(analysis: SkillAnalysis, runId: string, createdAt: string): unknown {
  return {
    id: `workflow.skill.${shortHash(analysis.document.contentHash)}`,
    schemaVersion: "1.0.0",
    name: analysis.name,
    prompt: analysis.description || `Run ${analysis.document.ref}`,
    revision: 1,
    nodes: [
      {
        id: "skill.run",
        kind: "skill",
        label: analysis.name,
        description: analysis.description,
        inputs: { input: { type: "object", additionalProperties: true } },
        outputs: { result: { type: "object", additionalProperties: true } },
        config: {
          skillRef: analysis.document.ref,
          runId
        },
        runtime: {
          image: "kelpclaw/audit-first-skill-runner:local",
          command: ["kelp-claw", "run-skill", analysis.document.ref],
          timeoutSeconds: 300,
          retry: { maxAttempts: 1, backoffSeconds: 0 },
          environment: {},
          resources: { cpu: "1", memoryMb: 512 }
        },
        determinism: {
          externalCalls: analysis.network === "declared" ? ["declared-network"] : [],
          seededRandomness: { enabled: false },
          replayBehavior: "record"
        },
        skillId: `skill.external.${shortHash(analysis.document.contentHash)}`
      }
    ],
    edges: [],
    approval: null,
    createdAt,
    updatedAt: createdAt
  };
}

function bomJson(
  analysis: SkillAnalysis,
  runId: string,
  policyPack: string,
  generatedAt: string,
  agentRun: AgentRunRecord | undefined
): unknown {
  return {
    kelpclawBomVersion: "1.0.0",
    runId,
    generatedAt,
    skillRef: analysis.document.ref,
    skillHash: analysis.document.contentHash,
    toolsDetected: analysis.toolsDetected,
    requiredSecrets: analysis.requiredSecrets,
    network: analysis.network,
    sandboxProfile: analysis.sandboxProfile,
    policyPack,
    artifacts: auditBundleFiles,
    ...(agentRun
      ? {
          agent: agentRun.agent,
          agentCommand: agentRun.command,
          generatedArtifacts: agentRun.generatedArtifacts,
          workspaceFiles: agentRun.workspaceFiles,
          exitCode: agentRun.exitCode
        }
      : {})
  };
}

function auditJsonl(input: {
  readonly analysis: SkillAnalysis;
  readonly compatibility: SkillCompatibilityReport;
  readonly plannedDecisions: readonly PolicyDecisionRecord[];
  readonly observedDecisions: readonly PolicyDecisionRecord[];
  readonly runId: string;
  readonly createdAt: string;
  readonly status: SkillRunOutput["status"];
  readonly agentRun?: AgentRunRecord | undefined;
}): string {
  const policyDecisions =
    input.observedDecisions.length > 0 ? input.observedDecisions : input.plannedDecisions;
  const events = [
    {
      id: `${input.runId}.event.0`,
      runId: input.runId,
      timestamp: input.createdAt,
      action: "skill.loaded",
      metadata: {
        ref: input.analysis.document.ref,
        contentHash: input.analysis.document.contentHash
      }
    },
    {
      id: `${input.runId}.event.1`,
      runId: input.runId,
      timestamp: input.createdAt,
      action: "skill.compatibility.checked",
      metadata: input.compatibility
    },
    ...(input.agentRun
      ? [
          {
            id: `${input.runId}.event.2`,
            runId: input.runId,
            timestamp: input.agentRun.finishedAt,
            action: "agent.live.completed",
            metadata: input.agentRun
          }
        ]
      : []),
    ...policyDecisions.map((record, index) => ({
      id: `${input.runId}.event.${index + (input.agentRun ? 3 : 2)}`,
      runId: input.runId,
      timestamp: input.createdAt,
      action: "policy.decision",
      metadata: record
    })),
    {
      id: `${input.runId}.event.${policyDecisions.length + (input.agentRun ? 3 : 2)}`,
      runId: input.runId,
      timestamp: input.createdAt,
      action: "skill.run.completed",
      metadata: {
        status: input.status
      }
    }
  ];
  return `${events.map((event) => stableJsonStringify(event as JsonValue)).join("\n")}\n`;
}

async function auditIndexHtml(runDir: string): Promise<string> {
  const [skill, bom, compatibility, decisions] = await Promise.all([
    readFile(join(runDir, "skill.json"), "utf8"),
    readFile(join(runDir, "bom.json"), "utf8"),
    readFile(join(runDir, "compatibility.json"), "utf8").catch(() => "{}"),
    readFile(join(runDir, "policy-decisions.json"), "utf8")
  ]);
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>KelpClaw Audit Bundle</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #1f2937; }
    h1 { font-size: 24px; }
    h2 { font-size: 16px; margin-top: 24px; }
    pre { background: #f8fafc; border: 1px solid #d9e2ec; border-radius: 6px; padding: 12px; overflow: auto; }
  </style>
</head>
<body>
  <h1>KelpClaw Audit Bundle</h1>
  <h2>Skill</h2>
  <pre>${escapeHtml(skill)}</pre>
  <h2>Compatibility</h2>
  <pre>${escapeHtml(compatibility)}</pre>
  <h2>Policy Decisions</h2>
  <pre>${escapeHtml(decisions)}</pre>
  <h2>BOM</h2>
  <pre>${escapeHtml(bom)}</pre>
</body>
</html>
`;
}

async function ensureAuditSigningKey(keyDir: string): Promise<AuditKeyFile> {
  await mkdir(keyDir, { recursive: true });
  const keyPath = join(keyDir, "audit-ed25519.json");
  if (await fileExists(keyPath)) {
    const existing = JSON.parse(await readFile(keyPath, "utf8")) as AuditKeyFile;
    if (existing.algorithm !== "ed25519" || !existing.privateKeyPem || !existing.publicKeyPem) {
      throw new Error(`${keyPath} is not a valid KelpClaw Ed25519 audit key.`);
    }
    return existing;
  }
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" }
  });
  const key: AuditKeyFile = {
    schemaVersion: "1.0.0",
    algorithm: "ed25519",
    keyId: `sha256:${createHash("sha256").update(publicKey, "utf8").digest("hex")}`,
    publicKeyPem: publicKey,
    privateKeyPem: privateKey
  };
  await writeJson(keyPath, key);
  return key;
}

async function signAuditBundle(input: {
  readonly bundleDir: string;
  readonly runId: string;
  readonly files: readonly string[];
  readonly key: AuditKeyFile;
}): Promise<string> {
  const manifest: AuditBundleManifest = {
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
  return "manifest.json";
}

async function writeAuditAttestation(input: {
  readonly bundleDir: string;
  readonly runId: string;
  readonly files: readonly string[];
  readonly key: AuditKeyFile;
}): Promise<readonly string[]> {
  const [skill, policy, result] = await Promise.all([
    readJsonIfExists(join(input.bundleDir, "skill.json")) as Promise<JsonRecord | undefined>,
    readJsonIfExists(join(input.bundleDir, "policy-decisions.json")) as Promise<
      JsonRecord | undefined
    >,
    readJsonIfExists(join(input.bundleDir, "result.json")) as Promise<JsonRecord | undefined>
  ]);
  const attestation: AuditBundleAttestation = {
    schemaVersion: "1.0.0",
    runId: input.runId,
    generatedAt: new Date().toISOString(),
    ...(stringField(skill ?? {}, "contentHash")
      ? { skillHash: stringField(skill ?? {}, "contentHash") }
      : {}),
    ...(stringField(policy ?? {}, "policyPack") || stringField(result ?? {}, "policyPack")
      ? {
          policyPack:
            stringField(policy ?? {}, "policyPack") ?? stringField(result ?? {}, "policyPack")
        }
      : {}),
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
      governanceReport: input.files.includes("governance-report.json"),
      controls: input.files.includes("controls.md"),
      sarif: input.files.includes("findings.sarif"),
      webEvidence: input.files.includes("web-evidence.json"),
      evidenceWorkspace: input.files.includes("evidence-summary.json"),
      hookEvents: input.files.includes("hook-events.jsonl"),
      agentRun: input.files.includes("agent-run.json")
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
  return ["attestation.json", "attestation.sig"];
}

async function verifyAuditAttestation(input: {
  readonly bundleDir: string;
  readonly manifest: Partial<AuditBundleManifest>;
  readonly publicKeyPem?: string | undefined;
}): Promise<{
  readonly valid: boolean;
  readonly signed: boolean;
  readonly manifestHash?: string | undefined;
  readonly referencedFiles: readonly string[];
  readonly failures: readonly string[];
}> {
  const failures: string[] = [];
  let attestation: Partial<AuditBundleAttestation>;
  let signatureValid = false;
  try {
    attestation = JSON.parse(
      await readFile(join(input.bundleDir, "attestation.json"), "utf8")
    ) as Partial<AuditBundleAttestation>;
  } catch (error) {
    return {
      valid: false,
      signed: false,
      referencedFiles: [],
      failures: [`unable to read attestation.json: ${errorMessage(error)}`]
    };
  }

  try {
    if (!input.publicKeyPem) {
      failures.push("cannot verify attestation without manifest public key.");
    } else {
      const signature = Buffer.from(
        (await readFile(join(input.bundleDir, "attestation.sig"), "utf8")).trim(),
        "base64"
      );
      signatureValid = verifyBytes(
        null,
        Buffer.from(stableJsonStringify(attestation as JsonValue), "utf8"),
        createPublicKey(input.publicKeyPem),
        signature
      );
      if (!signatureValid) {
        failures.push("attestation signature is invalid.");
      }
    }
  } catch (error) {
    failures.push(`unable to verify attestation signature: ${errorMessage(error)}`);
  }

  const manifestHash = await sha256File(join(input.bundleDir, "manifest.json")).catch((error) => {
    failures.push(`unable to hash manifest.json: ${errorMessage(error)}`);
    return undefined;
  });
  if (attestation.schemaVersion !== "1.0.0") {
    failures.push("attestation schemaVersion must be 1.0.0.");
  }
  if (attestation.runId !== input.manifest.runId) {
    failures.push("attestation runId does not match manifest runId.");
  }
  if (attestation.manifest?.path !== "manifest.json") {
    failures.push("attestation manifest path must be manifest.json.");
  }
  if (manifestHash && attestation.manifest?.sha256 !== manifestHash) {
    failures.push("attestation manifest hash does not match manifest.json.");
  }
  if (attestation.manifest?.signaturePath !== "manifest.sig") {
    failures.push("attestation signaturePath must be manifest.sig.");
  }
  if (attestation.manifest?.publicKeyPath !== "manifest.pub.json") {
    failures.push("attestation publicKeyPath must be manifest.pub.json.");
  }
  const referencedFiles = Array.isArray(attestation.files)
    ? attestation.files.filter((file): file is string => typeof file === "string")
    : [];
  const manifestFiles = (input.manifest.files ?? [])
    .map((file) => file.path)
    .sort((left, right) => left.localeCompare(right));
  const sortedReferencedFiles = referencedFiles
    .slice()
    .sort((left, right) => left.localeCompare(right));
  if (stableJsonStringify(sortedReferencedFiles) !== stableJsonStringify(manifestFiles)) {
    failures.push("attestation files do not exactly match manifest files.");
  }
  for (const file of referencedFiles) {
    if (!isSafeBundlePath(file)) {
      failures.push(`unsafe bundle path in attestation: ${file}`);
      continue;
    }
    if (!(await fileExists(join(input.bundleDir, file)))) {
      failures.push(`attestation references missing file: ${file}`);
    }
  }
  return {
    valid: signatureValid && failures.length === 0,
    signed: signatureValid,
    ...(manifestHash ? { manifestHash: `sha256:${manifestHash}` } : {}),
    referencedFiles: sortedReferencedFiles,
    failures
  };
}

async function auditManifestFile(
  bundleDir: string,
  file: string
): Promise<AuditBundleManifestFile> {
  const absolute = join(bundleDir, file);
  const [fileStat, content] = await Promise.all([stat(absolute), readFile(absolute)]);
  return {
    path: file,
    size: fileStat.size,
    sha256: createHash("sha256").update(content).digest("hex")
  };
}

async function sha256File(path: string): Promise<string> {
  return createHash("sha256")
    .update(await readFile(path))
    .digest("hex");
}

function isSafeBundlePath(path: string): boolean {
  return !path.startsWith("/") && !path.split(/[\\/]/u).includes("..") && path.trim().length > 0;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function recordedReplaySummary(
  agent: string,
  steps: readonly PlannedToolStep[],
  decisions: readonly PolicyDecisionRecord[],
  result: SkillRunInternalResult
): AgentReplaySummary {
  return {
    ...replaySummaryForAgent(agent, steps, decisions),
    runId: result.runId,
    runDir: result.runDir,
    ...(result.agentRun ? { exitCode: result.agentRun.exitCode } : {})
  };
}

function replaySummaryForAgent(
  agent: string,
  steps: readonly PlannedToolStep[],
  decisions: readonly PolicyDecisionRecord[]
): AgentReplaySummary {
  const normalizedHashes = steps.map((step, index) =>
    hashJson({
      tool: step.tool,
      args: step.args,
      policy: decisions[index]?.decision ?? null
    })
  );
  const outputHashes = steps.map((step, index) =>
    hashJson({
      tool: step.tool,
      index,
      output: syntheticOutput(step)
    })
  );
  return {
    agent,
    tools: steps.map((step) => step.tool),
    normalizedHashes,
    outputHashes,
    policyDecisionActions: decisions.map((record) => record.decision.action)
  };
}

function replayDifferences(runs: readonly AgentReplaySummary[]): readonly string[] {
  if (runs.length <= 1) {
    return [];
  }
  const first = runs[0];
  if (!first) {
    return [];
  }
  const differences: string[] = [];
  for (const run of runs.slice(1)) {
    if (run.tools.join("\n") !== first.tools.join("\n")) {
      differences.push(`${run.agent} has a different tool sequence.`);
    }
    if (run.normalizedHashes.join("\n") !== first.normalizedHashes.join("\n")) {
      differences.push(`${run.agent} has different normalized step hashes.`);
    }
    if (run.outputHashes.join("\n") !== first.outputHashes.join("\n")) {
      differences.push(`${run.agent} has different output hashes.`);
    }
    if (run.policyDecisionActions.join("\n") !== first.policyDecisionActions.join("\n")) {
      differences.push(`${run.agent} has different policy decisions.`);
    }
  }
  return differences;
}

function syntheticOutput(step: PlannedToolStep): JsonRecord {
  if (step.result !== undefined) {
    return { observed: step.result };
  }
  if (step.tool === "Bash") {
    return { exitCode: 0, stdout: "", stderr: "" };
  }
  if (step.tool === "Read") {
    return { contentHash: hashJson(step.args) };
  }
  return { ok: true };
}

function writeJson(path: string, value: unknown): Promise<void> {
  return writeFile(path, `${stableJsonStringify(value as JsonValue)}\n`, "utf8");
}

async function writeJsonWithParents(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeJson(path, value);
}

async function writeTextWithParents(path: string, value: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, value, "utf8");
}

async function fileExists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

async function listFilesIfPresent(rootDir: string): Promise<readonly string[]> {
  if (!(await fileExists(rootDir))) {
    return [];
  }
  return listFiles(rootDir, rootDir);
}

async function listFiles(rootDir: string, currentDir: string): Promise<readonly string[]> {
  const entries = await readdir(currentDir, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    const absolute = join(currentDir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listFiles(rootDir, absolute)));
    } else if (entry.isFile()) {
      files.push(relative(rootDir, absolute));
    }
  }
  return files.sort((left, right) => left.localeCompare(right));
}

async function childDirectories(rootDir: string): Promise<readonly string[]> {
  if (!(await fileExists(rootDir))) {
    return [];
  }
  const entries = await readdir(rootDir, { withFileTypes: true });
  return entries
    .filter((entry) => entry.isDirectory())
    .map((entry) => join(rootDir, entry.name))
    .sort((left, right) => left.localeCompare(right));
}

async function scanFiles(
  rootDir: string,
  predicate: (file: string) => boolean
): Promise<readonly string[]> {
  if (!(await fileExists(rootDir))) {
    return [];
  }
  const files: string[] = [];
  await scanFilesInto(rootDir, files, predicate);
  return files.sort((left, right) => left.localeCompare(right));
}

async function scanFilesInto(
  currentDir: string,
  files: string[],
  predicate: (file: string) => boolean
): Promise<void> {
  const entries = await readdir(currentDir, { withFileTypes: true }).catch(() => []);
  for (const entry of entries) {
    if (entry.isDirectory()) {
      if (!inventoryExcludedDirs.has(entry.name)) {
        await scanFilesInto(join(currentDir, entry.name), files, predicate);
      }
      continue;
    }
    if (entry.isFile()) {
      const file = join(currentDir, entry.name);
      if (predicate(file)) {
        files.push(file);
      }
    }
  }
}

const inventoryExcludedDirs = new Set([
  ".git",
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".next",
  ".turbo",
  ".pnpm-store"
]);

function inventoryFormat(args: readonly string[]): "markdown" | "mermaid" {
  const format = option(args, "--format") ?? "markdown";
  if (format !== "markdown" && format !== "mermaid") {
    throw new Error("Inventory graph --format must be markdown or mermaid.");
  }
  return format;
}

function coverageFormat(args: readonly string[]): "json" | "markdown" {
  const format = option(args, "--format") ?? "json";
  if (format !== "json" && format !== "markdown") {
    throw new Error("Inventory coverage --format must be json or markdown.");
  }
  return format;
}

function inventoryGraphMarkdown(inventory: InventoryScanOutput): string {
  const rows = inventory.permissionEdges
    .map(
      (edge) =>
        `| ${markdownCell(edge.source)} | ${markdownCell(edge.kind)} | ${markdownCell(edge.target)} |`
    )
    .join("\n");
  return `# KelpClaw Permission Graph

Root: ${inventory.root}

Policy: ${inventory.policyPack}

| Source | Relationship | Target |
| --- | --- | --- |
${rows || "| none | none | none |"}
`;
}

function inventoryMermaid(inventory: InventoryScanOutput): string {
  const lines = ["flowchart LR"];
  for (const edge of inventory.permissionEdges) {
    lines.push(
      `  ${mermaidNodeId(edge.source)}["${escapeMermaid(edge.source)}"] -->|${escapeMermaid(edge.kind)}| ${mermaidNodeId(edge.target)}["${escapeMermaid(edge.target)}"]`
    );
  }
  if (lines.length === 1) {
    lines.push('  empty["No permission edges found"]');
  }
  return `${lines.join("\n")}\n`;
}

function inventoryCoverageMarkdown(inventory: InventoryScanOutput): string {
  const summary = coverageSummary(inventory.coverageFindings);
  const rows = inventory.coverageFindings
    .map(
      (finding) =>
        `| ${finding.severity} | ${finding.category} | ${markdownCell(finding.title)} | ${markdownCell(finding.evidence)} | ${markdownCell(finding.recommendation)} |`
    )
    .join("\n");
  return `# KelpClaw Inventory Coverage

Root: ${inventory.root}

Policy: ${inventory.policyPack}

Summary: ${summary.high} high, ${summary.moderate} moderate, ${summary.info} info

| Severity | Category | Finding | Evidence | Recommendation |
| --- | --- | --- | --- | --- |
${rows || "| info | coverage | No findings | Inventory coverage is complete for scanned evidence. | Keep evidence current. |"}
`;
}

function coverageSummary(findings: readonly InventoryCoverageFinding[]): {
  readonly high: number;
  readonly moderate: number;
  readonly info: number;
} {
  return {
    high: findings.filter((finding) => finding.severity === "high").length,
    moderate: findings.filter((finding) => finding.severity === "moderate").length,
    info: findings.filter((finding) => finding.severity === "info").length
  };
}

function coverageShouldFail(
  findings: readonly InventoryCoverageFinding[],
  failOn: string
): boolean {
  if (failOn === "none") {
    return false;
  }
  if (failOn === "high") {
    return findings.some((finding) => finding.severity === "high");
  }
  if (failOn === "moderate") {
    return findings.some(
      (finding) => finding.severity === "high" || finding.severity === "moderate"
    );
  }
  throw new Error("Inventory coverage --fail-on must be high, moderate, or none.");
}

function dedupeInventoryEdges(
  edges: readonly InventoryPermissionEdge[]
): readonly InventoryPermissionEdge[] {
  const seen = new Set<string>();
  return edges
    .filter((edge) => {
      const key = `${edge.source}\0${edge.kind}\0${edge.target}`;
      if (seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    })
    .sort(
      (left, right) =>
        left.source.localeCompare(right.source) ||
        left.kind.localeCompare(right.kind) ||
        left.target.localeCompare(right.target)
    );
}

function dedupeInventoryFindings(
  findings: readonly InventoryCoverageFinding[]
): readonly InventoryCoverageFinding[] {
  const seen = new Set<string>();
  return findings
    .filter((finding) => {
      const key = `${finding.severity}\0${finding.category}\0${finding.title}\0${finding.evidence}`;
      if (seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    })
    .sort(
      (left, right) =>
        severityRank(right.severity) - severityRank(left.severity) ||
        left.category.localeCompare(right.category) ||
        left.title.localeCompare(right.title)
    );
}

function severityRank(severity: InventoryCoverageSeverity): number {
  return severity === "high" ? 3 : severity === "moderate" ? 2 : 1;
}

function resolvePath(root: string, value: string): string {
  return value.startsWith("/") ? value : resolve(root, value);
}

function relativePath(root: string, path: string): string {
  const relativePathValue = relative(root, path);
  return relativePathValue && !relativePathValue.startsWith("..") ? relativePathValue : path;
}

function likelyTextFile(path: string): boolean {
  return /\.(?:md|mdx|ya?ml|json|toml|sh|bash|zsh|ts|tsx|js|mjs|cjs)$/iu.test(path);
}

function policyFromCommand(command: string): string | undefined {
  return /--policy\s+([^\s]+)/u.exec(command)?.[1];
}

function mermaidNodeId(value: string): string {
  return `n${createHash("sha256").update(value).digest("hex").slice(0, 12)}`;
}

function escapeMermaid(value: string): string {
  return value.replace(/"/gu, '\\"');
}

function jsonRecord(value: unknown, label: string): JsonRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must contain a JSON object.`);
  }
  return value as JsonRecord;
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  const value = index >= 0 ? args[index + 1] : undefined;
  return value && !value.startsWith("--") ? value : undefined;
}

function options(args: readonly string[], name: string): readonly string[] {
  const values: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === name) {
      const value = args[index + 1];
      if (value && !value.startsWith("--")) {
        values.push(value);
      }
    }
  }
  return values;
}

function stripOption(args: readonly string[], name: string): readonly string[] {
  const stripped: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] !== name) {
      stripped.push(args[index] ?? "");
      continue;
    }
    const value = args[index + 1];
    if (value && !value.startsWith("--")) {
      index += 1;
    }
  }
  return stripped;
}

function hasFlag(args: readonly string[], name: string): boolean {
  return args.includes(name);
}

function forwardedAgentArgs(args: readonly string[]): readonly string[] {
  const forwarded: string[] = [];
  if (hasFlag(args, "--wrapper")) {
    forwarded.push("--wrapper");
  }
  if (hasFlag(args, "--enforce-policy")) {
    forwarded.push("--enforce-policy");
  }
  const agentCommand = option(args, "--agent-command");
  if (agentCommand) {
    forwarded.push("--agent-command", agentCommand);
  }
  for (const agentArg of options(args, "--agent-arg")) {
    forwarded.push("--agent-arg", agentArg);
  }
  const codexBin = option(args, "--codex-bin");
  if (codexBin) {
    forwarded.push("--codex-bin", codexBin);
  }
  return forwarded;
}

function requiredOption(args: readonly string[], name: string): string {
  const value = option(args, name);
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

function requiredPositional(args: readonly string[], index: number): string {
  const value = args.filter((arg) => !arg.startsWith("--"))[index];
  if (!value) {
    throw new Error(`Missing positional argument ${index + 1}.`);
  }
  return value;
}

function firstMarkdownHeading(content: string): string | undefined {
  return content
    .split(/\r?\n/u)
    .map((line) => /^#\s+(.+)$/u.exec(line)?.[1]?.trim())
    .find((line): line is string => Boolean(line));
}

function firstParagraph(content: string): string | undefined {
  return content
    .replace(/^---[\s\S]*?\n---/u, "")
    .split(/\n\s*\n/u)
    .map((part) => part.replace(/^#+\s+/u, "").trim())
    .find((part) => part.length > 0);
}

function stringField(object: Readonly<Record<string, unknown>>, key: string): string | undefined {
  const value = object[key];
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : undefined;
}

function stringArrayField(
  object: Readonly<Record<string, unknown>>,
  key: string
): readonly string[] {
  const value = object[key];
  if (Array.isArray(value)) {
    return value.filter((entry): entry is string => typeof entry === "string" && entry.length > 0);
  }
  if (typeof value === "string" && value.trim().length > 0) {
    return value
      .split(",")
      .map((entry) => stripQuotes(entry.trim()))
      .filter(Boolean);
  }
  return [];
}

function firstUrl(content: string): string | undefined {
  return /https?:\/\/[^\s)]+/iu.exec(content)?.[0];
}

function hostnameFromUrl(value: string): string | undefined {
  try {
    return new URL(value).hostname;
  } catch {
    return undefined;
  }
}

function uniqueSorted(values: readonly string[]): readonly string[] {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right));
}

function uniqueCount(values: readonly string[]): number {
  return new Set(values).size;
}

function hashJson(value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(stableJsonStringify(value as JsonValue), "utf8")
    .digest("hex")}`;
}

function shortHash(hash: string): string {
  return hash.replace(/^sha256:/u, "").slice(0, 12);
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/gu, "&amp;")
    .replace(/</gu, "&lt;")
    .replace(/>/gu, "&gt;")
    .replace(/"/gu, "&quot;");
}

const ignoredSecretWords = new Set(["requiredSecrets", "required-secrets"]);
