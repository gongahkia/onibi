import type { WebEvidenceBundle } from "./types.js";
export declare function writeWebEvidenceFiles(outDir: string, bundle: WebEvidenceBundle): Promise<readonly string[]>;
export declare function readWebEvidenceBundle(path: string): Promise<WebEvidenceBundle>;
export declare function webBom(bundle: WebEvidenceBundle): {
    schemaVersion: string;
    generatedAt: string;
    provider: import("./types.js").WebIntelProvider;
    operations: import("./types.js").WebIntelOperation[];
    sourceCount: number;
    sources: {
        url: string | undefined;
        title: string | undefined;
        contentHash: string;
        fullContentStored: boolean;
        redacted: boolean;
    }[];
    eventHashes: string[];
    bundleHash: string;
};
export declare function renderWebEvidenceHtml(bundle: WebEvidenceBundle): string;
//# sourceMappingURL=evidence.d.ts.map