import { assertKnownFlags, integerOption, option, requiredOption } from "./sentinel.js";

interface FindEvilMcpCliOptions {
  readonly evidenceRoot: string;
  readonly maxRuntimeSeconds: number;
}

type RunFindEvilMcpServer = (opts: FindEvilMcpCliOptions) => void;

export async function runFindEvilMcpCommand(args: readonly string[]): Promise<void> {
  try {
    const options = parseMcpArgs(args);
    const runFindEvilMcpServer = await loadRunFindEvilMcpServer();
    runFindEvilMcpServer(options);
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 2;
  }
}

export function parseMcpArgs(args: readonly string[]): FindEvilMcpCliOptions {
  assertKnownFlags(args, ["--evidence-root", "--max-runtime-seconds"]);
  return {
    evidenceRoot: requiredOption(args, "--evidence-root"),
    maxRuntimeSeconds: integerOption(
      option(args, "--max-runtime-seconds") ?? "120",
      "--max-runtime-seconds"
    )
  };
}

async function loadRunFindEvilMcpServer(): Promise<RunFindEvilMcpServer> {
  const module = (await import("@kelpclaw/findevil")) as {
    readonly runFindEvilMcpServer?: unknown;
  };
  if (typeof module.runFindEvilMcpServer !== "function") {
    throw new Error("@kelpclaw/findevil package does not export runFindEvilMcpServer.");
  }
  return module.runFindEvilMcpServer as RunFindEvilMcpServer;
}
