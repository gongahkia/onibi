import SwaggerParser from "@apidevtools/swagger-parser";
import { createHttpAdapterMetadata, HttpAdapter } from "./http-adapter.js";
import type { HttpAdapterRoute, HttpAdapterOptions } from "./http-adapter.js";
import type { Adapter } from "./types.js";
import type {
  JsonRecord,
  JsonSchemaShape,
  JsonValue,
  WorkflowConnectorAuthRequirement,
  WorkflowConnectorOperation,
  WorkflowConnectorRecord
} from "@kelpclaw/workflow-spec";

const httpMethods = new Set(["get", "put", "post", "delete", "options", "head", "patch"]);
const objectSchema = {
  type: "object",
  additionalProperties: true
} as const satisfies JsonSchemaShape;

export interface ImportOpenApiConnectorInput {
  readonly id: string;
  readonly name?: string | undefined;
  readonly sourceUrl?: string | undefined;
  readonly document?: string | JsonRecord | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  readonly now?: string | undefined;
}

export async function importOpenApiConnector(
  input: ImportOpenApiConnectorInput
): Promise<WorkflowConnectorRecord> {
  const api = await dereferenceOpenApi(input);
  const title = stringValue(readPath(api, ["info", "title"]), input.name ?? input.id);
  const serverUrl = firstServerUrl(api, input.sourceUrl);
  const auth = collectAuthRequirements(api, input.secretRefs ?? {});
  const operations = collectOpenApiOperations(api, serverUrl);
  const now = input.now ?? new Date().toISOString();

  return {
    id: input.id,
    name: input.name ?? title,
    kind: "openapi",
    adapterId: `adapter.openapi.${slugify(input.id)}`,
    sourceUrl: input.sourceUrl,
    endpointUrl: serverUrl,
    allowedHosts: collectAllowedHosts(serverUrl, operations),
    auth,
    operations,
    secretRefs: input.secretRefs ?? {},
    createdAt: now,
    updatedAt: now,
    lastTest: {
      status: "untested"
    },
    metadata: {
      title,
      version: stringValue(readPath(api, ["info", "version"]), "unknown")
    }
  };
}

export function createOpenApiAdapter(
  connector: WorkflowConnectorRecord,
  options: HttpAdapterOptions = {}
): Adapter {
  const routes = connector.operations.map((operation) =>
    openApiRouteForOperation(operation, connector.auth[0])
  );
  const metadata = createHttpAdapterMetadata({
    id: connector.adapterId,
    kind: "openapi",
    displayName: connector.name,
    allowedHosts: connector.allowedHosts,
    operations: connector.operations.map((operation) => ({
      name: operation.name,
      version: operation.version,
      description: operation.description,
      inputSchema: operation.inputSchema,
      outputSchema: operation.outputSchema,
      metadata: operation.metadata
    })),
    requiredSecrets: connector.auth
      .filter((auth) => auth.scheme !== "none")
      .map((auth) => ({
        name: auth.name,
        description: auth.description ?? `${auth.scheme} credential for ${connector.name}.`,
        mockRef: `mock:${auth.name}`
      }))
  });

  return new HttpAdapter(metadata, routes, options);
}

export async function testOpenApiConnector(
  connector: WorkflowConnectorRecord
): Promise<WorkflowConnectorRecord> {
  const now = new Date().toISOString();
  return {
    ...connector,
    updatedAt: now,
    lastTest: {
      status: connector.operations.length > 0 ? "succeeded" : "failed",
      testedAt: now,
      operationCount: connector.operations.length,
      message:
        connector.operations.length > 0
          ? `Imported ${connector.operations.length} OpenAPI operation(s).`
          : "OpenAPI connector has no callable operations."
    }
  };
}

async function dereferenceOpenApi(input: ImportOpenApiConnectorInput): Promise<JsonRecord> {
  const parser = new SwaggerParser();
  const source =
    input.document === undefined
      ? input.sourceUrl
      : typeof input.document === "string"
        ? parseDocument(input.document)
        : input.document;
  if (!source) {
    throw new Error("OpenAPI import requires sourceUrl or document.");
  }
  return (await parser.dereference(source as never)) as unknown as JsonRecord;
}

function parseDocument(document: string): JsonRecord {
  const parsed = JSON.parse(document) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("OpenAPI document must be a JSON object.");
  }

  return parsed as JsonRecord;
}

function firstServerUrl(api: JsonRecord, sourceUrl: string | undefined): string {
  const servers = readPath(api, ["servers"]);
  if (Array.isArray(servers)) {
    for (const server of servers) {
      const url = readPath(server, ["url"]);
      if (typeof url === "string" && url.length > 0) {
        return absolutizeServerUrl(url, sourceUrl);
      }
    }
  }

  if (sourceUrl) {
    const url = new URL(sourceUrl);
    return `${url.protocol}//${url.host}`;
  }

  throw new Error("OpenAPI document must declare at least one server URL.");
}

function collectOpenApiOperations(
  api: JsonRecord,
  defaultServerUrl: string
): readonly WorkflowConnectorOperation[] {
  const paths = readPath(api, ["paths"]);
  if (!isRecord(paths)) {
    return [];
  }

  const operations: WorkflowConnectorOperation[] = [];
  for (const [path, pathItem] of Object.entries(paths)) {
    if (!isRecord(pathItem)) {
      continue;
    }
    for (const [method, operation] of Object.entries(pathItem)) {
      if (!httpMethods.has(method) || !isRecord(operation)) {
        continue;
      }
      const operationId = stringValue(operation.operationId, `${method.toUpperCase()} ${path}`);
      const serverUrl = operationServerUrl(operation, pathItem, defaultServerUrl);
      operations.push({
        name: operationId,
        version: "1.0.0",
        description: stringValue(
          operation.description,
          stringValue(operation.summary, `${method.toUpperCase()} ${path}`)
        ),
        inputSchema: openApiInputSchema(operation),
        outputSchema: {
          type: "object",
          required: ["response"],
          properties: {
            response: objectSchema
          }
        },
        method: method.toUpperCase(),
        path,
        metadata: {
          serverUrl,
          url: joinUrl(serverUrl, path),
          tags: Array.isArray(operation.tags) ? operation.tags.filter(isString) : []
        }
      });
    }
  }

  return operations.sort((left, right) => left.name.localeCompare(right.name));
}

function openApiInputSchema(operation: JsonRecord): JsonSchemaShape {
  const parameters = Array.isArray(operation.parameters)
    ? operation.parameters.filter(isRecord)
    : [];
  const pathProperties = parameterSchema(parameters, "path");
  const queryProperties = parameterSchema(parameters, "query");
  const headerProperties = parameterSchema(parameters, "header");
  const bodySchema = requestBodySchema(operation);

  return {
    type: "object",
    properties: {
      path: {
        type: "object",
        additionalProperties: true,
        properties: pathProperties
      },
      query: {
        type: "object",
        additionalProperties: true,
        properties: queryProperties
      },
      headers: {
        type: "object",
        additionalProperties: true,
        properties: headerProperties
      },
      body: bodySchema
    },
    additionalProperties: true
  };
}

function parameterSchema(
  parameters: readonly JsonRecord[],
  location: string
): Record<string, JsonValue> {
  return Object.fromEntries(
    parameters
      .filter((parameter) => parameter.in === location && typeof parameter.name === "string")
      .map((parameter) => [
        String(parameter.name),
        isRecord(parameter.schema) ? parameter.schema : objectSchema
      ])
  );
}

function requestBodySchema(operation: JsonRecord): JsonSchemaShape {
  const content = readPath(operation, ["requestBody", "content"]);
  if (!isRecord(content)) {
    return objectSchema;
  }
  const json = readPath(content, ["application/json", "schema"]);
  return isRecord(json) ? json : objectSchema;
}

function operationServerUrl(
  operation: JsonRecord,
  pathItem: JsonRecord,
  fallback: string
): string {
  for (const candidate of [operation, pathItem]) {
    const servers = candidate.servers;
    if (!Array.isArray(servers)) {
      continue;
    }
    const first = servers.find(isRecord);
    if (typeof first?.url === "string") {
      return absolutizeServerUrl(first.url, fallback);
    }
  }

  return fallback;
}

function collectAuthRequirements(
  api: JsonRecord,
  secretRefs: Readonly<Record<string, string>>
): readonly WorkflowConnectorAuthRequirement[] {
  const schemes = readPath(api, ["components", "securitySchemes"]);
  if (!isRecord(schemes)) {
    return [];
  }

  return Object.entries(schemes).map(([name, scheme]) => {
    const record = isRecord(scheme) ? scheme : {};
    const type = typeof record.type === "string" ? record.type : "apiKey";
    const bearer =
      type === "http" && typeof record.scheme === "string" && record.scheme.toLowerCase() === "bearer";
    const basic =
      type === "http" && typeof record.scheme === "string" && record.scheme.toLowerCase() === "basic";
    return {
      name,
      scheme: bearer ? "bearer" : basic ? "basic" : type === "oauth2" ? "oauth" : "apiKey",
      location:
        record.in === "query" || record.in === "cookie" || record.in === "header"
          ? record.in
          : "header",
      parameterName: typeof record.name === "string" ? record.name : undefined,
      secretRef: secretRefs[name],
      description: typeof record.description === "string" ? record.description : undefined
    };
  });
}

function openApiRouteForOperation(
  operation: WorkflowConnectorOperation,
  authRequirement: WorkflowConnectorAuthRequirement | undefined
): HttpAdapterRoute {
  const url = readPath(operation.metadata ?? {}, ["url"]);
  if (typeof url !== "string") {
    throw new Error(`OpenAPI operation '${operation.name}' is missing route metadata.`);
  }

  const location =
    authRequirement?.location === "header" ||
    authRequirement?.location === "query" ||
    authRequirement?.location === "cookie"
      ? authRequirement.location
      : undefined;
  const auth =
    authRequirement && authRequirement.scheme !== "none" && authRequirement.scheme !== "oauth"
      ? {
          secretName: authRequirement.name,
          scheme: authRequirement.scheme,
          ...(location ? { location } : {}),
          ...(authRequirement.parameterName
            ? { parameterName: authRequirement.parameterName }
            : {})
        }
      : undefined;

  return {
    operation: operation.name,
    version: operation.version,
    method: operation.method ?? "GET",
    url,
    ...(auth ? { auth } : {})
  };
}

function collectAllowedHosts(
  serverUrl: string,
  operations: readonly WorkflowConnectorOperation[]
): readonly string[] {
  const hosts = new Set<string>([new URL(serverUrl).hostname]);
  for (const operation of operations) {
    const url = readPath(operation.metadata ?? {}, ["url"]);
    if (typeof url === "string") {
      hosts.add(new URL(url).hostname);
    }
  }

  return [...hosts].sort();
}

function absolutizeServerUrl(value: string, sourceUrl: string | undefined): string {
  return new URL(value, sourceUrl ?? "http://localhost").toString().replace(/\/$/u, "");
}

function joinUrl(serverUrl: string, path: string): string {
  return `${serverUrl.replace(/\/$/u, "")}/${path.replace(/^\//u, "")}`;
}

function readPath(source: unknown, path: readonly string[]): JsonValue | undefined {
  let current: unknown = source;
  for (const segment of path) {
    if (!isRecord(current)) {
      return undefined;
    }
    current = current[segment];
  }

  return current as JsonValue | undefined;
}

function stringValue(value: JsonValue | undefined, fallback: string): string {
  return typeof value === "string" && value.length > 0 ? value : fallback;
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function slugify(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "-").replace(/^-+|-+$/gu, "");
}
