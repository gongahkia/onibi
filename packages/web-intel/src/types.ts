import type { JsonRecord } from "@kelpclaw/workflow-spec";

export type WebIntelProvider = "exa" | "tinyfish";

export type WebIntelOperation =
  | "web.search"
  | "web.fetch"
  | "web.answer"
  | "web.browser.session"
  | "web.browser.action"
  | "web.agent.task";

export type WebIntelEscalationLevel = "search" | "fetch" | "browser" | "agent";

export interface WebIntelRequest {
  readonly operation: WebIntelOperation;
  readonly provider?: WebIntelProvider;
  readonly query?: string;
  readonly url?: string;
  readonly question?: string;
  readonly goal?: string;
  readonly domains?: readonly string[];
  readonly numResults?: number;
  readonly storeFullContent?: boolean;
  readonly browserSessionId?: string;
  readonly action?: string;
  readonly metadata?: JsonRecord;
}

export interface WebIntelSource {
  readonly provider: WebIntelProvider;
  readonly operation: WebIntelOperation;
  readonly title?: string;
  readonly url?: string;
  readonly excerpt?: string;
  readonly contentHash: string;
  readonly score?: number;
  readonly publishedDate?: string;
  readonly fullContentStored: boolean;
  readonly redacted: boolean;
}

export interface WebIntelEvent {
  readonly id: string;
  readonly timestamp: string;
  readonly toolName: string;
  readonly provider: WebIntelProvider;
  readonly operation: WebIntelOperation;
  readonly status: "succeeded" | "failed";
  readonly args: JsonRecord;
  readonly resultHash: string;
  readonly latencyMs: number;
  readonly sourceUrls: readonly string[];
  readonly contentStored: boolean;
  readonly contentRedacted: boolean;
  readonly error?: string;
}

export interface WebEvidenceBundle {
  readonly schemaVersion: "1.0.0";
  readonly generatedAt: string;
  readonly request: WebIntelRequest;
  readonly escalationLevel: WebIntelEscalationLevel;
  readonly selectedProvider: WebIntelProvider;
  readonly events: readonly WebIntelEvent[];
  readonly sources: readonly WebIntelSource[];
  readonly summary: {
    readonly sourceCount: number;
    readonly storedFullContent: boolean;
    readonly redacted: boolean;
    readonly errorCount: number;
  };
}

export interface WebIntelClientOptions {
  readonly fetch?: typeof fetch;
  readonly now?: () => Date;
  readonly exaApiKey?: string;
  readonly tinyfishApiKey?: string;
  readonly exaBaseUrl?: string;
  readonly tinyfishBaseUrl?: string;
}
