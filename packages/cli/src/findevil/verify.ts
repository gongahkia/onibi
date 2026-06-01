import {
  assertKnownFlags,
  integerOption,
  loadRunSentinel,
  printResult,
  requiredOption,
  timestampModeOption
} from "./sentinel.js";

export async function runFindEvilVerifyCommand(args: readonly string[]): Promise<void> {
  try {
    const options = parseVerifyArgs(args);
    const runSentinel = await loadRunSentinel();
    const result = await runSentinel({
      casePath: options.casePath,
      evidenceRoot: options.evidenceRoot,
      outDir: options.outDir,
      maxIterations: options.maxIterations,
      tracePath: options.siftRun,
      mode: "verify",
      skipFirewall: true,
      skipSpoliation: true,
      timestampMode: options.timestampMode
    });
    printResult(result);
    process.exitCode = result.ok ? 0 : result.status === "policy_denied" ? 1 : 2;
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 2;
  }
}

interface ParsedVerifyArgs {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly siftRun: string;
  readonly timestampMode: "live" | "skip";
}

function parseVerifyArgs(args: readonly string[]): ParsedVerifyArgs {
  assertKnownFlags(args, [
    "--case",
    "--sift-run",
    "--max-iterations",
    "--evidence-root",
    "--out",
    "--timestamp"
  ]);
  return {
    casePath: requiredOption(args, "--case"),
    evidenceRoot: requiredOption(args, "--evidence-root"),
    outDir: requiredOption(args, "--out"),
    maxIterations: integerOption(requiredOption(args, "--max-iterations"), "--max-iterations"),
    siftRun: requiredOption(args, "--sift-run"),
    timestampMode: timestampModeOption(args)
  };
}
