import { spawn } from "node:child_process";

type JsonRecord = Record<string, unknown>;

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
          name: "wipe",
          usage: "kelp-claw pi wipe --force [--data-dir PATH] [--agent-bin PATH]"
        }
      ]
    };
  }
  if (command === "approve") {
    return approveCommand(rest);
  }
  if (command === "wipe") {
    return wipeCommand(rest);
  }
  throw new Error("Usage: kelp-claw pi <approve|wipe|--help>");
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
  args: readonly string[]
): Promise<{ readonly code: number | null; readonly stdout: string; readonly stderr: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, [...args], { stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk: string) => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
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

function hasFlag(args: readonly string[], name: string): boolean {
  return args.includes(name);
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
