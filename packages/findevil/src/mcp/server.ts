import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { createReadStream } from "node:fs";
import { opendir, readFile, stat } from "node:fs/promises";
import { basename, join, relative, resolve, sep } from "node:path";
import { stdin, stdout } from "node:process";

type JsonRecord = Record<string, unknown>;

export interface FindEvilMcpOptions {
  readonly evidenceRoot: string;
  readonly maxRuntimeSeconds?: number | undefined;
  readonly runner?: ReadOnlyCommandRunner | undefined;
}

export interface ReadOnlyCommandOptions {
  readonly maxRuntimeSeconds: number;
  readonly maxOutputBytes: number;
}

export interface ReadOnlyCommandResult {
  readonly command: string;
  readonly args: readonly string[];
  readonly exitCode: number | null;
  readonly signal?: NodeJS.Signals | undefined;
  readonly stdout: string;
  readonly stderr: string;
  readonly truncated: boolean;
}

export type ReadOnlyCommandRunner = (
  command: string,
  args: readonly string[],
  options: ReadOnlyCommandOptions
) => Promise<ReadOnlyCommandResult>;

export interface McpToolDefinition {
  readonly name: string;
  readonly description: string;
  readonly inputSchema: JsonRecord;
}

export interface JsonRpcRequest {
  readonly jsonrpc: "2.0";
  readonly id?: string | number | null | undefined;
  readonly method: string;
  readonly params?: JsonRecord | undefined;
}

export interface FindEvilMcpServer {
  readonly tools: readonly McpToolDefinition[];
  readonly callTool: (name: string, args: JsonRecord) => Promise<JsonRecord>;
  readonly handleRequest: (request: JsonRpcRequest) => Promise<JsonRecord>;
}

interface NormalizedMcpOptions {
  readonly evidenceRoot: string;
  readonly maxRuntimeSeconds: number;
  readonly runner: ReadOnlyCommandRunner;
}

interface EvidenceFileRecord {
  readonly path: string;
  readonly sizeBytes: number;
  readonly sha256: string;
}

interface TextSearchMatch {
  readonly path: string;
  readonly line: number;
  readonly text: string;
}

const defaultMaxRuntimeSeconds = 120;
const defaultMaxOutputBytes = 1_000_000;
const defaultMaxInventoryFiles = 10_000;
const defaultMaxSearchMatches = 100;
const defaultMaxSearchFileBytes = 10_000_000;

export const findEvilMcpTools: readonly McpToolDefinition[] = [
  {
    name: "findevil.case_inventory",
    description:
      "Read-only evidence inventory. Recursively lists evidence-root files with size and SHA-256.",
    inputSchema: objectSchema({
      path: stringSchema("Optional subdirectory under evidenceRoot.", false),
      maxFiles: numberSchema("Maximum files to return.", false)
    })
  },
  {
    name: "findevil.hash_evidence_file",
    description: "Read-only SHA-256 or MD5 hash for a single contained evidence file.",
    inputSchema: objectSchema({
      path: stringSchema("Evidence-relative file path to hash.", true),
      algorithm: enumSchema(["sha256", "md5"], "Hash algorithm. Defaults to sha256.", false)
    })
  },
  {
    name: "findevil.get_partition_table",
    description:
      "Runs Sleuth Kit mmls against a contained image path and returns parsed partition rows.",
    inputSchema: objectSchema({
      imagePath: stringSchema("Evidence-relative disk image path.", true)
    })
  },
  {
    name: "findevil.list_filesystem",
    description:
      "Runs Sleuth Kit fls in recursive/path mode against a contained image path and optional image offset.",
    inputSchema: objectSchema({
      imagePath: stringSchema("Evidence-relative disk image path.", true),
      offset: numberSchema("Filesystem start sector for fls -o.", false),
      maxOutputBytes: numberSchema("Maximum command output bytes to return.", false)
    })
  },
  {
    name: "findevil.extract_file_by_inode",
    description:
      "Runs Sleuth Kit icat against a contained image path and inode, returning a clipped preview plus hashes.",
    inputSchema: objectSchema({
      imagePath: stringSchema("Evidence-relative disk image path.", true),
      inode: stringSchema("Sleuth Kit inode/address to extract.", true),
      offset: numberSchema("Filesystem start sector for icat -o.", false),
      maxOutputBytes: numberSchema("Maximum extracted bytes to return.", false)
    })
  },
  {
    name: "findevil.search_recovered_artifacts",
    description:
      "Literal, read-only text search over recovered artifacts under evidenceRoot. Does not execute shell.",
    inputSchema: objectSchema({
      pattern: stringSchema("Case-insensitive literal text pattern.", true),
      path: stringSchema("Optional contained subdirectory to search.", false),
      maxMatches: numberSchema("Maximum matches to return.", false),
      maxFileBytes: numberSchema("Maximum size per searched file.", false)
    })
  }
];

export function createFindEvilMcpServer(options: FindEvilMcpOptions): FindEvilMcpServer {
  const normalized = normalizeMcpOptions(options);
  return {
    tools: findEvilMcpTools,
    callTool: (name, args) => callTool(normalized, name, args),
    handleRequest: (request) => handleJsonRpcRequest(normalized, request)
  };
}

export function runFindEvilMcpServer(options: FindEvilMcpOptions): void {
  const server = createFindEvilMcpServer(options);
  let buffer = Buffer.alloc(0);
  stdin.on("data", (chunk: Buffer) => {
    buffer = Buffer.concat([buffer, chunk]);
    for (;;) {
      const headerEnd = buffer.indexOf("\r\n\r\n");
      if (headerEnd < 0) {
        return;
      }
      const header = buffer.slice(0, headerEnd).toString("utf8");
      const length = Number(/^Content-Length:\s*(\d+)/imu.exec(header)?.[1] ?? 0);
      if (buffer.length < headerEnd + 4 + length) {
        return;
      }
      const body = buffer.slice(headerEnd + 4, headerEnd + 4 + length).toString("utf8");
      buffer = buffer.slice(headerEnd + 4 + length);
      void respond(server, JSON.parse(body) as JsonRpcRequest);
    }
  });
}

async function handleJsonRpcRequest(
  options: NormalizedMcpOptions,
  request: JsonRpcRequest
): Promise<JsonRecord> {
  try {
    return {
      jsonrpc: "2.0",
      id: request.id ?? null,
      result: await handleMcpMethod(options, request)
    };
  } catch (error) {
    return {
      jsonrpc: "2.0",
      id: request.id ?? null,
      error: {
        code: -32000,
        message: error instanceof Error ? error.message : String(error)
      }
    };
  }
}

async function handleMcpMethod(
  options: NormalizedMcpOptions,
  request: JsonRpcRequest
): Promise<JsonRecord> {
  switch (request.method) {
    case "initialize":
      return {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: {
          name: "kelpclaw-findevil-readonly-sift",
          version: "0.1.0"
        }
      };
    case "tools/list":
      return { tools: findEvilMcpTools };
    case "tools/call":
      return toolContent(
        await callTool(
          options,
          stringArg(request.params, "name"),
          jsonObject(request.params?.arguments)
        )
      );
    default:
      throw new Error(`Unsupported MCP method '${request.method}'.`);
  }
}

async function callTool(
  options: NormalizedMcpOptions,
  name: string,
  args: JsonRecord
): Promise<JsonRecord> {
  switch (name) {
    case "findevil.case_inventory":
      return caseInventory(options, args);
    case "findevil.hash_evidence_file":
      return hashEvidenceFile(options, args);
    case "findevil.get_partition_table":
      return getPartitionTable(options, args);
    case "findevil.list_filesystem":
      return listFilesystem(options, args);
    case "findevil.extract_file_by_inode":
      return extractFileByInode(options, args);
    case "findevil.search_recovered_artifacts":
      return searchRecoveredArtifacts(options, args);
    default:
      throw new Error(`Unknown Find Evil MCP tool '${name}'.`);
  }
}

async function caseInventory(options: NormalizedMcpOptions, args: JsonRecord): Promise<JsonRecord> {
  const startPath = await resolveContainedPath(
    options.evidenceRoot,
    optionalString(args.path) ?? "."
  );
  const maxFiles = boundedInteger(args.maxFiles, defaultMaxInventoryFiles, 1, 100_000);
  const files: EvidenceFileRecord[] = [];
  for await (const file of walkFiles(startPath, options.evidenceRoot)) {
    files.push(file);
    if (files.length >= maxFiles) {
      break;
    }
  }
  return {
    ok: true,
    evidenceRoot: options.evidenceRoot,
    scannedPath: displayPath(options.evidenceRoot, startPath),
    files,
    truncated: files.length >= maxFiles,
    readOnly: true
  };
}

async function hashEvidenceFile(
  options: NormalizedMcpOptions,
  args: JsonRecord
): Promise<JsonRecord> {
  const path = await resolveContainedPath(options.evidenceRoot, stringArg(args, "path"));
  const metadata = await stat(path);
  if (!metadata.isFile()) {
    throw new Error(
      `Hash target must be a file under evidenceRoot: ${displayPath(options.evidenceRoot, path)}`
    );
  }
  const algorithm = enumArg(args.algorithm, ["sha256", "md5"], "sha256");
  return {
    ok: true,
    path: displayPath(options.evidenceRoot, path),
    algorithm,
    digest: await hashFile(path, algorithm),
    sizeBytes: metadata.size,
    readOnly: true
  };
}

async function getPartitionTable(
  options: NormalizedMcpOptions,
  args: JsonRecord
): Promise<JsonRecord> {
  const image = await resolveContainedPath(options.evidenceRoot, stringArg(args, "imagePath"));
  const result = await runSiftCommand(options, "mmls", [image], defaultMaxOutputBytes);
  return {
    ok: result.exitCode === 0,
    imagePath: displayPath(options.evidenceRoot, image),
    command: commandEnvelope(result),
    partitions: parseMmls(result.stdout),
    readOnly: true
  };
}

async function listFilesystem(
  options: NormalizedMcpOptions,
  args: JsonRecord
): Promise<JsonRecord> {
  const image = await resolveContainedPath(options.evidenceRoot, stringArg(args, "imagePath"));
  const offset = optionalInteger(args.offset, 0, Number.MAX_SAFE_INTEGER);
  const commandArgs = ["-pr"];
  if (offset !== undefined) {
    commandArgs.push("-o", String(offset));
  }
  commandArgs.push(image);
  const result = await runSiftCommand(
    options,
    "fls",
    commandArgs,
    boundedInteger(args.maxOutputBytes, defaultMaxOutputBytes, 1_024, 20_000_000)
  );
  return {
    ok: result.exitCode === 0,
    imagePath: displayPath(options.evidenceRoot, image),
    ...(offset !== undefined ? { offset } : {}),
    command: commandEnvelope(result),
    entries: parseFls(result.stdout),
    readOnly: true
  };
}

async function extractFileByInode(
  options: NormalizedMcpOptions,
  args: JsonRecord
): Promise<JsonRecord> {
  const image = await resolveContainedPath(options.evidenceRoot, stringArg(args, "imagePath"));
  const inode = stringArg(args, "inode");
  if (!/^[A-Za-z0-9._:-]+$/u.test(inode)) {
    throw new Error("inode must be a Sleuth Kit address without shell metacharacters.");
  }
  const offset = optionalInteger(args.offset, 0, Number.MAX_SAFE_INTEGER);
  const commandArgs = [];
  if (offset !== undefined) {
    commandArgs.push("-o", String(offset));
  }
  commandArgs.push(image, inode);
  const result = await runSiftCommand(
    options,
    "icat",
    commandArgs,
    boundedInteger(args.maxOutputBytes, 128_000, 1_024, 5_000_000)
  );
  const preview = Buffer.from(result.stdout, "utf8");
  return {
    ok: result.exitCode === 0,
    imagePath: displayPath(options.evidenceRoot, image),
    inode,
    ...(offset !== undefined ? { offset } : {}),
    command: commandEnvelope(result),
    extracted: {
      sha256: createHash("sha256").update(preview).digest("hex"),
      sizeBytesReturned: preview.length,
      previewText: result.stdout.slice(0, 16_000)
    },
    readOnly: true
  };
}

async function searchRecoveredArtifacts(
  options: NormalizedMcpOptions,
  args: JsonRecord
): Promise<JsonRecord> {
  const pattern = stringArg(args, "pattern");
  const searchRoot = await resolveContainedPath(
    options.evidenceRoot,
    optionalString(args.path) ?? "."
  );
  const maxMatches = boundedInteger(args.maxMatches, defaultMaxSearchMatches, 1, 10_000);
  const maxFileBytes = boundedInteger(args.maxFileBytes, defaultMaxSearchFileBytes, 1, 100_000_000);
  const matches: TextSearchMatch[] = [];
  for await (const file of walkFilePaths(searchRoot)) {
    const metadata = await stat(file);
    if (!metadata.isFile() || metadata.size > maxFileBytes) {
      continue;
    }
    matches.push(
      ...textMatches(
        file,
        options.evidenceRoot,
        await readFile(file),
        pattern,
        maxMatches - matches.length
      )
    );
    if (matches.length >= maxMatches) {
      break;
    }
  }
  return {
    ok: true,
    searchedPath: displayPath(options.evidenceRoot, searchRoot),
    pattern,
    matches,
    truncated: matches.length >= maxMatches,
    readOnly: true
  };
}

async function runSiftCommand(
  options: NormalizedMcpOptions,
  command: string,
  args: readonly string[],
  maxOutputBytes: number
): Promise<ReadOnlyCommandResult> {
  assertAllowedReadOnlyCommand(command);
  const result = await options.runner(command, args, {
    maxRuntimeSeconds: options.maxRuntimeSeconds,
    maxOutputBytes
  });
  if (result.exitCode !== 0 && !result.truncated) {
    throw new Error(
      `${command} exited with status ${String(result.exitCode)}: ${result.stderr.trim()}`
    );
  }
  return result;
}

async function defaultRunner(
  command: string,
  args: readonly string[],
  options: ReadOnlyCommandOptions
): Promise<ReadOnlyCommandResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      shell: false,
      stdio: ["ignore", "pipe", "pipe"]
    });
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let truncated = false;
    const runtimeTimer = setTimeout(
      () => {
        truncated = true;
        child.kill("SIGTERM");
      },
      Math.ceil(options.maxRuntimeSeconds * 1000)
    );
    runtimeTimer.unref?.();

    child.stdout.on("data", (chunk: Buffer) => {
      if (stdoutBytes < options.maxOutputBytes) {
        stdoutChunks.push(chunk.slice(0, Math.max(0, options.maxOutputBytes - stdoutBytes)));
      }
      stdoutBytes += chunk.length;
      if (stdoutBytes > options.maxOutputBytes && !truncated) {
        truncated = true;
        child.kill("SIGTERM");
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      if (stderrBytes < options.maxOutputBytes) {
        stderrChunks.push(chunk.slice(0, Math.max(0, options.maxOutputBytes - stderrBytes)));
      }
      stderrBytes += chunk.length;
    });
    child.on("error", (error) => {
      clearTimeout(runtimeTimer);
      reject(error);
    });
    child.on("close", (exitCode, signal) => {
      clearTimeout(runtimeTimer);
      resolve({
        command,
        args,
        exitCode,
        ...(signal ? { signal } : {}),
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8"),
        truncated
      });
    });
  });
}

function normalizeMcpOptions(options: FindEvilMcpOptions): NormalizedMcpOptions {
  if (!options.evidenceRoot.trim()) {
    throw new Error("Find Evil MCP evidenceRoot is required.");
  }
  return {
    evidenceRoot: resolve(options.evidenceRoot),
    maxRuntimeSeconds: positiveNumber(options.maxRuntimeSeconds ?? defaultMaxRuntimeSeconds),
    runner: options.runner ?? defaultRunner
  };
}

function assertAllowedReadOnlyCommand(command: string): void {
  if (command !== "mmls" && command !== "fls" && command !== "icat") {
    throw new Error(`Command '${command}' is not exposed by the read-only Find Evil MCP server.`);
  }
}

async function resolveContainedPath(root: string, requested: string): Promise<string> {
  const candidate = resolve(root, requested);
  const rootWithSeparator = root.endsWith(sep) ? root : `${root}${sep}`;
  if (candidate !== root && !candidate.startsWith(rootWithSeparator)) {
    throw new Error(`Path escapes evidenceRoot: ${requested}`);
  }
  return candidate;
}

async function* walkFiles(startPath: string, root: string): AsyncGenerator<EvidenceFileRecord> {
  for await (const filePath of walkFilePaths(startPath)) {
    const metadata = await stat(filePath);
    if (metadata.isFile()) {
      yield {
        path: displayPath(root, filePath),
        sizeBytes: metadata.size,
        sha256: await hashFile(filePath, "sha256")
      };
    }
  }
}

async function* walkFilePaths(startPath: string): AsyncGenerator<string> {
  const metadata = await stat(startPath);
  if (metadata.isFile()) {
    yield startPath;
    return;
  }
  if (!metadata.isDirectory()) {
    return;
  }
  const directory = await opendir(startPath);
  for await (const entry of directory) {
    const child = join(startPath, entry.name);
    if (entry.isDirectory()) {
      yield* walkFilePaths(child);
    } else if (entry.isFile()) {
      yield child;
    }
  }
}

function textMatches(
  path: string,
  root: string,
  content: Buffer,
  pattern: string,
  remaining: number
): TextSearchMatch[] {
  if (remaining <= 0) {
    return [];
  }
  const lowerPattern = pattern.toLowerCase();
  const rows: TextSearchMatch[] = [];
  const text = content.toString("utf8");
  const lines = text.split(/\r?\n/u);
  for (const [index, line] of lines.entries()) {
    if (line.toLowerCase().includes(lowerPattern)) {
      rows.push({
        path: displayPath(root, path),
        line: index + 1,
        text: line.slice(0, 500)
      });
      if (rows.length >= remaining) {
        break;
      }
    }
  }
  return rows;
}

function parseMmls(stdoutText: string): readonly JsonRecord[] {
  return stdoutText.split(/\r?\n/u).flatMap((line) => {
    const match = /^\s*(\d+):\s+([^\s].*?)\s+(\d+)\s+(\d+)\s+(\d+)(?:\s+(.*))?$/u.exec(line);
    if (!match?.[1] || !match[2] || !match[3] || !match[4] || !match[5]) {
      return [];
    }
    return [
      {
        slot: Number(match[1]),
        description: match[2].trim(),
        startSector: Number(match[3]),
        endSector: Number(match[4]),
        lengthSectors: Number(match[5]),
        ...(match[6] ? { notes: match[6].trim() } : {})
      }
    ];
  });
}

function parseFls(stdoutText: string): readonly JsonRecord[] {
  return stdoutText
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(0, 10_000)
    .map((line) => {
      const match = /^([drvl-])\/([A-Za-z*]+)\s+(\d+(?:-\d+(?:-\d+)?)?):\s*(.*)$/u.exec(line);
      if (!match?.[1] || !match[2] || !match[3]) {
        return { raw: line };
      }
      return {
        kind: match[1],
        metadata: match[2],
        inode: match[3],
        name: match[4] ?? ""
      };
    });
}

function commandEnvelope(result: ReadOnlyCommandResult): JsonRecord {
  return {
    command: result.command,
    args: result.args,
    exitCode: result.exitCode,
    ...(result.signal ? { signal: result.signal } : {}),
    stdout: result.stdout,
    stderr: result.stderr,
    truncated: result.truncated,
    readOnly: true
  };
}

async function hashFile(path: string, algorithm: "sha256" | "md5"): Promise<string> {
  const hash = createHash(algorithm);
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

function toolContent(value: JsonRecord): JsonRecord {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value, null, 2)
      }
    ]
  };
}

async function respond(server: FindEvilMcpServer, request: JsonRpcRequest): Promise<void> {
  const payload = await server.handleRequest(request);
  const content = Buffer.from(JSON.stringify(payload), "utf8");
  stdout.write(`Content-Length: ${content.length}\r\n\r\n`);
  stdout.write(content);
}

function displayPath(root: string, path: string): string {
  const text = relative(root, path).split(sep).join("/");
  return text.length > 0 ? text : basename(root);
}

function objectSchema(properties: Readonly<Record<string, JsonRecord>>): JsonRecord {
  const required = Object.entries(properties)
    .filter(([, schema]) => schema.required === true)
    .map(([name]) => name);
  return {
    type: "object",
    additionalProperties: false,
    properties: Object.fromEntries(
      Object.entries(properties).map(([name, schema]) => {
        const { required: _required, ...rest } = schema;
        return [name, rest];
      })
    ),
    ...(required.length > 0 ? { required } : {})
  };
}

function stringSchema(description: string, required: boolean): JsonRecord {
  return { type: "string", description, required };
}

function numberSchema(description: string, required: boolean): JsonRecord {
  return { type: "number", description, required };
}

function enumSchema(values: readonly string[], description: string, required: boolean): JsonRecord {
  return { type: "string", enum: values, description, required };
}

function jsonObject(value: unknown): JsonRecord {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as JsonRecord) : {};
}

function stringArg(args: JsonRecord | undefined, key: string): string {
  const value = args?.[key];
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Tool argument '${key}' is required.`);
  }
  return value.trim();
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : undefined;
}

function enumArg<T extends string>(value: unknown, values: readonly T[], fallback: T): T {
  if (value === undefined) {
    return fallback;
  }
  if (typeof value === "string" && values.includes(value as T)) {
    return value as T;
  }
  throw new Error(`Expected one of ${values.join(", ")}.`);
}

function boundedInteger(value: unknown, fallback: number, min: number, max: number): number {
  const parsed = optionalInteger(value, min, max);
  return parsed ?? fallback;
}

function optionalInteger(value: unknown, min: number, max: number): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
    throw new Error(`Expected integer between ${min} and ${max}.`);
  }
  return parsed;
}

function positiveNumber(value: number): number {
  if (!Number.isFinite(value) || value <= 0) {
    throw new Error("Find Evil MCP maxRuntimeSeconds must be positive.");
  }
  return value;
}
