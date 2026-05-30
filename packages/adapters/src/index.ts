import { createHash } from "node:crypto";
import { stableJsonStringify } from "@kelpclaw/workflow-spec";
import { createMcpAdapter, importMcpConnector, testMcpConnector } from "./mcp-adapter.js";
import type {
  Adapter,
  AdapterFixturePayload,
  AdapterInvocation,
  AdapterMetadata,
  AdapterOperationDefinition,
  AdapterResult,
  AdapterSecretRequirement,
  RecordedAdapterInvocation
} from "./types.js";
import type { JsonRecord, JsonSchemaShape, JsonValue } from "@kelpclaw/workflow-spec";

const objectSchema = {
  type: "object",
  additionalProperties: true
} as const satisfies JsonSchemaShape;
const arraySchema = { type: "array", items: objectSchema } as const satisfies JsonSchemaShape;
const stringSchema = { type: "string" } as const satisfies JsonSchemaShape;
const noneNetworkPolicy = { mode: "none", allowedHosts: [] } as const;
const defaultRateLimit = { maxRequests: 60, perSeconds: 60 } as const;
const defaultRetry = {
  maxAttempts: 3,
  backoffSeconds: 2,
  retryableErrorCodes: ["RATE_LIMITED", "TEMPORARY_UNAVAILABLE"]
} as const;

export { createMcpAdapter, importMcpConnector, testMcpConnector };
export type { ImportMcpConnectorInput } from "./mcp-adapter.js";
export type {
  Adapter,
  AdapterAuditEvent,
  AdapterAuditEventLevel,
  AdapterErrorDetail,
  AdapterFixturePayload,
  AdapterInvocation,
  AdapterKind,
  AdapterMetadata,
  AdapterNetworkMode,
  AdapterNetworkPolicy,
  AdapterOperationDefinition,
  AdapterOperationStatus,
  AdapterProviderMetadata,
  AdapterRateLimitPolicy,
  AdapterResult,
  AdapterRetryPolicy,
  AdapterRuntimeContext,
  AdapterSecretRequirement,
  RecordedAdapterInvocation
} from "./types.js";

export const builtinAdapterMetadata = [
  adapter({
    id: "adapter.gmail",
    kind: "gmail",
    displayName: "Gmail",
    capabilities: ["gmail.trigger", "gmail.receipts.search"],
    requiredSecrets: [secret("gmail.oauth", "OAuth token reference for Gmail scopes.")],
    allowedHosts: ["oauth2.googleapis.com", "gmail.googleapis.com"],
    operations: [
      operation("gmail.trigger.poll", "Polls Gmail for trigger messages."),
      operation("gmail.receipts.search", "Searches Gmail for receipt messages.")
    ]
  }),
  adapter({
    id: "adapter.sheets",
    kind: "sheets",
    displayName: "Google Sheets",
    capabilities: ["sheets.rows.append", "sheets.rows.update", "sheets.rows.lookup"],
    requiredSecrets: [secret("sheets.oauth", "OAuth token reference for Google Sheets scopes.")],
    allowedHosts: ["oauth2.googleapis.com", "sheets.googleapis.com"],
    operations: [
      operation("sheets.rows.append", "Appends rows to a Google Sheet."),
      operation("sheets.rows.update", "Updates rows in a Google Sheet."),
      operation("sheets.rows.lookup", "Looks up rows in a Google Sheet.")
    ]
  }),
  adapter({
    id: "adapter.email",
    kind: "email",
    displayName: "SMTP Email Delivery",
    capabilities: ["email.approval.request", "email.results.send"],
    requiredSecrets: [secret("email.delivery", "Provider key or SMTP credential reference.")],
    allowedHosts: ["smtp"],
    operations: [
      operation("email.approval.request", "Sends an email approval request."),
      operation("email.results.send", "Delivers workflow results by email.")
    ]
  }),
  adapter({
    id: "adapter.whatsapp",
    kind: "whatsapp",
    displayName: "WhatsApp Cloud Alerts",
    capabilities: ["whatsapp.alert.send"],
    requiredSecrets: [secret("whatsapp.apiKey", "WhatsApp Business API key reference.")],
    allowedHosts: ["graph.facebook.com"],
    operations: [operation("whatsapp.alert.send", "Sends a WhatsApp alert.")]
  }),
  adapter({
    id: "adapter.telegram",
    kind: "telegram",
    displayName: "Telegram Alerts",
    capabilities: ["telegram.alert.send"],
    requiredSecrets: [secret("telegram.botToken", "Telegram bot token reference.")],
    allowedHosts: ["api.telegram.org"],
    operations: [operation("telegram.alert.send", "Sends a Telegram alert.")]
  }),
  adapter({
    id: "adapter.github",
    kind: "github",
    displayName: "GitHub",
    capabilities: ["github.issue.create", "github.issue.comment"],
    requiredSecrets: [secret("github.token", "GitHub token reference.")],
    allowedHosts: ["api.github.com"],
    operations: [
      operation("github.issue.create", "Creates a GitHub issue."),
      operation("github.issue.comment", "Adds a GitHub issue comment.")
    ]
  }),
  genericAdapter("adapter.slack", "slack", "Slack", "slack.message.send", "slack.botToken", [
    "slack.com"
  ]),
  genericAdapter(
    "adapter.discord",
    "discord",
    "Discord",
    "discord.message.send",
    "discord.botToken",
    ["discord.com"]
  ),
  genericAdapter("adapter.notion", "notion", "Notion", "notion.page.create", "notion.apiKey", [
    "api.notion.com"
  ]),
  genericAdapter("adapter.linear", "linear", "Linear", "linear.issue.create", "linear.apiKey", [
    "api.linear.app"
  ]),
  genericAdapter("adapter.jira", "jira", "Jira Cloud", "jira.issue.create", "jira.basicAuth", [
    "*.atlassian.net"
  ]),
  genericAdapter(
    "adapter.airtable",
    "airtable",
    "Airtable",
    "airtable.record.create",
    "airtable.apiKey",
    ["api.airtable.com"]
  ),
  genericAdapter("adapter.webhook", "webhook", "Generic Webhook", "webhook.post", "webhook.token", [
    "*"
  ]),
  genericAdapter(
    "adapter.database",
    "database",
    "Database",
    "database.query",
    "database.connection",
    ["database"]
  ),
  createOtlpExportAdapterMetadata()
] as const satisfies readonly AdapterMetadata[];

export const mockAdapterMetadata = builtinAdapterMetadata.map((metadata) => ({
  ...metadata,
  id: `${metadata.id}.fake`,
  displayName: `Mock ${metadata.displayName}`,
  networkPolicy: noneNetworkPolicy,
  live: false
})) as readonly AdapterMetadata[];

export const fakeAdapterMetadata = mockAdapterMetadata;

export class MockAdapter implements Adapter {
  readonly invocations: RecordedAdapterInvocation[] = [];

  public constructor(public readonly metadata: AdapterMetadata) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    if (invocation.adapterId !== this.metadata.id) {
      throw new Error(
        `Invocation targeted adapter '${invocation.adapterId}' but mock adapter is '${this.metadata.id}'.`
      );
    }
    if (
      !this.metadata.operations.some(
        (candidate) =>
          candidate.name === invocation.operation &&
          candidate.version === invocation.operationVersion
      )
    ) {
      throw new Error(
        `Adapter '${this.metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
      );
    }
    for (const required of this.metadata.requiredSecrets) {
      if (!invocation.secretRefs[required.name]) {
        throw new Error(
          `Adapter '${this.metadata.id}' requires secret reference '${required.name}'.`
        );
      }
    }

    const recorded = {
      ...invocation,
      secretRefs: Object.fromEntries(
        Object.keys(invocation.secretRefs).map((name) => [name, "[REDACTED]"])
      ),
      sequence: this.invocations.length + 1
    } satisfies RecordedAdapterInvocation;
    this.invocations.push(recorded);

    const providerResponseId = deterministicResponseId(invocation);
    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status: "succeeded",
      output: createMockOperationOutput(invocation, providerResponseId),
      providerMetadata: {
        adapterId: invocation.adapterId,
        provider: this.metadata.kind,
        providerResponseId,
        mock: true,
        sequence: recorded.sequence,
        operation: invocation.operation
      },
      auditEvents: [
        {
          id: `audit.${providerResponseId}`,
          timestamp: "2026-05-18T00:00:00.000Z",
          level: "info",
          message: `Mock adapter '${this.metadata.id}' recorded '${invocation.operation}'.`
        }
      ]
    };
  }
}

export const FakeAdapter = MockAdapter;

export function createMockAdapter(metadata: AdapterMetadata): MockAdapter {
  return new MockAdapter(metadata);
}

export const createFakeAdapter = createMockAdapter;

export function createDefaultMockAdapters(): Map<string, MockAdapter> {
  return new Map(
    [...builtinAdapterMetadata, ...mockAdapterMetadata].map((metadata) => [
      metadata.id,
      createMockAdapter(metadata)
    ])
  );
}

export const createDefaultFakeAdapters = createDefaultMockAdapters;

export function requireMockAdapter(
  adapterId: string,
  adapters: ReadonlyMap<string, MockAdapter> = createDefaultMockAdapters()
): MockAdapter {
  const adapterInstance = adapters.get(adapterId);
  if (!adapterInstance) {
    throw new Error(`Unknown mock adapter '${adapterId}'.`);
  }
  return adapterInstance;
}

export const requireFakeAdapter = requireMockAdapter;

export function createDefaultLiveAdapters(): Map<string, Adapter> {
  return new Map(
    builtinAdapterMetadata.map((metadata) => [
      metadata.id,
      {
        metadata,
        async invoke() {
          throw new Error(`Adapter '${metadata.id}' is shelved in this DFIR build.`);
        }
      }
    ])
  );
}

export interface OtlpTraceExportOptions {
  readonly fetch?: typeof fetch | undefined;
}

export interface OtlpTraceExportResult {
  readonly accepted: boolean;
  readonly statusCode: number;
  readonly spanCount: number;
  readonly endpoint: string;
  readonly responseText?: string | undefined;
}

export interface OtlpTraceEvent {
  readonly sourceAgent: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId: string;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: string;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
  readonly classification?: string | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
  readonly policyAction?: string | undefined;
}

export interface PromotedSkillOtlpTraceInput {
  readonly endpoint: string;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly serviceName?: string | undefined;
  readonly serviceVersion?: string | undefined;
  readonly runId: string;
  readonly skillId: string;
  readonly sourceAgent: string;
  readonly promotedAt: string;
  readonly events: readonly OtlpTraceEvent[];
}

export interface OtlpTraceExportRequest {
  readonly endpoint: string;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly payload: OtlpJsonExportTraceServiceRequest;
  readonly fetch?: typeof fetch | undefined;
}

export type OtlpJsonExportTraceServiceRequest = JsonRecord;

export class OtlpExportAdapter implements Adapter {
  public readonly metadata = createOtlpExportAdapterMetadata();
  private readonly fetchImpl: typeof fetch;

  public constructor(options: OtlpTraceExportOptions = {}) {
    this.fetchImpl = options.fetch ?? fetch;
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    const endpoint = stringField(invocation.payload.endpoint);
    if (!endpoint) {
      throw new Error("OTLP trace export requires payload.endpoint.");
    }
    const payload = jsonRecordField(invocation.payload.payload) ?? invocation.payload;
    const result = await exportOtlpTraces({ endpoint, payload, fetch: this.fetchImpl });
    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status: result.accepted ? "succeeded" : "failed",
      output: result as unknown as JsonRecord,
      providerMetadata: {
        adapterId: invocation.adapterId,
        provider: "otlp",
        providerResponseId: `otlp.${result.statusCode}.${hashText(`${result.endpoint}:${result.spanCount}`).slice(0, 12)}`,
        mock: false,
        sequence: invocation.context.attempt,
        operation: invocation.operation
      },
      auditEvents: [
        {
          id: `audit.otlp.${invocation.context.runId}.${invocation.context.nodeId}.${invocation.context.attempt}`,
          timestamp: new Date().toISOString(),
          level: result.accepted ? "info" : "error",
          message: `OTLP trace export sent ${result.spanCount} spans.`
        }
      ]
    };
  }
}

export function createOtlpExportAdapterMetadata(): AdapterMetadata {
  return {
    id: "adapter.otlp.export",
    kind: "otlp",
    displayName: "OTLP Trace Export",
    version: "1.0.0",
    capabilities: ["otlp-trace-export"],
    operations: [
      {
        name: "otlp.traces.export",
        version: "1.0.0",
        description: "Exports Kelp promoted trajectory spans over OTLP/HTTP JSON.",
        inputSchema: objectSchema,
        outputSchema: objectSchema
      }
    ],
    requiredSecrets: [],
    networkPolicy: { mode: "declared", allowedHosts: ["*"] },
    rateLimit: { maxRequests: 120, perSeconds: 60 },
    retry: { maxAttempts: 3, backoffSeconds: 2, retryableErrorCodes: ["OTLP_EXPORT_FAILED"] },
    fixtures: [],
    live: true
  };
}

export function createPromotedSkillOtlpTracePayload(
  input: PromotedSkillOtlpTraceInput
): OtlpJsonExportTraceServiceRequest {
  const traceId = hashText(`${input.runId}:${input.skillId}`).slice(0, 32);
  return {
    resourceSpans: [
      {
        resource: {
          attributes: [
            stringAttribute("service.name", input.serviceName ?? "kelpclaw-cli"),
            stringAttribute("service.version", input.serviceVersion ?? "0.1.0"),
            stringAttribute("kelpclaw.run.id", input.runId),
            stringAttribute("kelpclaw.skill.id", input.skillId),
            stringAttribute("kelpclaw.source_agent", input.sourceAgent)
          ]
        },
        scopeSpans: [
          {
            scope: { name: "kelpclaw.agent-hooks" },
            spans: input.events.map((event) => ({
              traceId,
              spanId: hashText(`${traceId}:${event.chainIndex}:${event.contentHash}`).slice(0, 16),
              name: `${event.toolName} ${event.hookEvent}`,
              startTimeUnixNano: unixNano(event.startedAt, input.promotedAt),
              endTimeUnixNano: unixNano(event.finishedAt ?? event.startedAt, input.promotedAt),
              attributes: [
                stringAttribute("tool.name", event.toolName),
                stringAttribute("event.status", event.status),
                stringAttribute("kelpclaw.hook_event", event.hookEvent),
                stringAttribute("kelpclaw.tool_use_id", event.toolUseId),
                intAttribute("kelpclaw.chain_index", event.chainIndex),
                stringAttribute("kelpclaw.content_hash", event.contentHash),
                stringAttribute("kelpclaw.prev_event_hash", event.prevEventHash)
              ]
            }))
          }
        ]
      }
    ]
  };
}

export async function exportOtlpTraces(
  input: OtlpTraceExportRequest
): Promise<OtlpTraceExportResult> {
  const fetchImpl = input.fetch ?? fetch;
  const response = await fetchImpl(input.endpoint, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...(input.headers ?? {})
    },
    body: JSON.stringify(input.payload)
  });
  const responseText = await response.text();
  return {
    accepted: response.ok,
    statusCode: response.status,
    spanCount: countOtlpSpans(input.payload),
    endpoint: input.endpoint,
    ...(responseText ? { responseText } : {})
  };
}

function adapter(input: {
  readonly id: string;
  readonly kind: AdapterMetadata["kind"];
  readonly displayName: string;
  readonly capabilities: readonly string[];
  readonly requiredSecrets: readonly AdapterSecretRequirement[];
  readonly allowedHosts: readonly string[];
  readonly operations: readonly AdapterOperationDefinition[];
}): AdapterMetadata {
  return {
    id: input.id,
    kind: input.kind,
    displayName: input.displayName,
    version: "1.0.0",
    capabilities: input.capabilities,
    operations: input.operations,
    requiredSecrets: input.requiredSecrets,
    networkPolicy: { mode: "declared", allowedHosts: input.allowedHosts },
    rateLimit: defaultRateLimit,
    retry: defaultRetry,
    fixtures: [],
    live: true
  };
}

function genericAdapter(
  id: string,
  kind: AdapterMetadata["kind"],
  displayName: string,
  operationName: string,
  secretName: string,
  allowedHosts: readonly string[]
): AdapterMetadata {
  return adapter({
    id,
    kind,
    displayName,
    capabilities: [operationName],
    requiredSecrets: [secret(secretName, `${displayName} credential reference.`)],
    allowedHosts,
    operations: [operation(operationName, `${displayName} operation.`)]
  });
}

function operation(name: string, description: string): AdapterOperationDefinition {
  return {
    name,
    version: "1.0.0",
    description,
    inputSchema: objectSchema,
    outputSchema: objectSchema
  };
}

function secret(name: string, description: string): AdapterSecretRequirement {
  return {
    name,
    description,
    mockRef: `mock:${name}`
  };
}

function deterministicResponseId(invocation: AdapterInvocation): string {
  const idempotencyKey =
    invocation.idempotencyKey ??
    stableJsonStringify({
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      payload: invocation.payload,
      context: invocation.context as unknown as JsonRecord
    });
  return `mock.${sanitize(invocation.adapterId)}.${sanitize(invocation.operation)}.${hashText(idempotencyKey).slice(0, 16)}`;
}

function createMockOperationOutput(
  invocation: AdapterInvocation,
  providerResponseId: string
): JsonRecord {
  switch (invocation.operation) {
    case "gmail.trigger.poll":
    case "gmail.receipts.search":
      return {
        receipts: [
          { id: "receipt-1", merchant: "Acme Supplies", total: 42.1, currency: "USD" },
          { id: "receipt-2", merchant: "Contoso Travel", total: 118.5, currency: "USD" }
        ],
        query: stringField(invocation.payload.query) ?? "",
        providerResponseId
      };
    case "sheets.rows.append": {
      const rows = Array.isArray(invocation.payload.rows) ? invocation.payload.rows : [];
      return {
        channel: "sheets",
        spreadsheetId: stringField(invocation.payload.spreadsheetId) ?? "sheet.receipts",
        range: stringField(invocation.payload.range) ?? "Receipts!A:D",
        appendedRows: rows.length,
        rows,
        providerResponseId
      };
    }
    case "email.approval.request":
    case "email.results.send":
      return {
        messageId: providerResponseId,
        channel: "email",
        delivered: true,
        to: stringField(invocation.payload.to) ?? "owner@example.com",
        providerResponseId
      };
    case "whatsapp.alert.send":
      return { messageId: providerResponseId, channel: "whatsapp", delivered: true };
    case "telegram.alert.send":
      return { messageId: providerResponseId, channel: "telegram", delivered: true };
    default:
      return { recorded: true, providerResponseId };
  }
}

function stringField(value: JsonValue | undefined): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function jsonRecordField(value: JsonValue | undefined): JsonRecord | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as JsonRecord)
    : undefined;
}

function countOtlpSpans(payload: JsonRecord): number {
  const resourceSpans = Array.isArray(payload.resourceSpans) ? payload.resourceSpans : [];
  return resourceSpans.reduce<number>((total, resourceSpan) => {
    const record = jsonRecordField(resourceSpan as JsonValue);
    const scopeSpans = Array.isArray(record?.scopeSpans) ? record.scopeSpans : [];
    return (
      total +
      scopeSpans.reduce<number>((scopeTotal, scopeSpan) => {
        const scopeRecord = jsonRecordField(scopeSpan as JsonValue);
        return scopeTotal + (Array.isArray(scopeRecord?.spans) ? scopeRecord.spans.length : 0);
      }, 0)
    );
  }, 0);
}

function stringAttribute(key: string, value: string): JsonRecord {
  return { key, value: { stringValue: value } };
}

function intAttribute(key: string, value: number): JsonRecord {
  return { key, value: { intValue: String(value) } };
}

function unixNano(value: string, fallback: string): string {
  const timestamp = Date.parse(value);
  const millis = Number.isFinite(timestamp) ? timestamp : Date.parse(fallback);
  return BigInt(millis * 1_000_000).toString();
}

function hashText(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function sanitize(value: string): string {
  return value.replace(/[^a-z0-9]+/giu, "-").replace(/^-+|-+$/gu, "");
}
