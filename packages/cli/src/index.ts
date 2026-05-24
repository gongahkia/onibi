#!/usr/bin/env node
import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import {
  createPromotedSkillOtlpTracePayload,
  exportOtlpTraces,
  type OtlpTraceEvent
} from "@kelpclaw/adapters";
import {
  createCrossAgentReplayRuns,
  crossAgentReplaySkillMdFixture,
  synthesizeWorkflowFromTrajectory,
  trajectoryReplayShape
} from "@kelpclaw/codegen";

type JsonRecord = Record<string, unknown>;

const apiBaseUrl = process.env.KELPCLAW_API_URL ?? "http://127.0.0.1:8787";
const apiToken = process.env.KELPCLAW_API_TOKEN ?? process.env.KELPCLAW_ADMIN_TOKEN;

async function main(argv: readonly string[]): Promise<void> {
  const [command, ...args] = argv;
  switch (command) {
    case "start-recording":
      return printJson(
        await postJson("/api/agent-runs", {
          sourceAgent: requiredOption(args, "--agent"),
          sessionId: requiredOption(args, "--session-id"),
          ...(option(args, "--title") ? { title: option(args, "--title") } : {})
        })
      );
    case "record-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events`,
          {
            hookEvent: requiredOption(args, "--hook-event"),
            toolName: requiredOption(args, "--tool-name"),
            args: jsonOption(args, "--args-json") ?? {},
            ...(jsonOption(args, "--result-json") !== undefined
              ? { result: jsonOption(args, "--result-json") }
              : {}),
            ...(option(args, "--status") ? { status: option(args, "--status") } : {})
          }
        )
      );
    case "stop-recording":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/stop`,
          {
            status: option(args, "--status") ?? "stopped"
          }
        )
      );
    case "approve-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events/${encodeURIComponent(requiredOption(args, "--event-id"))}/approve`,
          {
            ...(option(args, "--reviewed-by") ? { reviewedBy: option(args, "--reviewed-by") } : {}),
            ...(option(args, "--reason") ? { reason: option(args, "--reason") } : {})
          }
        )
      );
    case "deny-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events/${encodeURIComponent(requiredOption(args, "--event-id"))}/deny`,
          {
            ...(option(args, "--reviewed-by") ? { reviewedBy: option(args, "--reviewed-by") } : {}),
            ...(option(args, "--reason") ? { reason: option(args, "--reason") } : {})
          }
        )
      );
    case "promote":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/promote`,
          {
            skillName: requiredOption(args, "--skill-name"),
            ...(option(args, "--capability")
              ? { capabilities: [option(args, "--capability") as string] }
              : {})
          }
        )
      );
    case "policy":
      return printJson(
        await putJson("/api/policies", {
          yaml: await readFile(requiredOption(args, "--file"), "utf8")
        })
      );
    case "audit-verify":
      return printJson(
        await getJson(
          `/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/audit/verify`
        )
      );
    case "audit-anchor":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/audit/anchor`,
          {}
        )
      );
    case "tbom-export":
      return printJson(
        await getJson(`/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/tbom`)
      );
    case "mint-role-token":
      return printJson(
        await postJson("/api/auth/role-tokens", {
          roles: requiredOption(args, "--roles")
            .split(",")
            .map((role) => role.trim())
            .filter((role) => role.length > 0),
          ...(option(args, "--subject") ? { subject: option(args, "--subject") } : {}),
          ...(option(args, "--expires-at") ? { expiresAt: option(args, "--expires-at") } : {}),
          ...(numberOption(args, "--ttl-seconds") !== undefined
            ? { ttlSeconds: numberOption(args, "--ttl-seconds") }
            : {})
        })
      );
    case "inspect-role-token":
      return printJson(
        await postJson("/api/auth/role-tokens/inspect", {
          token: requiredOption(args, "--token")
        })
      );
    case "otlp-smoke":
    case "datadog-otlp-smoke":
      return printJson(await runOtlpSmoke(args));
    case "cross-agent-replay-smoke":
      return printJson(runCrossAgentReplaySmoke());
    case "mcp":
      return runMcp(args);
    default:
      throw new Error(
        "Usage: kelp-claw <start-recording|record-step|stop-recording|approve-step|deny-step|promote|mcp|policy|audit-verify|audit-anchor|tbom-export|mint-role-token|inspect-role-token|otlp-smoke|cross-agent-replay-smoke>"
      );
  }
}

async function runOtlpSmoke(args: readonly string[]): Promise<JsonRecord> {
  const endpoint = requiredOption(args, "--endpoint", otlpEndpointFromEnv());
  const runId = option(args, "--run-id") ?? `agent-run.otlp-smoke.${Date.now()}`;
  const skillId = option(args, "--skill-id") ?? "skill.promoted.otlp-smoke";
  const promotedAt = option(args, "--promoted-at") ?? new Date().toISOString();
  const headers = {
    ...parseHeaderEnv(process.env.OTEL_EXPORTER_OTLP_HEADERS),
    ...parseHeaderEnv(process.env.KELPCLAW_OTLP_HEADERS),
    ...headersFromArgs(args),
    ...(process.env.DD_API_KEY ? { "DD-API-KEY": process.env.DD_API_KEY } : {})
  };
  const payload = createPromotedSkillOtlpTracePayload({
    endpoint,
    headers,
    serviceName: option(args, "--service-name") ?? "kelpclaw-cli",
    serviceVersion: option(args, "--service-version") ?? "0.1.0",
    runId,
    skillId,
    sourceAgent: "claude-code",
    promotedAt,
    events: smokeOtlpEvents(promotedAt)
  });
  const result = await exportOtlpTraces({ endpoint, headers, payload });
  const output = {
    ok: result.accepted,
    endpoint: result.endpoint,
    statusCode: result.statusCode,
    traceCount: 1,
    spanCount: result.spanCount,
    runId,
    skillId,
    headerNames: Object.keys(headers).sort(),
    ...(result.responseText ? { responseText: result.responseText.slice(0, 500) } : {})
  };
  if (!result.accepted) {
    process.exitCode = 1;
  }
  return output;
}

function runCrossAgentReplaySmoke(): JsonRecord {
  const runs = createCrossAgentReplayRuns();
  const shapes = runs.map(trajectoryReplayShape);
  const workflows = runs.map((run) =>
    synthesizeWorkflowFromTrajectory(run, { createdAt: "2026-05-23T00:00:00.000Z" })
  );
  const expectedShape = JSON.stringify(shapes[0]);
  const shapeMatches = shapes.every((shape) => JSON.stringify(shape) === expectedShape);
  const workflowKinds = workflows.map((workflow) => workflow.nodes.map((node) => node.kind));
  const workflowKindsMatch = workflowKinds.every(
    (kinds) => JSON.stringify(kinds) === JSON.stringify(workflowKinds[0])
  );
  const agentTags = workflows.map((workflow) =>
    workflow.nodes
      .filter((node) => node.kind === "agent-step")
      .map((node) => node.agentStep?.sourceAgent)
  );
  const ok = shapeMatches && workflowKindsMatch;
  if (!ok) {
    process.exitCode = 1;
  }
  return {
    ok,
    skillName: "kelpclaw-replay-smoke",
    skillMdBytes: crossAgentReplaySkillMdFixture.length,
    agents: runs.map((run) => run.sourceAgent),
    eventCount: shapes[0]?.eventCount ?? 0,
    tools: shapes[0]?.tools ?? [],
    statuses: shapes[0]?.statuses ?? [],
    outputs: shapes[0]?.outputs ?? [],
    workflowKinds,
    agentTags
  };
}

function smokeOtlpEvents(timestamp: string): readonly OtlpTraceEvent[] {
  return [
    {
      sourceAgent: "claude-code",
      hookEvent: "PostToolUse",
      toolName: "Bash",
      toolUseId: "toolu.otlp-smoke.write",
      args: { command: "printf kelpclaw-otlp-smoke" },
      result: { stdout: "kelpclaw-otlp-smoke", exitCode: 0 },
      status: "succeeded",
      contentHash: `sha256:${"1".repeat(64)}`,
      prevEventHash: `sha256:${"0".repeat(64)}`,
      chainIndex: 0,
      startedAt: timestamp,
      finishedAt: timestamp
    },
    {
      sourceAgent: "claude-code",
      hookEvent: "PostToolUse",
      toolName: "Read",
      toolUseId: "toolu.otlp-smoke.read",
      args: { filePath: ".kelpclaw-otlp-smoke.txt" },
      result: { content: "kelpclaw-otlp-smoke\n" },
      status: "succeeded",
      contentHash: `sha256:${"2".repeat(64)}`,
      prevEventHash: `sha256:${"1".repeat(64)}`,
      chainIndex: 1,
      startedAt: timestamp,
      finishedAt: timestamp
    }
  ];
}

async function runMcp(args: readonly string[]): Promise<void> {
  const command = process.env.KELPCLAW_MCP_COMMAND ?? "kelp-mcp";
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, [...args], {
      stdio: "inherit",
      env: process.env
    });
    child.on("error", reject);
    child.on("exit", (code, signal) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`MCP sidecar exited with ${signal ?? code ?? "unknown status"}.`));
      }
    });
  });
}

async function getJson(path: string): Promise<unknown> {
  return requestJson("GET", path);
}

async function postJson(path: string, body: JsonRecord): Promise<unknown> {
  return requestJson("POST", path, body);
}

async function putJson(path: string, body: JsonRecord): Promise<unknown> {
  return requestJson("PUT", path, body);
}

async function requestJson(method: string, path: string, body?: JsonRecord): Promise<unknown> {
  const response = await fetch(new URL(path, apiBaseUrl), {
    method,
    headers: {
      "content-type": "application/json",
      ...(apiToken ? { authorization: `Bearer ${apiToken}` } : {})
    },
    ...(body ? { body: JSON.stringify(body) } : {})
  });
  const payload = (await response.json()) as unknown;
  if (!response.ok) {
    throw new Error(JSON.stringify(payload));
  }
  return payload;
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  const value = index >= 0 ? args[index + 1] : undefined;
  return value && !value.startsWith("--") ? value : undefined;
}

function options(args: readonly string[], name: string): readonly string[] {
  const values: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === name) {
      const value = args[index + 1];
      if (value && !value.startsWith("--")) {
        values.push(value);
      }
    }
  }
  return values;
}

function requiredOption(args: readonly string[], name: string, fallback?: string): string {
  const value = option(args, name) ?? fallback;
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

function requiredPositional(args: readonly string[], index: number): string {
  const value = args.filter((arg) => !arg.startsWith("--"))[index];
  if (!value) {
    throw new Error(`Missing positional argument ${index + 1}.`);
  }
  return value;
}

function jsonOption(args: readonly string[], name: string): unknown {
  const value = option(args, name);
  return value ? JSON.parse(value) : undefined;
}

function numberOption(args: readonly string[], name: string): number | undefined {
  const value = option(args, name);
  if (!value) {
    return undefined;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error(`Option ${name} must be a number.`);
  }
  return parsed;
}

function otlpEndpointFromEnv(): string | undefined {
  return (
    process.env.KELPCLAW_OTLP_TRACES_ENDPOINT ??
    process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ??
    tracesEndpointFromBase(
      process.env.KELPCLAW_OTLP_ENDPOINT ?? process.env.OTEL_EXPORTER_OTLP_ENDPOINT
    )
  );
}

function tracesEndpointFromBase(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  return `${value.replace(/\/+$/u, "")}/v1/traces`;
}

function headersFromArgs(args: readonly string[]): Readonly<Record<string, string>> {
  return Object.fromEntries(
    options(args, "--header")
      .map((entry) => {
        const [name, ...rest] = entry.split("=");
        return [name ?? "", rest.join("=")] as const;
      })
      .filter(([name, value]) => name.length > 0 && value.length > 0)
  );
}

function parseHeaderEnv(value: string | undefined): Readonly<Record<string, string>> {
  if (!value) {
    return {};
  }
  if (value.trim().startsWith("{")) {
    const parsed = JSON.parse(value) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("OTLP headers JSON must be an object.");
    }
    return Object.fromEntries(
      Object.entries(parsed).filter(
        (entry): entry is [string, string] => typeof entry[1] === "string"
      )
    );
  }
  return Object.fromEntries(
    value
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean)
      .map((part) => {
        const [name, ...rest] = part.split("=");
        return [decodeURIComponent(name ?? ""), decodeURIComponent(rest.join("="))] as const;
      })
      .filter(([name]) => name.length > 0)
  );
}

function printJson(value: unknown): void {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

main(process.argv.slice(2)).catch((error: unknown) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
