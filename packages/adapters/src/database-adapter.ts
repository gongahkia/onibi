import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { assertAdapterCredentialRefs } from "./credentials.js";
import type {
  Adapter,
  AdapterAuditEvent,
  AdapterInvocation,
  AdapterKind,
  AdapterMetadata,
  AdapterProviderMetadata,
  AdapterResult
} from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export interface DatabaseConnectionConfig {
  readonly engine: string;
  readonly connectionString?: string | undefined;
  readonly databasePath?: string | undefined;
  readonly host?: string | undefined;
  readonly port?: number | undefined;
  readonly database?: string | undefined;
  readonly username?: string | undefined;
  readonly password?: string | undefined;
  readonly ssl?: boolean | JsonRecord | undefined;
  readonly allowWrites?: boolean | undefined;
  readonly options?: JsonRecord | undefined;
}

export interface DatabaseQueryInput {
  readonly operation: string;
  readonly statement: string;
  readonly parameters: readonly JsonValue[];
  readonly readonly: boolean;
  readonly maxRows: number;
  readonly timeoutMs: number;
  readonly connection: DatabaseConnectionConfig;
  readonly context: AdapterInvocation["context"];
  readonly idempotencyKey?: string | undefined;
}

export interface DatabaseQueryResult {
  readonly rows: readonly JsonRecord[];
  readonly rowCount: number;
  readonly fields?: readonly string[] | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface DatabaseClient {
  query(input: DatabaseQueryInput): Promise<DatabaseQueryResult>;
}

export interface DatabaseAdapterOptions {
  readonly client?: DatabaseClient | undefined;
  readonly sqliteBin?: string | undefined;
}

export class DatabaseAdapter implements Adapter {
  private readonly client: DatabaseClient;

  public constructor(
    public readonly metadata: AdapterMetadata,
    options: DatabaseAdapterOptions = {}
  ) {
    this.client = options.client ?? new SqliteDatabaseClient({ sqliteBin: options.sqliteBin });
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const statement = normalizedStatement(requiredString(invocation.payload.statement, "statement"));
    const isQuery = invocation.operation === "database.query";
    const readonly = isQuery || booleanValue(invocation.payload.readonly, false);
    if (readonly) {
      assertReadOnlyStatement(statement);
    }

    const connection = parseDatabaseConnection(requiredSecret(invocation, "database.connection"));
    if (!readonly && connection.allowWrites !== true) {
      throw new Error(
        "Database write operations require the database.connection secret to set allowWrites=true."
      );
    }

    const result = await this.client.query({
      operation: invocation.operation,
      statement,
      parameters: arrayValue(invocation.payload.parameters),
      readonly,
      maxRows: boundedInteger(invocation.payload.maxRows, 100, 1, 1000),
      timeoutMs: boundedInteger(invocation.payload.timeoutMs, 10_000, 100, 120_000),
      connection,
      context: invocation.context,
      idempotencyKey: invocation.idempotencyKey
    });
    const rows = result.rows.slice(0, boundedInteger(invocation.payload.maxRows, 100, 1, 1000));
    const providerResponseId = `database.${invocation.context.runId}.${invocation.context.nodeId}.${randomUUID()}`;

    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status: "succeeded",
      output: {
        channel: "database",
        engine: connection.engine,
        rows,
        rowCount: result.rowCount,
        fields: result.fields ?? fieldsForRows(rows),
        truncated: result.rows.length > rows.length,
        ...(result.metadata ? { metadata: result.metadata } : {})
      },
      providerMetadata: providerMetadata(invocation, this.metadata.kind, providerResponseId),
      auditEvents: [
        auditEvent(
          providerResponseId,
          `Database adapter '${invocation.adapterId}' completed '${invocation.operation}'.`
        )
      ]
    };
  }
}

export interface SqliteDatabaseClientOptions {
  readonly sqliteBin?: string | undefined;
}

export class SqliteDatabaseClient implements DatabaseClient {
  private readonly sqliteBin: string;

  public constructor(options: SqliteDatabaseClientOptions = {}) {
    this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
  }

  public async query(input: DatabaseQueryInput): Promise<DatabaseQueryResult> {
    if (input.connection.engine !== "sqlite") {
      throw new Error(
        `No built-in database client is available for engine '${input.connection.engine}'. Pass a DatabaseClient in createDefaultLiveAdapters({ database }).`
      );
    }
    const databasePath = requiredString(input.connection.databasePath, "databasePath");
    const sql = sqliteScript(input);
    const output = execFileSync(this.sqliteBin, ["-json", databasePath], {
      input: sql,
      encoding: "utf8",
      timeout: input.timeoutMs,
      maxBuffer: 10 * 1024 * 1024
    });
    const resultSets = parseSqliteJsonResultSets(output);

    if (input.operation === "database.execute") {
      const changes = rowCountFromChanges(resultSets.at(-1) ?? []);
      const returnedRows = resultSets.length > 1 ? (resultSets[0] ?? []) : [];
      return {
        rows: returnedRows.slice(0, input.maxRows),
        rowCount: changes,
        fields: fieldsForRows(returnedRows)
      };
    }

    const rows = resultSets[0] ?? [];
    return {
      rows: rows.slice(0, input.maxRows),
      rowCount: rows.length,
      fields: fieldsForRows(rows)
    };
  }
}

function assertInvocation(metadata: AdapterMetadata, invocation: AdapterInvocation): void {
  if (invocation.adapterId !== metadata.id) {
    throw new Error(
      `Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`
    );
  }
  assertAdapterCredentialRefs(metadata, invocation.secretRefs, { requireLiveCredentials: true });
  const operation = metadata.operations.find(
    (candidate) =>
      candidate.name === invocation.operation && candidate.version === invocation.operationVersion
  );
  if (!operation) {
    throw new Error(
      `Adapter '${metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
    );
  }
}

function parseDatabaseConnection(secret: string): DatabaseConnectionConfig {
  const parsed = parseSecretJson(secret);
  if (parsed) {
    const engine = stringValue(parsed.engine, stringValue(parsed.driver, ""));
    const connectionString = optionalString(parsed.connectionString);
    const databasePath = optionalString(parsed.databasePath) ?? optionalString(parsed.filename);
    return {
      engine: engine || engineFromConnection(connectionString, databasePath),
      ...(connectionString ? { connectionString } : {}),
      ...(databasePath ? { databasePath } : {}),
      ...(optionalString(parsed.host) ? { host: optionalString(parsed.host) } : {}),
      ...(optionalNumber(parsed.port) ? { port: optionalNumber(parsed.port) } : {}),
      ...(optionalString(parsed.database) ? { database: optionalString(parsed.database) } : {}),
      ...(optionalString(parsed.username) ? { username: optionalString(parsed.username) } : {}),
      ...(optionalString(parsed.password) ? { password: optionalString(parsed.password) } : {}),
      ...(typeof parsed.ssl === "boolean" || isRecord(parsed.ssl) ? { ssl: parsed.ssl } : {}),
      ...(typeof parsed.allowWrites === "boolean" ? { allowWrites: parsed.allowWrites } : {}),
      ...(isRecord(parsed.options) ? { options: parsed.options } : {})
    };
  }

  if (secret.startsWith("sqlite://")) {
    const url = new URL(secret);
    return {
      engine: "sqlite",
      databasePath: decodeURIComponent(url.pathname)
    };
  }

  return {
    engine: engineFromConnection(secret, undefined),
    connectionString: secret
  };
}

function engineFromConnection(
  connectionString: string | undefined,
  databasePath: string | undefined
): string {
  if (databasePath) {
    return "sqlite";
  }
  if (connectionString?.startsWith("postgres://") || connectionString?.startsWith("postgresql://")) {
    return "postgres";
  }
  if (connectionString?.startsWith("mysql://")) {
    return "mysql";
  }
  if (connectionString?.startsWith("sqlserver://")) {
    return "mssql";
  }

  return "custom";
}

function sqliteScript(input: DatabaseQueryInput): string {
  const commands = [
    ".parameter init",
    ...input.parameters.map((parameter, index) => `.parameter set ?${index + 1} ${sqliteLiteral(parameter)}`)
  ];
  const queryOnly = input.readonly ? ["PRAGMA query_only = ON;"] : [];
  const statement = `${input.statement};`;
  const changes = input.operation === "database.execute" ? ["SELECT changes() AS rowCount;"] : [];
  return [...commands, ...queryOnly, statement, ...changes].join("\n");
}

function parseSqliteJsonResultSets(output: string): JsonRecord[][] {
  return output
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const parsed: unknown = JSON.parse(line);
      if (!Array.isArray(parsed)) {
        return [];
      }
      return parsed.filter(isRecord);
    });
}

function rowCountFromChanges(rows: readonly JsonRecord[]): number {
  const value = rows[0]?.rowCount;
  return typeof value === "number" ? value : 0;
}

function normalizedStatement(statement: string): string {
  const normalized = statement.trim().replace(/;+$/u, "").trim();
  if (normalized.length === 0) {
    throw new Error("Database statement must not be empty.");
  }
  if (normalized.includes(";")) {
    throw new Error("Database adapter accepts one SQL statement per invocation.");
  }

  return normalized;
}

function assertReadOnlyStatement(statement: string): void {
  const lowered = statement.replace(/^\s*--.*$/gmu, "").trim().toLowerCase();
  if (
    !lowered.startsWith("select ") &&
    !lowered.startsWith("with ") &&
    !lowered.startsWith("pragma ")
  ) {
    throw new Error("database.query only accepts read-only SQL statements.");
  }
}

function sqliteLiteral(value: JsonValue): string {
  if (value === null) {
    return "null";
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new Error("Database parameters must be finite JSON values.");
    }
    return String(value);
  }
  if (typeof value === "boolean") {
    return value ? "1" : "0";
  }
  const text = typeof value === "string" ? value : JSON.stringify(value);
  return `'${text.replace(/'/gu, "''")}'`;
}

function fieldsForRows(rows: readonly JsonRecord[]): readonly string[] {
  const first = rows[0];
  return first ? Object.keys(first) : [];
}

function providerMetadata(
  invocation: AdapterInvocation,
  provider: AdapterKind,
  providerResponseId: string
): AdapterProviderMetadata {
  return {
    adapterId: invocation.adapterId,
    provider,
    providerResponseId,
    mock: false,
    sequence: invocation.context.attempt,
    operation: invocation.operation
  };
}

function auditEvent(providerResponseId: string, message: string): AdapterAuditEvent {
  return {
    id: `audit.${providerResponseId}`,
    timestamp: new Date().toISOString(),
    level: "info",
    message
  };
}

function requiredSecret(invocation: AdapterInvocation, secretName: string): string {
  const value = invocation.secrets?.[secretName];
  if (!value) {
    throw new Error(
      `Resolved secret '${secretName}' is required for adapter '${invocation.adapterId}'.`
    );
  }

  return value;
}

function requiredString(value: JsonValue | undefined, name: string): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new Error(`Database field '${name}' is required.`);
  }

  return value;
}

function optionalString(value: JsonValue | undefined): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}

function optionalNumber(value: JsonValue | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function stringValue(value: JsonValue | undefined, fallback: string): string {
  return optionalString(value) ?? fallback;
}

function booleanValue(value: JsonValue | undefined, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function boundedInteger(
  value: JsonValue | undefined,
  fallback: number,
  min: number,
  max: number
): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }

  return Math.min(Math.max(Math.trunc(value), min), max);
}

function arrayValue(value: JsonValue | undefined): readonly JsonValue[] {
  return Array.isArray(value) ? value : [];
}

function parseSecretJson(value: string): JsonRecord | null {
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
