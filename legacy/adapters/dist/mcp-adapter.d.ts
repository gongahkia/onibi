import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";
import type { WorkflowConnectorRecord } from "@kelpclaw/workflow-spec";
export interface ImportMcpConnectorInput {
    readonly id: string;
    readonly name?: string | undefined;
    readonly endpointUrl: string;
    readonly secretRefs?: Readonly<Record<string, string>> | undefined;
    readonly now?: string | undefined;
}
export declare function importMcpConnector(input: ImportMcpConnectorInput): Promise<WorkflowConnectorRecord>;
export declare function testMcpConnector(connector: WorkflowConnectorRecord): Promise<WorkflowConnectorRecord>;
export declare class McpToolAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    constructor(metadata: AdapterMetadata);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export declare function createMcpAdapter(connector: WorkflowConnectorRecord): Adapter;
//# sourceMappingURL=mcp-adapter.d.ts.map