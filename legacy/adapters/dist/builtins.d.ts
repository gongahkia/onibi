import type { MockAdapter } from "./mock-adapter.js";
import type { AdapterMetadata } from "./types.js";
export declare const builtinAdapterMetadata: readonly [AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata, AdapterMetadata];
export declare const mockAdapterMetadata: readonly AdapterMetadata[];
export declare const fakeAdapterMetadata: readonly AdapterMetadata[];
export declare function createDefaultMockAdapters(): Map<string, MockAdapter>;
export declare const createDefaultFakeAdapters: typeof createDefaultMockAdapters;
export declare function requireMockAdapter(adapterId: string, adapters?: Map<string, MockAdapter>): MockAdapter;
export declare const requireFakeAdapter: typeof requireMockAdapter;
//# sourceMappingURL=builtins.d.ts.map