import { EventEmitter } from "node:events";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { PassThrough } from "node:stream";
import type { SpawnOptions } from "node:child_process";
import { describe, expect, it } from "vitest";
import {
  runProtocolSift,
  type SiftChildProcess,
  type SpawnSiftProcess
} from "../src/sentinel/sift-runner.js";

describe("Protocol SIFT runner", () => {
  it("streams child JSONL and stderr envelopes to agent-execution.jsonl", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-sift-runner-"));
    const agentExecutionPath = join(directory, "agent-execution.jsonl");
    const child = new FakeChildProcess();
    let spawned:
      | {
          readonly command: string;
          readonly options: SpawnOptions;
        }
      | undefined;
    const toolCall = JSON.stringify({
      event: "tool_call",
      callId: "call-001",
      tool: "ProtocolSIFT",
      arguments: { caseDir: "/mnt/case-ro" }
    });
    const finalReport = JSON.stringify({
      event: "final_report",
      content: "Protocol SIFT completed the case."
    });
    const stderrEvent = JSON.stringify({
      event: "process_stderr",
      timestamp: "2026-05-30T00:00:00.000Z",
      runId: "run-001",
      stream: "stderr",
      content: "mcp handshake recovered"
    });
    const summary = await runProtocolSift({
      command: "protocol-sift run --case-dir /mnt/case-ro --output-jsonl",
      agentExecutionPath,
      maxRuntimeSeconds: 5,
      runId: "run-001",
      hookPath: "/tmp/kelp-agent-hook.js",
      now: () => new Date("2026-05-30T00:00:00.000Z"),
      spawnProcess: fakeSpawn(child, (command, options) => {
        spawned = { command, options };
        queueMicrotask(() => {
          child.stdout.write(`${toolCall}\n`);
          child.stderr.write("mcp handshake recovered\n");
          child.stdout.write(`${finalReport}\n`);
          child.close(0);
        });
      })
    });

    await expect(readFile(agentExecutionPath, "utf8")).resolves.toBe(
      `${toolCall}\n${stderrEvent}\n${finalReport}\n`
    );
    expect(summary.finalReport).toBe("Protocol SIFT completed the case.");
    expect(summary.rawEvents).toHaveLength(3);
    expect(summary.stdoutLines).toBe(2);
    expect(summary.stderrLines).toBe(1);
    expect(spawned?.command).toBe("protocol-sift run --case-dir /mnt/case-ro --output-jsonl");
    expect(spawned?.options.env).toMatchObject({
      KELP_AGENT_HOOK_PATH: "/tmp/kelp-agent-hook.js",
      KELPCLAW_AGENT_HOOK_NORMALIZER: "@kelpclaw/agent-hooks",
      KELP_AGENT_EXECUTION_JSONL: resolve(agentExecutionPath),
      KELPCLAW_AGENT_RUN_ID: "run-001"
    });
  });

  it("kills the child process when the runtime budget is exceeded", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-sift-runner-timeout-"));
    const child = new FakeChildProcess();
    const promise = runProtocolSift({
      command: "protocol-sift run --case-dir /mnt/case-ro --output-jsonl",
      agentExecutionPath: join(directory, "agent-execution.jsonl"),
      maxRuntimeSeconds: 0.01,
      runId: "run-timeout",
      spawnProcess: fakeSpawn(child)
    });

    await expect(promise).rejects.toThrow("exceeded maxRuntimeSeconds=0.01");
    expect(child.killSignals).toContain("SIGTERM");
  });
});

class FakeChildProcess extends EventEmitter {
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  readonly killSignals: NodeJS.Signals[] = [];

  kill(signal: NodeJS.Signals = "SIGTERM"): boolean {
    this.killSignals.push(signal);
    queueMicrotask(() => this.close(null, signal));
    return true;
  }

  close(exitCode: number | null, signal: NodeJS.Signals | null = null): void {
    this.stdout.end();
    this.stderr.end();
    this.emit("close", exitCode, signal);
  }
}

function fakeSpawn(
  child: FakeChildProcess,
  onSpawn?: (command: string, options: SpawnOptions) => void
): SpawnSiftProcess {
  return (command, options) => {
    onSpawn?.(command, options);
    return child as unknown as SiftChildProcess;
  };
}
