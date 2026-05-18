import type { JsonRecord } from "@kelpclaw/workflow-spec";

export type AdapterKind = "gmail" | "sheets" | "email" | "whatsapp" | "telegram";

export interface AdapterMetadata {
  readonly id: string;
  readonly kind: AdapterKind;
  readonly displayName: string;
  readonly capabilities: readonly string[];
  readonly live: false;
}

export interface AdapterInvocation {
  readonly adapterId: string;
  readonly operation: string;
  readonly payload: JsonRecord;
  readonly idempotencyKey?: string | undefined;
}

export interface AdapterResult {
  readonly adapterId: string;
  readonly operation: string;
  readonly status: "recorded";
  readonly receipt: JsonRecord;
}

export interface Adapter {
  readonly metadata: AdapterMetadata;
  invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}

export interface RecordedAdapterInvocation extends AdapterInvocation {
  readonly sequence: number;
}
