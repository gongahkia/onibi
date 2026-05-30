import type { HttpAdapterOptions } from "./http-adapter.js";
import type { Adapter } from "./types.js";
import type { JsonRecord, WorkflowConnectorRecord } from "@kelpclaw/workflow-spec";
export interface ImportOpenApiConnectorInput {
    readonly id: string;
    readonly name?: string | undefined;
    readonly sourceUrl?: string | undefined;
    readonly document?: string | JsonRecord | undefined;
    readonly secretRefs?: Readonly<Record<string, string>> | undefined;
    readonly now?: string | undefined;
}
export declare function importOpenApiConnector(input: ImportOpenApiConnectorInput): Promise<WorkflowConnectorRecord>;
export declare function createOpenApiAdapter(connector: WorkflowConnectorRecord, options?: HttpAdapterOptions): Adapter;
export declare function testOpenApiConnector(connector: WorkflowConnectorRecord): Promise<WorkflowConnectorRecord>;
//# sourceMappingURL=openapi.d.ts.map