import { createHash } from "node:crypto";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";
import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";

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

export type OtlpJsonExportTraceServiceRequest = JsonRecord;

export class OtlpExportAdapter implements Adapter {
  public readonly metadata = createOtlpExportAdapterMetadata();
  private readonly fetchImpl: typeof fetch;

  public constructor(options: OtlpTraceExportOptions = {}) {
    this.fetchImpl = options.fetch ?? fetch;
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    if (invocation.adapterId !== this.metadata.id) {
      throw new Error(
        `Invocation targeted adapter '${invocation.adapterId}' but adapter is '${this.metadata.id}'.`
      );
    }
    if (invocation.operation !== "otlp.traces.export") {
      throw new Error(`OTLP adapter does not support operation '${invocation.operation}'.`);
    }
    const endpoint = stringField(invocation.payload, "endpoint");
    if (!endpoint) {
      throw new Error("OTLP trace export requires payload.endpoint.");
    }
    const payload = createPromotedSkillOtlpTracePayload({
      endpoint,
      headers: stringRecord(invocation.payload.headers),
      serviceName: stringField(invocation.payload, "serviceName"),
      serviceVersion: stringField(invocation.payload, "serviceVersion"),
      runId: requireStringField(invocation.payload, "runId"),
      skillId: requireStringField(invocation.payload, "skillId"),
      sourceAgent: requireStringField(invocation.payload, "sourceAgent"),
      promotedAt: requireStringField(invocation.payload, "promotedAt"),
      events: traceEventsFromJson(invocation.payload.events)
    });
    const result = await exportOtlpTraces({
      endpoint,
      headers: stringRecord(invocation.payload.headers),
      payload,
      fetch: this.fetchImpl
    });

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
      ...(result.accepted
        ? {}
        : {
            error: {
              code: "OTLP_EXPORT_FAILED",
              message: `OTLP export failed with HTTP ${result.statusCode}.`,
              retryable:
                result.statusCode === 408 || result.statusCode === 429 || result.statusCode >= 500
            }
          }),
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
        description: "Exports KelpClaw promoted trajectory spans over OTLP/HTTP JSON.",
        inputSchema: {
          type: "object",
          required: ["endpoint", "runId", "skillId", "sourceAgent", "promotedAt", "events"],
          properties: {
            endpoint: { type: "string" },
            headers: { type: "object", additionalProperties: { type: "string" } },
            serviceName: { type: "string" },
            serviceVersion: { type: "string" },
            runId: { type: "string" },
            skillId: { type: "string" },
            sourceAgent: { type: "string" },
            promotedAt: { type: "string" },
            events: { type: "array" }
          }
        },
        outputSchema: {
          type: "object",
          required: ["accepted", "statusCode", "spanCount", "endpoint"],
          properties: {
            accepted: { type: "boolean" },
            statusCode: { type: "number" },
            spanCount: { type: "number" },
            endpoint: { type: "string" }
          }
        }
      }
    ],
    requiredSecrets: [],
    networkPolicy: {
      mode: "declared",
      allowedHosts: ["*"]
    },
    rateLimit: {
      maxRequests: 120,
      perSeconds: 60
    },
    retry: {
      maxAttempts: 3,
      backoffSeconds: 2,
      retryableErrorCodes: ["OTLP_EXPORT_FAILED"]
    },
    fixtures: [],
    live: true
  };
}

export function createPromotedSkillOtlpTracePayload(
  input: PromotedSkillOtlpTraceInput
): OtlpJsonExportTraceServiceRequest {
  const traceId = hexDigest(`${input.runId}:${input.skillId}`, 32);
  const spans = input.events.map((event, index) => {
    const spanId = hexDigest(
      `${input.runId}:${input.skillId}:${event.chainIndex}:${event.contentHash}`,
      16
    );
    const previous = input.events[index - 1];
    const parentSpanId = previous
      ? hexDigest(
          `${input.runId}:${input.skillId}:${previous.chainIndex}:${previous.contentHash}`,
          16
        )
      : undefined;
    const start = unixNano(event.startedAt, input.promotedAt);
    const end = unixNano(event.finishedAt ?? event.startedAt, input.promotedAt);
    return withoutUndefined({
      traceId,
      spanId,
      ...(parentSpanId ? { parentSpanId } : {}),
      name: `${event.toolName} ${event.hookEvent}`,
      kind: 1,
      startTimeUnixNano: start,
      endTimeUnixNano: end === start ? (BigInt(end) + 1_000_000n).toString() : end,
      attributes: attributes({
        "kelpclaw.run.id": input.runId,
        "kelpclaw.skill.id": input.skillId,
        "kelpclaw.source_agent": event.sourceAgent,
        "kelpclaw.hook_event": event.hookEvent,
        "kelpclaw.tool_use_id": event.toolUseId,
        "kelpclaw.chain_index": event.chainIndex,
        "kelpclaw.content_hash": event.contentHash,
        "kelpclaw.prev_event_hash": event.prevEventHash,
        "kelpclaw.classification": event.classification,
        "kelpclaw.policy_action": event.policyAction,
        "tool.name": event.toolName,
        "event.status": event.status,
        "code.function": event.hookEvent,
        "input.hash": hashJson(event.args),
        "output.hash": event.result === undefined ? undefined : hashJson(event.result)
      }),
      status: {
        code: event.status === "failed" || event.status === "denied" ? 2 : 1
      }
    });
  });
  return {
    resourceSpans: [
      {
        resource: {
          attributes: attributes({
            "service.name": input.serviceName ?? "kelpclaw-api",
            "service.version": input.serviceVersion ?? "0.1.0",
            "telemetry.sdk.name": "kelpclaw-otlp-json",
            "kelpclaw.run.id": input.runId,
            "kelpclaw.skill.id": input.skillId,
            "kelpclaw.source_agent": input.sourceAgent
          })
        },
        scopeSpans: [
          {
            scope: {
              name: "kelpclaw.trajectory",
              version: "1.0.0"
            },
            spans
          }
        ]
      }
    ]
  };
}

export async function exportOtlpTraces(input: {
  readonly endpoint: string;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly payload: OtlpJsonExportTraceServiceRequest;
  readonly fetch?: typeof fetch | undefined;
}): Promise<OtlpTraceExportResult> {
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
    spanCount: spanCount(input.payload),
    endpoint: input.endpoint,
    ...(responseText ? { responseText } : {})
  };
}

function traceEventsFromJson(value: JsonValue | undefined): readonly OtlpTraceEvent[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter(isJsonRecord).map((event) => ({
    sourceAgent: requireStringField(event, "sourceAgent"),
    hookEvent: requireStringField(event, "hookEvent"),
    toolName: requireStringField(event, "toolName"),
    toolUseId: requireStringField(event, "toolUseId"),
    args: isJsonRecord(event.args) ? event.args : {},
    ...(event.result !== undefined ? { result: event.result } : {}),
    status: requireStringField(event, "status"),
    contentHash: requireStringField(event, "contentHash"),
    prevEventHash: requireStringField(event, "prevEventHash"),
    chainIndex: typeof event.chainIndex === "number" ? event.chainIndex : 0,
    ...(typeof event.classification === "string" ? { classification: event.classification } : {}),
    startedAt: requireStringField(event, "startedAt"),
    ...(typeof event.finishedAt === "string" ? { finishedAt: event.finishedAt } : {}),
    ...(typeof event.policyAction === "string" ? { policyAction: event.policyAction } : {})
  }));
}

function attributes(values: Readonly<Record<string, JsonValue | undefined>>) {
  const items: { readonly key: string; readonly value: JsonRecord }[] = [];
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined) {
      items.push({ key, value: otlpAnyValue(value) });
    }
  }
  return items;
}

function otlpAnyValue(value: JsonValue): JsonRecord {
  if (typeof value === "string") {
    return { stringValue: value };
  }
  if (typeof value === "number") {
    return Number.isInteger(value) ? { intValue: String(value) } : { doubleValue: value };
  }
  if (typeof value === "boolean") {
    return { boolValue: value };
  }
  return { stringValue: JSON.stringify(value) };
}

function unixNano(value: string, fallback: string): string {
  const millis = Date.parse(value);
  const fallbackMillis = Date.parse(fallback);
  return BigInt(Number.isFinite(millis) ? millis : fallbackMillis).valueOf() * 1_000_000n + "";
}

function spanCount(payload: OtlpJsonExportTraceServiceRequest): number {
  const resourceSpans: unknown[] = Array.isArray(payload.resourceSpans)
    ? payload.resourceSpans
    : [];
  return resourceSpans.reduce<number>((total, resourceSpan) => {
    if (!isJsonRecord(resourceSpan) || !Array.isArray(resourceSpan.scopeSpans)) {
      return total;
    }
    return (
      total +
      resourceSpan.scopeSpans.reduce<number>((scopeTotal, scopeSpan) => {
        if (!isJsonRecord(scopeSpan) || !Array.isArray(scopeSpan.spans)) {
          return scopeTotal;
        }
        return scopeTotal + scopeSpan.spans.length;
      }, 0)
    );
  }, 0);
}

function hashJson(value: JsonValue): string {
  return `sha256:${hashText(typeof value === "string" ? value : JSON.stringify(value))}`;
}

function hexDigest(value: string, bytes: 16 | 32): string {
  return hashText(value).slice(0, bytes * 2);
}

function hashText(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function stringField(record: JsonRecord, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function requireStringField(record: JsonRecord, key: string): string {
  const value = stringField(record, key);
  if (!value) {
    throw new Error(`OTLP payload field '${key}' must be a non-empty string.`);
  }
  return value;
}

function stringRecord(value: JsonValue | undefined): Readonly<Record<string, string>> {
  if (!isJsonRecord(value)) {
    return {};
  }
  const record: Record<string, string> = {};
  for (const [key, entry] of Object.entries(value)) {
    if (typeof entry === "string") {
      record[key] = entry;
    }
  }
  return record;
}

function isJsonRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function withoutUndefined<T extends JsonRecord>(value: T): T {
  return Object.fromEntries(Object.entries(value).filter(([, entry]) => entry !== undefined)) as T;
}
