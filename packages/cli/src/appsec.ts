import { spawn } from "node:child_process";
import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign as signBytes
} from "node:crypto";
import {
  copyFile,
  mkdir,
  readFile,
  readdir,
  stat,
  writeFile
} from "node:fs/promises";
import { basename, dirname, join, relative, resolve } from "node:path";
import {
  createEvidenceWorkspace,
  importBurpEvidence,
  importNessusEvidence,
  importNmapEvidence,
  importNucleiEvidence,
  importSarifEvidence,
  importZapEvidence,
  loadEvidenceWorkspace,
  signEvidenceWorkspace,
  type EvidenceImportResult,
  type NormalizedEvidenceFinding
} from "@kelpclaw/evidence";
import { evaluatePolicy, requirePolicyPack, type PolicyDecision } from "@kelpclaw/policy";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";

type AppsecStatus = "succeeded" | "failed" | "blocked";

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
}

export async function runAppsecCommand(args: readonly string[]): Promise<void> {
  const [command, ...commandArgs] = args;
  if (command === "audit") {
    printJson(await appsecAudit(commandArgs));
    return;
  }
  throw new Error("Usage: kelp-claw appsec audit --context DIR --dockerfile Dockerfile --agent-command CMD");
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
  const dockerBuildCommand = [
    dockerBin,
    "build",
    "-f",
    dockerfile,
    "-t",
    imageTag,
    contextDir
  ];
  const policyDecisions: AppsecPolicyRecord[] = [
    policyRecord("docker-build", "Bash", { command: dockerBuildCommand.join(" ") }, policyPack.ruleset)
  ];
  const buildDenied = policyDecisions.some((record) => record.decision.action === "deny");
  const build = buildDenied || hasFlag(args, "--skip-docker-build")
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

  const agentCommand = option(args, "--agent-command");
  const agentDenied = agentCommand
    ? policyRecord("agent-command", "Bash", { command: agentCommand }, policyPack.ruleset)
    : undefined;
  if (agentDenied) {
    policyDecisions.push(agentDenied);
  }
  const agentBlocked = agentDenied?.decision.action === "deny";
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

  const triage = await readTriageOutput(triageOutputPath, agentCommand !== undefined);
  const status: AppsecStatus =
    buildDenied || agentBlocked
      ? "blocked"
      : build.exitCode !== 0 || agent.exitCode !== 0 || !triage.ok
        ? "failed"
        : "succeeded";
  const appsecRun = {
    schemaVersion: "kelpclaw.appsec.run.v1",
    runId,
    status,
    ok: status === "succeeded",
    outDir,
    policyPack: policyPack.name,
    target: triageInput.target,
    docker: {
      built: !hasFlag(args, "--skip-docker-build") && !buildDenied,
      exitCode: build.exitCode,
      command: build.command,
      ...(imageId ? { imageId } : {})
    },
    agent: {
      ran: agentCommand !== undefined && !agentBlocked,
      exitCode: agent.exitCode,
      command: agent.command
    },
    scannerImports: imports,
    evidence: {
      workspace: evidenceWorkspace,
      importedFindings: evidenceState.findings.findings.length,
      manifestPath: evidenceSignature.manifestPath,
      signaturePath: evidenceSignature.signaturePath
    },
    triage: triage.ok ? triage.output : { error: triage.error }
  };
  await writeJson(join(outDir, "appsec-run.json"), appsecRun);
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
    policyPack: policyPack.name
  });
  const sarif = appsecSarif({
    runId,
    evidenceFindings: evidenceState.findings.findings,
    triage: triage.ok ? triage.output : undefined
  });
  await writeJson(join(outDir, "findings.sarif"), sarif);
  await writeAuditBundle({
    outDir,
    bundleDir,
    runId,
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
      built: !hasFlag(args, "--skip-docker-build") && !buildDenied,
      exitCode: build.exitCode,
      imageTag,
      ...(imageId ? { imageId } : {})
    },
    agent: {
      ran: agentCommand !== undefined && !agentBlocked,
      exitCode: agent.exitCode
    }
  };
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
  return {
    id: finding.id,
    title: finding.title,
    severity: finding.severity,
    confidence: finding.confidence,
    status: finding.status,
    weaknessIds: finding.weaknessIds,
    sourceReferences: finding.sourceReferences.map((source) => ({
      tool: source.tool,
      rawPath: source.rawPath,
      locator: source.locator
    }))
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

function appsecSarif(input: {
  readonly runId: string;
  readonly evidenceFindings: readonly NormalizedEvidenceFinding[];
  readonly triage?: AppsecTriageOutput | undefined;
}): JsonRecord {
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
      weaknessIds: finding.weaknessIds
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
      evidenceIds: finding.evidenceIds,
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
  };
}

async function writeAuditBundle(input: {
  readonly outDir: string;
  readonly bundleDir: string;
  readonly runId: string;
  readonly keyDir: string;
  readonly signed: boolean;
}): Promise<void> {
  const files = [
    "appsec-run.json",
    "appsec-input.json",
    "appsec-triage.json",
    "result.json",
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
  await writeFile(join(input.bundleDir, "index.html"), appsecIndexHtml(input.runId, copied), "utf8");
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
    files: copied.filter((file) => !["manifest.json", "manifest.sig", "manifest.pub.json"].includes(file)),
    key
  });
}

function appsecIndexHtml(runId: string, files: readonly string[]): string {
  return `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>KelpClaw AppSec Audit Bundle</title></head>
<body>
<h1>KelpClaw AppSec Audit Bundle</h1>
<p>Run: ${escapeHtml(runId)}</p>
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

async function auditManifestFile(root: string, path: string): Promise<AppsecAuditBundleManifestFile> {
  const absolutePath = join(root, path);
  const info = await stat(absolutePath);
  return {
    path,
    size: info.size,
    sha256: `sha256:${await sha256File(absolutePath)}`
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
  return createHash("sha256").update(await readFile(path)).digest("hex");
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

function printJson(value: unknown): void {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}
