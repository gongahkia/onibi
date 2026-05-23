import { randomUUID } from "node:crypto";
import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export interface HttpAdapterRoute {
  readonly operation: string;
  readonly version: string;
  readonly method: string;
  readonly url: string;
  readonly auth?: HttpAdapterAuth | undefined;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly bodyPayloadKey?: string | undefined;
  readonly pathKeys?: readonly string[] | undefined;
  readonly urlPayloadKey?: string | undefined;
}

export interface HttpAdapterAuth {
  readonly secretName: string;
  readonly scheme: "apiKey" | "bearer" | "basic";
  readonly location?: "header" | "query" | "cookie" | undefined;
  readonly parameterName?: string | undefined;
}

export interface HttpAdapterOptions {
  readonly fetch?: typeof fetch | undefined;
}

export class HttpAdapter implements Adapter {
  private readonly routes: ReadonlyMap<string, HttpAdapterRoute>;
  private readonly fetchImpl: typeof fetch;

  public constructor(
    public readonly metadata: AdapterMetadata,
    routes: readonly HttpAdapterRoute[],
    options: HttpAdapterOptions = {}
  ) {
    this.routes = new Map(routes.map((route) => [routeKey(route.operation, route.version), route]));
    this.fetchImpl = options.fetch ?? fetch;
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const route = this.routes.get(routeKey(invocation.operation, invocation.operationVersion));
    if (!route) {
      throw new Error(
        `Adapter '${this.metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
      );
    }

    const url = buildUrl(route, invocation.payload);
    assertAllowedHost(this.metadata, url);
    const headers = headersFromRecord({
      ...(route.headers ?? {}),
      ...recordValue(invocation.payload.headers)
    });
    applyHttpAuth(headers, url, route.auth, invocation);
    const body = bodyFor(route.method, invocation.payload, route);
    const response = await this.fetchImpl(url, {
      method: route.method.toUpperCase(),
      headers,
      ...(body === undefined ? {} : { body })
    });
    const output = await readHttpOutput(response);
    const status = response.ok ? "succeeded" : "failed";

    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status,
      output,
      providerMetadata: {
        adapterId: invocation.adapterId,
        provider: this.metadata.kind,
        providerResponseId:
          response.headers.get("x-request-id") ??
          response.headers.get("request-id") ??
          `${this.metadata.kind}.${response.status}.${randomUUID()}`,
        mock: this.metadata.live === false,
        sequence: invocation.context.attempt,
        operation: invocation.operation
      },
      ...(status === "failed"
        ? {
            error: {
              code: "HTTP_REQUEST_FAILED",
              message: `HTTP ${response.status} ${response.statusText}`.trim(),
              retryable:
                response.status === 408 || response.status === 429 || response.status >= 500
            }
          }
        : {}),
      auditEvents: [
        {
          id: `audit.${this.metadata.id}.${invocation.context.runId}.${invocation.context.nodeId}.${invocation.context.attempt}.${randomUUID()}`,
          timestamp: new Date().toISOString(),
          level: status === "succeeded" ? "info" : "error",
          message: `HTTP adapter '${this.metadata.id}' called '${invocation.operation}'.`
        }
      ]
    };
  }
}

export function createHttpAdapterMetadata(input: {
  readonly id: string;
  readonly kind?: AdapterMetadata["kind"] | undefined;
  readonly displayName: string;
  readonly version?: string | undefined;
  readonly allowedHosts: readonly string[];
  readonly operations: AdapterMetadata["operations"];
  readonly requiredSecrets?: AdapterMetadata["requiredSecrets"] | undefined;
  readonly live?: boolean | undefined;
}): AdapterMetadata {
  return {
    id: input.id,
    kind: input.kind ?? "http",
    displayName: input.displayName,
    version: input.version ?? "1.0.0",
    capabilities: input.operations.map((operation) => operation.name),
    operations: input.operations,
    requiredSecrets: input.requiredSecrets ?? [],
    networkPolicy: {
      mode: input.allowedHosts.length > 0 ? "declared" : "none",
      allowedHosts: [...input.allowedHosts].sort()
    },
    rateLimit: {
      maxRequests: 60,
      perSeconds: 60
    },
    retry: {
      maxAttempts: 3,
      backoffSeconds: 2,
      retryableErrorCodes: ["HTTP_REQUEST_FAILED"]
    },
    fixtures: [],
    live: input.live ?? true
  };
}

function assertInvocation(metadata: AdapterMetadata, invocation: AdapterInvocation): void {
  if (invocation.adapterId !== metadata.id) {
    throw new Error(
      `Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`
    );
  }
}

function routeKey(operation: string, version: string): string {
  return `${operation}\u0000${version}`;
}

function headersFromRecord(record: JsonRecord): Headers {
  const headers = new Headers();
  for (const [name, value] of Object.entries(record)) {
    if (value === null) {
      continue;
    }
    headers.set(name, typeof value === "string" ? value : JSON.stringify(value));
  }

  return headers;
}

function buildUrl(route: HttpAdapterRoute, payload: JsonRecord): URL {
  if (route.urlPayloadKey) {
    const value = payload[route.urlPayloadKey];
    if (typeof value !== "string" || value.trim().length === 0) {
      throw new Error(`Payload field '${route.urlPayloadKey}' must contain a URL.`);
    }
    return new URL(value);
  }

  const template = route.url;
  const directPathValues = Object.fromEntries(
    (route.pathKeys ?? [])
      .map((key) => [key, payload[key]] as const)
      .filter((entry): entry is readonly [string, JsonValue] => entry[1] !== undefined)
  );
  const pathValues = {
    ...directPathValues,
    ...recordValue(payload.path)
  };
  let urlText = template;
  for (const [name, value] of Object.entries(pathValues)) {
    urlText = urlText.replaceAll(`{${name}}`, encodeURIComponent(String(value)));
  }

  const url = new URL(urlText);
  const query = recordValue(payload.query);
  for (const [name, value] of Object.entries(query)) {
    if (value === undefined || value === null) {
      continue;
    }
    if (Array.isArray(value)) {
      for (const item of value) {
        url.searchParams.append(name, String(item));
      }
    } else {
      url.searchParams.set(name, String(value));
    }
  }

  return url;
}

function assertAllowedHost(metadata: AdapterMetadata, url: URL): void {
  if (metadata.networkPolicy.mode !== "declared") {
    return;
  }
  const allowed = new Set(metadata.networkPolicy.allowedHosts.map((host) => host.toLowerCase()));
  const hostname = url.hostname.toLowerCase();
  const matched = [...allowed].some(
    (host) =>
      host === "*" ||
      host === hostname ||
      (host.startsWith("*.") && hostname.endsWith(host.slice(1)))
  );
  if (!matched) {
    throw new Error(`Host '${url.hostname}' is not declared for adapter '${metadata.id}'.`);
  }
}

function applyHttpAuth(
  headers: Headers,
  url: URL,
  auth: HttpAdapterAuth | undefined,
  invocation: AdapterInvocation
): void {
  if (!auth) {
    return;
  }
  const secret = invocation.secrets?.[auth.secretName];
  if (!secret) {
    throw new Error(`Secret '${auth.secretName}' is required for '${invocation.adapterId}'.`);
  }
  switch (auth.scheme) {
    case "bearer":
      headers.set("authorization", `Bearer ${secret}`);
      return;
    case "basic":
      headers.set("authorization", `Basic ${Buffer.from(secret, "utf8").toString("base64")}`);
      return;
    case "apiKey": {
      const name = auth.parameterName ?? "x-api-key";
      if (auth.location === "query") {
        url.searchParams.set(name, secret);
      } else if (auth.location === "cookie") {
        headers.set("cookie", `${name}=${encodeURIComponent(secret)}`);
      } else {
        headers.set(name, secret);
      }
    }
  }
}

function bodyFor(
  method: string,
  payload: JsonRecord,
  route: HttpAdapterRoute
): BodyInit | undefined {
  if (["GET", "HEAD"].includes(method.toUpperCase())) {
    return undefined;
  }
  const body = route.bodyPayloadKey ? payload[route.bodyPayloadKey] : implicitBody(payload, route);
  if (body === undefined) return undefined;
  if (typeof body === "string") {
    return body;
  }

  return JSON.stringify(body);
}

function implicitBody(payload: JsonRecord, route: HttpAdapterRoute): JsonRecord | undefined {
  const reserved = new Set([
    "allowedHosts",
    "headers",
    "path",
    "query",
    "url",
    ...(route.bodyPayloadKey ? [route.bodyPayloadKey] : []),
    ...(route.pathKeys ?? [])
  ]);
  const entries = Object.entries(payload).filter(([key]) => !reserved.has(key));
  return entries.length > 0 ? Object.fromEntries(entries) : undefined;
}

async function readHttpOutput(response: Response): Promise<JsonRecord> {
  const headers = Object.fromEntries(response.headers.entries());
  const contentType = response.headers.get("content-type") ?? "";
  const body = contentType.includes("application/json")
    ? ((await response.json()) as JsonValue)
    : await response.text();

  return {
    response: {
      status: response.status,
      statusText: response.statusText,
      headers,
      body
    }
  };
}

function recordValue(value: JsonValue | undefined): Record<string, JsonValue> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, JsonValue>)
    : {};
}
