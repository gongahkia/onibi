import { createHash } from "node:crypto";
import { stableJsonStringify, type JsonRecord, type JsonValue } from "@kelpclaw/workflow-spec";
import type {
  WebEvidenceBundle,
  WebIntelClientOptions,
  WebIntelEscalationLevel,
  WebIntelOperation,
  WebIntelProvider,
  WebIntelRequest,
  WebIntelSource
} from "./types.js";

const defaultExaBaseUrl = "https://api.exa.ai";
const defaultTinyfishBaseUrl = "https://api.tinyfish.ai";

type FetchLike = typeof fetch;

export class WebIntelClient {
  private readonly fetchImpl: FetchLike;
  private readonly now: () => Date;
  private readonly exaApiKey: string | undefined;
  private readonly tinyfishApiKey: string | undefined;
  private readonly exaBaseUrl: string;
  private readonly tinyfishBaseUrl: string;

  constructor(options: WebIntelClientOptions = {}) {
    this.fetchImpl = options.fetch ?? fetch;
    this.now = options.now ?? (() => new Date());
    this.exaApiKey = options.exaApiKey ?? process.env.EXA_API_KEY;
    this.tinyfishApiKey = options.tinyfishApiKey ?? process.env.TINYFISH_API_KEY;
    this.exaBaseUrl = trimTrailingSlash(options.exaBaseUrl ?? defaultExaBaseUrl);
    this.tinyfishBaseUrl = trimTrailingSlash(options.tinyfishBaseUrl ?? defaultTinyfishBaseUrl);
  }

  async run(request: WebIntelRequest): Promise<WebEvidenceBundle> {
    const provider = request.provider ?? defaultProviderForOperation(request.operation);
    const startedAt = Date.now();
    const payload = await this.dispatch(provider, request);
    const latencyMs = Math.max(0, Date.now() - startedAt);
    const sources = normalizeSources(payload, provider, request);
    const contentRedacted = sources.some((source) => source.redacted);
    const resultHash = hashJson(payload);
    const timestamp = this.now().toISOString();
    const event = {
      id: `web-event.${hashText(
        `${timestamp}:${provider}:${request.operation}:${resultHash}`
      ).slice("sha256:".length, "sha256:".length + 16)}`,
      timestamp,
      toolName: toolNameForWebRequest(request, provider),
      provider,
      operation: request.operation,
      status: "succeeded" as const,
      args: policyArgsForWebRequest(request, provider),
      resultHash,
      latencyMs,
      sourceUrls: sources.map((source) => source.url).filter((url): url is string => Boolean(url)),
      contentStored: Boolean(request.storeFullContent),
      contentRedacted
    };

    return {
      schemaVersion: "1.0.0",
      generatedAt: timestamp,
      request,
      escalationLevel: escalationLevelForOperation(request.operation),
      selectedProvider: provider,
      events: [event],
      sources,
      summary: {
        sourceCount: sources.length,
        storedFullContent: Boolean(request.storeFullContent),
        redacted: contentRedacted,
        errorCount: 0
      }
    };
  }

  private async dispatch(provider: WebIntelProvider, request: WebIntelRequest): Promise<unknown> {
    if (provider === "exa") {
      return this.callExa(request);
    }
    return this.callTinyfish(request);
  }

  private async callExa(request: WebIntelRequest): Promise<unknown> {
    const apiKey = requiredSecret(this.exaApiKey, "EXA_API_KEY");
    const path =
      request.operation === "web.fetch"
        ? "/contents"
        : request.operation === "web.answer"
          ? "/answer"
          : "/search";
    const body: JsonRecord =
      request.operation === "web.fetch"
        ? { urls: request.url ? [request.url] : [] }
        : request.operation === "web.answer"
          ? { query: requiredText(request.question ?? request.query, "question") }
          : {
              query: requiredText(request.query ?? request.goal, "query"),
              numResults: request.numResults ?? 5,
              ...(request.domains?.length ? { includeDomains: [...request.domains] } : {})
            };
    return this.postJson(`${this.exaBaseUrl}${path}`, body, {
      "x-api-key": apiKey
    });
  }

  private async callTinyfish(request: WebIntelRequest): Promise<unknown> {
    const apiKey = requiredSecret(this.tinyfishApiKey, "TINYFISH_API_KEY");
    const path =
      request.operation === "web.fetch"
        ? "/fetch"
        : request.operation === "web.search"
          ? "/search"
          : request.operation === "web.answer"
            ? "/answer"
            : request.operation === "web.agent.task"
              ? "/agent"
              : "/browser/sessions";
    const body: JsonRecord =
      request.operation === "web.fetch"
        ? { url: requiredText(request.url, "url") }
        : request.operation === "web.answer"
          ? { question: requiredText(request.question ?? request.query, "question") }
          : request.operation === "web.agent.task"
            ? { goal: requiredText(request.goal ?? request.query, "goal") }
            : request.operation === "web.browser.action"
              ? {
                  sessionId: requiredText(request.browserSessionId, "browserSessionId"),
                  action: requiredText(request.action, "action")
                }
              : {
                  query: requiredText(request.query ?? request.goal, "query"),
                  numResults: request.numResults ?? 5,
                  ...(request.domains?.length ? { domains: [...request.domains] } : {})
                };
    return this.postJson(`${this.tinyfishBaseUrl}${path}`, body, {
      authorization: `Bearer ${apiKey}`
    });
  }

  private async postJson(
    url: string,
    body: JsonRecord,
    headers: Readonly<Record<string, string>>
  ): Promise<unknown> {
    const response = await this.fetchImpl(url, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...headers
      },
      body: JSON.stringify(body)
    });
    const text = await response.text();
    const payload = text ? (JSON.parse(text) as unknown) : {};
    if (!response.ok) {
      throw new Error(`Web provider request failed with HTTP ${response.status}: ${text}`);
    }
    return payload;
  }
}

export function createWebIntelClient(options: WebIntelClientOptions = {}): WebIntelClient {
  return new WebIntelClient(options);
}

export function defaultProviderForOperation(operation: WebIntelOperation): WebIntelProvider {
  if (
    operation === "web.fetch" ||
    operation.startsWith("web.browser") ||
    operation === "web.agent.task"
  ) {
    return "tinyfish";
  }
  return "exa";
}

export function escalationLevelForOperation(operation: WebIntelOperation): WebIntelEscalationLevel {
  if (operation === "web.agent.task") {
    return "agent";
  }
  if (operation.startsWith("web.browser")) {
    return "browser";
  }
  if (operation === "web.fetch") {
    return "fetch";
  }
  return "search";
}

export function toolNameForWebRequest(
  request: WebIntelRequest,
  provider: WebIntelProvider = request.provider ?? defaultProviderForOperation(request.operation)
): string {
  if (provider === "exa") {
    if (request.operation === "web.fetch") {
      return "exa.contents";
    }
    if (request.operation === "web.answer") {
      return "exa.answer";
    }
    return "exa.search";
  }
  if (request.operation === "web.fetch") {
    return "tinyfish.fetch";
  }
  if (request.operation === "web.answer") {
    return "tinyfish.answer";
  }
  if (request.operation === "web.agent.task") {
    return "tinyfish.agent.run";
  }
  if (request.operation === "web.browser.action") {
    return "tinyfish.browser.action";
  }
  if (request.operation === "web.browser.session") {
    return "tinyfish.browser.session";
  }
  return "tinyfish.search";
}

export function policyArgsForWebRequest(
  request: WebIntelRequest,
  provider: WebIntelProvider = request.provider ?? defaultProviderForOperation(request.operation)
): JsonRecord {
  const args: JsonRecord = {
    operation: request.operation,
    provider,
    escalationLevel: escalationLevelForOperation(request.operation),
    storeFullContent: String(Boolean(request.storeFullContent))
  };
  setIfString(args, "query", request.query);
  setIfString(args, "url", request.url);
  setIfString(args, "question", request.question);
  setIfString(args, "goal", request.goal);
  setIfString(args, "browserSessionId", request.browserSessionId);
  setIfString(args, "action", request.action);
  if (request.domains?.length) {
    args.domains = request.domains.join(",");
  }
  if (request.numResults !== undefined) {
    args.numResults = request.numResults;
  }
  return args;
}

export function normalizeSources(
  payload: unknown,
  provider: WebIntelProvider,
  request: WebIntelRequest
): readonly WebIntelSource[] {
  const records = sourceRecords(payload);
  const sources = records
    .map((record) => sourceFromRecord(record, provider, request))
    .filter((source): source is WebIntelSource => Boolean(source));

  if (sources.length > 0) {
    return sources;
  }

  const fallbackExcerpt = firstString(jsonRecord(payload), [
    "answer",
    "content",
    "text",
    "markdown",
    "summary"
  ]);
  const fallbackRecord = jsonRecord(payload);
  const fallbackTitle = firstString(fallbackRecord, ["title", "name"]);
  const fallbackUrl = firstString(fallbackRecord, ["url", "link", "sourceUrl"]) ?? request.url;
  if (!fallbackExcerpt && !fallbackUrl && !fallbackTitle) {
    return [];
  }
  const { value: excerpt, redacted } = redactWebText(fallbackExcerpt ?? "");
  return [
    {
      provider,
      operation: request.operation,
      ...(fallbackTitle ? { title: fallbackTitle } : {}),
      ...(fallbackUrl ? { url: fallbackUrl } : {}),
      ...(excerpt ? { excerpt } : {}),
      contentHash: hashJson({ title: fallbackTitle, url: fallbackUrl, excerpt: fallbackExcerpt }),
      fullContentStored: Boolean(request.storeFullContent),
      redacted
    }
  ];
}

export function hashJson(value: unknown): string {
  return hashText(stableJsonStringify(toJsonValue(value)));
}

export function hashText(value: string): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

export function redactWebText(value: string): {
  readonly value: string;
  readonly redacted: boolean;
} {
  const redacted = value
    .replace(
      /\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|API_KEY|PRIVATE_KEY)[A-Z0-9_]*)\b\s*[:=]\s*["']?[^"',\s]+/giu,
      "$1=<redacted>"
    )
    .replace(/\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{12,}/gu, "<redacted-auth-header>")
    .replace(/\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/gu, "<redacted-email>");
  return { value: redacted, redacted: redacted !== value };
}

function sourceRecords(payload: unknown): readonly unknown[] {
  if (Array.isArray(payload)) {
    return payload;
  }
  const record = jsonRecord(payload);
  for (const field of ["results", "sources", "data", "items", "citations"]) {
    const value = record[field];
    if (Array.isArray(value)) {
      return value;
    }
  }
  const nested = jsonRecord(record.result);
  for (const field of ["results", "sources", "data", "items", "citations"]) {
    const value = nested[field];
    if (Array.isArray(value)) {
      return value;
    }
  }
  return [];
}

function sourceFromRecord(
  value: unknown,
  provider: WebIntelProvider,
  request: WebIntelRequest
): WebIntelSource | undefined {
  const record = jsonRecord(value);
  const rawExcerpt =
    typeof value === "string"
      ? value
      : firstString(record, [
          "text",
          "content",
          "excerpt",
          "snippet",
          "markdown",
          "summary",
          "answer"
        ]);
  const url = firstString(record, ["url", "link", "sourceUrl"]);
  const title = firstString(record, ["title", "name"]);
  const publishedDate = firstString(record, ["publishedDate", "published_at", "date"]);
  const score = numberField(record, "score");
  if (!rawExcerpt && !url && !title) {
    return undefined;
  }
  const { value: excerpt, redacted } = redactWebText(rawExcerpt ?? "");
  return {
    provider,
    operation: request.operation,
    ...(title ? { title } : {}),
    ...(url ? { url } : {}),
    ...(excerpt ? { excerpt } : {}),
    contentHash: hashJson({ title, url, excerpt: rawExcerpt, publishedDate }),
    ...(score !== undefined ? { score } : {}),
    ...(publishedDate ? { publishedDate } : {}),
    fullContentStored: Boolean(request.storeFullContent),
    redacted
  };
}

function toJsonValue(value: unknown): JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((entry) => toJsonValue(entry));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, entry]) => [
        key,
        toJsonValue(entry)
      ])
    );
  }
  return null;
}

function jsonRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as JsonRecord) : {};
}

function firstString(record: JsonRecord, fields: readonly string[]): string | undefined {
  for (const field of fields) {
    const value = record[field];
    if (typeof value === "string" && value.trim()) {
      return value;
    }
  }
  return undefined;
}

function numberField(record: JsonRecord, field: string): number | undefined {
  const value = record[field];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function setIfString(record: JsonRecord, key: string, value: string | undefined): void {
  if (value !== undefined && value.length > 0) {
    record[key] = value;
  }
}

function requiredSecret(value: string | undefined, name: string): string {
  if (!value) {
    throw new Error(`Missing required provider credential ${name}.`);
  }
  return value;
}

function requiredText(value: string | undefined, field: string): string {
  if (!value?.trim()) {
    throw new Error(`Web request field '${field}' is required.`);
  }
  return value;
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/u, "");
}
