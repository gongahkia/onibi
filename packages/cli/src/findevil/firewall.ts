import {
  assertKnownFlags,
  integerOption,
  loadRunSentinel,
  option,
  printResult,
  requiredOption
} from "./sentinel.js";

export async function runFindEvilFirewallCommand(args: readonly string[]): Promise<void> {
  try {
    const options = parseFirewallArgs(args);
    const runSentinel = await loadRunSentinel();
    const result = await runSentinel({
      casePath: options.casePath,
      evidenceRoot: options.evidenceRoot,
      outDir: options.outDir,
      maxIterations: options.maxIterations,
      mode: "firewall",
      skipClaimExtraction: true,
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

interface ParsedFirewallArgs {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
}

function parseFirewallArgs(args: readonly string[]): ParsedFirewallArgs {
  assertKnownFlags(args, [
    "--case",
    "--sift-command",
    "--trace",
    "--max-iterations",
    "--evidence-root",
    "--out"
  ]);
  const siftCommand = option(args, "--sift-command");
  const tracePath = option(args, "--trace");
  if ((siftCommand ? 1 : 0) + (tracePath ? 1 : 0) !== 1) {
    throw new Error(
      "Usage: kelp-claw findevil firewall requires exactly one of --sift-command or --trace."
    );
  }
  return {
    casePath: requiredOption(args, "--case"),
    evidenceRoot: requiredOption(args, "--evidence-root"),
    outDir: requiredOption(args, "--out"),
    maxIterations: integerOption(option(args, "--max-iterations") ?? "0", "--max-iterations"),
    ...(siftCommand ? { siftCommand } : {}),
    ...(tracePath ? { tracePath } : {})
  };
}
