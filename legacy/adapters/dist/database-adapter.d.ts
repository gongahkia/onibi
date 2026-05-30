import type { Adapter, AdapterInvocation, AdapterMetadata, AdapterResult } from "./types.js";
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
export declare class DatabaseAdapter implements Adapter {
    readonly metadata: AdapterMetadata;
    private readonly client;
    constructor(metadata: AdapterMetadata, options?: DatabaseAdapterOptions);
    invoke(invocation: AdapterInvocation): Promise<AdapterResult>;
}
export interface SqliteDatabaseClientOptions {
    readonly sqliteBin?: string | undefined;
}
export declare class SqliteDatabaseClient implements DatabaseClient {
    private readonly sqliteBin;
    constructor(options?: SqliteDatabaseClientOptions);
    query(input: DatabaseQueryInput): Promise<DatabaseQueryResult>;
}
//# sourceMappingURL=database-adapter.d.ts.map