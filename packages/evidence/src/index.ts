import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign as signBytes,
  verify as verifyBytes
} from "node:crypto";
import { copyFile, mkdir, readdir, readFile, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join, relative, resolve } from "node:path";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";

export const EVIDENCE_WORKSPACE_SCHEMA_VERSION = "kelpclaw.evidence.workspace.v1";
export const EVIDENCE_INDEX_SCHEMA_VERSION = "kelpclaw.evidence.index.v1";
export const EVIDENCE_FINDINGS_SCHEMA_VERSION = "kelpclaw.evidence.findings.v1";
export const EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION = "kelpclaw.evidence.audit-event.v1";
export const EVIDENCE_MANIFEST_SCHEMA_VERSION = "kelpclaw.evidence.manifest.v1";
export const EVIDENCE_QA_SCHEMA_VERSION = "kelpclaw.evidence.qa.v1";
export const EVIDENCE_RETEST_SCHEMA_VERSION = "kelpclaw.evidence.retest.v1";
export const EVIDENCE_SIGNATURE_ALGORITHM = "ed25519";

export const EVIDENCE_WORKSPACE_FILE = "workspace.json";
export const EVIDENCE_INDEX_FILE = "evidence/index.json";
export const EVIDENCE_FINDINGS_FILE = "normalized/findings.json";
export const EVIDENCE_AUDIT_LOG_FILE = "audit-log.jsonl";

export type EvidenceKind =
  | "screenshot"
  | "agent-run"
  | "web-evidence"
  | "scanner"
  | "sarif"
  | "transcript"
  | "note"
  | "other";
export type EvidenceSensitivity = "public" | "internal" | "sensitive" | "secret";
export type EvidenceSeverity = "info" | "low" | "medium" | "high" | "critical";
export type EvidenceConfidence = "info" | "tool-observed" | "low" | "medium" | "high" | "confirmed";
export type EvidenceFindingStatus =
  | "new"
  | "open"
  | "closed"
  | "changed"
  | "regressed"
  | "accepted-risk";
export type EvidenceRetestStatus =
  | "new"
  | "open"
  | "closed"
  | "changed"
  | "regressed"
  | "ambiguous";
export type EvidenceQaIssueLevel = "error" | "warning";

export interface EvidenceWorkspaceDocument {
  readonly schemaVersion: typeof EVIDENCE_WORKSPACE_SCHEMA_VERSION;
  readonly generatedBy: "kelpclaw";
  readonly piranesiLineage: true;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly engagement: {
    readonly client?: string | undefined;
    readonly project?: string | undefined;
    readonly scope: readonly string[];
  };
}

export interface EvidenceRecord {
  readonly id: string;
  readonly kind: EvidenceKind;
  readonly title: string;
  readonly rawPath: string;
  readonly sha256: string;
  readonly addedAt: string;
  readonly observedAt?: string | undefined;
  readonly source?: string | undefined;
  readonly sensitivity: EvidenceSensitivity;
  readonly tags: readonly string[];
  readonly notes?: string | undefined;
}

export interface EvidenceIndexDocument {
  readonly schemaVersion: typeof EVIDENCE_INDEX_SCHEMA_VERSION;
  readonly evidence: readonly EvidenceRecord[];
}

export interface EvidenceSnippet {
  readonly kind: string;
  readonly value: string;
  readonly redacted: boolean;
  readonly locator?: string | undefined;
}

export interface EvidenceSourceReference {
  readonly tool: string;
  readonly inputSha256: string;
  readonly rawPath: string;
  readonly locator?: string | undefined;
  readonly metadata: JsonRecord;
}

export interface EvidenceAffectedInstance {
  readonly asset: string;
  readonly location?: string | undefined;
  readonly metadata: JsonRecord;
}

export interface NormalizedEvidenceFinding {
  readonly id: string;
  readonly title: string;
  readonly severity: EvidenceSeverity;
  readonly confidence: EvidenceConfidence;
  readonly status: EvidenceFindingStatus;
  readonly description?: string | undefined;
  readonly remediation?: string | undefined;
  readonly asset?: string | undefined;
  readonly weaknessIds: readonly string[];
  readonly references: readonly string[];
  readonly tags: readonly string[];
  readonly evidence: readonly EvidenceSnippet[];
  readonly sourceReferences: readonly EvidenceSourceReference[];
  readonly affectedInstances: readonly EvidenceAffectedInstance[];
  readonly firstSeen: string;
  readonly lastSeen: string;
  readonly provenance: JsonRecord;
}

export interface EvidenceFindingsDocument {
  readonly schemaVersion: typeof EVIDENCE_FINDINGS_SCHEMA_VERSION;
  readonly findings: readonly NormalizedEvidenceFinding[];
}

export interface EvidenceAuditEvent {
  readonly schemaVersion: typeof EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION;
  readonly timestamp: string;
  readonly command: string;
  readonly inputPath?: string | undefined;
  readonly inputSha256?: string | undefined;
  readonly outputPath?: string | undefined;
  readonly outputSha256?: string | undefined;
  readonly summary: JsonRecord;
}

export interface EvidenceWorkspaceState {
  readonly root: string;
  readonly workspace: EvidenceWorkspaceDocument;
  readonly index: EvidenceIndexDocument;
  readonly findings: EvidenceFindingsDocument;
}

export interface EvidenceManifestArtifact {
  readonly path: string;
  readonly sha256: string;
  readonly size: number;
  readonly role:
    | "workspace"
    | "findings"
    | "evidence"
    | "audit-log"
    | "raw-input"
    | "report"
    | "signature";
}

export interface EvidenceAuditChainEntry {
  readonly line: number;
  readonly previousHash: string;
  readonly eventHash: string;
  readonly command?: string | undefined;
  readonly timestamp?: string | undefined;
}

export interface EvidenceManifest {
  readonly schemaVersion: typeof EVIDENCE_MANIFEST_SCHEMA_VERSION;
  readonly manifestId: string;
  readonly generatedAt: string;
  readonly workspaceSchemaVersion: string;
  readonly findingsSchemaVersion: string;
  readonly artifacts: readonly EvidenceManifestArtifact[];
  readonly auditChain: readonly EvidenceAuditChainEntry[];
  readonly auditChainHead: string;
  readonly limitations: readonly string[];
}

export interface EvidenceSigningKeyFile {
  readonly schemaVersion: "1.0.0";
  readonly algorithm: typeof EVIDENCE_SIGNATURE_ALGORITHM;
  readonly keyId: string;
  readonly publicKeyPem: string;
  readonly privateKeyPem: string;
}

export interface EvidenceManifestSignature {
  readonly signed: boolean;
  readonly valid: boolean;
  readonly algorithm?: typeof EVIDENCE_SIGNATURE_ALGORITHM | undefined;
  readonly keyId?: string | undefined;
  readonly signaturePath?: string | undefined;
  readonly publicKeyPath?: string | undefined;
}

export interface EvidenceVerificationFailure {
  readonly path: string;
  readonly message: string;
  readonly expectedSha256?: string | undefined;
  readonly actualSha256?: string | undefined;
}

export interface EvidenceVerificationResult {
  readonly ok: boolean;
  readonly manifestPath?: string | undefined;
  readonly manifestId?: string | undefined;
  readonly signature: EvidenceManifestSignature;
  readonly failures: readonly EvidenceVerificationFailure[];
}

export interface EvidenceQaIssue {
  readonly level: EvidenceQaIssueLevel;
  readonly code: string;
  readonly message: string;
  readonly path?: string | undefined;
  readonly subject?: string | undefined;
}

export interface EvidenceQaResult {
  readonly schemaVersion: typeof EVIDENCE_QA_SCHEMA_VERSION;
  readonly workspace: string;
  readonly valid: boolean;
  readonly errorCount: number;
  readonly warningCount: number;
  readonly issues: readonly EvidenceQaIssue[];
}

export interface EvidenceRetestFinding {
  readonly findingId: string;
  readonly status: EvidenceRetestStatus;
  readonly title: string;
  readonly asset?: string | undefined;
  readonly baselineId?: string | undefined;
  readonly currentId?: string | undefined;
  readonly matchedBy: "id" | "fallback" | "none" | "ambiguous";
  readonly details: JsonRecord;
}

export interface EvidenceRetestResult {
  readonly schemaVersion: typeof EVIDENCE_RETEST_SCHEMA_VERSION;
  readonly generatedAt: string;
  readonly baselineWorkspace: string;
  readonly currentWorkspace: string;
  readonly baselineDigest: string;
  readonly currentDigest: string;
  readonly summary: Readonly<Record<EvidenceRetestStatus, number>>;
  readonly findings: readonly EvidenceRetestFinding[];
  readonly ambiguousMatches: readonly JsonRecord[];
}

export interface EvidenceWorkspaceSummary {
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

export interface EvidenceImportResult {
  readonly workspace: string;
  readonly inputPath: string;
  readonly rawPath: string;
  readonly inputSha256: string;
  readonly importedFindings: number;
  readonly warnings: readonly string[];
  readonly metadata: JsonRecord;
}

export interface EvidenceBundleCopyResult {
  readonly sourceRoot: string;
  readonly targetRoot: string;
  readonly files: readonly string[];
}

export class EvidenceWorkspaceError extends Error {}

export async function createEvidenceWorkspace(
  root: string,
  options: {
    readonly client?: string | undefined;
    readonly project?: string | undefined;
    readonly scope?: readonly string[] | undefined;
  } = {}
): Promise<EvidenceWorkspaceState> {
  const workspaceRoot = resolve(root);
  await ensureEvidenceWorkspaceDirectories(workspaceRoot);
  const workspacePath = join(workspaceRoot, EVIDENCE_WORKSPACE_FILE);
  const now = utcNow();
  if (!(await fileExists(workspacePath))) {
    const workspace: EvidenceWorkspaceDocument = {
      schemaVersion: EVIDENCE_WORKSPACE_SCHEMA_VERSION,
      generatedBy: "kelpclaw",
      piranesiLineage: true,
      createdAt: now,
      updatedAt: now,
      engagement: {
        ...(options.client ? { client: options.client } : {}),
        ...(options.project ? { project: options.project } : {}),
        scope: [...(options.scope ?? [])]
      }
    };
    await writeJson(workspacePath, workspace);
  }
  await ensureJsonFile(join(workspaceRoot, EVIDENCE_INDEX_FILE), {
    schemaVersion: EVIDENCE_INDEX_SCHEMA_VERSION,
    evidence: []
  });
  await ensureJsonFile(join(workspaceRoot, EVIDENCE_FINDINGS_FILE), {
    schemaVersion: EVIDENCE_FINDINGS_SCHEMA_VERSION,
    findings: []
  });
  await ensureTextFile(join(workspaceRoot, EVIDENCE_AUDIT_LOG_FILE), "");
  if (options.client || options.project || options.scope?.length) {
    const state = await loadEvidenceWorkspace(workspaceRoot);
    await saveWorkspaceDocument(workspaceRoot, {
      ...state.workspace,
      updatedAt: now,
      engagement: {
        ...((options.client ?? state.workspace.engagement.client)
          ? { client: options.client ?? state.workspace.engagement.client }
          : {}),
        ...((options.project ?? state.workspace.engagement.project)
          ? { project: options.project ?? state.workspace.engagement.project }
          : {}),
        scope: options.scope ? [...options.scope] : state.workspace.engagement.scope
      }
    });
  }
  return loadEvidenceWorkspace(workspaceRoot);
}

export async function loadEvidenceWorkspace(root: string): Promise<EvidenceWorkspaceState> {
  const workspaceRoot = resolve(root);
  const workspace = await readJsonFile<EvidenceWorkspaceDocument>(
    join(workspaceRoot, EVIDENCE_WORKSPACE_FILE)
  );
  if (workspace.schemaVersion !== EVIDENCE_WORKSPACE_SCHEMA_VERSION) {
    throw new EvidenceWorkspaceError(
      `unsupported evidence workspace schema ${String(workspace.schemaVersion)}`
    );
  }
  const index = await readJsonFile<EvidenceIndexDocument>(join(workspaceRoot, EVIDENCE_INDEX_FILE));
  if (index.schemaVersion !== EVIDENCE_INDEX_SCHEMA_VERSION) {
    throw new EvidenceWorkspaceError(
      `unsupported evidence index schema ${String(index.schemaVersion)}`
    );
  }
  const findings = await readJsonFile<EvidenceFindingsDocument>(
    join(workspaceRoot, EVIDENCE_FINDINGS_FILE)
  );
  if (findings.schemaVersion !== EVIDENCE_FINDINGS_SCHEMA_VERSION) {
    throw new EvidenceWorkspaceError(
      `unsupported evidence findings schema ${String(findings.schemaVersion)}`
    );
  }
  return {
    root: workspaceRoot,
    workspace,
    index,
    findings
  };
}

export async function addEvidenceFile(
  root: string,
  input: {
    readonly filePath: string;
    readonly kind: EvidenceKind;
    readonly title?: string | undefined;
    readonly observedAt?: string | undefined;
    readonly source?: string | undefined;
    readonly sensitivity?: EvidenceSensitivity | undefined;
    readonly tags?: readonly string[] | undefined;
    readonly notes?: string | undefined;
  }
): Promise<{
  readonly workspace: string;
  readonly record: EvidenceRecord;
  readonly evidenceCount: number;
}> {
  const state = await createEvidenceWorkspace(root);
  const sourcePath = resolve(input.filePath);
  const sourceStat = await stat(sourcePath);
  if (!sourceStat.isFile()) {
    throw new EvidenceWorkspaceError(`evidence input is not a file: ${input.filePath}`);
  }
  const digest = await sha256File(sourcePath);
  const rawPath = `raw/${input.kind}/${digest.slice(0, 16)}-${safeFilename(basename(sourcePath))}`;
  const destination = evidenceWorkspacePath(state.root, rawPath, ["raw"]);
  await mkdir(dirname(destination), { recursive: true });
  if (!(await fileExists(destination))) {
    await copyFile(sourcePath, destination);
  }
  const record: EvidenceRecord = {
    id: deterministicEvidenceId("evidence", input.kind, digest),
    kind: input.kind,
    title: input.title ?? basename(sourcePath),
    rawPath,
    sha256: digest,
    addedAt: utcNow(),
    ...(input.observedAt ? { observedAt: input.observedAt } : {}),
    ...(input.source ? { source: input.source } : {}),
    sensitivity: input.sensitivity ?? "sensitive",
    tags: [...new Set(input.tags ?? [])].sort(),
    ...(input.notes ? { notes: input.notes } : {})
  };
  const records = [...state.index.evidence.filter((item) => item.id !== record.id), record].sort(
    (left, right) => left.id.localeCompare(right.id)
  );
  await writeJson(join(state.root, EVIDENCE_INDEX_FILE), {
    schemaVersion: EVIDENCE_INDEX_SCHEMA_VERSION,
    evidence: records
  } satisfies EvidenceIndexDocument);
  await appendEvidenceAuditEvent(state.root, {
    schemaVersion: EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION,
    timestamp: utcNow(),
    command: "evidence add",
    inputPath: sourcePath,
    inputSha256: digest,
    outputPath: rawPath,
    outputSha256: digest,
    summary: {
      kind: input.kind,
      title: record.title,
      sensitivity: record.sensitivity
    }
  });
  await touchWorkspaceUpdatedAt(state.root);
  return {
    workspace: state.root,
    record,
    evidenceCount: records.length
  };
}

export async function importSarifEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  const state = await createEvidenceWorkspace(root);
  const sourcePath = resolve(inputPath);
  const digest = await sha256File(sourcePath);
  const rawPath = `raw/sarif/${digest.slice(0, 16)}-${safeFilename(basename(sourcePath))}`;
  const destination = evidenceWorkspacePath(state.root, rawPath, ["raw"]);
  await mkdir(dirname(destination), { recursive: true });
  await copyFile(sourcePath, destination);
  const parsed = await parseSarifFile(destination, {
    inputSha256: digest,
    rawPath
  });
  const merged = upsertEvidenceFindings(state.findings.findings, parsed.findings);
  await writeJson(join(state.root, EVIDENCE_FINDINGS_FILE), {
    schemaVersion: EVIDENCE_FINDINGS_SCHEMA_VERSION,
    findings: merged
  } satisfies EvidenceFindingsDocument);
  await appendEvidenceAuditEvent(state.root, {
    schemaVersion: EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION,
    timestamp: utcNow(),
    command: "evidence import-sarif",
    inputPath: sourcePath,
    inputSha256: digest,
    outputPath: EVIDENCE_FINDINGS_FILE,
    outputSha256: await sha256File(join(state.root, EVIDENCE_FINDINGS_FILE)),
    summary: {
      importedFindings: parsed.findings.length,
      warnings: parsed.warnings.length,
      ...parsed.metadata
    }
  });
  await touchWorkspaceUpdatedAt(state.root);
  return {
    workspace: state.root,
    inputPath: sourcePath,
    rawPath,
    inputSha256: digest,
    importedFindings: parsed.findings.length,
    warnings: parsed.warnings,
    metadata: parsed.metadata
  };
}

export async function importNmapEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  return importPassiveScannerEvidence(root, inputPath, "nmap", parseNmapFile);
}

export async function importNucleiEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  return importPassiveScannerEvidence(root, inputPath, "nuclei", parseNucleiJsonlFile);
}

export async function importBurpEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  return importPassiveScannerEvidence(root, inputPath, "burp", parseBurpXmlFile);
}

export async function importZapEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  return importPassiveScannerEvidence(root, inputPath, "zap", parseZapJsonFile);
}

export async function importNessusEvidence(
  root: string,
  inputPath: string
): Promise<EvidenceImportResult> {
  return importPassiveScannerEvidence(root, inputPath, "nessus", parseNessusXmlFile);
}

export async function signEvidenceWorkspace(
  root: string,
  options: { readonly keyDir?: string | undefined } = {}
): Promise<{
  readonly ok: true;
  readonly workspace: string;
  readonly manifestPath: string;
  readonly signaturePath: string;
  readonly publicKeyPath: string;
  readonly keyId: string;
  readonly manifest: EvidenceManifest;
}> {
  const state = await loadEvidenceWorkspace(root);
  const key = await ensureEvidenceSigningKey(
    resolve(options.keyDir ?? defaultEvidenceSigningKeyDir(state.root))
  );
  await appendEvidenceAuditEvent(state.root, {
    schemaVersion: EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION,
    timestamp: utcNow(),
    command: "evidence sign",
    summary: {
      requested: true,
      algorithm: EVIDENCE_SIGNATURE_ALGORITHM,
      keyId: key.keyId
    }
  });
  const manifest = await buildEvidenceManifest(state.root, state.workspace, state.findings);
  const manifestPath = evidenceWorkspacePath(
    state.root,
    `signatures/manifest-${manifest.manifestId}.json`,
    ["signatures"]
  );
  const signaturePath = evidenceSignaturePath(manifestPath);
  const publicKeyPath = evidencePublicKeyPath(manifestPath);
  const payload = stableJsonStringify(manifest as unknown as JsonValue);
  const signature = signBytes(
    null,
    Buffer.from(payload, "utf8"),
    createPrivateKey(key.privateKeyPem)
  ).toString("base64");
  await writeFile(
    manifestPath,
    `${payload}\n`,
    "utf8"
  );
  await writeFile(signaturePath, `${signature}\n`, "utf8");
  await writeJson(publicKeyPath, {
    keyId: key.keyId,
    algorithm: key.algorithm,
    publicKeyPem: key.publicKeyPem
  });
  return {
    ok: true,
    workspace: state.root,
    manifestPath,
    signaturePath,
    publicKeyPath,
    keyId: key.keyId,
    manifest
  };
}

export async function verifyEvidenceWorkspace(
  root: string,
  manifestPath?: string
): Promise<EvidenceVerificationResult> {
  const state = await loadEvidenceWorkspace(root);
  const selectedManifest = manifestPath
    ? resolve(manifestPath)
    : await latestEvidenceManifestPath(state.root);
  if (!selectedManifest) {
    return {
      ok: false,
      signature: { signed: false, valid: false },
      failures: [{ path: "signatures/", message: "no evidence manifest found" }]
    };
  }
  const manifest = await readJsonFile<EvidenceManifest>(selectedManifest);
  const failures: EvidenceVerificationFailure[] = [];
  const expectedManifestId = evidenceManifestId({ ...manifest, manifestId: "" });
  if (expectedManifestId !== manifest.manifestId) {
    failures.push({
      path: relative(state.root, selectedManifest),
      message: "manifestId does not match canonical manifest content",
      expectedSha256: manifest.manifestId,
      actualSha256: expectedManifestId
    });
  }
  for (const artifact of manifest.artifacts) {
    const artifactPath = evidenceWorkspacePath(state.root, artifact.path);
    if (!(await fileExists(artifactPath))) {
      failures.push({ path: artifact.path, message: "covered file missing" });
      continue;
    }
    const actualSha = await sha256File(artifactPath);
    if (actualSha !== artifact.sha256) {
      failures.push({
        path: artifact.path,
        message: "covered file digest mismatch",
        expectedSha256: artifact.sha256,
        actualSha256: actualSha
      });
    }
  }
  const audit = await auditChain(join(state.root, EVIDENCE_AUDIT_LOG_FILE));
  if (audit.head !== manifest.auditChainHead) {
    failures.push({
      path: EVIDENCE_AUDIT_LOG_FILE,
      message: "audit chain head mismatch",
      expectedSha256: manifest.auditChainHead,
      actualSha256: audit.head
    });
  }
  const signature = await verifyEvidenceManifestSignature(state.root, selectedManifest, manifest);
  failures.push(...signature.failures);
  return {
    ok: failures.length === 0,
    manifestPath: selectedManifest,
    manifestId: manifest.manifestId,
    signature: signature.signature,
    failures
  };
}

export async function qaEvidenceWorkspace(root: string): Promise<EvidenceQaResult> {
  const state = await loadEvidenceWorkspace(root);
  const issues: EvidenceQaIssue[] = [];
  for (const record of state.index.evidence) {
    const rawPath = evidenceWorkspacePath(state.root, record.rawPath, ["raw"]);
    if (!(await fileExists(rawPath))) {
      issues.push({
        level: "error",
        code: "evidence-raw-missing",
        message: "evidence raw file is missing",
        path: record.rawPath,
        subject: record.id
      });
      continue;
    }
    const actualSha = await sha256File(rawPath);
    if (actualSha !== record.sha256) {
      issues.push({
        level: "error",
        code: "evidence-digest-mismatch",
        message: "evidence raw file digest does not match index",
        path: record.rawPath,
        subject: record.id
      });
    }
  }
  for (const finding of state.findings.findings) {
    if (finding.sourceReferences.length === 0) {
      issues.push({
        level: "warning",
        code: "finding-missing-source-reference",
        message: "finding has no source reference",
        subject: finding.id
      });
    }
  }
  if (state.findings.findings.length === 0) {
    issues.push({
      level: "warning",
      code: "workspace-has-no-findings",
      message: "evidence workspace has no normalized findings"
    });
  }
  const verification = await verifyEvidenceWorkspace(state.root);
  if (!verification.ok) {
    issues.push(
      ...verification.failures.map((failure) => ({
        level: "warning" as const,
        code: "manifest-verification-gap",
        message: failure.message,
        path: failure.path
      }))
    );
  }
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.filter((issue) => issue.level === "warning").length;
  return {
    schemaVersion: EVIDENCE_QA_SCHEMA_VERSION,
    workspace: state.root,
    valid: errorCount === 0,
    errorCount,
    warningCount,
    issues: issues.sort(
      (left, right) =>
        left.level.localeCompare(right.level) ||
        left.code.localeCompare(right.code) ||
        (left.path ?? "").localeCompare(right.path ?? "")
    )
  };
}

export async function compareEvidenceWorkspaces(
  baselineRoot: string,
  currentRoot: string
): Promise<EvidenceRetestResult> {
  const baseline = await loadEvidenceWorkspace(baselineRoot);
  const current = await loadEvidenceWorkspace(currentRoot);
  const baselineById = new Map(baseline.findings.findings.map((finding) => [finding.id, finding]));
  const usedBaseline = new Set<string>();
  const findings: EvidenceRetestFinding[] = [];
  const ambiguousMatches: JsonRecord[] = [];
  for (const currentFinding of [...current.findings.findings].sort((left, right) =>
    left.id.localeCompare(right.id)
  )) {
    const direct = baselineById.get(currentFinding.id);
    if (direct) {
      usedBaseline.add(direct.id);
      findings.push(classifyRetestFinding(direct, currentFinding, "id"));
      continue;
    }
    const candidates = baseline.findings.findings.filter(
      (candidate) =>
        !usedBaseline.has(candidate.id) && fallbackKey(candidate) === fallbackKey(currentFinding)
    );
    if (candidates.length === 1 && candidates[0]) {
      usedBaseline.add(candidates[0].id);
      findings.push(classifyRetestFinding(candidates[0], currentFinding, "fallback"));
    } else if (candidates.length > 1) {
      const candidateIds = candidates.map((candidate) => candidate.id);
      ambiguousMatches.push({
        currentId: currentFinding.id,
        candidateBaselineIds: candidateIds,
        reason: "multiple fallback candidates matched"
      });
      findings.push({
        findingId: currentFinding.id,
        status: "ambiguous",
        title: currentFinding.title,
        ...(currentFinding.asset ? { asset: currentFinding.asset } : {}),
        currentId: currentFinding.id,
        matchedBy: "ambiguous",
        details: { candidateBaselineIds: candidateIds }
      });
    } else {
      findings.push({
        findingId: currentFinding.id,
        status: "new",
        title: currentFinding.title,
        ...(currentFinding.asset ? { asset: currentFinding.asset } : {}),
        currentId: currentFinding.id,
        matchedBy: "none",
        details: {}
      });
    }
  }
  for (const baselineFinding of baseline.findings.findings) {
    if (usedBaseline.has(baselineFinding.id)) {
      continue;
    }
    if (current.findings.findings.some((finding) => finding.id === baselineFinding.id)) {
      continue;
    }
    findings.push({
      findingId: baselineFinding.id,
      status: "closed",
      title: baselineFinding.title,
      ...(baselineFinding.asset ? { asset: baselineFinding.asset } : {}),
      baselineId: baselineFinding.id,
      matchedBy: "none",
      details: {}
    });
  }
  const statuses: readonly EvidenceRetestStatus[] = [
    "new",
    "open",
    "closed",
    "changed",
    "regressed",
    "ambiguous"
  ];
  const summary = Object.fromEntries(
    statuses.map((status) => [
      status,
      findings.filter((finding) => finding.status === status).length
    ])
  ) as Readonly<Record<EvidenceRetestStatus, number>>;
  return {
    schemaVersion: EVIDENCE_RETEST_SCHEMA_VERSION,
    generatedAt: utcNow(),
    baselineWorkspace: baseline.root,
    currentWorkspace: current.root,
    baselineDigest: await evidenceWorkspaceDigest(baseline.root),
    currentDigest: await evidenceWorkspaceDigest(current.root),
    summary,
    findings: findings.sort(
      (left, right) =>
        left.status.localeCompare(right.status) || left.findingId.localeCompare(right.findingId)
    ),
    ambiguousMatches
  };
}

export async function evidenceWorkspaceSummary(root: string): Promise<EvidenceWorkspaceSummary> {
  const state = await loadEvidenceWorkspace(root);
  const manifest = await latestEvidenceManifestPath(state.root);
  const verification = await verifyEvidenceWorkspace(state.root);
  return {
    path: state.root,
    evidenceCount: state.index.evidence.length,
    findingCount: state.findings.findings.length,
    signed: verification.signature.signed,
    verified: verification.ok,
    highOrCriticalFindings: state.findings.findings.filter((finding) =>
      ["high", "critical"].includes(finding.severity)
    ).length,
    sourceReferenceGaps: state.findings.findings.filter(
      (finding) => finding.sourceReferences.length === 0
    ).length,
    ...(manifest ? { latestManifest: manifest } : {}),
    verificationFailures: verification.failures.map(
      (failure) => `${failure.path}: ${failure.message}`
    )
  };
}

export async function copyEvidenceWorkspaceBundle(
  sourceRoot: string,
  targetRoot: string
): Promise<EvidenceBundleCopyResult> {
  const source = resolve(sourceRoot);
  const target = resolve(targetRoot);
  const files = await collectEvidenceBundleRelativeFiles(source);
  for (const file of files) {
    const sourcePath = evidenceWorkspacePath(source, file);
    const targetPath = join(target, file);
    await mkdir(dirname(targetPath), { recursive: true });
    await copyFile(sourcePath, targetPath);
  }
  return {
    sourceRoot: source,
    targetRoot: target,
    files
  };
}

export async function renderEvidenceWorkspaceHtml(root: string): Promise<string> {
  const state = await loadEvidenceWorkspace(root);
  const summary = await evidenceWorkspaceSummary(root);
  const evidenceRows = state.index.evidence
    .map(
      (record) =>
        `<tr><td>${escapeHtml(record.kind)}</td><td>${escapeHtml(record.title)}</td><td>${escapeHtml(record.sensitivity)}</td><td>${escapeHtml(record.rawPath)}</td><td>${escapeHtml(record.sha256)}</td></tr>`
    )
    .join("\n");
  const findingRows = state.findings.findings
    .map(
      (finding) =>
        `<tr><td>${escapeHtml(finding.severity)}</td><td>${escapeHtml(finding.status)}</td><td>${escapeHtml(finding.title)}</td><td>${escapeHtml(finding.asset ?? "")}</td><td>${escapeHtml(finding.sourceReferences.map((reference) => reference.tool).join(", "))}</td></tr>`
    )
    .join("\n");
  const verificationRows = summary.verificationFailures
    .map((failure) => `<li>${escapeHtml(failure)}</li>`)
    .join("\n");
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>KelpClaw Evidence Workspace</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #1f2937; }
    h1 { font-size: 24px; margin-bottom: 8px; }
    h2 { font-size: 16px; margin-top: 24px; }
    table { border-collapse: collapse; width: 100%; margin-top: 8px; }
    th, td { border: 1px solid #d9e2ec; padding: 8px; text-align: left; vertical-align: top; }
    th { background: #f8fafc; }
    code { background: #f8fafc; border: 1px solid #d9e2ec; border-radius: 4px; padding: 1px 4px; }
  </style>
</head>
<body>
  <h1>KelpClaw Evidence Workspace</h1>
  <p><strong>Workspace:</strong> <code>${escapeHtml(summary.path)}</code></p>
  <p><strong>Evidence:</strong> ${summary.evidenceCount} / <strong>Findings:</strong> ${summary.findingCount} / <strong>Signed:</strong> ${summary.signed ? "yes" : "no"} / <strong>Verified:</strong> ${summary.verified ? "yes" : "no"}</p>
  <h2>Evidence</h2>
  <table><thead><tr><th>Kind</th><th>Title</th><th>Sensitivity</th><th>Path</th><th>SHA-256</th></tr></thead><tbody>${evidenceRows || "<tr><td colspan=\"5\">No evidence records.</td></tr>"}</tbody></table>
  <h2>Findings</h2>
  <table><thead><tr><th>Severity</th><th>Status</th><th>Title</th><th>Asset</th><th>Source Tools</th></tr></thead><tbody>${findingRows || "<tr><td colspan=\"5\">No normalized findings.</td></tr>"}</tbody></table>
  <h2>Verification</h2>
  <ul>${verificationRows || "<li>No verification failures.</li>"}</ul>
</body>
</html>
`;
}

export function renderEvidenceQaMarkdown(result: EvidenceQaResult): string {
  const rows = result.issues
    .map(
      (issue) =>
        `| ${issue.level} | ${issue.code} | ${markdownCell(issue.message)} | ${markdownCell(issue.path ?? "")} | ${markdownCell(issue.subject ?? "")} |`
    )
    .join("\n");
  return `# KelpClaw Evidence QA

Workspace: ${result.workspace}

Status: ${result.valid ? "valid" : "invalid"}

Errors: ${result.errorCount}

Warnings: ${result.warningCount}

| Level | Code | Message | Path | Subject |
| --- | --- | --- | --- | --- |
${rows || "| info | no-findings | No QA issues found. |  |  |"}
`;
}

export function renderEvidenceRetestMarkdown(result: EvidenceRetestResult): string {
  const rows = result.findings
    .map(
      (finding) =>
        `| ${finding.status} | ${markdownCell(finding.title)} | ${markdownCell(finding.baselineId ?? "")} | ${markdownCell(finding.currentId ?? "")} | ${finding.matchedBy} |`
    )
    .join("\n");
  return `# KelpClaw Evidence Retest Diff

Generated: ${result.generatedAt}

Baseline: ${result.baselineWorkspace}

Current: ${result.currentWorkspace}

Summary: ${Object.entries(result.summary)
    .map(([status, count]) => `${status}=${count}`)
    .join(", ")}

| Status | Finding | Baseline | Current | Matched By |
| --- | --- | --- | --- | --- |
${rows || "| open | No findings |  |  | none |"}
`;
}

type PassiveScannerFormat = "nmap" | "nuclei" | "burp" | "zap" | "nessus";

interface ParsedEvidenceFindings {
  readonly findings: readonly NormalizedEvidenceFinding[];
  readonly warnings: readonly string[];
  readonly metadata: JsonRecord;
}

interface PassiveScannerParseInput {
  readonly inputSha256: string;
  readonly rawPath: string;
  readonly format: PassiveScannerFormat;
}

async function importPassiveScannerEvidence(
  root: string,
  inputPath: string,
  format: PassiveScannerFormat,
  parse: (path: string, input: PassiveScannerParseInput) => Promise<ParsedEvidenceFindings>
): Promise<EvidenceImportResult> {
  const state = await createEvidenceWorkspace(root);
  const sourcePath = resolve(inputPath);
  const digest = await sha256File(sourcePath);
  const rawPath = `raw/${format}/${digest.slice(0, 16)}-${safeFilename(basename(sourcePath))}`;
  const destination = evidenceWorkspacePath(state.root, rawPath, ["raw"]);
  await mkdir(dirname(destination), { recursive: true });
  await copyFile(sourcePath, destination);
  const parsed = await parse(destination, { inputSha256: digest, rawPath, format });
  const merged = upsertEvidenceFindings(state.findings.findings, parsed.findings);
  await writeJson(join(state.root, EVIDENCE_FINDINGS_FILE), {
    schemaVersion: EVIDENCE_FINDINGS_SCHEMA_VERSION,
    findings: merged
  } satisfies EvidenceFindingsDocument);
  await appendEvidenceAuditEvent(state.root, {
    schemaVersion: EVIDENCE_AUDIT_EVENT_SCHEMA_VERSION,
    timestamp: utcNow(),
    command: `evidence import-${format}`,
    inputPath: sourcePath,
    inputSha256: digest,
    outputPath: EVIDENCE_FINDINGS_FILE,
    outputSha256: await sha256File(join(state.root, EVIDENCE_FINDINGS_FILE)),
    summary: {
      importedFindings: parsed.findings.length,
      warnings: parsed.warnings.length,
      ...parsed.metadata
    }
  });
  await touchWorkspaceUpdatedAt(state.root);
  return {
    workspace: state.root,
    inputPath: sourcePath,
    rawPath,
    inputSha256: digest,
    importedFindings: parsed.findings.length,
    warnings: parsed.warnings,
    metadata: parsed.metadata
  };
}

function parseSarifFile(
  path: string,
  input: { readonly inputSha256: string; readonly rawPath: string }
): Promise<{
  readonly findings: readonly NormalizedEvidenceFinding[];
  readonly warnings: readonly string[];
  readonly metadata: JsonRecord;
}> {
  return readFile(path, "utf8").then((content) => {
    const payload = jsonRecord(JSON.parse(content) as unknown, "SARIF");
    if (payload.version !== "2.1.0") {
      throw new EvidenceWorkspaceError(`unsupported SARIF version ${String(payload.version)}`);
    }
    const runs = Array.isArray(payload.runs) ? payload.runs : [];
    if (runs.length === 0) {
      throw new EvidenceWorkspaceError("empty SARIF: document contains no runs");
    }
    const warnings: string[] = [];
    const findings: NormalizedEvidenceFinding[] = [];
    let resultCount = 0;
    for (const [runIndex, runValue] of runs.entries()) {
      const run = jsonRecord(runValue, `SARIF run ${runIndex + 1}`);
      const rules = sarifRulesById(run);
      const toolName = sarifToolName(run);
      const results = Array.isArray(run.results) ? run.results : [];
      for (const [resultIndex, resultValue] of results.entries()) {
        resultCount += 1;
        const result = jsonRecord(resultValue, `SARIF result ${resultIndex + 1}`);
        const finding = sarifFindingFromResult(result, {
          rules,
          toolName,
          inputSha256: input.inputSha256,
          rawPath: input.rawPath,
          runIndex: runIndex + 1,
          resultIndex: resultIndex + 1,
          warnings
        });
        if (finding) {
          findings.push(finding);
        }
      }
    }
    if (resultCount === 0) {
      throw new EvidenceWorkspaceError("empty SARIF: document contains no results");
    }
    if (findings.length === 0) {
      throw new EvidenceWorkspaceError("SARIF contained no valid result records");
    }
    return {
      findings,
      warnings,
      metadata: {
        format: "sarif",
        sarifVersion: "2.1.0",
        runs: runs.length,
        records: resultCount,
        validRecords: findings.length,
        malformedRecords: resultCount - findings.length,
        rules: [...new Set(findings.map((finding) => String(finding.provenance.ruleId ?? "")))]
          .filter(Boolean)
          .sort()
      }
    };
  });
}

async function parseNucleiJsonlFile(
  path: string,
  input: PassiveScannerParseInput
): Promise<ParsedEvidenceFindings> {
  const content = await readFile(path, "utf8");
  const findings: NormalizedEvidenceFinding[] = [];
  const warnings: string[] = [];
  let records = 0;
  for (const [index, rawLine] of content.split(/\r?\n/u).entries()) {
    const line = rawLine.trim();
    if (!line) {
      continue;
    }
    records += 1;
    const lineNumber = index + 1;
    try {
      const record = jsonRecord(JSON.parse(line) as unknown, `nuclei line ${lineNumber}`);
      const info = jsonRecord(record.info);
      const templateId =
        stringField(record, "template-id") ??
        stringField(record, "templateID") ??
        `line-${lineNumber}`;
      const title = stringField(info, "name") ?? templateId;
      const asset =
        stringField(record, "matched-at") ??
        stringField(record, "host") ??
        stringField(record, "url");
      findings.push(
        scannerFinding({
          tool: input.format,
          idParts: [templateId, asset ?? "", stringField(record, "matcher-name") ?? ""],
          title,
          severity: scannerSeverity(stringField(info, "severity")),
          ...(asset ? { asset } : {}),
          description: stringField(info, "description"),
          remediation: stringField(info, "remediation"),
          weaknessIds: cweIds(...stringArrayOrCsvField(info, "classification")),
          references: stringArrayOrCsvField(info, "reference"),
          tags: ["nuclei", templateId, ...stringArrayOrCsvField(info, "tags")],
          evidenceKind: "nuclei-result",
          evidenceValue: `${title}${asset ? ` on ${asset}` : ""}`,
          locator: `line ${lineNumber}`,
          source: {
            templateId,
            ...(stringField(record, "matcher-name")
              ? { matcherName: stringField(record, "matcher-name") }
              : {}),
            ...(stringField(record, "type") ? { type: stringField(record, "type") } : {})
          },
          inputSha256: input.inputSha256,
          rawPath: input.rawPath
        })
      );
    } catch (error) {
      warnings.push(`line ${lineNumber}: ${errorMessage(error)}`);
    }
  }
  if (records === 0 || findings.length === 0) {
    throw new EvidenceWorkspaceError("nuclei JSONL contained no supported result records");
  }
  return {
    findings,
    warnings,
    metadata: {
      format: input.format,
      records,
      validRecords: findings.length,
      malformedRecords: warnings.length
    }
  };
}

async function parseNmapFile(
  path: string,
  input: PassiveScannerParseInput
): Promise<ParsedEvidenceFindings> {
  const content = await readFile(path, "utf8");
  const findings: NormalizedEvidenceFinding[] = [];
  const warnings: string[] = [];
  let hosts = 0;
  let openPorts = 0;
  for (const hostMatch of content.matchAll(/<host\b[\s\S]*?<\/host>/gu)) {
    const hostBlock = hostMatch[0];
    hosts += 1;
    const addressTag = hostBlock.match(/<address\b[^>]*>/u)?.[0] ?? "";
    const hostAddress = xmlAttr(addressTag, "addr") ?? `host-${hosts}`;
    for (const portMatch of hostBlock.matchAll(/<port\b([^>]*)>([\s\S]*?)<\/port>/gu)) {
      const portAttrs = portMatch[1] ?? "";
      const portBody = portMatch[2] ?? "";
      const stateTag = portBody.match(/<state\b[^>]*>/u)?.[0] ?? "";
      if (xmlAttr(stateTag, "state") !== "open") {
        continue;
      }
      const protocol = xmlAttr(portAttrs, "protocol") ?? "tcp";
      const portId = xmlAttr(portAttrs, "portid") ?? "unknown";
      const serviceTag = portBody.match(/<service\b[^>]*>/u)?.[0] ?? "";
      const serviceName = xmlAttr(serviceTag, "name");
      const product = xmlAttr(serviceTag, "product");
      const version = xmlAttr(serviceTag, "version");
      const serviceLabel = [serviceName, product, version].filter(Boolean).join(" ");
      openPorts += 1;
      findings.push(
        scannerFinding({
          tool: input.format,
          idParts: [hostAddress, protocol, portId, serviceLabel],
          title: `Open ${protocol}/${portId} on ${hostAddress}`,
          severity: "info",
          asset: hostAddress,
          location: `${protocol}/${portId}`,
          tags: ["nmap", "open-port", protocol, portId],
          evidenceKind: "nmap-open-port",
          evidenceValue: serviceLabel
            ? `${protocol}/${portId} open (${serviceLabel})`
            : `${protocol}/${portId} open`,
          locator: `host ${hosts} port ${openPorts}`,
          source: {
            protocol,
            port: portId,
            ...(serviceName ? { service: serviceName } : {}),
            ...(product ? { product } : {}),
            ...(version ? { version } : {})
          },
          inputSha256: input.inputSha256,
          rawPath: input.rawPath
        })
      );
    }
  }
  if (hosts === 0 || findings.length === 0) {
    throw new EvidenceWorkspaceError("nmap XML contained no open port records");
  }
  return {
    findings,
    warnings,
    metadata: {
      format: input.format,
      hosts,
      records: openPorts,
      validRecords: findings.length,
      malformedRecords: warnings.length
    }
  };
}

async function parseBurpXmlFile(
  path: string,
  input: PassiveScannerParseInput
): Promise<ParsedEvidenceFindings> {
  const content = await readFile(path, "utf8");
  const findings: NormalizedEvidenceFinding[] = [];
  const issueBlocks = [...content.matchAll(/<issue\b[\s\S]*?<\/issue>/gu)].map((match) => match[0]);
  for (const [index, issueBlock] of issueBlocks.entries()) {
    const title = xmlTagText(issueBlock, "name") ?? `Burp issue ${index + 1}`;
    const host = xmlTagText(issueBlock, "host");
    const pathText = xmlTagText(issueBlock, "path");
    const location = [host, pathText].filter(Boolean).join("");
    const issueType = xmlTagText(issueBlock, "type");
    findings.push(
      scannerFinding({
        tool: input.format,
        idParts: [issueType ?? title, host ?? "", pathText ?? ""],
        title,
        severity: scannerSeverity(xmlTagText(issueBlock, "severity")),
        ...(host ? { asset: host } : {}),
        ...(location ? { location } : {}),
        description: xmlTagText(issueBlock, "issueBackground") ?? xmlTagText(issueBlock, "detail"),
        remediation: xmlTagText(issueBlock, "remediationBackground"),
        tags: ["burp", ...(issueType ? [issueType] : [])],
        evidenceKind: "burp-issue",
        evidenceValue: location ? `${title} at ${location}` : title,
        locator: `issue ${index + 1}`,
        source: {
          ...(issueType ? { issueType } : {})
        },
        inputSha256: input.inputSha256,
        rawPath: input.rawPath
      })
    );
  }
  if (findings.length === 0) {
    throw new EvidenceWorkspaceError("Burp XML contained no issue records");
  }
  return scannerParseResult(input.format, findings, [], issueBlocks.length);
}

async function parseZapJsonFile(
  path: string,
  input: PassiveScannerParseInput
): Promise<ParsedEvidenceFindings> {
  const payload = jsonRecord(JSON.parse(await readFile(path, "utf8")) as unknown, "ZAP JSON");
  const siteValues = Array.isArray(payload.site)
    ? payload.site
    : Array.isArray(payload.sites)
      ? payload.sites
      : [];
  const findings: NormalizedEvidenceFinding[] = [];
  let alertCount = 0;
  for (const [siteIndex, siteValue] of siteValues.entries()) {
    const site = jsonRecord(siteValue, `ZAP site ${siteIndex + 1}`);
    const siteName = stringField(site, "name") ?? stringField(site, "@name");
    const alerts = Array.isArray(site.alerts) ? site.alerts : [];
    for (const [alertIndex, alertValue] of alerts.entries()) {
      const alert = jsonRecord(alertValue, `ZAP alert ${alertIndex + 1}`);
      const title = stringField(alert, "name") ?? `ZAP alert ${alertIndex + 1}`;
      const instances = Array.isArray(alert.instances) && alert.instances.length > 0
        ? alert.instances
        : [{}];
      alertCount += 1;
      for (const [instanceIndex, instanceValue] of instances.entries()) {
        const instance = jsonRecord(instanceValue);
        const asset =
          stringField(instance, "uri") ??
          stringField(instance, "url") ??
          stringField(alert, "url") ??
          siteName;
        const cwe = stringField(alert, "cweid") ?? stringField(alert, "cweId");
        findings.push(
          scannerFinding({
            tool: input.format,
            idParts: [
              stringField(alert, "pluginid") ?? stringField(alert, "pluginId") ?? title,
              asset ?? "",
              stringField(instance, "param") ?? ""
            ],
            title,
            severity: scannerSeverity(
              stringField(alert, "riskdesc") ??
                stringField(alert, "risk") ??
                stringField(alert, "riskcode")
            ),
            ...(asset ? { asset } : {}),
            description: stringField(alert, "desc"),
            remediation: stringField(alert, "solution"),
            weaknessIds: cweIds(cwe),
            references: stringArrayOrCsvField(alert, "reference"),
            tags: ["zap", ...(stringField(alert, "alertRef") ? [stringField(alert, "alertRef") as string] : [])],
            evidenceKind: "zap-alert",
            evidenceValue: asset ? `${title} at ${asset}` : title,
            locator: `site ${siteIndex + 1} alert ${alertIndex + 1} instance ${instanceIndex + 1}`,
            source: {
              ...(stringField(alert, "pluginid") ? { pluginId: stringField(alert, "pluginid") } : {}),
              ...(stringField(alert, "alertRef") ? { alertRef: stringField(alert, "alertRef") } : {})
            },
            inputSha256: input.inputSha256,
            rawPath: input.rawPath
          })
        );
      }
    }
  }
  if (findings.length === 0) {
    throw new EvidenceWorkspaceError("ZAP JSON contained no alert records");
  }
  return scannerParseResult(input.format, findings, [], alertCount);
}

async function parseNessusXmlFile(
  path: string,
  input: PassiveScannerParseInput
): Promise<ParsedEvidenceFindings> {
  const content = await readFile(path, "utf8");
  const findings: NormalizedEvidenceFinding[] = [];
  let reportItems = 0;
  for (const hostMatch of content.matchAll(/<ReportHost\b([^>]*)>([\s\S]*?)<\/ReportHost>/gu)) {
    const hostAttrs = hostMatch[1] ?? "";
    const hostBody = hostMatch[2] ?? "";
    const hostName = xmlAttr(hostAttrs, "name") ?? "unknown";
    for (const itemMatch of hostBody.matchAll(/<ReportItem\b([^>]*)>([\s\S]*?)<\/ReportItem>/gu)) {
      const itemAttrs = itemMatch[1] ?? "";
      const itemBody = itemMatch[2] ?? "";
      const pluginId = xmlAttr(itemAttrs, "pluginID") ?? xmlAttr(itemAttrs, "plugin_id") ?? "";
      const pluginName = xmlAttr(itemAttrs, "pluginName") ?? `Nessus plugin ${pluginId}`;
      const port = xmlAttr(itemAttrs, "port") ?? "0";
      const protocol = xmlAttr(itemAttrs, "protocol") ?? "tcp";
      reportItems += 1;
      findings.push(
        scannerFinding({
          tool: input.format,
          idParts: [pluginId, hostName, protocol, port],
          title: pluginName,
          severity: nessusSeverity(xmlAttr(itemAttrs, "severity")),
          asset: hostName,
          location: `${protocol}/${port}`,
          description: xmlTagText(itemBody, "description"),
          remediation: xmlTagText(itemBody, "solution"),
          weaknessIds: cweIds(xmlTagText(itemBody, "cwe")),
          references: stringArrayFromText(xmlTagText(itemBody, "see_also")),
          tags: ["nessus", ...(pluginId ? [`plugin-${pluginId}`] : [])],
          evidenceKind: "nessus-report-item",
          evidenceValue: `${pluginName} on ${hostName}:${port}`,
          locator: `host ${hostName} report item ${reportItems}`,
          source: {
            ...(pluginId ? { pluginId } : {}),
            protocol,
            port
          },
          inputSha256: input.inputSha256,
          rawPath: input.rawPath
        })
      );
    }
  }
  if (findings.length === 0) {
    throw new EvidenceWorkspaceError("Nessus XML contained no report item records");
  }
  return scannerParseResult(input.format, findings, [], reportItems);
}

function sarifFindingFromResult(
  result: JsonRecord,
  input: {
    readonly rules: Readonly<Record<string, JsonRecord>>;
    readonly toolName?: string | undefined;
    readonly inputSha256: string;
    readonly rawPath: string;
    readonly runIndex: number;
    readonly resultIndex: number;
    readonly warnings: string[];
  }
): NormalizedEvidenceFinding | undefined {
  const ruleId = stringField(result, "ruleId");
  if (!ruleId) {
    input.warnings.push(`run ${input.runIndex} result ${input.resultIndex}: missing ruleId`);
    return undefined;
  }
  const rule = input.rules[ruleId] ?? {};
  const location = sarifPrimaryLocation(result);
  const asset = location?.uri;
  const message = sarifMessageText(result.message);
  const title = stringField(rule, "name") ?? ruleId;
  const now = utcNow();
  const locator = `run[${input.runIndex}]/result[${input.resultIndex}]`;
  const weaknessIds = sarifWeaknessIds(rule, result);
  const resultLevel = stringField(result, "level");
  return {
    id: deterministicEvidenceId(
      "sarif",
      input.toolName ?? "",
      ruleId,
      asset ?? "",
      String(location?.startLine ?? ""),
      message ?? ""
    ),
    title,
    severity: sarifSeverity(result, rule),
    confidence: "tool-observed",
    status: "open",
    ...((sarifMessageText(rule.fullDescription) ?? message)
      ? { description: sarifMessageText(rule.fullDescription) ?? message }
      : {}),
    ...(sarifMessageText(rule.help) ? { remediation: sarifMessageText(rule.help) } : {}),
    ...(asset ? { asset } : {}),
    weaknessIds,
    references: sarifReferences(rule),
    tags: [
      ...new Set(
        ["sarif", input.toolName, ruleId, ...weaknessIds].filter(
          (tag): tag is string => typeof tag === "string" && tag.length > 0
        )
      )
    ].sort(),
    evidence: [
      {
        kind: "sarif-result",
        value: `${message ?? "SARIF result"}${locationLabel(location) ? ` at ${locationLabel(location)}` : ""}`,
        redacted: false,
        locator
      }
    ],
    sourceReferences: [
      {
        tool: "sarif",
        inputSha256: input.inputSha256,
        rawPath: input.rawPath,
        locator,
        metadata: {
          ruleId,
          ...(input.toolName ? { toolName: input.toolName } : {}),
          ...(resultLevel ? { level: resultLevel } : {}),
          ...(asset ? { uri: asset } : {}),
          ...(location?.startLine !== undefined ? { startLine: location.startLine } : {})
        }
      }
    ],
    affectedInstances: [
      {
        asset: asset ?? "unknown",
        ...(locationLabel(location) ? { location: locationLabel(location) } : {}),
        metadata: {
          ruleId,
          ...(input.toolName ? { toolName: input.toolName } : {})
        }
      }
    ],
    firstSeen: now,
    lastSeen: now,
    provenance: {
      tool: "sarif",
      type: "result",
      ...(input.toolName ? { sarifTool: input.toolName } : {}),
      ruleId
    }
  };
}

function scannerFinding(input: {
  readonly tool: PassiveScannerFormat;
  readonly idParts: readonly string[];
  readonly title: string;
  readonly severity: EvidenceSeverity;
  readonly asset?: string | undefined;
  readonly location?: string | undefined;
  readonly description?: string | undefined;
  readonly remediation?: string | undefined;
  readonly weaknessIds?: readonly string[] | undefined;
  readonly references?: readonly string[] | undefined;
  readonly tags?: readonly string[] | undefined;
  readonly evidenceKind: string;
  readonly evidenceValue: string;
  readonly locator: string;
  readonly source: JsonRecord;
  readonly inputSha256: string;
  readonly rawPath: string;
}): NormalizedEvidenceFinding {
  const now = utcNow();
  const weaknessIds = [...new Set(input.weaknessIds ?? [])].sort();
  const references = [...new Set(input.references ?? [])].sort();
  const tags = [
    ...new Set(
      [input.tool, ...(input.tags ?? [])].filter(
        (tag): tag is string => typeof tag === "string" && tag.length > 0
      )
    )
  ].sort();
  return {
    id: deterministicEvidenceId(input.tool, ...input.idParts),
    title: input.title,
    severity: input.severity,
    confidence: "tool-observed",
    status: "open",
    ...(input.description ? { description: input.description } : {}),
    ...(input.remediation ? { remediation: input.remediation } : {}),
    ...(input.asset ? { asset: input.asset } : {}),
    weaknessIds,
    references,
    tags,
    evidence: [
      {
        kind: input.evidenceKind,
        value: input.evidenceValue,
        redacted: false,
        locator: input.locator
      }
    ],
    sourceReferences: [
      {
        tool: input.tool,
        inputSha256: input.inputSha256,
        rawPath: input.rawPath,
        locator: input.locator,
        metadata: input.source
      }
    ],
    affectedInstances: [
      {
        asset: input.asset ?? "unknown",
        ...(input.location ? { location: input.location } : {}),
        metadata: input.source
      }
    ],
    firstSeen: now,
    lastSeen: now,
    provenance: {
      tool: input.tool,
      ...input.source
    }
  };
}

function scannerParseResult(
  format: PassiveScannerFormat,
  findings: readonly NormalizedEvidenceFinding[],
  warnings: readonly string[],
  records: number
): ParsedEvidenceFindings {
  return {
    findings,
    warnings,
    metadata: {
      format,
      records,
      validRecords: findings.length,
      malformedRecords: warnings.length
    }
  };
}

function upsertEvidenceFindings(
  existing: readonly NormalizedEvidenceFinding[],
  incoming: readonly NormalizedEvidenceFinding[]
): readonly NormalizedEvidenceFinding[] {
  const byId = new Map(existing.map((finding) => [finding.id, finding]));
  const now = utcNow();
  for (const finding of incoming) {
    const previous = byId.get(finding.id);
    byId.set(finding.id, previous ? mergeEvidenceFinding(previous, finding, now) : finding);
  }
  return [...byId.values()].sort((left, right) => left.id.localeCompare(right.id));
}

function mergeEvidenceFinding(
  previous: NormalizedEvidenceFinding,
  incoming: NormalizedEvidenceFinding,
  lastSeen: string
): NormalizedEvidenceFinding {
  return {
    ...previous,
    lastSeen,
    severity: maxSeverity(previous.severity, incoming.severity),
    confidence: maxConfidence(previous.confidence, incoming.confidence),
    evidence: dedupeJson([...previous.evidence, ...incoming.evidence]),
    sourceReferences: dedupeJson([...previous.sourceReferences, ...incoming.sourceReferences]),
    affectedInstances: dedupeJson([...previous.affectedInstances, ...incoming.affectedInstances]),
    references: [...new Set([...previous.references, ...incoming.references])].sort(),
    tags: [...new Set([...previous.tags, ...incoming.tags])].sort()
  };
}

async function buildEvidenceManifest(
  root: string,
  workspace: EvidenceWorkspaceDocument,
  findings: EvidenceFindingsDocument
): Promise<EvidenceManifest> {
  const audit = await auditChain(join(root, EVIDENCE_AUDIT_LOG_FILE));
  const payload: EvidenceManifest = {
    schemaVersion: EVIDENCE_MANIFEST_SCHEMA_VERSION,
    manifestId: "",
    generatedAt: utcNow(),
    workspaceSchemaVersion: workspace.schemaVersion,
    findingsSchemaVersion: findings.schemaVersion,
    artifacts: await collectManifestArtifacts(root),
    auditChain: audit.entries,
    auditChainHead: audit.head,
    limitations: [
      "This evidence manifest uses local SHA-256 digests.",
      "It proves local artifact integrity, not external identity or timestamping."
    ]
  };
  return { ...payload, manifestId: evidenceManifestId(payload) };
}

async function collectManifestArtifacts(
  root: string
): Promise<readonly EvidenceManifestArtifact[]> {
  const files = await collectEvidenceBundleRelativeFiles(root);
  const artifacts = await Promise.all(
    files.map(async (file): Promise<EvidenceManifestArtifact> => {
      const path = evidenceWorkspacePath(root, file);
      const metadata = await stat(path);
      return {
        path: file,
        sha256: await sha256File(path),
        size: metadata.size,
        role: evidenceArtifactRole(file)
      };
    })
  );
  return artifacts.sort((left, right) => left.path.localeCompare(right.path));
}

async function collectEvidenceBundleRelativeFiles(root: string): Promise<readonly string[]> {
  const fixedCandidates = [
    EVIDENCE_WORKSPACE_FILE,
    EVIDENCE_INDEX_FILE,
    EVIDENCE_FINDINGS_FILE,
    EVIDENCE_AUDIT_LOG_FILE
  ];
  const fixed = (
    await Promise.all(
      fixedCandidates.map(async (file) => ((await fileExists(join(root, file))) ? file : undefined))
    )
  ).filter((file): file is string => Boolean(file));
  const rawFiles = await listFilesRecursive(join(root, "raw"), root);
  const reportFiles = await listFilesRecursive(join(root, "reports"), root);
  const latestManifest = await latestEvidenceManifestPath(root);
  return [
    ...fixed,
    ...rawFiles,
    ...reportFiles,
    ...(latestManifest ? [relative(root, latestManifest)] : [])
  ].sort((left, right) => left.localeCompare(right));
}

function evidenceArtifactRole(path: string): EvidenceManifestArtifact["role"] {
  if (path === EVIDENCE_WORKSPACE_FILE) {
    return "workspace";
  }
  if (path === EVIDENCE_FINDINGS_FILE) {
    return "findings";
  }
  if (path === EVIDENCE_INDEX_FILE) {
    return "evidence";
  }
  if (path === EVIDENCE_AUDIT_LOG_FILE) {
    return "audit-log";
  }
  if (path.startsWith("raw/")) {
    return "raw-input";
  }
  if (path.startsWith("reports/")) {
    return "report";
  }
  return "signature";
}

function evidenceManifestId(manifest: EvidenceManifest): string {
  return `sha256:${createHash("sha256")
    .update(stableJsonStringify({ ...manifest, manifestId: "" } as unknown as JsonValue))
    .digest("hex")}`;
}

async function auditChain(path: string): Promise<{
  readonly entries: readonly EvidenceAuditChainEntry[];
  readonly head: string;
}> {
  if (!(await fileExists(path))) {
    return { entries: [], head: "0".repeat(64) };
  }
  const lines = (await readFile(path, "utf8")).split(/\r?\n/u).filter(Boolean);
  let previousHash = "0".repeat(64);
  const entries: EvidenceAuditChainEntry[] = [];
  for (const [index, line] of lines.entries()) {
    const event = jsonRecord(JSON.parse(line) as unknown, `audit line ${index + 1}`);
    const eventHash = createHash("sha256").update(`${previousHash}\n${line}`, "utf8").digest("hex");
    entries.push({
      line: index + 1,
      previousHash,
      eventHash,
      ...(stringField(event, "command") ? { command: stringField(event, "command") } : {}),
      ...(stringField(event, "timestamp") ? { timestamp: stringField(event, "timestamp") } : {})
    });
    previousHash = eventHash;
  }
  return { entries, head: previousHash };
}

async function latestEvidenceManifestPath(root: string): Promise<string | undefined> {
  const signaturesRoot = join(root, "signatures");
  if (!(await fileExists(signaturesRoot))) {
    return undefined;
  }
  const entries = await readdir(signaturesRoot);
  const manifests = entries
    .filter((entry) => /^manifest-sha256:[a-f0-9]{64}\.json$/u.test(entry))
    .sort((left, right) => left.localeCompare(right));
  const selected = manifests.at(-1);
  return selected ? join(signaturesRoot, selected) : undefined;
}

function classifyRetestFinding(
  baseline: NormalizedEvidenceFinding,
  current: NormalizedEvidenceFinding,
  matchedBy: "id" | "fallback"
): EvidenceRetestFinding {
  const severityDelta = severityRank(current.severity) - severityRank(baseline.severity);
  const status: EvidenceRetestStatus =
    severityDelta > 0
      ? "regressed"
      : severityDelta < 0 || materialFindingHash(baseline) !== materialFindingHash(current)
        ? "changed"
        : "open";
  return {
    findingId: current.id,
    status,
    title: current.title,
    ...(current.asset ? { asset: current.asset } : {}),
    baselineId: baseline.id,
    currentId: current.id,
    matchedBy,
    details: {
      baselineSeverity: baseline.severity,
      currentSeverity: current.severity
    }
  };
}

function fallbackKey(finding: NormalizedEvidenceFinding): string {
  return [finding.title, finding.asset ?? "", finding.weaknessIds.join(",")]
    .join("\0")
    .toLowerCase();
}

function materialFindingHash(finding: NormalizedEvidenceFinding): string {
  return hashJson({
    title: finding.title,
    severity: finding.severity,
    asset: finding.asset,
    weaknessIds: finding.weaknessIds,
    affectedInstances: finding.affectedInstances
  });
}

async function evidenceWorkspaceDigest(root: string): Promise<string> {
  const files = await collectEvidenceBundleRelativeFiles(root);
  const entries = await Promise.all(
    files.map(async (file) => [file, await sha256File(evidenceWorkspacePath(root, file))] as const)
  );
  return hashJson(Object.fromEntries(entries));
}

function sarifRulesById(run: JsonRecord): Readonly<Record<string, JsonRecord>> {
  const tool = jsonRecord(run.tool);
  const driver = jsonRecord(tool.driver);
  const rules = Array.isArray(driver.rules) ? driver.rules : [];
  return Object.fromEntries(
    rules
      .map((rule: unknown) => jsonRecord(rule))
      .filter((rule: JsonRecord) => typeof rule.id === "string")
      .map((rule: JsonRecord) => [String(rule.id), rule])
  );
}

function sarifToolName(run: JsonRecord): string | undefined {
  const tool = jsonRecord(run.tool);
  const driver = jsonRecord(tool.driver);
  return stringField(driver, "name");
}

function sarifPrimaryLocation(
  result: JsonRecord
): { readonly uri?: string; readonly startLine?: number } | undefined {
  const locations = Array.isArray(result.locations) ? result.locations : [];
  for (const locationValue of locations) {
    const location = jsonRecord(locationValue);
    const physical = jsonRecord(location.physicalLocation);
    const artifact = jsonRecord(physical.artifactLocation);
    const region = jsonRecord(physical.region);
    const uri = stringField(artifact, "uri");
    const startLine = typeof region.startLine === "number" ? region.startLine : undefined;
    if (uri || startLine !== undefined) {
      return {
        ...(uri ? { uri } : {}),
        ...(startLine !== undefined ? { startLine } : {})
      };
    }
  }
  return undefined;
}

function sarifMessageText(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value;
  }
  const record = jsonRecord(value);
  return stringField(record, "text") ?? stringField(record, "markdown");
}

function sarifSeverity(result: JsonRecord, rule: JsonRecord): EvidenceSeverity {
  const level =
    stringField(result, "level") ?? stringField(jsonRecord(rule.defaultConfiguration), "level");
  if (level === "error") {
    return "high";
  }
  if (level === "warning") {
    return "medium";
  }
  if (level === "note") {
    return "low";
  }
  return "info";
}

function sarifWeaknessIds(rule: JsonRecord, result: JsonRecord): readonly string[] {
  const tags = [
    ...stringArrayField(jsonRecord(rule.properties), "tags"),
    ...stringArrayField(jsonRecord(result.properties), "tags")
  ];
  return [
    ...new Set(tags.filter((tag) => /^CWE-\d+$/iu.test(tag)).map((tag) => tag.toUpperCase()))
  ].sort();
}

function sarifReferences(rule: JsonRecord): readonly string[] {
  const helpUri = stringField(rule, "helpUri");
  const properties = jsonRecord(rule.properties);
  const refs = [
    ...(helpUri ? [helpUri] : []),
    ...stringArrayField(properties, "references")
  ].filter((value) => /^https?:\/\//iu.test(value));
  return [...new Set(refs)].sort();
}

function locationLabel(
  location: { readonly uri?: string; readonly startLine?: number } | undefined
): string | undefined {
  if (!location?.uri) {
    return undefined;
  }
  return location.startLine !== undefined ? `${location.uri}:${location.startLine}` : location.uri;
}

async function ensureEvidenceWorkspaceDirectories(root: string): Promise<void> {
  for (const directory of ["raw", "normalized", "reports", "signatures", "evidence"]) {
    await mkdir(join(root, directory), { recursive: true });
  }
}

function evidenceWorkspacePath(
  root: string,
  relativePath: string,
  allowedRoots?: readonly string[]
): string {
  if (relativePath.startsWith("/")) {
    throw new EvidenceWorkspaceError(`evidence workspace path must be relative: ${relativePath}`);
  }
  const parts = relativePath.split(/[\\/]+/u);
  if (parts.some((part) => !part || part === "." || part === "..")) {
    throw new EvidenceWorkspaceError(
      `evidence workspace path cannot contain traversal: ${relativePath}`
    );
  }
  if (allowedRoots && !allowedRoots.includes(parts[0] ?? "")) {
    throw new EvidenceWorkspaceError(
      `evidence workspace path must be under: ${allowedRoots.join(", ")}`
    );
  }
  const workspaceRoot = resolve(root);
  const candidate = resolve(workspaceRoot, relativePath);
  if (relative(workspaceRoot, candidate).startsWith("..")) {
    throw new EvidenceWorkspaceError(`evidence workspace path escapes root: ${relativePath}`);
  }
  return candidate;
}

async function appendEvidenceAuditEvent(root: string, event: EvidenceAuditEvent): Promise<void> {
  const logPath = join(root, EVIDENCE_AUDIT_LOG_FILE);
  await mkdir(dirname(logPath), { recursive: true });
  const existing = (await readFile(logPath, "utf8").catch(() => "")) || "";
  await writeFile(logPath, `${existing}${stableJsonLine(event)}\n`, "utf8");
}

async function saveWorkspaceDocument(
  root: string,
  workspace: EvidenceWorkspaceDocument
): Promise<void> {
  await writeJson(join(root, EVIDENCE_WORKSPACE_FILE), workspace);
}

async function touchWorkspaceUpdatedAt(root: string): Promise<void> {
  const state = await loadEvidenceWorkspace(root);
  await saveWorkspaceDocument(root, { ...state.workspace, updatedAt: utcNow() });
}

async function ensureJsonFile(path: string, value: JsonValue): Promise<void> {
  if (!(await fileExists(path))) {
    await writeJson(path, value);
  }
}

async function ensureTextFile(path: string, value: string): Promise<void> {
  if (!(await fileExists(path))) {
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, value, "utf8");
  }
}

async function writeJson(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${stableJsonStringify(value as JsonValue)}\n`, "utf8");
}

async function readJsonFile<T>(path: string): Promise<T> {
  return JSON.parse(await readFile(path, "utf8")) as T;
}

async function listFilesRecursive(root: string, relativeRoot: string): Promise<readonly string[]> {
  if (!(await fileExists(root))) {
    return [];
  }
  const files: string[] = [];
  const entries = await readdir(root, { withFileTypes: true });
  for (const entry of entries) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listFilesRecursive(path, relativeRoot)));
    } else if (entry.isFile()) {
      files.push(relative(relativeRoot, path));
    }
  }
  return files.sort((left, right) => left.localeCompare(right));
}

async function fileExists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

async function sha256File(path: string): Promise<string> {
  const digest = createHash("sha256");
  digest.update(await readFile(path));
  return digest.digest("hex");
}

function deterministicEvidenceId(...parts: readonly string[]): string {
  return `evidence:${createHash("sha256")
    .update(parts.map((part) => part.trim().toLowerCase()).join("\x1f"))
    .digest("hex")
    .slice(0, 24)}`;
}

function hashJson(value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(stableJsonStringify(value as JsonValue))
    .digest("hex")}`;
}

function stableJsonLine(value: unknown): string {
  return JSON.stringify(JSON.parse(stableJsonStringify(value as JsonValue)));
}

function safeFilename(name: string): string {
  const cleaned = name.replace(/[^A-Za-z0-9._-]/gu, "-").replace(/^[.-]+|[.-]+$/gu, "");
  return cleaned || "evidence";
}

function utcNow(): string {
  return new Date().toISOString();
}

function jsonRecord(value: unknown, label = "value"): JsonRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    if (label === "value") {
      return {};
    }
    throw new EvidenceWorkspaceError(`${label} must be a JSON object`);
  }
  return value as JsonRecord;
}

function stringField(record: JsonRecord, field: string): string | undefined {
  const value = record[field];
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function stringArrayField(record: JsonRecord, field: string): readonly string[] {
  const value = record[field];
  return Array.isArray(value)
    ? value.filter((entry): entry is string => typeof entry === "string")
    : [];
}

function dedupeJson<T>(items: readonly T[]): readonly T[] {
  const seen = new Set<string>();
  const deduped: T[] = [];
  for (const item of items) {
    const key = stableJsonStringify(item as JsonValue);
    if (!seen.has(key)) {
      seen.add(key);
      deduped.push(item);
    }
  }
  return deduped;
}

function maxSeverity(left: EvidenceSeverity, right: EvidenceSeverity): EvidenceSeverity {
  return severityRank(left) >= severityRank(right) ? left : right;
}

function severityRank(severity: EvidenceSeverity): number {
  return { info: 0, low: 1, medium: 2, high: 3, critical: 4 }[severity];
}

function maxConfidence(left: EvidenceConfidence, right: EvidenceConfidence): EvidenceConfidence {
  return confidenceRank(left) >= confidenceRank(right) ? left : right;
}

function confidenceRank(confidence: EvidenceConfidence): number {
  return { info: 0, "tool-observed": 1, low: 2, medium: 3, high: 4, confirmed: 5 }[confidence];
}

function markdownCell(value: string): string {
  return value.replace(/\|/gu, "\\|").replace(/\r?\n/gu, "<br>");
}
