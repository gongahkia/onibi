import { spawn as nodeSpawn, type SpawnOptions } from "node:child_process";
import { once } from "node:events";
import { createWriteStream, type WriteStream } from "node:fs";
import { mkdir } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import type { JsonRecord } from "@kelpclaw/workflow-spec";

export interface SentinelTraceSummary {
  readonly rawEvents: readonly JsonRecord[];
  readonly finalReport: string;
  readonly traceClaims: readonly JsonRecord[];
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly exitCode: number | null;
  readonly signal?: NodeJS.Signals | undefined;
  readonly timedOut: boolean;
  readonly stdoutLines: number;
  readonly stderrLines: number;
}

export interface SiftRunnerOptions {
  readonly command: string;
  readonly agentExecutionPath: string;
  readonly maxRuntimeSeconds: number;
  readonly runId?: string | undefined;
  readonly env?: NodeJS.ProcessEnv | undefined;
  readonly hookPath?: string | undefined;
  readonly now?: (() => Date) | undefined;
  readonly spawnProcess?: SpawnSiftProcess | undefined;
}

export interface SiftChildProcess {
  readonly stdout: NodeJS.ReadableStream | null;
  readonly stderr: NodeJS.ReadableStream | null;
  on(event: "close", listener: (code: number | null, signal: NodeJS.Signals | null) => void): this;
  on(event: "error", listener: (error: Error) => void): this;
  kill(signal?: NodeJS.Signals): boolean;
}

export type SpawnSiftProcess = (command: string, options: SpawnOptions) => SiftChildProcess;

type StreamName = "stdout" | "stderr";

const forceKillGraceMs = 5000;
const requireFromHere = createRequire(import.meta.url);

export async function runProtocolSift(options: SiftRunnerOptions): Promise<SentinelTraceSummary> {
  if (!Number.isFinite(options.maxRuntimeSeconds) || options.maxRuntimeSeconds <= 0) {
    throw new Error("Protocol SIFT maxRuntimeSeconds must be a positive number.");
  }
  await mkdir(dirname(options.agentExecutionPath), { recursive: true });
  const now = options.now ?? (() => new Date());
  const startedAt = now().toISOString();
  const rawEvents: JsonRecord[] = [];
  const stdoutText: string[] = [];
  let stdoutLines = 0;
  let stderrLines = 0;
  let writeError: unknown;
  let writeQueue = Promise.resolve();
  const output = createWriteStream(options.agentExecutionPath, { flags: "w" });
  const appendEvent = (event: JsonRecord): void => {
    rawEvents.push(event);
    writeQueue = writeQueue
      .then(() => writeJsonlEvent(output, event))
      .catch((error: unknown) => {
        writeError = error;
      });
  };
  const stdout = createLineCollector((line) => {
    stdoutLines += 1;
    stdoutText.push(line);
    appendEvent(jsonEventFromLine(line) ?? processStreamEvent("stdout", line, now, options.runId));
  });
  const stderr = createLineCollector((line) => {
    stderrLines += 1;
    appendEvent(processStreamEvent("stderr", line, now, options.runId));
  });

  const child = (options.spawnProcess ?? spawnSiftProcess)(options.command, {
    shell: true,
    stdio: ["ignore", "pipe", "pipe"],
    env: childEnv(options)
  });
  child.stdout?.on("data", stdout.push);
  child.stderr?.on("data", stderr.push);
  child.stdout?.on("end", stdout.flush);
  child.stderr?.on("end", stderr.flush);

  let timedOut = false;
  let forceKillTimer: NodeJS.Timeout | undefined;
  const runtimeTimer = setTimeout(
    () => {
      timedOut = true;
      child.kill("SIGTERM");
      forceKillTimer = setTimeout(() => child.kill("SIGKILL"), forceKillGraceMs);
      forceKillTimer.unref?.();
    },
    Math.ceil(options.maxRuntimeSeconds * 1000)
  );
  runtimeTimer.unref?.();

  let exitCode: number | null = null;
  let signal: NodeJS.Signals | null = null;
  try {
    const result = await waitForExit(child);
    exitCode = result.exitCode;
    signal = result.signal;
  } finally {
    clearTimeout(runtimeTimer);
    if (forceKillTimer) {
      clearTimeout(forceKillTimer);
    }
    stdout.flush();
    stderr.flush();
    await writeQueue;
    await finishWriteStream(output);
  }
  if (writeError) {
    throw writeError;
  }
  const finalReport = finalReportFromEvents(rawEvents) ?? stdoutText.join("\n").trim();
  const summary: SentinelTraceSummary = {
    rawEvents,
    finalReport,
    traceClaims: traceClaimsFromEvents(rawEvents),
    startedAt,
    finishedAt: now().toISOString(),
    exitCode,
    ...(signal ? { signal } : {}),
    timedOut,
    stdoutLines,
    stderrLines
  };
  if (timedOut) {
    throw errorWithSummary(
      `Protocol SIFT command exceeded maxRuntimeSeconds=${options.maxRuntimeSeconds}.`,
      summary
    );
  }
  if (exitCode !== 0) {
    throw errorWithSummary(
      `Protocol SIFT command exited with status ${exitCode ?? "signal " + String(signal)}.`,
      summary
    );
  }
  return summary;
}

function spawnSiftProcess(command: string, options: SpawnOptions): SiftChildProcess {
  return nodeSpawn(command, options) as SiftChildProcess;
}

function childEnv(options: SiftRunnerOptions): NodeJS.ProcessEnv {
  const hookPath = options.hookPath ?? defaultHookPath();
  return {
    ...process.env,
    ...options.env,
    KELP_AGENT_HOOK_PATH: hookPath,
    KELPCLAW_AGENT_HOOK_NORMALIZER: "@kelpclaw/agent-hooks",
    KELP_AGENT_EXECUTION_JSONL: resolve(options.agentExecutionPath),
    ...(options.runId ? { KELPCLAW_AGENT_RUN_ID: options.runId } : {})
  };
}

function defaultHookPath(): string {
  try {
    return requireFromHere.resolve("@kelpclaw/agent-hooks");
  } catch {
    return resolve(process.cwd(), "packages/agent-hooks/dist/index.js");
  }
}

function createLineCollector(onLine: (line: string) => void): {
  readonly push: (chunk: Buffer | string) => void;
  readonly flush: () => void;
} {
  let buffer = "";
  const emit = (): void => {
    let newline = buffer.search(/\r?\n/u);
    while (newline >= 0) {
      const line = buffer.slice(0, newline).trimEnd();
      buffer = buffer.slice(
        buffer[newline] === "\r" && buffer[newline + 1] === "\n" ? newline + 2 : newline + 1
      );
      if (line.trim().length > 0) {
        onLine(line);
      }
      newline = buffer.search(/\r?\n/u);
    }
  };
  return {
    push(chunk: Buffer | string): void {
      buffer += Buffer.isBuffer(chunk) ? chunk.toString("utf8") : chunk;
      emit();
    },
    flush(): void {
      const line = buffer.trimEnd();
      buffer = "";
      if (line.trim().length > 0) {
        onLine(line);
      }
    }
  };
}

function jsonEventFromLine(line: string): JsonRecord | undefined {
  try {
    const parsed = JSON.parse(line) as unknown;
    return isJsonRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

function processStreamEvent(
  stream: StreamName,
  content: string,
  now: () => Date,
  runId: string | undefined
): JsonRecord {
  return {
    event: stream === "stdout" ? "process_stdout" : "process_stderr",
    timestamp: now().toISOString(),
    ...(runId ? { runId } : {}),
    stream,
    content
  };
}

async function waitForExit(
  child: SiftChildProcess
): Promise<{ readonly exitCode: number | null; readonly signal: NodeJS.Signals | null }> {
  return new Promise((resolveExit, reject) => {
    child.on("error", reject);
    child.on("close", (exitCode, signal) => resolveExit({ exitCode, signal }));
  });
}

async function writeJsonlEvent(output: WriteStream, event: JsonRecord): Promise<void> {
  if (!output.write(`${JSON.stringify(event)}\n`)) {
    await once(output, "drain");
  }
}

async function finishWriteStream(output: WriteStream): Promise<void> {
  const finished = once(output, "finish");
  const errored = once(output, "error").then(([error]) => {
    throw error;
  });
  output.end();
  await Promise.race([finished, errored]);
}

function finalReportFromEvents(events: readonly JsonRecord[]): string | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (!event) {
      continue;
    }
    if (eventName(event) === "final_report" && typeof event.content === "string") {
      return event.content.trim();
    }
  }
  return undefined;
}

function traceClaimsFromEvents(events: readonly JsonRecord[]): JsonRecord[] {
  return events.flatMap((event) =>
    eventName(event) === "claim_extracted" && isJsonRecord(event.claim) ? [event.claim] : []
  );
}

function eventName(event: JsonRecord): string {
  return typeof event.event === "string" ? event.event : "";
}

function errorWithSummary(message: string, summary: SentinelTraceSummary): Error {
  return Object.assign(new Error(message), { summary });
}

function isJsonRecord(input: unknown): input is JsonRecord {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}
