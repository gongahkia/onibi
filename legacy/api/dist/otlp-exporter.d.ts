import type { TrajectoryBillOfMaterials, TrajectoryRun } from "@kelpclaw/codegen";
import type { OtlpJsonExportTraceServiceRequest } from "@kelpclaw/adapters";
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
export declare class DisabledApiOtlpExporter implements ApiOtlpExporter {
    exportPromotion(): Promise<ApiOtlpPromotionExportResult>;
}
export declare class HttpJsonApiOtlpExporter implements ApiOtlpExporter {
    private readonly endpoint;
    private readonly headers;
    private readonly serviceName;
    private readonly serviceVersion;
    private readonly fetchImpl;
    constructor(options: ConfiguredApiOtlpExporterOptions);
    exportPromotion(input: ApiOtlpPromotionExportInput): Promise<ApiOtlpPromotionExportResult>;
}
export declare function createConfiguredApiOtlpExporter(): ApiOtlpExporter;
//# sourceMappingURL=otlp-exporter.d.ts.map