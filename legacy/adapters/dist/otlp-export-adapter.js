import { createHash } from "node:crypto";
export class OtlpExportAdapter {
    metadata = createOtlpExportAdapterMetadata();
    fetchImpl;
    constructor(options = {}) {
        this.fetchImpl = options.fetch ?? fetch;
    }
    async invoke(invocation) {
        if (invocation.adapterId !== this.metadata.id) {
            throw new Error(`Invocation targeted adapter '${invocation.adapterId}' but adapter is '${this.metadata.id}'.`);
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
            output: result,
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
                        retryable: result.statusCode === 408 || result.statusCode === 429 || result.statusCode >= 500
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
export function createOtlpExportAdapterMetadata() {
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
export function createPromotedSkillOtlpTracePayload(input) {
    const traceId = hexDigest(`${input.runId}:${input.skillId}`, 32);
    const spans = input.events.map((event, index) => {
        const spanId = hexDigest(`${input.runId}:${input.skillId}:${event.chainIndex}:${event.contentHash}`, 16);
        const previous = input.events[index - 1];
        const parentSpanId = previous
            ? hexDigest(`${input.runId}:${input.skillId}:${previous.chainIndex}:${previous.contentHash}`, 16)
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
            endTimeUnixNano: end === start ? (BigInt(end) + 1000000n).toString() : end,
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
export async function exportOtlpTraces(input) {
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
function traceEventsFromJson(value) {
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
function attributes(values) {
    const items = [];
    for (const [key, value] of Object.entries(values)) {
        if (value !== undefined) {
            items.push({ key, value: otlpAnyValue(value) });
        }
    }
    return items;
}
function otlpAnyValue(value) {
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
function unixNano(value, fallback) {
    const millis = Date.parse(value);
    const fallbackMillis = Date.parse(fallback);
    return BigInt(Number.isFinite(millis) ? millis : fallbackMillis).valueOf() * 1000000n + "";
}
function spanCount(payload) {
    const resourceSpans = Array.isArray(payload.resourceSpans)
        ? payload.resourceSpans
        : [];
    return resourceSpans.reduce((total, resourceSpan) => {
        if (!isJsonRecord(resourceSpan) || !Array.isArray(resourceSpan.scopeSpans)) {
            return total;
        }
        return (total +
            resourceSpan.scopeSpans.reduce((scopeTotal, scopeSpan) => {
                if (!isJsonRecord(scopeSpan) || !Array.isArray(scopeSpan.spans)) {
                    return scopeTotal;
                }
                return scopeTotal + scopeSpan.spans.length;
            }, 0));
    }, 0);
}
function hashJson(value) {
    return `sha256:${hashText(typeof value === "string" ? value : JSON.stringify(value))}`;
}
function hexDigest(value, bytes) {
    return hashText(value).slice(0, bytes * 2);
}
function hashText(value) {
    return createHash("sha256").update(value, "utf8").digest("hex");
}
function stringField(record, key) {
    const value = record[key];
    return typeof value === "string" && value.trim() ? value.trim() : undefined;
}
function requireStringField(record, key) {
    const value = stringField(record, key);
    if (!value) {
        throw new Error(`OTLP payload field '${key}' must be a non-empty string.`);
    }
    return value;
}
function stringRecord(value) {
    if (!isJsonRecord(value)) {
        return {};
    }
    const record = {};
    for (const [key, entry] of Object.entries(value)) {
        if (typeof entry === "string") {
            record[key] = entry;
        }
    }
    return record;
}
function isJsonRecord(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
function withoutUndefined(value) {
    return Object.fromEntries(Object.entries(value).filter(([, entry]) => entry !== undefined));
}
//# sourceMappingURL=otlp-export-adapter.js.map