import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { assertAdapterCredentialRefs } from "./credentials.js";
export class DatabaseAdapter {
    metadata;
    client;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.client = options.client ?? new SqliteDatabaseClient({ sqliteBin: options.sqliteBin });
    }
    async invoke(invocation) {
        assertInvocation(this.metadata, invocation);
        const statement = normalizedStatement(requiredString(invocation.payload.statement, "statement"));
        const isQuery = invocation.operation === "database.query";
        const readonly = isQuery || booleanValue(invocation.payload.readonly, false);
        if (readonly) {
            assertReadOnlyStatement(statement);
        }
        const connection = parseDatabaseConnection(requiredSecret(invocation, "database.connection"));
        if (!readonly && connection.allowWrites !== true) {
            throw new Error("Database write operations require the database.connection secret to set allowWrites=true.");
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
                fields: [...(result.fields ?? fieldsForRows(rows))],
                truncated: result.rows.length > rows.length,
                ...(result.metadata ? { metadata: result.metadata } : {})
            },
            providerMetadata: providerMetadata(invocation, this.metadata.kind, providerResponseId),
            auditEvents: [
                auditEvent(providerResponseId, `Database adapter '${invocation.adapterId}' completed '${invocation.operation}'.`)
            ]
        };
    }
}
export class SqliteDatabaseClient {
    sqliteBin;
    constructor(options = {}) {
        this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
    }
    async query(input) {
        if (input.connection.engine !== "sqlite") {
            throw new Error(`No built-in database client is available for engine '${input.connection.engine}'. Pass a DatabaseClient in createDefaultLiveAdapters({ database }).`);
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
function assertInvocation(metadata, invocation) {
    if (invocation.adapterId !== metadata.id) {
        throw new Error(`Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`);
    }
    assertAdapterCredentialRefs(metadata, invocation.secretRefs, { requireLiveCredentials: true });
    const operation = metadata.operations.find((candidate) => candidate.name === invocation.operation && candidate.version === invocation.operationVersion);
    if (!operation) {
        throw new Error(`Adapter '${metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`);
    }
}
function parseDatabaseConnection(secret) {
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
function engineFromConnection(connectionString, databasePath) {
    if (databasePath) {
        return "sqlite";
    }
    if (connectionString?.startsWith("postgres://") ||
        connectionString?.startsWith("postgresql://")) {
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
function sqliteScript(input) {
    const commands = [
        ".parameter init",
        ...input.parameters.map((parameter, index) => `.parameter set ?${index + 1} ${sqliteLiteral(parameter)}`)
    ];
    const queryOnly = input.readonly ? ["PRAGMA query_only = ON;"] : [];
    const statement = `${input.statement};`;
    const changes = input.operation === "database.execute" ? ["SELECT changes() AS rowCount;"] : [];
    return [...commands, ...queryOnly, statement, ...changes].join("\n");
}
function parseSqliteJsonResultSets(output) {
    return output
        .split(/\r?\n/u)
        .map((line) => line.trim())
        .filter(Boolean)
        .map((line) => {
        const parsed = JSON.parse(line);
        if (!Array.isArray(parsed)) {
            return [];
        }
        return parsed.filter(isRecord);
    });
}
function rowCountFromChanges(rows) {
    const value = rows[0]?.rowCount;
    return typeof value === "number" ? value : 0;
}
function normalizedStatement(statement) {
    const normalized = statement.trim().replace(/;+$/u, "").trim();
    if (normalized.length === 0) {
        throw new Error("Database statement must not be empty.");
    }
    if (normalized.includes(";")) {
        throw new Error("Database adapter accepts one SQL statement per invocation.");
    }
    return normalized;
}
function assertReadOnlyStatement(statement) {
    const lowered = statement
        .replace(/^\s*--.*$/gmu, "")
        .trim()
        .toLowerCase();
    if (!/^(select|with|pragma)\b/u.test(lowered)) {
        throw new Error("database.query only accepts read-only SQL statements.");
    }
}
function sqliteLiteral(value) {
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
function fieldsForRows(rows) {
    const first = rows[0];
    return first ? Object.keys(first) : [];
}
function providerMetadata(invocation, provider, providerResponseId) {
    return {
        adapterId: invocation.adapterId,
        provider,
        providerResponseId,
        mock: false,
        sequence: invocation.context.attempt,
        operation: invocation.operation
    };
}
function auditEvent(providerResponseId, message) {
    return {
        id: `audit.${providerResponseId}`,
        timestamp: new Date().toISOString(),
        level: "info",
        message
    };
}
function requiredSecret(invocation, secretName) {
    const value = invocation.secrets?.[secretName];
    if (!value) {
        throw new Error(`Resolved secret '${secretName}' is required for adapter '${invocation.adapterId}'.`);
    }
    return value;
}
function requiredString(value, name) {
    if (typeof value !== "string" || value.trim().length === 0) {
        throw new Error(`Database field '${name}' is required.`);
    }
    return value;
}
function optionalString(value) {
    return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}
function optionalNumber(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
function stringValue(value, fallback) {
    return optionalString(value) ?? fallback;
}
function booleanValue(value, fallback) {
    return typeof value === "boolean" ? value : fallback;
}
function boundedInteger(value, fallback, min, max) {
    if (typeof value !== "number" || !Number.isFinite(value)) {
        return fallback;
    }
    return Math.min(Math.max(Math.trunc(value), min), max);
}
function arrayValue(value) {
    return Array.isArray(value) ? value : [];
}
function parseSecretJson(value) {
    try {
        const parsed = JSON.parse(value);
        return isRecord(parsed) ? parsed : null;
    }
    catch {
        return null;
    }
}
function isRecord(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
//# sourceMappingURL=database-adapter.js.map