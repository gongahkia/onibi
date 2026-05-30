import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";
export interface HttpAdapterRoute {
    readonly operation: string;
    readonly version: string;
    readonly method: string;
    readonly url: string;
    readonly auth?: HttpAdapterAuth | undefined;
    readonly headers?: Readonly<Record<string, string>> | undefined;
    readonly bodyPayloadKey?: string | undefined;
    readonly pathKeys?: readonly string[] | undefined;
    readonly urlPayloadKey?: string | undefined;
}
export interface HttpAdapterAuth {
    readonly secretName: string;
    readonly scheme: "apiKey" | "bearer" | "basic";
    readonly location?: "header" | "query" | "cookie" | undefined;
    readonly parameterName?: string | undefined;
}
export interface HttpAdapterOptions {
    readonly fetch?: typeof fetch | undefined;
}
export declare class HttpAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    private readonly routes;
    private readonly fetchImpl;
    constructor(metadata: AdapterMetadata, routes: readonly HttpAdapterRoute[], options?: HttpAdapterOptions);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export declare function createHttpAdapterMetadata(input: {
    readonly id: string;
    readonly kind?: AdapterMetadata["kind"] | undefined;
    readonly displayName: string;
    readonly version?: string | undefined;
    readonly allowedHosts: readonly string[];
    readonly operations: AdapterMetadata["operations"];
    readonly requiredSecrets?: AdapterMetadata["requiredSecrets"] | undefined;
    readonly live?: boolean | undefined;
}): AdapterMetadata;
//# sourceMappingURL=http-adapter.d.ts.map