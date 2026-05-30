interface SentinelCliOptions {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
  readonly mode?: "sentinel" | "verify" | "firewall" | undefined;
  readonly skipFirewall?: boolean | undefined;
  readonly skipSpoliation?: boolean | undefined;
  readonly skipClaimExtraction?: boolean | undefined;
}

interface SentinelCliResult {
  readonly ok: boolean;
  readonly status: "succeeded" | "policy_denied";
  readonly runId: string;
  readonly mode: string;
  readonly outDir: string;
  readonly outputs: Record<string, string>;
  readonly policyDenials: number;
  readonly uncorrectedPolicyDenials: number;
}

type RunSentinel = (opts: SentinelCliOptions) => Promise<SentinelCliResult>;

export async function runFindEvilSentinelCommand(args: readonly string[]): Promise<void> {
  try {
    const options = parseSentinelArgs(args);
    const runSentinel = await loadRunSentinel();
    const result = await runSentinel({
      casePath: options.casePath,
      evidenceRoot: options.evidenceRoot,
      outDir: options.outDir,
      maxIterations: options.maxIterations,
      mode: "sentinel",
      ...(options.siftCommand ? { siftCommand: options.siftCommand } : {}),
      ...(options.tracePath ? { tracePath: options.tracePath } : {})
    });
    printResult(result);
    process.exitCode = result.ok ? 0 : result.status === "policy_denied" ? 1 : 2;
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 2;
  }
}

export interface ParsedSentinelArgs {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
}

export function parseSentinelArgs(args: readonly string[]): ParsedSentinelArgs {
  assertKnownFlags(args, [
    "--case",
    "--sift-command",
    "--trace",
    "--max-iterations",
    "--evidence-root",
    "--out"
  ]);
  const casePath = requiredOption(args, "--case");
  const evidenceRoot = requiredOption(args, "--evidence-root");
  const outDir = requiredOption(args, "--out");
  const maxIterations = integerOption(requiredOption(args, "--max-iterations"), "--max-iterations");
  const siftCommand = option(args, "--sift-command");
  const tracePath = option(args, "--trace");
  if ((siftCommand ? 1 : 0) + (tracePath ? 1 : 0) !== 1) {
    throw new Error(
      "Usage: kelp-claw findevil sentinel requires exactly one of --sift-command or --trace."
    );
  }
  return {
    casePath,
    evidenceRoot,
    outDir,
    maxIterations,
    ...(siftCommand ? { siftCommand } : {}),
    ...(tracePath ? { tracePath } : {})
  };
}

export async function loadRunSentinel(): Promise<RunSentinel> {
  const module = (await import("@kelpclaw/findevil")) as {
    readonly runSentinel?: unknown;
  };
  if (typeof module.runSentinel !== "function") {
    throw new Error("@kelpclaw/findevil package does not export runSentinel.");
  }
  return module.runSentinel as RunSentinel;
}

export function printResult(result: SentinelCliResult): void {
  process.stdout.write(
    `${JSON.stringify(
      {
        ok: result.ok,
        status: result.status,
        runId: result.runId,
        mode: result.mode,
        outDir: result.outDir,
        outputs: result.outputs,
        policyDenials: result.policyDenials,
        uncorrectedPolicyDenials: result.uncorrectedPolicyDenials
      },
      null,
      2
    )}\n`
  );
}

export function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  if (index < 0) {
    return undefined;
  }
  const value = args[index + 1];
  return value && !value.startsWith("--") ? value : undefined;
}

export function requiredOption(args: readonly string[], name: string): string {
  const value = option(args, name);
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

export function integerOption(value: string, name: string): number {
  if (!/^\d+$/u.test(value)) {
    throw new Error(`${name} must be a non-negative integer.`);
  }
  return Number(value);
}

export function assertKnownFlags(args: readonly string[], knownFlags: readonly string[]): void {
  const known = new Set(knownFlags);
  for (const arg of args) {
    if (arg.startsWith("--") && !known.has(arg)) {
      throw new Error(`Unknown findevil option ${arg}.`);
    }
  }
}
