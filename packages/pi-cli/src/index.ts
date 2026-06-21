import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign as signBytes
} from "node:crypto";
import { spawn } from "node:child_process";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { pipeline } from "node:stream/promises";

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
type PiDoctorStatus = "pass" | "warn" | "fail";
type PiDoctorCheck = JsonRecord & {
  readonly id: string;
  readonly status: PiDoctorStatus;
  readonly required: boolean;
  readonly message: string;
  readonly details?: JsonRecord | undefined;
};
type RemotePiOptions = {
  readonly host: string;
  readonly user: string;
  readonly sshBin: string;
  readonly sshOptions: readonly string[];
  readonly agentCommand: string;
};

export function piCliHelp(): string {
  return "Manage Raspberry Pi 5 field-appliance commands.";
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
          name: "bundle import",
          usage: "kelp-claw pi bundle import --input ENVELOPE --out PATH"
        },
        {
          name: "connect",
          usage:
            "kelp-claw pi connect --host HOST [--user kelp-pi] [--agent-command kelp-pi-agent] [--dry-run]"
        },
        {
          name: "doctor",
          usage:
            "kelp-claw pi doctor [--host HOST] [--profile managed-ap] [--agent-bin PATH] [--helper-bin PATH]"
        },
        {
          name: "flash",
          usage:
            "kelp-claw pi flash --image PATH --image-sha256 SHA256 --device PATH --ssh-public-key PATH --boot-seed-dir PATH --yes"
        },
        {
          name: "policy sync",
          usage:
            "kelp-claw pi policy sync --policy-pack-id ID (--policy-file PATH|--policy-json JSON) [--trust-epoch N] [--device-id ID] [--cp-key PATH] [--data-dir PATH] [--agent-bin PATH]"
        },
        {
          name: "scope set",
          usage:
            "kelp-claw pi scope set (--cidr CIDR|--host HOST|--ip IP|--url URL)... --until RFC3339 [--port PORT...] [--scope-id ID] [--from RFC3339] [--cp-key PATH] [--data-dir PATH] [--agent-bin PATH]"
        },
        {
          name: "validate",
          usage:
            "kelp-claw pi validate --host HOST [--profile managed-ap] [--validator-command 'sudo kelp-pi validate-node'] [--dry-run]"
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
  if (command === "connect") {
    return connectCommand(rest);
  }
  if (command === "doctor") {
    return doctorCommand(rest);
  }
  if (command === "flash") {
    return flashCommand(rest);
  }
  if (command === "policy") {
    return policyCommand(rest);
  }
  if (command === "scope") {
    return scopeCommand(rest);
  }
  if (command === "validate") {
    return validateCommand(rest);
  }
  if (command === "wipe") {
    return wipeCommand(rest);
  }
  throw new Error(
    "Usage: kelp-claw pi <approve|bundle|connect|doctor|flash|policy|scope|validate|wipe|--help>"
  );
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
  if (command === "fetch") {
    return bundleFetchCommand(rest);
  }
  if (command === "import") {
    return bundleImportCommand(rest);
  }
  throw new Error("Usage: kelp-claw pi bundle <fetch|import> ...");
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
  const written = await writeBundlePayloadFiles(payload, outDir, "bundle.fetch");
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

async function bundleImportCommand(args: readonly string[]): Promise<JsonRecord> {
  const input = option(args, "--input");
  const outDir = option(args, "--out");
  if (!input || !outDir) {
    throw new Error("Usage: kelp-claw pi bundle import --input ENVELOPE --out PATH");
  }
  const envelope = JSON.parse(await readFile(input, "utf8")) as unknown;
  if (!isRecord(envelope) || envelope.kind !== "bundle.export" || !isRecord(envelope.payload)) {
    throw new Error(`${input} is not a bundle.export envelope`);
  }
  const payload = envelope.payload;
  const written = await writeBundlePayloadFiles(payload, outDir, "bundle.export");
  const responseRunId = stringField(payload, "run_id");
  return {
    ok: true,
    bundleId: stringField(payload, "bundle_id") ?? "",
    ...(responseRunId ? { runId: responseRunId } : {}),
    bundleDir: outDir,
    manifestHash: stringField(payload, "manifest_hash") ?? "",
    sizeBytes: numberField(payload, "size_bytes") ?? 0,
    files: written,
    response: envelope
  };
}

async function connectCommand(args: readonly string[]): Promise<JsonRecord> {
  const remote = remotePiOptions(args);
  const sshArgs = sshCommandArgs(remote, [remote.agentCommand, "version"]);
  if (hasFlag(args, "--dry-run")) {
    return {
      ok: true,
      host: remote.host,
      user: remote.user,
      command: [remote.sshBin, ...sshArgs],
      agentBin: sshAgentBin(remote)
    };
  }
  const result = await runChild(remote.sshBin, sshArgs);
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${remote.sshBin} exited ${result.code}`);
  }
  return {
    ok: true,
    host: remote.host,
    user: remote.user,
    agentVersion: result.stdout.trim(),
    agentBin: sshAgentBin(remote)
  };
}

async function doctorCommand(args: readonly string[]): Promise<JsonRecord> {
  const profile = option(args, "--profile") ?? process.env.KELP_PI_PROFILE ?? "managed-ap";
  const host = option(args, "--host") ?? process.env.KELP_PI_HOST;
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const helperBin = option(args, "--helper-bin") ?? process.env.KELP_PI_HELPER_BIN ?? "kelp-pi";
  const cargoBin = option(args, "--cargo-bin") ?? process.env.KELP_PI_CARGO_BIN ?? "cargo";
  const crossBin = option(args, "--cross-bin") ?? process.env.KELP_PI_CROSS_BIN ?? "cross";
  const controlEndpoint =
    option(args, "--control-endpoint") ?? process.env.KELP_PI_CONTROL_ENDPOINT;
  const releaseRepo =
    option(args, "--release-repo") ?? process.env.KELP_PI_RELEASE_REPO ?? "gongahkia/kelp";
  const releaseAsset =
    option(args, "--release-asset") ?? process.env.KELP_PI_RELEASE_ASSET ?? "kelp-pi-agent-aarch64";
  const checks: PiDoctorCheck[] = [
    profileCheck(profile),
    releaseAssetCheck(releaseRepo, releaseAsset),
    envValueCheck("KELP_PI_HOST", host, false),
    envValueCheck("KELP_PI_CONTROL_ENDPOINT", controlEndpoint, false),
    await localCommandCheck(agentBin, ["version"], {
      id: "command:kelp-pi-agent",
      required: false
    }),
    await localCommandCheck(helperBin, ["version"], {
      id: "command:kelp-pi-helper",
      required: false
    }),
    await localCommandCheck(cargoBin, ["--version"], {
      id: "command:cargo",
      required: false
    }),
    await localCommandCheck(crossBin, ["--version"], {
      id: "command:cross",
      required: false
    })
  ];
  const ok = checks.every((check) => check.status !== "fail");
  return {
    ok,
    profile,
    host: host ?? "",
    hardware: {
      minimum: "Raspberry Pi 5 4GB",
      recommended: "Raspberry Pi 5 8GB with NVMe",
      profile
    },
    checks,
    recommendations: piDoctorRecommendations(checks)
  };
}

async function flashCommand(args: readonly string[]): Promise<JsonRecord> {
  const image = option(args, "--image");
  const expectedSha256 = normalizeSha256(option(args, "--image-sha256"));
  const device = option(args, "--device");
  const sshPublicKeyPath = option(args, "--ssh-public-key");
  const bootSeedDir = option(args, "--boot-seed-dir");
  if (!image || !expectedSha256 || !device || !sshPublicKeyPath || !bootSeedDir) {
    throw new Error(
      "Usage: kelp-claw pi flash --image PATH --image-sha256 SHA256 --device PATH --ssh-public-key PATH --boot-seed-dir PATH --yes"
    );
  }
  if (!hasFlag(args, "--yes")) {
    throw new Error("kelp-claw pi flash requires --yes before writing a device");
  }
  if (image === device) {
    throw new Error("--image and --device must not be the same path");
  }
  const foundSha256 = await sha256File(image);
  if (foundSha256 !== expectedSha256) {
    throw new Error(
      `image hash mismatch: expected sha256:${expectedSha256}, found sha256:${foundSha256}`
    );
  }
  const sshPublicKey = (await readFile(sshPublicKeyPath, "utf8")).trim();
  if (!isSshPublicKey(sshPublicKey)) {
    throw new Error("--ssh-public-key must contain an SSH public key");
  }
  await mkdir(bootSeedDir, { recursive: true });
  await pipeline(createReadStream(image), createWriteStream(device, { flags: "w", mode: 0o600 }));
  await writeFile(join(bootSeedDir, "ssh"), "", { mode: 0o644 });
  await writeFile(join(bootSeedDir, "authorized_keys"), `${sshPublicKey}\n`, {
    mode: 0o600
  });
  await writeJson(join(bootSeedDir, "kelp-pi-flash.json"), {
    schemaVersion: "kelpclaw.pi.flash.v1",
    image,
    device,
    imageSha256: `sha256:${foundSha256}`,
    sshAuthorizedKeys: "authorized_keys"
  });
  return {
    ok: true,
    image,
    device,
    imageSha256: `sha256:${foundSha256}`,
    bootSeedDir,
    seededFiles: ["ssh", "authorized_keys", "kelp-pi-flash.json"]
  };
}

async function policyCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...rest] = args;
  if (command === "sync") {
    return policySyncCommand(rest);
  }
  throw new Error("Usage: kelp-claw pi policy sync --policy-pack-id ID ...");
}

async function policySyncCommand(args: readonly string[]): Promise<JsonRecord> {
  const policyPackId = option(args, "--policy-pack-id");
  if (!policyPackId) {
    throw new Error("Usage: kelp-claw pi policy sync --policy-pack-id ID ...");
  }
  const policy = await readPolicyInput(args);
  const issuedAt = rfc3339Seconds(new Date());
  const keyPath = option(args, "--cp-key") ?? ".kelpclaw/pi/control-plane-ed25519.json";
  const key = await loadOrCreateControlPlaneKey(keyPath);
  const publicKeyHex = publicRawHex(key.publicKeyPem);
  const dataDir = option(args, "--data-dir");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const deviceId = option(args, "--device-id");
  const pull = await runAgent(agentBin, [
    "policy",
    "pull",
    ...(deviceId ? ["--device-id", deviceId] : []),
    ...(dataDir ? ["--data-dir", dataDir] : [])
  ]);
  if (pull.kind !== "policy.pull" || !isRecord(pull.payload)) {
    throw new Error(`${agentBin} returned invalid policy.pull envelope`);
  }
  const pullPayload = pull.payload;
  const pulledDeviceId = stringField(pullPayload, "device_id") ?? deviceId ?? "";
  const knownPolicyPacks = arrayOfStrings(pullPayload.known_policy_packs);
  const priorTrustEpoch = numberField(pullPayload, "trust_epoch") ?? 0;
  const trustEpoch = numberOption(args, "--trust-epoch") ?? priorTrustEpoch + 1;
  const policyHash = sha256Json(policy);
  const payload = {
    policy_pack_id: policyPackId,
    policy_hash: policyHash,
    trust_epoch: trustEpoch,
    issued_at: issuedAt,
    trust_list: [
      {
        key_id: `sha256:${createHash("sha256").update(Buffer.from(publicKeyHex, "hex")).digest("hex")}`,
        device_id: pulledDeviceId || "kelp-pi",
        state: "trusted"
      }
    ],
    policy
  };
  const envelope = signEnvelope(
    {
      msg_id: `policy.push.${policyPackId}.${Date.now()}`,
      ts: issuedAt,
      sender: "cp",
      kind: "policy.push",
      payload
    },
    key
  );
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
  const response = parseWireResponse(result.stdout, "policy.push");
  return {
    ok: true,
    policyPackId,
    policyHash,
    trustEpoch,
    knownPolicyPacks,
    controlPlanePublicKeyHex: publicKeyHex,
    pull,
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

async function validateCommand(args: readonly string[]): Promise<JsonRecord> {
  const profile = option(args, "--profile") ?? process.env.KELP_PI_PROFILE ?? "managed-ap";
  if (profile !== "managed-ap") {
    throw new Error("kelp-claw pi validate currently supports --profile managed-ap");
  }
  const remote = remotePiOptions(args);
  const validatorCommand = option(args, "--validator-command") ?? "sudo kelp-pi validate-node";
  const sshArgs = sshCommandArgs(remote, splitCommand(validatorCommand));
  if (hasFlag(args, "--dry-run")) {
    return {
      ok: true,
      profile,
      host: remote.host,
      user: remote.user,
      command: [remote.sshBin, ...sshArgs],
      acceptance:
        "managed-ap requires node, WPA3 AP, AP isolation, DNS sinkhole, outbound allowlist, scoped scan, and bundle verification evidence"
    };
  }
  const result = await runChild(remote.sshBin, sshArgs);
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${remote.sshBin} exited ${result.code}`);
  }
  return {
    ok: true,
    profile,
    host: remote.host,
    user: remote.user,
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim()
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

async function localCommandCheck(
  command: string,
  args: readonly string[],
  options: { readonly id: string; readonly required: boolean }
): Promise<PiDoctorCheck> {
  try {
    const result = await runChild(command, args);
    if (result.code === 0) {
      return {
        id: options.id,
        status: "pass",
        required: options.required,
        message: `${command} is available.`,
        details: { command, args, stdout: result.stdout.trim() }
      };
    }
    return {
      id: options.id,
      status: options.required ? "fail" : "warn",
      required: options.required,
      message: `${command} exited with ${result.code ?? "unknown status"}.`,
      details: { command, args, stderr: result.stderr.trim() }
    };
  } catch (error) {
    return {
      id: options.id,
      status: options.required ? "fail" : "warn",
      required: options.required,
      message: `${command} is unavailable.`,
      details: { command, args, error: error instanceof Error ? error.message : String(error) }
    };
  }
}

function profileCheck(profile: string): PiDoctorCheck {
  const ok = profile === "managed-ap";
  return {
    id: "pi-profile",
    status: ok ? "pass" : "fail",
    required: true,
    message: ok ? "Managed AP is the first-class Kelp Pi profile." : "Unsupported Kelp Pi profile.",
    details: { profile, supported: ["managed-ap"] }
  };
}

function releaseAssetCheck(releaseRepo: string, releaseAsset: string): PiDoctorCheck {
  const ok = releaseRepo.length > 0 && releaseAsset === "kelp-pi-agent-aarch64";
  return {
    id: "pi-release-asset",
    status: ok ? "pass" : "fail",
    required: true,
    message: ok
      ? "Pi release asset naming is configured for aarch64."
      : "Pi release asset must default to kelp-pi-agent-aarch64.",
    details: { releaseRepo, releaseAsset }
  };
}

function envValueCheck(name: string, value: string | undefined, required: boolean): PiDoctorCheck {
  const present = Boolean(value);
  return {
    id: `env:${name}`,
    status: present ? "pass" : required ? "fail" : "warn",
    required,
    message: present
      ? `${name} is configured.`
      : required
        ? `${name} is required.`
        : `${name} is not configured; remote Pi validation will need explicit CLI args.`,
    details: { name, present }
  };
}

function piDoctorRecommendations(checks: readonly PiDoctorCheck[]): readonly string[] {
  const recommendations = new Set<string>();
  if (checks.some((check) => check.id === "env:KELP_PI_HOST" && check.status !== "pass")) {
    recommendations.add("Set KELP_PI_HOST or pass --host for SSH-on-LAN Pi validation.");
  }
  if (
    checks.some((check) => check.id === "env:KELP_PI_CONTROL_ENDPOINT" && check.status !== "pass")
  ) {
    recommendations.add("Set KELP_PI_CONTROL_ENDPOINT before managed-AP outbound validation.");
  }
  if (checks.some((check) => check.id === "command:kelp-pi-agent" && check.status !== "pass")) {
    recommendations.add("Install kelp-pi-agent on a Raspberry Pi 5 before hardware acceptance.");
  }
  if (checks.some((check) => check.id === "command:kelp-pi-helper" && check.status !== "pass")) {
    recommendations.add("Run scripts/install-kelp-pi.sh on the Pi to install the kelp-pi helper.");
  }
  if (checks.some((check) => check.id === "command:cargo" && check.status !== "pass")) {
    recommendations.add("Install Rust cargo before local Pi agent source builds.");
  }
  if (checks.some((check) => check.id === "command:cross" && check.status !== "pass")) {
    recommendations.add("Install cross before local aarch64 Pi agent release builds.");
  }
  for (const check of checks) {
    if (check.status === "fail") {
      recommendations.add(`Resolve failed Pi readiness check: ${check.id}.`);
    }
  }
  return [...recommendations];
}

function remotePiOptions(args: readonly string[]): RemotePiOptions {
  const host = option(args, "--host") ?? process.env.KELP_PI_HOST;
  if (!host) {
    throw new Error("Kelp Pi SSH commands require --host or KELP_PI_HOST.");
  }
  return {
    host,
    user: option(args, "--user") ?? process.env.KELP_PI_USER ?? "kelp-pi",
    sshBin: option(args, "--ssh-bin") ?? process.env.KELP_PI_SSH_BIN ?? "ssh",
    sshOptions: [
      ...defaultSshOptions(args),
      ...splitCommand(process.env.KELP_PI_SSH_OPTS ?? ""),
      ...options(args, "--ssh-option")
    ],
    agentCommand: option(args, "--agent-command") ?? "kelp-pi-agent"
  };
}

function defaultSshOptions(args: readonly string[]): readonly string[] {
  return hasFlag(args, "--no-default-ssh-options")
    ? []
    : ["-o", "BatchMode=yes", "-o", "ConnectTimeout=10"];
}

function sshCommandArgs(
  remote: RemotePiOptions,
  remoteCommand: readonly string[]
): readonly string[] {
  return [...remote.sshOptions, `${remote.user}@${remote.host}`, ...remoteCommand];
}

function sshAgentBin(remote: RemotePiOptions): string {
  return `${remote.sshBin} ${remote.sshOptions.join(" ")} ${remote.user}@${remote.host} ${remote.agentCommand}`.replace(
    /\s+/gu,
    " "
  );
}

function splitCommand(value: string): readonly string[] {
  return value.split(/\s+/u).filter(Boolean);
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

async function writeBundlePayloadFiles(
  payload: JsonRecord,
  outDir: string,
  kind: string
): Promise<readonly string[]> {
  const files = payload.files;
  if (!Array.isArray(files)) {
    throw new Error(`${kind} payload does not contain files`);
  }
  await mkdir(outDir, { recursive: true });
  const written: string[] = [];
  for (const file of files) {
    if (!isRecord(file)) {
      throw new Error(`${kind} file entry is not an object`);
    }
    const path = stringField(file, "path");
    const contentBase64 = stringField(file, "content_base64");
    if (!path || !contentBase64 || !isSafeBundlePath(path)) {
      throw new Error(`unsafe or incomplete ${kind} file entry: ${path ?? "<missing>"}`);
    }
    const absolutePath = join(outDir, ...path.split("/"));
    await mkdir(dirname(absolutePath), { recursive: true });
    await writeFile(absolutePath, Buffer.from(contentBase64, "base64"));
    written.push(path);
  }
  return written;
}

async function readPolicyInput(args: readonly string[]): Promise<JsonRecord> {
  const policyFile = option(args, "--policy-file");
  const policyJson = option(args, "--policy-json");
  if ((policyFile ? 1 : 0) + (policyJson ? 1 : 0) !== 1) {
    throw new Error("policy sync requires exactly one of --policy-file or --policy-json");
  }
  const parsed = JSON.parse(policyFile ? await readFile(policyFile, "utf8") : (policyJson ?? ""));
  if (!isRecord(parsed)) {
    throw new Error("policy must be a JSON object");
  }
  return parsed;
}

async function writeJson(path: string, value: JsonRecord): Promise<void> {
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
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

function numberOption(args: readonly string[], name: string): number | undefined {
  const value = option(args, name);
  if (value === undefined) {
    return undefined;
  }
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 0) {
    throw new Error(`${name} must be a non-negative integer`);
  }
  return parsed;
}

function normalizeSha256(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  const normalized = value.startsWith("sha256:") ? value.slice("sha256:".length) : value;
  if (!/^[a-f0-9]{64}$/u.test(normalized)) {
    throw new Error("--image-sha256 must be sha256:<64 hex chars> or 64 hex chars");
  }
  return normalized;
}

async function sha256File(path: string): Promise<string> {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

function isSshPublicKey(value: string): boolean {
  return /^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ecdsa-sha2-nistp521) [A-Za-z0-9+/=]+(?: .*)?$/u.test(
    value
  );
}

function arrayOfStrings(value: JsonValue | undefined): readonly string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

function sha256Json(value: JsonRecord): string {
  return `sha256:${createHash("sha256")
    .update(JSON.stringify(canonicalize(value)))
    .digest("hex")}`;
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
