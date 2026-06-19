import {
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign as signBytes
} from "node:crypto";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

type JsonValue = string | number | boolean | null | readonly JsonValue[] | JsonRecord;
type JsonRecord = { readonly [key: string]: JsonValue };
type ScopeTarget = {
  readonly type: "cidr" | "host" | "ip" | "url";
  readonly value: string;
  readonly ports?: readonly number[];
};
type ControlPlaneKey = {
  readonly algorithm: "ed25519";
  readonly publicKeyPem: string;
  readonly privateKeyPem: string;
};

export function piCliHelp(): string {
  return "Manage local Kelp Pi operator commands.";
}

export async function runPiCliCommand(args: readonly string[] = []): Promise<JsonRecord> {
  const [command, ...rest] = args;
  if (command === undefined || command === "--help" || command === "-h" || command === "help") {
    return {
      ok: true,
      name: "kelp-claw pi",
      usage: "kelp-claw pi <command> [options]",
      description: piCliHelp(),
      commands: [
        {
          name: "approve",
          usage: "kelp-claw pi approve TOKEN [--data-dir PATH] [--agent-bin PATH]"
        },
        {
          name: "bundle fetch",
          usage:
            "kelp-claw pi bundle fetch --bundle-id ID --out PATH [--run-id ID] [--cp-key PATH] [--data-dir PATH] [--agent-bin PATH]"
        },
        {
          name: "scope set",
          usage:
            "kelp-claw pi scope set (--cidr CIDR|--host HOST|--ip IP|--url URL)... --until RFC3339 [--port PORT...] [--scope-id ID] [--from RFC3339] [--cp-key PATH] [--data-dir PATH] [--agent-bin PATH]"
        },
        {
          name: "wipe",
          usage: "kelp-claw pi wipe --force [--data-dir PATH] [--agent-bin PATH]"
        }
      ]
    };
  }
  if (command === "approve") {
    return approveCommand(rest);
  }
  if (command === "bundle") {
    return bundleCommand(rest);
  }
  if (command === "scope") {
    return scopeCommand(rest);
  }
  if (command === "wipe") {
    return wipeCommand(rest);
  }
  throw new Error("Usage: kelp-claw pi <approve|bundle|scope|wipe|--help>");
}

async function approveCommand(args: readonly string[]): Promise<JsonRecord> {
  const token = requiredPositional(
    args,
    0,
    "Usage: kelp-claw pi approve TOKEN [--data-dir PATH] [--agent-bin PATH]"
  );
  const dataDir = option(args, "--data-dir");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const forwarded = ["approve", token, ...(dataDir ? ["--data-dir", dataDir] : [])];
  return runAgent(agentBin, forwarded);
}

async function wipeCommand(args: readonly string[]): Promise<JsonRecord> {
  if (!hasFlag(args, "--force")) {
    throw new Error("Usage: kelp-claw pi wipe --force [--data-dir PATH] [--agent-bin PATH]");
  }
  const dataDir = option(args, "--data-dir");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const forwarded = ["wipe", "--force", ...(dataDir ? ["--data-dir", dataDir] : [])];
  return runAgent(agentBin, forwarded);
}

async function bundleCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...rest] = args;
  if (command !== "fetch") {
    throw new Error("Usage: kelp-claw pi bundle fetch --bundle-id ID --out PATH");
  }
  return bundleFetchCommand(rest);
}

async function bundleFetchCommand(args: readonly string[]): Promise<JsonRecord> {
  const bundleId = option(args, "--bundle-id");
  const outDir = option(args, "--out");
  if (!bundleId || !outDir) {
    throw new Error("Usage: kelp-claw pi bundle fetch --bundle-id ID --out PATH");
  }
  const runId = option(args, "--run-id");
  const issuedAt = rfc3339Seconds(new Date());
  const requestId = option(args, "--request-id") ?? `bundle.fetch.${bundleId}.${Date.now()}`;
  const keyPath = option(args, "--cp-key") ?? ".kelpclaw/pi/control-plane-ed25519.json";
  const key = await loadOrCreateControlPlaneKey(keyPath);
  const envelope = signEnvelope(
    {
      msg_id: requestId,
      ts: issuedAt,
      sender: "cp",
      kind: "bundle.fetch",
      payload: {
        request_id: requestId,
        bundle_id: bundleId,
        ...(runId ? { run_id: runId } : {})
      }
    },
    key
  );
  const publicKeyHex = publicRawHex(key.publicKeyPem);
  const dataDir = option(args, "--data-dir");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const result = await runChild(
    agentBin,
    [
      "wire",
      "--stdio",
      "--trusted-cp-public-key-hex",
      publicKeyHex,
      ...(dataDir ? ["--data-dir", dataDir] : [])
    ],
    `${JSON.stringify(envelope)}\n`
  );
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${agentBin} exited ${result.code}`);
  }
  const response = parseWireResponse(result.stdout, "bundle.fetch");
  const payload = response.payload;
  if (!isRecord(payload)) {
    throw new Error(`${agentBin} returned bundle.fetch without object payload`);
  }
  const files = payload.files;
  if (!Array.isArray(files)) {
    throw new Error(`${agentBin} returned bundle.fetch without files`);
  }
  await mkdir(outDir, { recursive: true });
  const written: string[] = [];
  for (const file of files) {
    if (!isRecord(file)) {
      throw new Error("bundle.fetch file entry is not an object");
    }
    const path = stringField(file, "path");
    const contentBase64 = stringField(file, "content_base64");
    if (!path || !contentBase64 || !isSafeBundlePath(path)) {
      throw new Error(`unsafe or incomplete bundle.fetch file entry: ${path ?? "<missing>"}`);
    }
    const absolutePath = join(outDir, ...path.split("/"));
    await mkdir(dirname(absolutePath), { recursive: true });
    await writeFile(absolutePath, Buffer.from(contentBase64, "base64"));
    written.push(path);
  }
  const responseRunId = stringField(payload, "run_id");
  return {
    ok: true,
    bundleId,
    ...(responseRunId ? { runId: responseRunId } : {}),
    bundleDir: outDir,
    manifestHash: stringField(payload, "manifest_hash") ?? "",
    sizeBytes: numberField(payload, "size_bytes") ?? 0,
    files: written,
    response
  };
}

async function scopeCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...rest] = args;
  if (command !== "set") {
    throw new Error(
      "Usage: kelp-claw pi scope set (--cidr CIDR|--host HOST|--ip IP|--url URL)... --until RFC3339"
    );
  }
  return scopeSetCommand(rest);
}

async function scopeSetCommand(args: readonly string[]): Promise<JsonRecord> {
  const until = option(args, "--until");
  if (!until) {
    throw new Error("Usage: kelp-claw pi scope set ... --until RFC3339");
  }
  const ports = options(args, "--port").map(parsePort);
  const targets = [
    ...options(args, "--cidr").map((value) => scopeTarget("cidr", value, ports)),
    ...options(args, "--host").map((value) => scopeTarget("host", value, ports)),
    ...options(args, "--ip").map((value) => scopeTarget("ip", value, ports)),
    ...options(args, "--url").map((value) => scopeTarget("url", value, ports))
  ];
  if (targets.length === 0) {
    throw new Error("scope set requires at least one --cidr, --host, --ip, or --url");
  }
  const issuedAt = rfc3339Seconds(new Date());
  const validFrom = normalizeRfc3339(option(args, "--from") ?? issuedAt);
  const validUntil = normalizeRfc3339(until);
  const scopeId = option(args, "--scope-id") ?? `scope-${issuedAt.replace(/[^0-9TZ]/gu, "")}`;
  const keyPath = option(args, "--cp-key") ?? ".kelpclaw/pi/control-plane-ed25519.json";
  const key = await loadOrCreateControlPlaneKey(keyPath);
  const payload = {
    scope_id: scopeId,
    issued_at: issuedAt,
    valid_from: validFrom,
    valid_until: validUntil,
    targets
  };
  const envelope = signEnvelope(
    {
      msg_id: `scope.set.${scopeId}.${Date.now()}`,
      ts: issuedAt,
      sender: "cp",
      kind: "scope.set",
      payload
    },
    key
  );
  const publicKeyHex = publicRawHex(key.publicKeyPem);
  const dataDir = option(args, "--data-dir");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const result = await runChild(
    agentBin,
    [
      "wire",
      "--stdio",
      "--trusted-cp-public-key-hex",
      publicKeyHex,
      ...(dataDir ? ["--data-dir", dataDir] : [])
    ],
    `${JSON.stringify(envelope)}\n`
  );
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${agentBin} exited ${result.code}`);
  }
  const response = JSON.parse(result.stdout.trim()) as unknown;
  if (!isRecord(response)) {
    throw new Error(`${agentBin} returned non-object JSON`);
  }
  return {
    ok: true,
    scopeId,
    targetCount: targets.length,
    validFrom,
    validUntil,
    controlPlanePublicKeyHex: publicKeyHex,
    response
  };
}

async function runAgent(command: string, args: readonly string[]): Promise<JsonRecord> {
  const result = await runChild(command, args);
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${command} exited ${result.code}`);
  }
  const output = result.stdout.trim();
  if (!output) {
    return { ok: true };
  }
  const parsed = JSON.parse(output) as unknown;
  if (!isRecord(parsed)) {
    throw new Error(`${command} returned non-object JSON`);
  }
  return parsed;
}

function runChild(
  command: string,
  args: readonly string[],
  stdin?: string
): Promise<{ readonly code: number | null; readonly stdout: string; readonly stderr: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, [...args], {
      stdio: [stdin === undefined ? "ignore" : "pipe", "pipe", "pipe"]
    });
    let stdout = "";
    let stderr = "";
    child.stdout!.setEncoding("utf8");
    child.stderr!.setEncoding("utf8");
    child.stdout!.on("data", (chunk: string) => {
      stdout += chunk;
    });
    child.stderr!.on("data", (chunk: string) => {
      stderr += chunk;
    });
    if (stdin !== undefined) {
      child.stdin!.end(stdin);
    }
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
}

function parseWireResponse(stdout: string, kind: string): JsonRecord {
  const lines = stdout
    .trim()
    .split(/\n/u)
    .map((line) => line.trim())
    .filter(Boolean);
  for (const line of lines) {
    const parsed = JSON.parse(line) as unknown;
    if (isRecord(parsed) && parsed.kind === kind) {
      return parsed;
    }
  }
  throw new Error(`wire response did not include ${kind}`);
}

function requiredPositional(args: readonly string[], index: number, usage: string): string {
  const positional = args.filter((value, valueIndex) => {
    if (value.startsWith("-")) {
      return false;
    }
    const previous = args[valueIndex - 1];
    return previous !== "--data-dir" && previous !== "--agent-bin";
  });
  const value = positional[index];
  if (!value) {
    throw new Error(usage);
  }
  return value;
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  if (index === -1) {
    return undefined;
  }
  const value = args[index + 1];
  if (!value) {
    throw new Error(`${name} requires a value`);
  }
  return value;
}

function options(args: readonly string[], name: string): readonly string[] {
  const values: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] !== name) {
      continue;
    }
    const value = args[index + 1];
    if (!value) {
      throw new Error(`${name} requires a value`);
    }
    values.push(value);
  }
  return values;
}

function hasFlag(args: readonly string[], name: string): boolean {
  return args.includes(name);
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(record: JsonRecord, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" ? value : undefined;
}

function numberField(record: JsonRecord, key: string): number | undefined {
  const value = record[key];
  return typeof value === "number" ? value : undefined;
}

function isSafeBundlePath(value: string): boolean {
  if (value.startsWith("/") || value.includes("\\") || value.length === 0) {
    return false;
  }
  return value.split("/").every((part) => part.length > 0 && part !== "." && part !== "..");
}

function scopeTarget(
  type: ScopeTarget["type"],
  value: string,
  ports: readonly number[]
): ScopeTarget {
  return ports.length > 0 ? { type, value, ports } : { type, value };
}

function parsePort(value: string): number {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`invalid --port: ${value}`);
  }
  return port;
}

function normalizeRfc3339(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`invalid RFC3339 date-time: ${value}`);
  }
  return rfc3339Seconds(date);
}

function rfc3339Seconds(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/u, "Z");
}

async function loadOrCreateControlPlaneKey(path: string): Promise<ControlPlaneKey> {
  try {
    const existing = JSON.parse(await readFile(path, "utf8")) as Partial<ControlPlaneKey>;
    if (
      existing.algorithm === "ed25519" &&
      typeof existing.publicKeyPem === "string" &&
      typeof existing.privateKeyPem === "string"
    ) {
      return existing as ControlPlaneKey;
    }
  } catch (error) {
    if (!isNodeErrorWithCode(error, "ENOENT")) {
      throw error;
    }
  }
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" }
  });
  const generated: ControlPlaneKey = {
    algorithm: "ed25519",
    publicKeyPem: publicKey,
    privateKeyPem: privateKey
  };
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(generated, null, 2)}\n`, { mode: 0o600 });
  return generated;
}

function signEnvelope(unsigned: JsonRecord, key: ControlPlaneKey): JsonRecord {
  const canonical = Buffer.from(JSON.stringify(canonicalize(unsigned)));
  const signature = signBytes(null, canonical, createPrivateKey(key.privateKeyPem));
  return {
    ...unsigned,
    sig: signature.toString("base64url")
  };
}

function canonicalize(value: JsonValue): JsonValue {
  if (Array.isArray(value)) {
    return value.map(canonicalize);
  }
  if (isRecord(value)) {
    return Object.fromEntries(
      Object.entries(value)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, child]) => [key, canonicalize(child as JsonValue)])
    );
  }
  return value;
}

function publicRawHex(publicKeyPem: string): string {
  const publicDer = Buffer.from(
    createPublicKey(publicKeyPem).export({
      type: "spki",
      format: "der"
    })
  );
  return publicDer.subarray(publicDer.length - 32).toString("hex");
}

function isNodeErrorWithCode(error: unknown, code: string): boolean {
  return (
    error instanceof Error && "code" in error && (error as NodeJS.ErrnoException).code === code
  );
}
