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
export declare class OtlpExportAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    private readonly fetchImpl;
    constructor(options?: OtlpTraceExportOptions);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export declare function createOtlpExportAdapterMetadata(): AdapterMetadata;
export declare function createPromotedSkillOtlpTracePayload(input: PromotedSkillOtlpTraceInput): OtlpJsonExportTraceServiceRequest;
export declare function exportOtlpTraces(input: {
    readonly endpoint: string;
    readonly headers?: Readonly<Record<string, string>> | undefined;
    readonly payload: OtlpJsonExportTraceServiceRequest;
    readonly fetch?: typeof fetch | undefined;
}): Promise<OtlpTraceExportResult>;
//# sourceMappingURL=otlp-export-adapter.d.ts.map