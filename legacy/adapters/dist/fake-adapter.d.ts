import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult, RecordedAdapterInvocation } from "./types.js";
export declare class FakeAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    readonly invocations: RecordedAdapterInvocation[];
    constructor(metadata: AdapterMetadata);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export declare function createFakeAdapter(metadata: AdapterMetadata): FakeAdapter;
//# sourceMappingURL=fake-adapter.d.ts.map