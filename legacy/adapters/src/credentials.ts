import type { AdapterMetadata } from "./types.js";

export type AdapterCredentialValidationCode =
  | "ADAPTER_SECRET_MISSING"
  | "ADAPTER_SECRET_RAW_VALUE"
  | "ADAPTER_REAL_CREDENTIALS_REQUIRED";

export interface AdapterCredentialValidationIssue {
  readonly code: AdapterCredentialValidationCode;
  readonly message: string;
  readonly adapterId: string;
  readonly secretName: string;
}

export class AdapterCredentialError extends Error {
  public readonly issues: readonly AdapterCredentialValidationIssue[];

  public constructor(issues: readonly AdapterCredentialValidationIssue[]) {
    super(issues.map((issue) => issue.message).join(", "));
    this.name = "AdapterCredentialError";
    this.issues = issues;
  }
}

export function validateAdapterCredentialRefs(
  metadata: AdapterMetadata,
  secretRefs: Readonly<Record<string, string>>,
  options: { readonly requireLiveCredentials?: boolean } = {}
): readonly AdapterCredentialValidationIssue[] {
  const issues: AdapterCredentialValidationIssue[] = [];

  for (const secret of metadata.requiredSecrets) {
    const value = secretRefs[secret.name];
    if (!value) {
      issues.push({
        code: "ADAPTER_SECRET_MISSING",
        message: `Missing secret reference '${secret.name}' for adapter '${metadata.id}'.`,
        adapterId: metadata.id,
        secretName: secret.name
      });
      continue;
    }

    if (looksLikeRawSecret(value)) {
      issues.push({
        code: "ADAPTER_SECRET_RAW_VALUE",
        message: `Secret '${secret.name}' for adapter '${metadata.id}' must be a reference, not a raw value.`,
        adapterId: metadata.id,
        secretName: secret.name
      });
    }

    if ((metadata.live || options.requireLiveCredentials) && value.startsWith("mock:")) {
      issues.push({
        code: "ADAPTER_REAL_CREDENTIALS_REQUIRED",
        message: `Real adapter '${metadata.id}' requires a non-mock credential reference for '${secret.name}'.`,
        adapterId: metadata.id,
        secretName: secret.name
      });
    }
  }

  return issues;
}

export function assertAdapterCredentialRefs(
  metadata: AdapterMetadata,
  secretRefs: Readonly<Record<string, string>>,
  options: { readonly requireLiveCredentials?: boolean } = {}
): void {
  const issues = validateAdapterCredentialRefs(metadata, secretRefs, options);
  if (issues.length > 0) {
    throw new AdapterCredentialError(issues);
  }
}

function looksLikeRawSecret(value: string): boolean {
  return value.startsWith("raw:") || value.includes("\n");
}
