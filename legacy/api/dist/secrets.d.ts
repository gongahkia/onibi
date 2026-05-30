import type { SecretValueStore } from "@kelpclaw/nanoclaw";
export interface SecretMetadata {
    readonly name: string;
    readonly createdAt: string;
    readonly updatedAt: string;
}
export interface SecretStore extends SecretValueStore {
    listSecrets(): readonly SecretMetadata[];
    putSecret(name: string, value: string): SecretMetadata;
    deleteSecret(name: string): boolean;
}
export declare class InMemorySecretStore implements SecretStore {
    private readonly secrets;
    getSecretValue(name: string): Promise<string | null>;
    listSecrets(): readonly SecretMetadata[];
    putSecret(name: string, value: string): SecretMetadata;
    deleteSecret(name: string): boolean;
}
export interface SqliteSecretStoreOptions {
    readonly databasePath: string;
    readonly masterKey: string;
    readonly sqliteBin?: string | undefined;
}
export declare class SqliteSecretStore implements SecretStore {
    private readonly databasePath;
    private readonly sqliteBin;
    private readonly key;
    constructor(options: SqliteSecretStoreOptions);
    getSecretValue(name: string): Promise<string | null>;
    listSecrets(): readonly SecretMetadata[];
    putSecret(name: string, value: string): SecretMetadata;
    deleteSecret(name: string): boolean;
    private runSql;
    private querySql;
}
export declare function secretReadiness(secretStore: SecretStore): readonly {
    readonly id: string;
    readonly ready: boolean;
    readonly requiredSecrets: readonly string[];
}[];
export declare function createOAuthState(secretStore: SecretStore): string;
export declare function consumeOAuthState(secretStore: SecretStore, state: string): Promise<boolean>;
//# sourceMappingURL=secrets.d.ts.map