import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult, RecordedAdapterInvocation } from "./types.js";
export declare class MockAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    readonly invocations: RecordedAdapterInvocation[];
    constructor(metadata: AdapterMetadata);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export declare function createMockAdapter(metadata: AdapterMetadata): MockAdapter;
export declare const FakeAdapter: typeof MockAdapter;
export declare const createFakeAdapter: typeof createMockAdapter;
//# sourceMappingURL=mock-adapter.d.ts.map