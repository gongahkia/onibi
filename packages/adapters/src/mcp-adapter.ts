import { randomUUID } from "node:crypto";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { CallToolResultSchema, ListToolsResultSchema } from "@modelcontextprotocol/sdk/types.js";
import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import type {
  JsonRecord,
  JsonSchemaShape,
  JsonValue,
  WorkflowConnectorRecord
} from "@kelpclaw/workflow-spec";

const objectSchema = {
  type: "object",
  additionalProperties: true
} as const satisfies JsonSchemaShape;

export interface ImportMcpConnectorInput {
  readonly id: string;
  readonly name?: string | undefined;
  readonly endpointUrl: string;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  readonly now?: string | undefined;
}

export async function importMcpConnector(
  input: ImportMcpConnectorInput
): Promise<WorkflowConnectorRecord> {
  const tools = await listMcpTools(input.endpointUrl);
  const now = input.now ?? new Date().toISOString();
  const endpoint = new URL(input.endpointUrl);

  return {
    id: input.id,
    name: input.name ?? input.id,
    kind: "mcp",
    adapterId: `adapter.mcp.${slugify(input.id)}`,
    endpointUrl: endpoint.toString(),
    transport: "streamable-http",
    allowedHosts: [endpoint.hostname],
    auth: [],
    operations: tools.map((tool) => ({
      name: tool.name,
      version: "1.0.0",
      description:
        typeof tool.description === "string" && tool.description.length > 0
          ? tool.description
          : `Call MCP tool '${tool.name}'.`,
      inputSchema: isRecord(tool.inputSchema) ? tool.inputSchema : objectSchema,
      outputSchema: objectSchema,
      toolName: tool.name,
      metadata: {
        endpointUrl: endpoint.toString()
      }
    })),
    secretRefs: input.secretRefs ?? {},
    createdAt: now,
    updatedAt: now,
    lastTest: {
      status: "succeeded",
      testedAt: now,
      operationCount: tools.length,
      message: `Discovered ${tools.length} MCP tool(s).`
    }
  };
}

export async function testMcpConnector(
  connector: WorkflowConnectorRecord
): Promise<WorkflowConnectorRecord> {
  if (!connector.endpointUrl) {
    throw new Error(`MCP connector '${connector.id}' is missing endpointUrl.`);
  }
  const tools = await listMcpTools(connector.endpointUrl);
  const now = new Date().toISOString();
  return {
    ...connector,
    updatedAt: now,
    lastTest: {
      status: "succeeded",
      testedAt: now,
      operationCount: tools.length,
      message: `Discovered ${tools.length} MCP tool(s).`
    }
  };
}

export class McpToolAdapter implements Adapter {
  public constructor(public readonly metadata: AdapterMetadata) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const operation = this.metadata.operations.find(
      (candidate) =>
        candidate.name === invocation.operation && candidate.version === invocation.operationVersion
    );
    if (!operation) {
      throw new Error(
        `Adapter '${this.metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
      );
    }
    const endpointUrl = readMetadataString(operation.metadata, "endpointUrl");
    if (!endpointUrl) {
      throw new Error(`MCP operation '${operation.name}' is missing endpoint metadata.`);
    }
    assertAllowedHost(this.metadata, endpointUrl);
    const toolName = readMetadataString(operation.metadata, "toolName") ?? operation.name;
    const result = await callMcpTool(endpointUrl, toolName, invocation.payload);

    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status: "succeeded",
      output: {
        tool: toolName,
        result: result as JsonValue
      },
      providerMetadata: {
        adapterId: invocation.adapterId,
        provider: "mcp",
        providerResponseId: `mcp.${toolName}.${randomUUID()}`,
        mock: false,
        sequence: invocation.context.attempt,
        operation: invocation.operation
      },
      auditEvents: [
        {
          id: `audit.${this.metadata.id}.${invocation.context.runId}.${invocation.context.nodeId}.${invocation.context.attempt}.${randomUUID()}`,
          timestamp: new Date().toISOString(),
          level: "info",
          message: `MCP adapter '${this.metadata.id}' called tool '${toolName}'.`
        }
      ]
    };
  }
}

export function createMcpAdapter(connector: WorkflowConnectorRecord): Adapter {
  return new McpToolAdapter({
    id: connector.adapterId,
    kind: "mcp",
    displayName: connector.name,
    version: "1.0.0",
    capabilities: connector.operations.map((operation) => `mcp.tool.${operation.name}`),
    operations: connector.operations.map((operation) => ({
      name: operation.name,
      version: operation.version,
      description: operation.description,
      inputSchema: operation.inputSchema,
      outputSchema: operation.outputSchema,
      metadata: {
        ...(operation.metadata ?? {}),
        toolName: operation.toolName ?? operation.name,
        endpointUrl: connector.endpointUrl ?? ""
      }
    })),
    requiredSecrets: [],
    networkPolicy: {
      mode: "declared",
      allowedHosts: connector.allowedHosts
    },
    rateLimit: {
      maxRequests: 60,
      perSeconds: 60
    },
    retry: {
      maxAttempts: 3,
      backoffSeconds: 2,
      retryableErrorCodes: ["MCP_TOOL_FAILED"]
    },
    fixtures: [],
    live: true
  });
}

async function listMcpTools(endpointUrl: string): Promise<
  readonly {
    readonly name: string;
    readonly description?: string;
    readonly inputSchema?: JsonRecord;
  }[]
> {
  const client = await connectMcp(endpointUrl);
  try {
    const result = await client.request(
      { method: "tools/list", params: {} },
      ListToolsResultSchema
    );
    return result.tools.map((tool) => ({
      name: tool.name,
      ...(tool.description ? { description: tool.description } : {}),
      ...(isRecord(tool.inputSchema) ? { inputSchema: tool.inputSchema } : {})
    }));
  } finally {
    await client.close();
  }
}

async function callMcpTool(
  endpointUrl: string,
  toolName: string,
  payload: JsonRecord
): Promise<unknown> {
  const client = await connectMcp(endpointUrl);
  try {
    return await client.request(
      {
        method: "tools/call",
        params: {
          name: toolName,
          arguments: payload
        }
      },
      CallToolResultSchema
    );
  } finally {
    await client.close();
  }
}

async function connectMcp(endpointUrl: string): Promise<Client> {
  const client = new Client({
    name: "kelpclaw-tool-gateway",
    version: "1.0.0"
  });
  await client.connect(
    new StreamableHTTPClientTransport(new URL(endpointUrl)) as unknown as Transport
  );
  return client;
}

function assertInvocation(metadata: AdapterMetadata, invocation: AdapterInvocation): void {
  if (invocation.adapterId !== metadata.id) {
    throw new Error(
      `Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`
    );
  }
}

function assertAllowedHost(metadata: AdapterMetadata, endpointUrl: string): void {
  const host = new URL(endpointUrl).hostname.toLowerCase();
  const allowed = new Set(
    metadata.networkPolicy.allowedHosts.map((candidate) => candidate.toLowerCase())
  );
  if (!allowed.has(host)) {
    throw new Error(`Host '${host}' is not declared for adapter '${metadata.id}'.`);
  }
}

function readMetadataString(metadata: JsonRecord | undefined, key: string): string | undefined {
  const value = metadata?.[key];
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/gu, "-")
    .replace(/^-+|-+$/gu, "");
}
