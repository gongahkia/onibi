import type { AdapterMetadata } from "./types.js";
export type AdapterCredentialValidationCode = "ADAPTER_SECRET_MISSING" | "ADAPTER_SECRET_RAW_VALUE" | "ADAPTER_REAL_CREDENTIALS_REQUIRED";
export interface AdapterCredentialValidationIssue {
    readonly code: AdapterCredentialValidationCode;
    readonly message: string;
    readonly adapterId: string;
    readonly secretName: string;
}
export declare class AdapterCredentialError extends Error {
    readonly issues: readonly AdapterCredentialValidationIssue[];
    constructor(issues: readonly AdapterCredentialValidationIssue[]);
}
export declare function validateAdapterCredentialRefs(metadata: AdapterMetadata, secretRefs: Readonly<Record<string, string>>, options?: {
    readonly requireLiveCredentials?: boolean;
}): readonly AdapterCredentialValidationIssue[];
export declare function assertAdapterCredentialRefs(metadata: AdapterMetadata, secretRefs: Readonly<Record<string, string>>, options?: {
    readonly requireLiveCredentials?: boolean;
}): void;
//# sourceMappingURL=credentials.d.ts.map