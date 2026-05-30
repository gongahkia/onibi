import { spawn } from "node:child_process";
import { appendFile, mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { normalizeClaudeCodeHook } from "@kelpclaw/agent-hooks";
import { claimLedgerSchema, claimSchema, type Claim, type ClaimLedger } from "../types/claim.js";
import { verifyClaim } from "../verifier/index.js";
import { generateRepairPrompt } from "./index.js";

export interface RepairAgentRequest {
  readonly claim: Claim;
  readonly prompt: string;
  readonly targetTools: readonly string[];
  readonly iteration: number;
}

export interface RepairAgentResult {
  readonly claim?: Claim | undefined;
  readonly output?: string | undefined;
  readonly events?: readonly Record<string, unknown>[] | undefined;
}

export type RepairAgentRunner = (request: RepairAgentRequest) => Promise<RepairAgentResult>;

export interface RepairTraceRow {
  readonly timestamp: string;
  readonly iteration: number;
  readonly claimId: string;
  readonly event: "repair_prompt" | "agent_event" | "repair_result" | "repair_error";
  readonly prompt?: string | undefined;
  readonly targetTools?: readonly string[] | undefined;
  readonly status?: string | undefined;
  readonly output?: string | undefined;
  readonly error?: string | undefined;
  readonly agentEvent?: unknown | undefined;
}

export interface RepairLoopOptions {
  readonly runner?: RepairAgentRunner | undefined;
  readonly tracePath?: string | undefined;
  readonly now?: () => string;
}

export interface RepairLoopResult {
  readonly ledger: ClaimLedger;
  readonly trace: readonly RepairTraceRow[];
}

const defaultTracePath = ".kelpclaw/findevil/repair-trace.jsonl";
const highSeverity = new Set(["high", "critical"]);
const repairStatuses = new Set(["unsupported", "contradicted"]);

export async function runRepairLoop(
  ledger: ClaimLedger,
  maxIterations: number,
  options: RepairLoopOptions = {}
): Promise<RepairLoopResult> {
  if (maxIterations < 0 || !Number.isInteger(maxIterations)) {
    throw new Error("maxIterations must be a non-negative integer.");
  }
  const now = options.now ?? (() => new Date().toISOString());
  const tracePath = options.tracePath ?? defaultTracePath;
  await resetTrace(tracePath);

  let currentLedger = claimLedgerSchema.parse(ledger);
  const trace: RepairTraceRow[] = [];
  const runner = options.runner ?? runClaudeCodeRepair;

  for (let iteration = 1; iteration <= maxIterations; iteration += 1) {
    const candidates = currentLedger.claims.filter(shouldRepairClaim);
    if (candidates.length === 0) {
      break;
    }
    for (const claim of candidates) {
      const repair = generateRepairPrompt(claim);
      await pushTrace(tracePath, trace, {
        timestamp: now(),
        iteration,
        claimId: claim.id,
        event: "repair_prompt",
        prompt: repair.prompt,
        targetTools: repair.targetTools,
        status: claim.status
      });
      try {
        const result = await runner({
          claim,
          prompt: repair.prompt,
          targetTools: repair.targetTools,
          iteration
        });
        for (const event of result.events ?? []) {
          await pushTrace(tracePath, trace, {
            timestamp: now(),
            iteration,
            claimId: claim.id,
            event: "agent_event",
            agentEvent: normalizeClaudeCodeHook(event, { sourceAgent: "claude-code" })
          });
        }
        if (result.claim) {
          const repaired = claimSchema.parse(result.claim);
          const verified = claimSchema.parse({
            ...repaired,
            status: verifyClaim(repaired)
          });
          currentLedger = replaceClaim(currentLedger, verified);
        }
        await pushTrace(tracePath, trace, {
          timestamp: now(),
          iteration,
          claimId: claim.id,
          event: "repair_result",
          status: currentLedger.claims.find((item) => item.id === claim.id)?.status ?? claim.status,
          ...(result.output ? { output: result.output } : {})
        });
      } catch (error) {
        await pushTrace(tracePath, trace, {
          timestamp: now(),
          iteration,
          claimId: claim.id,
          event: "repair_error",
          error: error instanceof Error ? error.message : String(error)
        });
        throw error;
      }
    }
  }

  return {
    ledger: currentLedger,
    trace
  };
}

async function runClaudeCodeRepair(request: RepairAgentRequest): Promise<RepairAgentResult> {
  const command = process.env.KELPCLAW_CLAUDE_CODE_COMMAND ?? "claude";
  const args = ["-p", request.prompt];
  const output = await runCommand(command, args, {
    KELPCLAW_REPAIR_TARGET_TOOLS: request.targetTools.join(",")
  });
  return {
    output: output.stdout,
    events: [
      {
        session_id: `kelpclaw.findevil.repair.${request.claim.id}.${request.iteration}`,
        hook_event_name: "PostToolUse",
        tool_name: "ClaudeCode",
        tool_input: {
          prompt: request.prompt,
          targetTools: request.targetTools
        },
        tool_response: {
          stdout: output.stdout,
          stderr: output.stderr,
          exitCode: output.exitCode
        }
      }
    ]
  };
}

function shouldRepairClaim(claim: Claim): boolean {
  return highSeverity.has(claim.severity) && repairStatuses.has(claim.status);
}

function replaceClaim(ledger: ClaimLedger, claim: Claim): ClaimLedger {
  return claimLedgerSchema.parse({
    ...ledger,
    claims: ledger.claims.map((item) => (item.id === claim.id ? claim : item))
  });
}

async function resetTrace(path: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, "", "utf8");
}

async function pushTrace(
  path: string,
  trace: RepairTraceRow[],
  row: RepairTraceRow
): Promise<void> {
  trace.push(row);
  await appendFile(path, `${JSON.stringify(row)}\n`, "utf8");
}

async function runCommand(
  command: string,
  args: readonly string[],
  env: Readonly<Record<string, string>>
): Promise<{ readonly stdout: string; readonly stderr: string; readonly exitCode: number }> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      env: { ...process.env, ...env },
      stdio: ["ignore", "pipe", "pipe"]
    });
    const stdout: Buffer[] = [];
    const stderr: Buffer[] = [];
    child.stdout.on("data", (chunk: Buffer) => stdout.push(chunk));
    child.stderr.on("data", (chunk: Buffer) => stderr.push(chunk));
    child.on("error", reject);
    child.on("close", (exitCode) => {
      const result = {
        stdout: Buffer.concat(stdout).toString("utf8"),
        stderr: Buffer.concat(stderr).toString("utf8"),
        exitCode: exitCode ?? 1
      };
      if (result.exitCode !== 0) {
        reject(
          new Error(`Claude Code repair exited with status ${result.exitCode}: ${result.stderr}`)
        );
        return;
      }
      resolve(result);
    });
  });
}
