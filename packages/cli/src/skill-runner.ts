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
import { basename, join, relative, resolve } from "node:path";
import {
  evaluatePolicy,
  policyPackToYaml,
  requirePolicyPack,
  type PolicyDecision,
  type PolicyRuleSet
} from "@kelpclaw/policy";
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
  readonly signature: {
    readonly valid: boolean;
    readonly keyId?: string | undefined;
    readonly algorithm?: string | undefined;
  };
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
  };
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
  if (hasFlag(args, "--include-governance")) {
    const governance = await governanceReportForRun({
      runId,
      runsRoot,
      region: option(args, "--region") ?? "sg",
      framework: option(args, "--framework") ?? "agentic-ai"
    });
    const governanceFiles = await writeGovernanceReportFiles(bundleDir, governance);
    copied.push(...governanceFiles);
  }
  const signed = !hasFlag(args, "--no-sign");
  let manifest: string | undefined;
  if (signed) {
    const key = await ensureAuditSigningKey(resolve(option(args, "--key-dir") ?? ".kelpclaw/keys"));
    manifest = await signAuditBundle({
      bundleDir,
      runId,
      files: copied,
      key
    });
    copied.push("manifest.json", "manifest.sig", "manifest.pub.json");
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
  const failures: string[] = [];
  let manifest: Partial<AuditBundleManifest>;
  let signatureValid = false;
  try {
    manifest = JSON.parse(
      await readFile(join(bundleDir, "manifest.json"), "utf8")
    ) as Partial<AuditBundleManifest>;
  } catch (error) {
    process.exitCode = 1;
    return {
      ok: false,
      bundleDir,
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
  const ok = signatureValid && failures.length === 0;
  if (!ok) {
    process.exitCode = 1;
  }
  return {
    ok,
    bundleDir,
    ...(manifest.runId ? { runId: manifest.runId } : {}),
    signature: {
      valid: signatureValid,
      ...(manifest.publicKeyId ? { keyId: manifest.publicKeyId } : {}),
      ...(manifest.algorithm ? { algorithm: manifest.algorithm } : {})
    },
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

export async function governanceReport(args: readonly string[]): Promise<GovernanceReportOutput> {
  const subject = requiredPositional(args, 0);
  const region = option(args, "--region") ?? "sg";
  const framework = option(args, "--framework") ?? "agentic-ai";
  const report = (await isSkillSubject(subject))
    ? await governanceReportForSkill({
        skillRef: subject,
        policyPackName: option(args, "--policy") ?? "baseline",
        region,
        framework
      })
    : await governanceReportForRun({
        runId: subject,
        runsRoot: resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs"),
        region,
        framework,
        bundleDir: option(args, "--bundle-dir")
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

async function governanceReportForSkill(input: {
  readonly skillRef: string;
  readonly policyPackName: string;
  readonly region: string;
  readonly framework: string;
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
    sourceText: analysis.document.content
  });
}

async function governanceReportForRun(input: {
  readonly runId: string;
  readonly runsRoot: string;
  readonly region: string;
  readonly framework: string;
  readonly bundleDir?: string | undefined;
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
    signedBundle: await fileExists(join(bundleDir, "manifest.json")),
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
    )
  });
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
    networkRisk: tier([hasNetwork && hasSecrets, hasNetwork]),
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
    failClosed: input.failClosed
  };
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
    failClosed: input.failClosed
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
    findings,
    frameworkMapping: governanceFrameworkMapping(controls, input.compatibility, findings),
    residualRisks: governanceResidualRisks(input.subject.kind, controls)
  };
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
        `Signed bundle: ${controls.signedBundle}`
      ]
    },
    {
      controlArea: "Data and third-party risk",
      status:
        compatibility.network === "none" && compatibility.requiredSecrets.length === 0
          ? "covered"
          : "partial",
      evidence: [
        `Network: ${compatibility.network}`,
        `Required secrets: ${compatibility.requiredSecrets.join(", ") || "none"}`
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
