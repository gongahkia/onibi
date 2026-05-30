import type { JsonRecord, JsonSchemaShape } from "@kelpclaw/workflow-spec";

export type AdapterKind =
  | "gmail"
  | "sheets"
  | "email"
  | "whatsapp"
  | "telegram"
  | "github"
  | "slack"
  | "discord"
  | "notion"
  | "linear"
  | "jira"
  | "airtable"
  | "webhook"
  | "database"
  | "http"
  | "openapi"
  | "mcp"
  | "otlp";
export type AdapterNetworkMode = "none" | "declared";
export type AdapterOperationStatus = "succeeded" | "failed";
export type AdapterAuditEventLevel = "info" | "error";

export interface AdapterNetworkPolicy {
  readonly mode: AdapterNetworkMode;
  readonly allowedHosts: readonly string[];
}

export interface AdapterRateLimitPolicy {
  readonly maxRequests: number;
  readonly perSeconds: number;
}

export interface AdapterRetryPolicy {
  readonly maxAttempts: number;
  readonly backoffSeconds: number;
  readonly retryableErrorCodes: readonly string[];
}

export interface AdapterSecretRequirement {
  readonly name: string;
  readonly description: string;
  readonly mockRef: string;
}

export interface AdapterOperationDefinition {
  readonly name: string;
  readonly version: string;
  readonly description: string;
  readonly inputSchema: JsonSchemaShape;
  readonly outputSchema: JsonSchemaShape;
  readonly metadata?: JsonRecord | undefined;
}

export interface AdapterFixturePayload {
  readonly id: string;
  readonly description: string;
  readonly operation: string;
  readonly input: JsonRecord;
  readonly output: JsonRecord;
}

export interface AdapterMetadata {
  readonly id: string;
  readonly kind: AdapterKind;
  readonly displayName: string;
  readonly version: string;
  readonly capabilities: readonly string[];
  readonly operations: readonly AdapterOperationDefinition[];
  readonly requiredSecrets: readonly AdapterSecretRequirement[];
  readonly networkPolicy: AdapterNetworkPolicy;
  readonly rateLimit: AdapterRateLimitPolicy;
  readonly retry: AdapterRetryPolicy;
  readonly fixtures: readonly AdapterFixturePayload[];
  readonly live: boolean;
}

export interface AdapterRuntimeContext {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly runId: string;
  readonly attempt: number;
}

export interface AdapterInvocation {
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
  readonly payload: JsonRecord;
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly secrets?: Readonly<Record<string, string>> | undefined;
  readonly context: AdapterRuntimeContext;
  readonly idempotencyKey?: string | undefined;
}

export interface AdapterProviderMetadata {
  readonly adapterId: string;
  readonly provider: AdapterKind;
  readonly providerResponseId: string;
  readonly mock: boolean;
  readonly sequence: number;
  readonly operation: string;
}

export interface AdapterErrorDetail {
  readonly code: string;
  readonly message: string;
  readonly retryable: boolean;
}

export interface AdapterAuditEvent {
  readonly id: string;
  readonly timestamp: string;
  readonly level: AdapterAuditEventLevel;
  readonly message: string;
}

export interface AdapterResult {
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
  readonly status: AdapterOperationStatus;
  readonly output: JsonRecord;
  readonly providerMetadata: AdapterProviderMetadata;
  readonly error?: AdapterErrorDetail | undefined;
  readonly auditEvents: readonly AdapterAuditEvent[];
}

export interface Adapter {
  readonly metadata: AdapterMetadata;
  invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}

export interface RecordedAdapterInvocation extends AdapterInvocation {
  readonly sequence: number;
}
