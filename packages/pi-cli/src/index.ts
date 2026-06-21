import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign as signBytes
} from "node:crypto";
import { spawn } from "node:child_process";
import { createReadStream, createWriteStream, readFileSync } from "node:fs";
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
type PiLabConfig = {
  readonly schemaVersion?: string | undefined;
  readonly profile?: string | undefined;
  readonly host?: string | undefined;
  readonly sshUser?: string | undefined;
  readonly controlUrl?: string | undefined;
  readonly controlEndpoint?: string | undefined;
  readonly targetIp?: string | undefined;
  readonly targetUrl?: string | undefined;
  readonly nucleiApprovalToken?: string | undefined;
  readonly nucleiTarget?: string | undefined;
  readonly clientA?: string | undefined;
  readonly clientB?: string | undefined;
  readonly clientSshUser?: string | undefined;
  readonly portalIp?: string | undefined;
  readonly upstreamInterface?: string | undefined;
  readonly dnsProbeCommand?: string | undefined;
  readonly forbiddenIps?: readonly string[] | undefined;
  readonly nucleiArgs?: readonly string[] | undefined;
};
type ManagedApValidateOptions = {
  readonly profile: "managed-ap";
  readonly remote: RemotePiOptions;
  readonly targetIp: string;
  readonly targetUrl: string;
  readonly nucleiApprovalToken: string;
  readonly nucleiTarget: string;
  readonly controlUrl: string;
  readonly currentConfig: string;
  readonly updatedConfig: string;
  readonly sessionCommand: string;
  readonly clientA: string;
  readonly clientB: string;
  readonly clientSshUser?: string | undefined;
  readonly portalIp: string;
  readonly upstreamInterface?: string | undefined;
  readonly dnsProbeCommand?: string | undefined;
  readonly forbiddenIps: readonly string[];
  readonly nucleiArgs: readonly string[];
  readonly ollamaMode?: "load" | "refuse" | undefined;
  readonly ollamaModel?: string | undefined;
  readonly readonlyRoot: boolean;
  readonly readonlyDataDir?: string | undefined;
  readonly maxSeconds: string;
  readonly remoteOutputDir: string;
  readonly localOutDir: string;
  readonly artifactPath: string;
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
          name: "bootstrap",
          usage:
            "kelp-claw pi bootstrap --host HOST --ssh-user USER --yes [--build-from-source|--fallback-source] [--dry-run]"
        },
        {
          name: "lab init",
          usage:
            "kelp-claw pi lab init --host HOST --ssh-user USER --control-url URL --target-ip IP --target-url URL --client-a IP --client-b IP [--out .kelpclaw/pi/lab.json]"
        },
        {
          name: "lab show",
          usage: "kelp-claw pi lab show [--config .kelpclaw/pi/lab.json]"
        },
        {
          name: "recover network",
          usage:
            "kelp-claw pi recover network --force [--backup latest|DIR] [--no-restart] [--dry-run]"
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
            "kelp-claw pi doctor [--strict] [--check-release-online] [--host HOST] [--profile managed-ap]"
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
            "kelp-claw pi validate --profile managed-ap --host HOST --target-ip IP --target-url URL --nuclei-approval-token TOKEN --client-a IP --client-b IP --forbidden-ip IP [--dry-run]"
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
  if (command === "bootstrap") {
    return bootstrapCommand(rest);
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
  if (command === "lab") {
    return labCommand(rest);
  }
  if (command === "recover") {
    return recoverCommand(rest);
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
    "Usage: kelp-claw pi <approve|bootstrap|bundle|connect|doctor|flash|lab|recover|policy|scope|validate|wipe|--help>"
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
  const labConfig = loadPiLabConfig(args);
  const remote = remotePiOptions(args, labConfig);
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
  const labConfig = loadPiLabConfig(args);
  const profile =
    option(args, "--profile") ?? process.env.KELP_PI_PROFILE ?? labConfig.profile ?? "managed-ap";
  const host = option(args, "--host") ?? process.env.KELP_PI_HOST ?? labConfig.host;
  const strict = hasFlag(args, "--strict") || process.env.KELP_PI_REQUIRED === "1";
  const checkReleaseOnline = hasFlag(args, "--check-release-online");
  const agentBin = option(args, "--agent-bin") ?? process.env.KELP_PI_AGENT_BIN ?? "kelp-pi-agent";
  const helperBin = option(args, "--helper-bin") ?? process.env.KELP_PI_HELPER_BIN ?? "kelp-pi";
  const cargoBin = option(args, "--cargo-bin") ?? process.env.KELP_PI_CARGO_BIN ?? "cargo";
  const crossBin = option(args, "--cross-bin") ?? process.env.KELP_PI_CROSS_BIN ?? "cross";
  const controlEndpoint =
    option(args, "--control-endpoint") ??
    process.env.KELP_PI_CONTROL_ENDPOINT ??
    labConfig.controlEndpoint;
  const controlUrl =
    option(args, "--control-url") ??
    process.env.KELP_PI_CONTROL_URL ??
    labConfig.controlUrl ??
    endpointToControlUrl(controlEndpoint);
  const releaseRepo =
    option(args, "--release-repo") ?? process.env.KELP_PI_RELEASE_REPO ?? "gongahkia/kelp";
  const releaseAsset =
    option(args, "--release-asset") ?? process.env.KELP_PI_RELEASE_ASSET ?? "kelp-pi-agent-aarch64";
  const checks: PiDoctorCheck[] = [
    profileCheck(profile),
    releaseAssetCheck(releaseRepo, releaseAsset),
    envValueCheck("KELP_PI_HOST", host, strict),
    envValueCheck("KELP_PI_CONTROL_URL", controlUrl, strict),
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
    }),
    ...(host
      ? [
          await remotePiAgentCheck(remotePiOptions(args, labConfig), strict),
          ...(await remoteHardwareChecks(remotePiOptions(args, labConfig), strict))
        ]
      : []),
    ...(checkReleaseOnline ? await releaseAssetOnlineChecks(releaseRepo, releaseAsset, strict) : [])
  ];
  const ok = checks.every((check) => check.status !== "fail");
  return {
    ok,
    strict,
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

async function bootstrapCommand(args: readonly string[]): Promise<JsonRecord> {
  const labConfig = loadPiLabConfig(args);
  const remote = remotePiOptions(args, labConfig);
  const releaseRepo =
    option(args, "--release-repo") ?? process.env.KELP_PI_RELEASE_REPO ?? "gongahkia/kelp";
  const releaseTag = option(args, "--release-tag") ?? process.env.KELP_PI_RELEASE_TAG ?? "main";
  const installerUrl =
    option(args, "--installer-url") ??
    process.env.KELP_PI_INSTALLER_URL ??
    `https://raw.githubusercontent.com/${releaseRepo}/${releaseTag}/scripts/install-kelp-pi.sh`;
  const installerArgs = piBootstrapInstallerArgs(args);
  const remoteScript = `set -eu
curl -fsSL ${shQuote(installerUrl)} | sudo sh -s -- ${installerArgs.map(shQuote).join(" ")}
kelp-pi status
`;
  const sshArgs = sshCommandArgs(remote, ["sh", "-s"]);
  if (hasFlag(args, "--dry-run")) {
    return {
      ok: true,
      host: remote.host,
      user: remote.user,
      command: [remote.sshBin, ...sshArgs],
      installerUrl,
      installerArgs,
      remoteScript
    };
  }
  if (!hasFlag(args, "--yes")) {
    throw new Error("kelp-claw pi bootstrap requires --yes unless --dry-run is set.");
  }
  const result = await runChild(remote.sshBin, sshArgs, remoteScript);
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${remote.sshBin} exited ${result.code}`);
  }
  return {
    ok: true,
    host: remote.host,
    user: remote.user,
    command: [remote.sshBin, ...sshArgs],
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim()
  };
}

function piBootstrapInstallerArgs(args: readonly string[]): readonly string[] {
  const valueOptions = [
    "--release-repo",
    "--release-tag",
    "--release-asset",
    "--agent-url",
    "--agent-sha256",
    "--repo",
    "--ref",
    "--source-dir",
    "--device-id"
  ];
  const flagOptions = [
    "--build-from-source",
    "--fallback-source",
    "--skip-apt",
    "--skip-nuclei",
    "--skip-start",
    "--allow-non-pi5",
    "--preflight-only"
  ];
  return [
    ...valueOptions.flatMap((name) => flatOptional(name, option(args, name))),
    ...flagOptions.filter((name) => hasFlag(args, name))
  ];
}

async function labCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...rest] = args;
  if (command === "init") {
    return labInitCommand(rest);
  }
  if (command === "show") {
    return labShowCommand(rest);
  }
  throw new Error("Usage: kelp-claw pi lab <init|show> ...");
}

async function labInitCommand(args: readonly string[]): Promise<JsonRecord> {
  const path = option(args, "--out") ?? piLabConfigPath(args);
  const config = compactRecord({
    schemaVersion: "kelp.pi.lab.v1",
    profile: option(args, "--profile") ?? process.env.KELP_PI_PROFILE ?? "managed-ap",
    host: option(args, "--host") ?? process.env.KELP_PI_HOST,
    sshUser:
      option(args, "--ssh-user") ??
      option(args, "--user") ??
      process.env.KELP_PI_SSH_USER ??
      process.env.KELP_PI_USER,
    controlUrl:
      option(args, "--control-url") ??
      process.env.KELP_PI_CONTROL_URL ??
      endpointToControlUrl(
        option(args, "--control-endpoint") ?? process.env.KELP_PI_CONTROL_ENDPOINT
      ),
    targetIp: option(args, "--target-ip") ?? process.env.KELP_PI_TARGET_IP,
    targetUrl: option(args, "--target-url") ?? process.env.KELP_PI_TARGET_URL,
    nucleiApprovalToken:
      option(args, "--nuclei-approval-token") ?? process.env.KELP_PI_NUCLEI_APPROVAL_TOKEN,
    nucleiTarget: option(args, "--nuclei-target") ?? process.env.KELP_PI_NUCLEI_TARGET,
    clientA: option(args, "--client-a") ?? process.env.KELP_PI_CLIENT_A,
    clientB: option(args, "--client-b") ?? process.env.KELP_PI_CLIENT_B,
    clientSshUser: option(args, "--client-ssh-user") ?? process.env.KELP_PI_CLIENT_SSH_USER,
    portalIp: option(args, "--portal-ip") ?? process.env.KELP_PI_PORTAL_IP ?? "10.42.0.1",
    upstreamInterface:
      option(args, "--upstream-interface") ?? process.env.KELP_PI_UPSTREAM_INTERFACE,
    dnsProbeCommand: option(args, "--dns-probe-command") ?? process.env.KELP_PI_DNS_PROBE_COMMAND,
    forbiddenIps: [
      ...options(args, "--forbidden-ip"),
      ...splitList(process.env.KELP_PI_FORBIDDEN_IPS)
    ],
    nucleiArgs: [...options(args, "--nuclei-arg"), ...splitList(process.env.KELP_PI_NUCLEI_ARGS)]
  });
  await writePrivateJson(path, config);
  return {
    ok: true,
    path,
    config,
    missing: missingLabFields(config),
    next: [
      `kelp-claw pi doctor --strict --config ${path}`,
      `kelp-claw pi bootstrap --config ${path} --yes`,
      `kelp-claw pi validate --config ${path}`
    ]
  };
}

async function labShowCommand(args: readonly string[]): Promise<JsonRecord> {
  const path = piLabConfigPath(args);
  const config = loadPiLabConfig(args);
  const configJson = piLabConfigJson(config);
  return {
    ok: true,
    path,
    config: configJson,
    missing: missingLabFields(configJson)
  };
}

async function recoverCommand(args: readonly string[]): Promise<JsonRecord> {
  const [subcommand, ...rest] = args;
  if (subcommand !== "network") {
    throw new Error(
      "Usage: kelp-claw pi recover network --force [--backup latest|DIR] [--no-restart] [--dry-run]"
    );
  }
  const labConfig = loadPiLabConfig(rest);
  const remote = remotePiOptions(rest, labConfig);
  const recoverArgs = [
    "recover",
    "network",
    ...flatOptional("--backup", option(rest, "--backup")),
    ...(hasFlag(rest, "--no-restart") ? ["--no-restart"] : []),
    ...(hasFlag(rest, "--force") ? ["--force"] : [])
  ];
  const sshArgs = sshCommandArgs(remote, ["sudo", "kelp-pi", ...recoverArgs]);
  if (hasFlag(rest, "--dry-run")) {
    return {
      ok: true,
      host: remote.host,
      user: remote.user,
      command: [remote.sshBin, ...sshArgs],
      recover: "network"
    };
  }
  if (!hasFlag(rest, "--force")) {
    throw new Error("kelp-claw pi recover network requires --force unless --dry-run is set.");
  }
  const result = await runChild(remote.sshBin, sshArgs);
  if (result.code !== 0) {
    throw new Error(result.stderr.trim() || `${remote.sshBin} exited ${result.code}`);
  }
  return {
    ok: true,
    host: remote.host,
    user: remote.user,
    command: [remote.sshBin, ...sshArgs],
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim()
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
  const labConfig = loadPiLabConfig(args);
  const profile =
    option(args, "--profile") ?? process.env.KELP_PI_PROFILE ?? labConfig.profile ?? "managed-ap";
  if (profile !== "managed-ap") {
    throw new Error("kelp-claw pi validate currently supports --profile managed-ap");
  }
  if (option(args, "--validator-command")) {
    throw new Error(
      "--validator-command was removed; pass managed-AP validation flags such as --target-ip and --client-a instead"
    );
  }
  const validateOptions = managedApValidateOptions(args, labConfig);
  const sshArgs = sshCommandArgs(validateOptions.remote, ["sh", "-s"]);
  const remoteScript = managedApValidationScript(validateOptions);
  if (hasFlag(args, "--dry-run")) {
    return {
      ok: true,
      profile: validateOptions.profile,
      host: validateOptions.remote.host,
      user: validateOptions.remote.user,
      command: [validateOptions.remote.sshBin, ...sshArgs],
      remoteScript,
      remoteOutputDir: validateOptions.remoteOutputDir,
      artifactPath: validateOptions.artifactPath,
      acceptance:
        "managed-ap requires node, WPA3 AP, AP isolation, DNS sinkhole, outbound allowlist, scoped scan, and bundle verification evidence"
    };
  }
  const startedAt = rfc3339Seconds(new Date());
  const result = await runChild(validateOptions.remote.sshBin, sshArgs, remoteScript);
  const finishedAt = rfc3339Seconds(new Date());
  const artifact = {
    ok: result.code === 0,
    schemaVersion: "kelp.pi.acceptance.v1",
    profile: validateOptions.profile,
    host: validateOptions.remote.host,
    user: validateOptions.remote.user,
    startedAt,
    finishedAt,
    remoteOutputDir: validateOptions.remoteOutputDir,
    command: [validateOptions.remote.sshBin, ...sshArgs],
    exitCode: result.code ?? -1,
    checks: parseAcceptanceChecks(`${result.stdout}\n${result.stderr}`),
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim(),
    artifactPath: validateOptions.artifactPath
  };
  await writeJson(validateOptions.artifactPath, artifact);
  if (result.code !== 0) {
    throw new Error(
      result.stderr.trim() ||
        `managed-AP validation failed with exit ${result.code}; artifact: ${validateOptions.artifactPath}`
    );
  }
  return artifact;
}

function managedApValidateOptions(
  args: readonly string[],
  labConfig: PiLabConfig = {}
): ManagedApValidateOptions {
  const remote = remotePiOptions(args, labConfig);
  const timestamp = rfc3339Seconds(new Date()).replace(/[^0-9TZ]/gu, "");
  const safeHost = safePathSegment(remote.host);
  const controlUrl = controlUrlOption(args, labConfig);
  const localOutDir = option(args, "--out") ?? join(".kelpclaw", "pi", safeHost);
  const remoteOutputDir =
    option(args, "--remote-output-dir") ?? `/var/lib/kelp-pi/bundles/field-acceptance-${timestamp}`;
  const portalIp =
    option(args, "--portal-ip") ??
    process.env.KELP_PI_PORTAL_IP ??
    labConfig.portalIp ??
    "10.42.0.1";
  const ollamaMode = hasFlag(args, "--ollama-expect-load")
    ? "load"
    : hasFlag(args, "--ollama-expect-refuse")
      ? "refuse"
      : process.env.KELP_PI_OLLAMA_EXPECT === "load" ||
          process.env.KELP_PI_OLLAMA_EXPECT === "refuse"
        ? process.env.KELP_PI_OLLAMA_EXPECT
        : undefined;
  const ollamaModel = option(args, "--ollama-model") ?? process.env.KELP_PI_OLLAMA_MODEL;
  if (ollamaModel && !ollamaMode) {
    throw new Error("--ollama-model requires --ollama-expect-load or --ollama-expect-refuse.");
  }
  const forbiddenIps = [
    ...options(args, "--forbidden-ip"),
    ...splitList(process.env.KELP_PI_FORBIDDEN_IPS),
    ...(labConfig.forbiddenIps ?? [])
  ];
  const targetIp = requiredField(args, "--target-ip", "KELP_PI_TARGET_IP", labConfig.targetIp);
  const targetUrl = requiredField(args, "--target-url", "KELP_PI_TARGET_URL", labConfig.targetUrl);
  return {
    profile: "managed-ap",
    remote,
    targetIp,
    targetUrl,
    nucleiApprovalToken: requiredField(
      args,
      "--nuclei-approval-token",
      "KELP_PI_NUCLEI_APPROVAL_TOKEN",
      labConfig.nucleiApprovalToken
    ),
    nucleiTarget:
      option(args, "--nuclei-target") ??
      process.env.KELP_PI_NUCLEI_TARGET ??
      labConfig.nucleiTarget ??
      targetUrl,
    controlUrl,
    currentConfig:
      option(args, "--current-config") ??
      process.env.KELP_PI_CURRENT_CONFIG ??
      "/etc/kelp-pi/network-hardening.json",
    updatedConfig:
      option(args, "--updated-config") ??
      process.env.KELP_PI_UPDATED_CONFIG ??
      option(args, "--current-config") ??
      process.env.KELP_PI_CURRENT_CONFIG ??
      "/etc/kelp-pi/network-hardening.json",
    sessionCommand:
      option(args, "--session-command") ??
      process.env.KELP_PI_SESSION_COMMAND ??
      `while sleep 5; do curl -fsS --max-time 5 ${shQuote(controlUrl)} >/dev/null || exit 1; done`,
    clientA: requiredField(args, "--client-a", "KELP_PI_CLIENT_A", labConfig.clientA),
    clientB: requiredField(args, "--client-b", "KELP_PI_CLIENT_B", labConfig.clientB),
    clientSshUser:
      option(args, "--client-ssh-user") ??
      process.env.KELP_PI_CLIENT_SSH_USER ??
      labConfig.clientSshUser,
    portalIp,
    upstreamInterface:
      option(args, "--upstream-interface") ??
      process.env.KELP_PI_UPSTREAM_INTERFACE ??
      labConfig.upstreamInterface,
    dnsProbeCommand:
      option(args, "--dns-probe-command") ??
      process.env.KELP_PI_DNS_PROBE_COMMAND ??
      labConfig.dnsProbeCommand,
    forbiddenIps: forbiddenIps.length > 0 ? forbiddenIps : ["198.51.100.10"],
    nucleiArgs: [
      ...options(args, "--nuclei-arg"),
      ...splitList(process.env.KELP_PI_NUCLEI_ARGS),
      ...(labConfig.nucleiArgs ?? [])
    ],
    ollamaMode,
    ollamaModel,
    readonlyRoot: hasFlag(args, "--readonly-root") || process.env.KELP_PI_READONLY_ROOT === "1",
    readonlyDataDir: option(args, "--readonly-data-dir") ?? process.env.KELP_PI_READONLY_DATA_DIR,
    maxSeconds:
      option(args, "--max-seconds") ?? process.env.KELP_PI_MAX_ACCEPTANCE_SECONDS ?? "1800",
    remoteOutputDir,
    localOutDir,
    artifactPath: join(localOutDir, "acceptance.json")
  };
}

function managedApValidationScript(options: ManagedApValidateOptions): string {
  const args = [
    "--output-dir",
    options.remoteOutputDir,
    "--target-ip",
    options.targetIp,
    "--target-url",
    options.targetUrl,
    "--nuclei-target",
    options.nucleiTarget,
    "--nuclei-approval-token",
    options.nucleiApprovalToken,
    "--control-url",
    options.controlUrl,
    "--current-config",
    options.currentConfig,
    "--updated-config",
    options.updatedConfig,
    "--session-command",
    options.sessionCommand,
    "--client-a",
    options.clientA,
    "--client-b",
    options.clientB,
    "--portal-ip",
    options.portalIp,
    "--max-seconds",
    options.maxSeconds,
    ...flatOptional("--ssh-user", options.clientSshUser),
    ...flatOptional("--upstream-interface", options.upstreamInterface),
    ...flatOptional("--dns-probe-command", options.dnsProbeCommand),
    ...options.forbiddenIps.flatMap((value) => ["--forbidden-ip", value]),
    ...options.nucleiArgs.flatMap((value) => ["--nuclei-arg", value]),
    ...(options.ollamaMode === "load" ? ["--ollama-expect-load"] : []),
    ...(options.ollamaMode === "refuse" ? ["--ollama-expect-refuse"] : []),
    ...flatOptional("--ollama-model", options.ollamaModel),
    ...(options.readonlyRoot
      ? ["--readonly-root", ...flatOptional("--readonly-data-dir", options.readonlyDataDir)]
      : [])
  ];
  return `set -eu
sudo kelp-pi-validate-field-acceptance ${args.map(shQuote).join(" ")}
`;
}

function parseAcceptanceChecks(output: string): readonly JsonRecord[] {
  return output
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("OK ") || line.startsWith("FAIL "))
    .map((line) => {
      const [status, name = "", ...rest] = line.split(/\s+/u);
      return {
        status: status === "OK" ? "pass" : "fail",
        name,
        detail: rest.join(" "),
        line
      };
    });
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

async function remotePiAgentCheck(
  remote: RemotePiOptions,
  required: boolean
): Promise<PiDoctorCheck> {
  const args = sshCommandArgs(remote, [remote.agentCommand, "version"]);
  try {
    const result = await runChild(remote.sshBin, args);
    const ok = result.code === 0;
    return {
      id: "remote:kelp-pi-agent",
      status: ok ? "pass" : required ? "fail" : "warn",
      required,
      message: ok
        ? `Remote kelp-pi-agent is reachable on ${remote.host}.`
        : `Remote kelp-pi-agent is not reachable on ${remote.host}.`,
      details: {
        host: remote.host,
        user: remote.user,
        command: [remote.sshBin, ...args],
        exitCode: result.code ?? -1,
        stdout: result.stdout.trim(),
        stderr: result.stderr.trim()
      }
    };
  } catch (error) {
    return {
      id: "remote:kelp-pi-agent",
      status: required ? "fail" : "warn",
      required,
      message: `Remote kelp-pi-agent check failed on ${remote.host}.`,
      details: {
        host: remote.host,
        user: remote.user,
        command: [remote.sshBin, ...args],
        error: error instanceof Error ? error.message : String(error)
      }
    };
  }
}

async function remoteHardwareChecks(
  remote: RemotePiOptions,
  required: boolean
): Promise<readonly PiDoctorCheck[]> {
  const args = sshCommandArgs(remote, ["sh", "-s"]);
  try {
    const result = await runChild(remote.sshBin, args, remoteHardwareProbeScript());
    if (result.code !== 0) {
      return [
        {
          id: "remote:hardware",
          status: required ? "fail" : "warn",
          required,
          message: `Remote hardware preflight failed on ${remote.host}.`,
          details: {
            host: remote.host,
            user: remote.user,
            command: [remote.sshBin, ...args],
            exitCode: result.code ?? -1,
            stdout: result.stdout.trim(),
            stderr: result.stderr.trim()
          }
        }
      ];
    }
    const facts = parseKeyValueLines(result.stdout);
    return remoteHardwareFactChecks(remote, facts, required);
  } catch (error) {
    return [
      {
        id: "remote:hardware",
        status: required ? "fail" : "warn",
        required,
        message: `Remote hardware preflight failed on ${remote.host}.`,
        details: {
          host: remote.host,
          user: remote.user,
          command: [remote.sshBin, ...args],
          error: error instanceof Error ? error.message : String(error)
        }
      }
    ];
  }
}

function remoteHardwareProbeScript(): string {
  return `set +e
kv() { printf '%s=%s\\n' "$1" "$2"; }
model="$(tr -d '\\000' </proc/device-tree/model 2>/dev/null || true)"
kv model "$model"
kv arch "$(uname -m 2>/dev/null || true)"
kv mem_kib "$(awk '/^MemTotal:/ { print $2 }' /proc/meminfo 2>/dev/null || true)"
if [ -r /etc/os-release ]; then
  . /etc/os-release
  kv os_id "$ID"
  kv os_version_codename "\${VERSION_CODENAME:-}"
else
  kv os_id ""
  kv os_version_codename ""
fi
if command -v vcgencmd >/dev/null 2>&1; then
  kv vcgencmd present
  kv throttled "$(vcgencmd get_throttled 2>/dev/null | sed 's/^throttled=//')"
else
  kv vcgencmd missing
  kv throttled ""
fi
if command -v nmcli >/dev/null 2>&1; then
  kv nmcli present
  kv nm_version "$(nmcli --version 2>/dev/null || true)"
else
  kv nmcli missing
fi
if command -v nft >/dev/null 2>&1; then
  kv nft present
  kv nft_version "$(nft --version 2>/dev/null || true)"
else
  kv nft missing
fi
if command -v systemctl >/dev/null 2>&1; then
  kv networkmanager_active "$(systemctl is-active NetworkManager 2>/dev/null || true)"
else
  kv networkmanager_active unknown
fi
`;
}

function remoteHardwareFactChecks(
  remote: RemotePiOptions,
  facts: JsonRecord,
  required: boolean
): readonly PiDoctorCheck[] {
  const model = stringField(facts, "model") ?? "";
  const arch = stringField(facts, "arch") ?? "";
  const memKiB = Number(stringField(facts, "mem_kib") ?? "0");
  const throttled = stringField(facts, "throttled") ?? "";
  const throttledValue = parseHex(throttled);
  const isPi5 = model.includes("Raspberry Pi 5");
  const isAarch64 = arch === "aarch64";
  const enoughRam = Number.isFinite(memKiB) && memKiB >= 3145728;
  const recommendedRam = Number.isFinite(memKiB) && memKiB >= 7340032;
  const throttledOk = throttledValue === 0;
  const hasVcgencmd = stringField(facts, "vcgencmd") === "present";
  const hasNmcli = stringField(facts, "nmcli") === "present";
  const hasNft = stringField(facts, "nft") === "present";
  const networkManagerActive = stringField(facts, "networkmanager_active") === "active";
  return [
    {
      id: "remote:hardware:model",
      status: isPi5 ? "pass" : required ? "fail" : "warn",
      required,
      message: isPi5 ? "Remote host reports Raspberry Pi 5." : "Remote host is not Raspberry Pi 5.",
      details: { host: remote.host, model }
    },
    {
      id: "remote:hardware:arch",
      status: isAarch64 ? "pass" : required ? "fail" : "warn",
      required,
      message: isAarch64 ? "Remote host is aarch64." : "Remote host is not aarch64.",
      details: { host: remote.host, arch }
    },
    {
      id: "remote:hardware:ram",
      status: enoughRam ? "pass" : required ? "fail" : "warn",
      required,
      message: enoughRam
        ? recommendedRam
          ? "Remote host meets recommended 8GB RAM profile."
          : "Remote host meets minimum 4GB RAM profile."
        : "Remote host does not meet the 4GB RAM floor.",
      details: { host: remote.host, memKiB, minimumKiB: 3145728, recommendedKiB: 7340032 }
    },
    {
      id: "remote:hardware:throttle",
      status: hasVcgencmd && throttledOk ? "pass" : required ? "fail" : "warn",
      required,
      message:
        hasVcgencmd && throttledOk
          ? "Remote Pi reports no current or historical throttle flags."
          : "Remote Pi throttle state needs attention.",
      details: { host: remote.host, vcgencmd: stringField(facts, "vcgencmd") ?? "", throttled }
    },
    {
      id: "remote:hardware:networkmanager",
      status: hasNmcli && networkManagerActive ? "pass" : required ? "fail" : "warn",
      required,
      message:
        hasNmcli && networkManagerActive
          ? "NetworkManager CLI is present and active."
          : "NetworkManager is missing or inactive.",
      details: {
        host: remote.host,
        nmcli: stringField(facts, "nmcli") ?? "",
        nmVersion: stringField(facts, "nm_version") ?? "",
        networkManagerActive: stringField(facts, "networkmanager_active") ?? ""
      }
    },
    {
      id: "remote:hardware:nftables",
      status: hasNft ? "pass" : required ? "fail" : "warn",
      required,
      message: hasNft ? "nftables CLI is present." : "nftables CLI is missing.",
      details: {
        host: remote.host,
        nft: stringField(facts, "nft") ?? "",
        nftVersion: stringField(facts, "nft_version") ?? ""
      }
    }
  ];
}

function parseKeyValueLines(stdout: string): JsonRecord {
  const entries = stdout
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter((line) => line.includes("="))
    .map((line) => {
      const index = line.indexOf("=");
      return [line.slice(0, index), line.slice(index + 1)] as const;
    });
  return Object.fromEntries(entries);
}

function parseHex(value: string): number | undefined {
  if (!/^0x[0-9a-f]+$/iu.test(value)) {
    return undefined;
  }
  return Number.parseInt(value.slice(2), 16);
}

async function releaseAssetOnlineChecks(
  releaseRepo: string,
  releaseAsset: string,
  required: boolean
): Promise<readonly PiDoctorCheck[]> {
  return Promise.all([
    releaseDownloadHeadCheck(releaseRepo, releaseAsset, required),
    releaseDownloadHeadCheck(releaseRepo, `${releaseAsset}.sha256`, required)
  ]);
}

async function releaseDownloadHeadCheck(
  releaseRepo: string,
  asset: string,
  required: boolean
): Promise<PiDoctorCheck> {
  const url = releaseDownloadUrl(releaseRepo, asset);
  try {
    const response = await fetch(url, { method: "HEAD", redirect: "manual" });
    const ok = response.status >= 200 && response.status < 400;
    return {
      id: `release-online:${asset}`,
      status: ok ? "pass" : required ? "fail" : "warn",
      required,
      message: ok
        ? `GitHub release asset is reachable: ${asset}.`
        : `GitHub release asset is not reachable: ${asset}.`,
      details: { url, status: response.status, location: response.headers.get("location") ?? "" }
    };
  } catch (error) {
    return {
      id: `release-online:${asset}`,
      status: required ? "fail" : "warn",
      required,
      message: `GitHub release asset check failed: ${asset}.`,
      details: { url, error: error instanceof Error ? error.message : String(error) }
    };
  }
}

function releaseDownloadUrl(releaseRepo: string, asset: string): string {
  return `https://github.com/${releaseRepo}/releases/latest/download/${encodeURIComponent(asset)}`;
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
  if (checks.some((check) => check.id === "env:KELP_PI_CONTROL_URL" && check.status !== "pass")) {
    recommendations.add(
      "Set KELP_PI_CONTROL_URL or KELP_PI_CONTROL_ENDPOINT before managed-AP outbound validation."
    );
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
  if (checks.some((check) => check.id === "remote:kelp-pi-agent" && check.status !== "pass")) {
    recommendations.add("Install and start kelp-pi-agent on the configured Pi host.");
  }
  if (checks.some((check) => check.id === "remote:hardware:model" && check.status !== "pass")) {
    recommendations.add("Use Raspberry Pi 5 as the Kelp Pi reference hardware.");
  }
  if (checks.some((check) => check.id === "remote:hardware:arch" && check.status !== "pass")) {
    recommendations.add("Use aarch64 Raspberry Pi OS for the Pi appliance.");
  }
  if (checks.some((check) => check.id === "remote:hardware:ram" && check.status !== "pass")) {
    recommendations.add("Use Raspberry Pi 5 4GB minimum; 8GB with NVMe remains recommended.");
  }
  if (checks.some((check) => check.id === "remote:hardware:throttle" && check.status !== "pass")) {
    recommendations.add("Fix Pi power or cooling before running field acceptance.");
  }
  if (
    checks.some((check) => check.id === "remote:hardware:networkmanager" && check.status !== "pass")
  ) {
    recommendations.add("Install and enable NetworkManager before managed-AP setup.");
  }
  if (checks.some((check) => check.id === "remote:hardware:nftables" && check.status !== "pass")) {
    recommendations.add("Install nftables before managed-AP firewall validation.");
  }
  if (checks.some((check) => check.id.startsWith("release-online:") && check.status !== "pass")) {
    recommendations.add("Publish or repair the latest GitHub Pi release assets.");
  }
  for (const check of checks) {
    if (check.status === "fail") {
      recommendations.add(`Resolve failed Pi readiness check: ${check.id}.`);
    }
  }
  return [...recommendations];
}

function requiredField(
  args: readonly string[],
  optionName: string,
  envName: string,
  configValue?: string | undefined
): string {
  const value = option(args, optionName) ?? process.env[envName] ?? configValue;
  if (!value) {
    throw new Error(`kelp-claw pi validate requires ${optionName}, ${envName}, or lab config.`);
  }
  return value;
}

function controlUrlOption(args: readonly string[], labConfig: PiLabConfig = {}): string {
  const value =
    option(args, "--control-url") ??
    process.env.KELP_PI_CONTROL_URL ??
    labConfig.controlUrl ??
    endpointToControlUrl(
      option(args, "--control-endpoint") ??
        process.env.KELP_PI_CONTROL_ENDPOINT ??
        labConfig.controlEndpoint
    );
  if (!value) {
    throw new Error(
      "kelp-claw pi validate requires --control-url, KELP_PI_CONTROL_URL, KELP_PI_CONTROL_ENDPOINT, or lab config."
    );
  }
  return value;
}

function endpointToControlUrl(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  return /^https?:\/\//u.test(value) ? value : `https://${value}/health`;
}

function flatOptional(name: string, value: string | undefined): readonly string[] {
  return value ? [name, value] : [];
}

function splitList(value: string | undefined): readonly string[] {
  return value?.split(/[,\s]+/u).filter(Boolean) ?? [];
}

function piLabConfigPath(args: readonly string[]): string {
  return (
    option(args, "--config") ??
    process.env.KELP_PI_LAB_CONFIG ??
    join(".kelpclaw", "pi", "lab.json")
  );
}

function loadPiLabConfig(args: readonly string[]): PiLabConfig {
  if (hasFlag(args, "--no-config")) {
    return {};
  }
  const path = piLabConfigPath(args);
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as unknown;
    if (!isRecord(parsed)) {
      throw new Error(`${path} must contain a JSON object`);
    }
    return normalizePiLabConfig(parsed);
  } catch (error) {
    if (isNodeErrorWithCode(error, "ENOENT")) {
      return {};
    }
    throw error;
  }
}

function normalizePiLabConfig(record: JsonRecord): PiLabConfig {
  return {
    schemaVersion: stringField(record, "schemaVersion"),
    profile: stringField(record, "profile"),
    host: stringField(record, "host"),
    sshUser: stringField(record, "sshUser") ?? stringField(record, "user"),
    controlUrl: stringField(record, "controlUrl"),
    controlEndpoint: stringField(record, "controlEndpoint"),
    targetIp: stringField(record, "targetIp"),
    targetUrl: stringField(record, "targetUrl"),
    nucleiApprovalToken: stringField(record, "nucleiApprovalToken"),
    nucleiTarget: stringField(record, "nucleiTarget"),
    clientA: stringField(record, "clientA"),
    clientB: stringField(record, "clientB"),
    clientSshUser: stringField(record, "clientSshUser"),
    portalIp: stringField(record, "portalIp"),
    upstreamInterface: stringField(record, "upstreamInterface"),
    dnsProbeCommand: stringField(record, "dnsProbeCommand"),
    forbiddenIps: arrayOfStrings(record.forbiddenIps),
    nucleiArgs: arrayOfStrings(record.nucleiArgs)
  };
}

function missingLabFields(config: PiLabConfig | JsonRecord): readonly string[] {
  const record = config as JsonRecord;
  const required = [
    "host",
    "sshUser",
    "targetIp",
    "targetUrl",
    "nucleiApprovalToken",
    "clientA",
    "clientB"
  ];
  const missing = required.filter(
    (key) => typeof record[key] !== "string" || record[key].length === 0
  );
  if (typeof record.controlUrl !== "string" && typeof record.controlEndpoint !== "string") {
    missing.push("controlUrl");
  }
  return missing;
}

function piLabConfigJson(config: PiLabConfig): JsonRecord {
  return compactRecord({
    schemaVersion: config.schemaVersion,
    profile: config.profile,
    host: config.host,
    sshUser: config.sshUser,
    controlUrl: config.controlUrl,
    controlEndpoint: config.controlEndpoint,
    targetIp: config.targetIp,
    targetUrl: config.targetUrl,
    nucleiApprovalToken: config.nucleiApprovalToken,
    nucleiTarget: config.nucleiTarget,
    clientA: config.clientA,
    clientB: config.clientB,
    clientSshUser: config.clientSshUser,
    portalIp: config.portalIp,
    upstreamInterface: config.upstreamInterface,
    dnsProbeCommand: config.dnsProbeCommand,
    forbiddenIps: config.forbiddenIps,
    nucleiArgs: config.nucleiArgs
  });
}

function compactRecord(record: { readonly [key: string]: JsonValue | undefined }): JsonRecord {
  const entries = Object.entries(record).filter((entry): entry is [string, JsonValue] => {
    const value = entry[1];
    if (value === undefined || value === "") {
      return false;
    }
    return !Array.isArray(value) || value.length > 0;
  });
  return Object.fromEntries(entries);
}

function shQuote(value: string): string {
  return `'${value.replace(/'/gu, `'\\''`)}'`;
}

function safePathSegment(value: string): string {
  return value.replace(/[^A-Za-z0-9_.-]+/gu, "_").replace(/^_+|_+$/gu, "") || "pi";
}

function remotePiOptions(args: readonly string[], labConfig: PiLabConfig = {}): RemotePiOptions {
  const host = option(args, "--host") ?? process.env.KELP_PI_HOST ?? labConfig.host;
  if (!host) {
    throw new Error("Kelp Pi SSH commands require --host or KELP_PI_HOST.");
  }
  return {
    host,
    user:
      option(args, "--ssh-user") ??
      option(args, "--user") ??
      process.env.KELP_PI_SSH_USER ??
      labConfig.sshUser ??
      process.env.KELP_PI_USER ??
      "pi",
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
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function writePrivateJson(path: string, value: JsonRecord): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
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
