import type { TrajectoryBillOfMaterials, TrajectoryRun } from "@kelpclaw/codegen";
import {
  createPromotedSkillOtlpTracePayload,
  exportOtlpTraces
} from "@kelpclaw/adapters";
import type {
  OtlpJsonExportTraceServiceRequest,
  OtlpTraceExportResult
} from "@kelpclaw/adapters";
import type { SkillMetadata } from "@kelpclaw/skill-registry";

export interface ApiOtlpPromotionExportInput {
  readonly run: TrajectoryRun;
  readonly skill: SkillMetadata;
  readonly tbom: TrajectoryBillOfMaterials;
}

export interface ApiOtlpPromotionExportResult {
  readonly enabled: boolean;
  readonly status: "disabled" | "succeeded" | "failed";
  readonly spanCount: number;
  readonly endpoint?: string | undefined;
  readonly tracePayload?: OtlpJsonExportTraceServiceRequest | undefined;
  readonly error?: string | undefined;
}

export interface ApiOtlpExporter {
  exportPromotion(input: ApiOtlpPromotionExportInput): Promise<ApiOtlpPromotionExportResult>;
}

export interface ConfiguredApiOtlpExporterOptions {
  readonly endpoint?: string | undefined;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly serviceName?: string | undefined;
  readonly serviceVersion?: string | undefined;
  readonly fetch?: typeof fetch | undefined;
}

export class DisabledApiOtlpExporter implements ApiOtlpExporter {
  public async exportPromotion(): Promise<ApiOtlpPromotionExportResult> {
    return {
      enabled: false,
      status: "disabled",
      spanCount: 0
    };
  }
}

export class HttpJsonApiOtlpExporter implements ApiOtlpExporter {
  private readonly endpoint: string;
  private readonly headers: Readonly<Record<string, string>>;
  private readonly serviceName: string;
  private readonly serviceVersion: string;
  private readonly fetchImpl: typeof fetch;

  public constructor(options: ConfiguredApiOtlpExporterOptions) {
    if (!options.endpoint) {
      throw new Error("OTLP exporter requires an endpoint.");
    }
    this.endpoint = options.endpoint;
    this.headers = options.headers ?? {};
    this.serviceName = options.serviceName ?? "kelpclaw-api";
    this.serviceVersion = options.serviceVersion ?? "0.1.0";
    this.fetchImpl = options.fetch ?? fetch;
  }

  public async exportPromotion(
    input: ApiOtlpPromotionExportInput
  ): Promise<ApiOtlpPromotionExportResult> {
    const payload = createPromotedSkillOtlpTracePayload({
      endpoint: this.endpoint,
      headers: this.headers,
      serviceName: this.serviceName,
      serviceVersion: this.serviceVersion,
      runId: input.run.id,
      skillId: input.skill.id,
      sourceAgent: input.run.sourceAgent,
      promotedAt: new Date().toISOString(),
      events: input.run.events.map((event) => {
        const policyDecision = (
          event as { readonly policyDecision?: { readonly action?: string | undefined } | undefined }
        ).policyDecision;
        return {
          sourceAgent: event.sourceAgent,
          hookEvent: event.hookEvent,
          toolName: event.toolName,
          toolUseId: event.toolUseId,
          args: event.args,
          ...(event.result !== undefined ? { result: event.result } : {}),
          status: event.status,
          contentHash: event.contentHash,
          prevEventHash: event.prevEventHash,
          chainIndex: event.chainIndex,
          ...(event.classification ? { classification: event.classification } : {}),
          startedAt: event.startedAt,
          ...(event.finishedAt ? { finishedAt: event.finishedAt } : {}),
          ...(policyDecision?.action ? { policyAction: policyDecision.action } : {})
        };
      })
    });
    try {
      const result: OtlpTraceExportResult = await exportOtlpTraces({
        endpoint: this.endpoint,
        headers: this.headers,
        payload,
        fetch: this.fetchImpl
      });
      return {
        enabled: true,
        status: result.accepted ? "succeeded" : "failed",
        endpoint: result.endpoint,
        spanCount: result.spanCount,
        tracePayload: payload,
        ...(result.accepted
          ? {}
          : { error: `OTLP export failed with HTTP ${result.statusCode}.` })
      };
    } catch (error) {
      return {
        enabled: true,
        status: "failed",
        endpoint: this.endpoint,
        spanCount: 0,
        tracePayload: payload,
        error: error instanceof Error ? error.message : String(error)
      };
    }
  }
}

export function createConfiguredApiOtlpExporter(): ApiOtlpExporter {
  const endpoint =
    process.env.KELPCLAW_OTLP_TRACES_ENDPOINT ??
    process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ??
    tracesEndpointFromBase(
      process.env.KELPCLAW_OTLP_ENDPOINT ?? process.env.OTEL_EXPORTER_OTLP_ENDPOINT
    );
  if (!endpoint) {
    return new DisabledApiOtlpExporter();
  }
  return new HttpJsonApiOtlpExporter({
    endpoint,
    headers: {
      ...parseHeaders(process.env.OTEL_EXPORTER_OTLP_HEADERS),
      ...parseHeaders(process.env.KELPCLAW_OTLP_HEADERS),
      ...(process.env.DD_API_KEY ? { "DD-API-KEY": process.env.DD_API_KEY } : {})
    },
    serviceName: process.env.KELPCLAW_OTLP_SERVICE_NAME ?? process.env.OTEL_SERVICE_NAME,
    serviceVersion: process.env.KELPCLAW_OTLP_SERVICE_VERSION
  });
}

function tracesEndpointFromBase(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  return `${value.replace(/\/+$/u, "")}/v1/traces`;
}

function parseHeaders(value: string | undefined): Readonly<Record<string, string>> {
  if (!value) {
    return {};
  }
  if (value.trim().startsWith("{")) {
    const parsed = JSON.parse(value) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("OTLP headers JSON must be an object.");
    }
    const headers: Record<string, string> = {};
    for (const [name, headerValue] of Object.entries(parsed)) {
      if (typeof headerValue === "string") {
        headers[name] = headerValue;
      }
    }
    return headers;
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
