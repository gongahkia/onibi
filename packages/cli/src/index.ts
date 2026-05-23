#!/usr/bin/env node
import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";

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
    case "tbom-export":
      return printJson(
        await getJson(`/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/tbom`)
      );
    case "mcp":
      return runMcp(args);
    default:
      throw new Error(
        "Usage: kelp-claw <start-recording|record-step|stop-recording|approve-step|deny-step|promote|mcp|policy|audit-verify|tbom-export>"
      );
  }
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

function requiredOption(args: readonly string[], name: string): string {
  const value = option(args, name);
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

function printJson(value: unknown): void {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

main(process.argv.slice(2)).catch((error: unknown) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
