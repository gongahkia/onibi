import { type JsonRecord } from "@kelpclaw/workflow-spec";
import type { WebEvidenceBundle, WebIntelClientOptions, WebIntelEscalationLevel, WebIntelOperation, WebIntelProvider, WebIntelRequest, WebIntelSource } from "./types.js";
export declare class WebIntelClient {
    private readonly fetchImpl;
    private readonly now;
    private readonly exaApiKey;
    private readonly tinyfishApiKey;
    private readonly exaBaseUrl;
    private readonly tinyfishBaseUrl;
    constructor(options?: WebIntelClientOptions);
    run(request: WebIntelRequest): Promise<WebEvidenceBundle>;
    private dispatch;
    private callExa;
    private callTinyfish;
    private postJson;
}
export declare function createWebIntelClient(options?: WebIntelClientOptions): WebIntelClient;
export declare function defaultProviderForOperation(operation: WebIntelOperation): WebIntelProvider;
export declare function escalationLevelForOperation(operation: WebIntelOperation): WebIntelEscalationLevel;
export declare function toolNameForWebRequest(request: WebIntelRequest, provider?: WebIntelProvider): string;
export declare function policyArgsForWebRequest(request: WebIntelRequest, provider?: WebIntelProvider): JsonRecord;
export declare function normalizeSources(payload: unknown, provider: WebIntelProvider, request: WebIntelRequest): readonly WebIntelSource[];
export declare function hashJson(value: unknown): string;
export declare function hashText(value: string): string;
export declare function redactWebText(value: string): {
    readonly value: string;
    readonly redacted: boolean;
};
//# sourceMappingURL=client.d.ts.map