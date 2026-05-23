#!/usr/bin/env node
import { createServer } from "node:http";
import { stdin, stdout } from "node:process";

type JsonRpcRequest = {
  readonly jsonrpc: "2.0";
  readonly id?: string | number | null;
  readonly method: string;
  readonly params?: Record<string, unknown> | undefined;
};

const apiBaseUrl = process.env.KELPCLAW_API_URL ?? "http://127.0.0.1:8787";
const apiToken = process.env.KELPCLAW_API_TOKEN ?? process.env.KELPCLAW_ADMIN_TOKEN;

const tools = [
  tool("kelp.start_recording", "Start recording a coding-agent session."),
  tool("kelp.record_step", "Record one coding-agent tool step."),
  tool("kelp.stop_recording", "Stop an agent-run recording."),
  tool("kelp.list_skills", "List registered KelpClaw skills."),
  tool("kelp.invoke_skill", "Invoke a registered KelpClaw skill."),
  tool("kelp.promote_trajectory", "Promote a verified trajectory into a skill."),
  tool("kelp.check_policy", "Evaluate a tool call against loaded policy rules.")
] as const;

async function handleRequest(request: JsonRpcRequest): Promise<unknown> {
  switch (request.method) {
    case "initialize":
      return {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: { name: "kelp-mcp", version: "0.1.0" }
      };
    case "tools/list":
      return { tools };
    case "tools/call":
      return callTool(String(request.params?.name ?? ""), jsonObject(request.params?.arguments));
    default:
      throw new Error(`Unsupported MCP method '${request.method}'.`);
  }
}

async function callTool(name: string, args: Record<string, unknown>): Promise<unknown> {
  switch (name) {
    case "kelp.start_recording":
      return toolContent(await postJson("/api/agent-runs", args));
    case "kelp.record_step":
      return toolContent(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(stringArg(args, "runId"))}/events`,
          args
        )
      );
    case "kelp.stop_recording":
      return toolContent(
        await postJson(`/api/agent-runs/${encodeURIComponent(stringArg(args, "runId"))}/stop`, args)
      );
    case "kelp.list_skills":
      return toolContent(await getJson(`/api/skills${query(args)}`));
    case "kelp.invoke_skill":
      return toolContent(
        await postJson(`/api/skills/${encodeURIComponent(stringArg(args, "skillId"))}/invoke`, {
          input: jsonObject(args.input)
        })
      );
    case "kelp.promote_trajectory":
      return toolContent(
        await postJson(`/api/agent-runs/${encodeURIComponent(stringArg(args, "runId"))}/promote`, {
          skillName: args.skillName,
          capabilities: args.capabilities
        })
      );
    case "kelp.check_policy":
      return toolContent(
        await postJson("/api/policies/check", {
          hookEvent: args.hookEvent ?? "PreToolUse",
          toolName: args.toolName,
          args: jsonObject(args.args),
          classification: args.classification
        })
      );
    default:
      throw new Error(`Unknown KelpClaw tool '${name}'.`);
  }
}

function runStdio(): void {
  let buffer = Buffer.alloc(0);
  stdin.on("data", (chunk: Buffer) => {
    buffer = Buffer.concat([buffer, chunk]);
    for (;;) {
      const headerEnd = buffer.indexOf("\r\n\r\n");
      if (headerEnd < 0) {
        return;
      }
      const header = buffer.slice(0, headerEnd).toString("utf8");
      const length = Number(/^Content-Length:\s*(\d+)/imu.exec(header)?.[1] ?? 0);
      if (buffer.length < headerEnd + 4 + length) {
        return;
      }
      const body = buffer.slice(headerEnd + 4, headerEnd + 4 + length).toString("utf8");
      buffer = buffer.slice(headerEnd + 4 + length);
      void respond(JSON.parse(body) as JsonRpcRequest);
    }
  });
}

function runHttp(port: number): void {
  createServer((request, response) => {
    if (request.method !== "POST") {
      response.writeHead(404).end();
      return;
    }
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      body += chunk;
    });
    request.on("end", () => {
      void handleJsonRpc(JSON.parse(body) as JsonRpcRequest).then((payload) => {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(JSON.stringify(payload));
      });
    });
  }).listen(port, "127.0.0.1");
}

async function respond(request: JsonRpcRequest): Promise<void> {
  const payload = await handleJsonRpc(request);
  const content = Buffer.from(JSON.stringify(payload), "utf8");
  stdout.write(`Content-Length: ${content.length}\r\n\r\n`);
  stdout.write(content);
}

async function handleJsonRpc(request: JsonRpcRequest): Promise<unknown> {
  try {
    return {
      jsonrpc: "2.0",
      id: request.id ?? null,
      result: await handleRequest(request)
    };
  } catch (error) {
    return {
      jsonrpc: "2.0",
      id: request.id ?? null,
      error: {
        code: -32000,
        message: error instanceof Error ? error.message : String(error)
      }
    };
  }
}

async function getJson(path: string): Promise<unknown> {
  return requestJson("GET", path);
}

async function postJson(path: string, body: Record<string, unknown>): Promise<unknown> {
  return requestJson("POST", path, body);
}

async function requestJson(
  method: string,
  path: string,
  body?: Record<string, unknown>
): Promise<unknown> {
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

function tool(name: string, description: string) {
  return {
    name,
    description,
    inputSchema: {
      type: "object",
      additionalProperties: true
    }
  };
}

function toolContent(value: unknown): unknown {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value, null, 2)
      }
    ]
  };
}

function jsonObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function stringArg(args: Record<string, unknown>, key: string): string {
  const value = args[key];
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Tool argument '${key}' is required.`);
  }
  return value;
}

function query(args: Record<string, unknown>): string {
  const params = new URLSearchParams();
  for (const key of ["capability", "prompt"]) {
    const value = args[key];
    if (typeof value === "string" && value.trim()) {
      params.set(key, value.trim());
    }
  }
  const text = params.toString();
  return text ? `?${text}` : "";
}

const httpIndex = process.argv.indexOf("--http");
if (httpIndex >= 0) {
  runHttp(Number(process.argv[httpIndex + 1] ?? 8788));
} else {
  runStdio();
}
