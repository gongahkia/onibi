import { createHash, randomUUID } from "node:crypto";
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
}

export interface AuditBundleOutput {
  readonly ok: true;
  readonly runId: string;
  readonly bundleDir: string;
  readonly files: readonly string[];
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

interface AgentRunRecord {
  readonly agent: string;
  readonly command: readonly string[];
  readonly workspaceDir: string;
  readonly artifactsDir: string;
  readonly stdoutPath: string;
  readonly stderrPath: string;
  readonly lastMessagePath?: string | undefined;
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly observedSteps: readonly PlannedToolStep[];
  readonly generatedArtifacts: readonly string[];
  readonly workspaceFiles: readonly string[];
  readonly startedAt: string;
  readonly finishedAt: string;
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
    ...(result.agent ? { agent: result.agent } : {})
  };
}

async function runSkillInternal(args: readonly string[]): Promise<SkillRunInternalResult> {
  const skillRef = requiredPositional(args, 0);
  const inputPath = requiredOption(args, "--input");
  const policyPackName = option(args, "--policy") ?? "baseline";
  const agent = option(args, "--agent");
  const runId = option(args, "--run-id") ?? `skill-run.${Date.now()}.${randomUUID()}`;
  const runsRoot = resolve(option(args, "--runs-dir") ?? ".kelpclaw/runs");
  const runDir = join(runsRoot, runId);
  const policyPack = requirePolicyPack(policyPackName);
  const input = jsonRecord(JSON.parse(await readFile(inputPath, "utf8")) as unknown, inputPath);
  const analysis = await analyzeSkillReference(skillRef, policyPack.ruleset);
  const compatibility = compatibilityFromAnalysis(analysis, policyPack.ruleset);
  const plannedDecisions = evaluatePlannedSteps(analysis, policyPack.ruleset);
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
      runDir
    });
    observedDecisions = evaluatePlannedSteps(
      { ...analysis, plannedSteps: agentRun.observedSteps },
      policyPack.ruleset
    );
    const observedBlocked = observedDecisions.some((record) => record.decision.action === "deny");
    status = observedBlocked ? "blocked" : agentRun.exitCode === 0 ? "succeeded" : "failed";
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

  return {
    ok: true,
    runId,
    bundleDir,
    files: copied
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

async function runLiveAgent(input: {
  readonly args: readonly string[];
  readonly agent: string;
  readonly analysis: SkillAnalysis;
  readonly input: JsonRecord;
  readonly runDir: string;
}): Promise<AgentRunRecord> {
  const workspaceDir = join(input.runDir, "workspace");
  const artifactsDir = join(workspaceDir, "artifacts");
  const stdoutPath = join(input.runDir, "stdout.log");
  const stderrPath = join(input.runDir, "stderr.log");
  const lastMessagePath = join(input.runDir, "last-message.md");
  const promptPath = join(workspaceDir, "prompt.md");
  await mkdir(artifactsDir, { recursive: true });
  await writeFile(join(workspaceDir, "SKILL.md"), input.analysis.document.content, "utf8");
  await writeJson(join(workspaceDir, "input.json"), input.input);
  const prompt = liveAgentPrompt(input.analysis, input.input, artifactsDir);
  await writeFile(promptPath, prompt, "utf8");
  const command = liveAgentCommand(input.args, input.agent, workspaceDir, lastMessagePath);
  const startedAt = new Date().toISOString();
  const result = await runCommand(command, prompt, workspaceDir);
  const finishedAt = new Date().toISOString();
  const observedSteps = parseObservedToolSteps(result.stdout, result.stderr);
  const generatedArtifacts = await listFilesIfPresent(artifactsDir);
  const workspaceFiles = await listFilesIfPresent(workspaceDir);
  return {
    agent: input.agent,
    command,
    workspaceDir,
    artifactsDir,
    stdoutPath,
    stderrPath,
    lastMessagePath,
    exitCode: result.exitCode,
    stdout: result.stdout,
    stderr: result.stderr,
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

function liveAgentPrompt(analysis: SkillAnalysis, input: JsonRecord, artifactsDir: string): string {
  return `You are running a KelpClaw SKILL.md in a temporary workspace.

Rules:
- Use only the current workspace.
- Treat SKILL.md as the authoritative skill instructions.
- Read input.json for the invocation input.
- Write generated files under ${artifactsDir}.
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
  cwd: string
): Promise<{ readonly exitCode: number; readonly stdout: string; readonly stderr: string }> {
  const [executable, ...args] = command;
  if (!executable) {
    throw new Error("Agent command is empty.");
  }
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, {
      cwd,
      env: {
        ...process.env,
        KELPCLAW_SKILL_WORKSPACE: cwd
      },
      stdio: ["pipe", "pipe", "pipe"]
    });
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    child.stdout.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
    child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
    child.on("error", reject);
    child.on("close", (code) =>
      resolve({
        exitCode: code ?? 1,
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8")
      })
    );
    child.stdin.end(stdin);
  });
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
  const type = stringFromUnknown(record.type);
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
  if (name && knownTools.includes(name as (typeof knownTools)[number])) {
    return {
      tool: name,
      args: argsFromRecord(record),
      ...(jsonValueField(record, "result") ? { result: jsonValueField(record, "result") } : {})
    };
  }
  if (type?.includes("tool") && name) {
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
  for (const key of ["args", "arguments", "input", "parameters"]) {
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
