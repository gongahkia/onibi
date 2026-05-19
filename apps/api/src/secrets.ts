import { createCipheriv, createDecipheriv, createHash, randomBytes, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
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

export class InMemorySecretStore implements SecretStore {
  private readonly secrets = new Map<string, { value: string; metadata: SecretMetadata }>();

  public async getSecretValue(name: string): Promise<string | null> {
    return this.secrets.get(name)?.value ?? null;
  }

  public listSecrets(): readonly SecretMetadata[] {
    return [...this.secrets.values()].map((entry) => entry.metadata).sort(byName);
  }

  public putSecret(name: string, value: string): SecretMetadata {
    const now = new Date().toISOString();
    const existing = this.secrets.get(name)?.metadata;
    const metadata = {
      name,
      createdAt: existing?.createdAt ?? now,
      updatedAt: now
    };
    this.secrets.set(name, { value, metadata });
    return metadata;
  }

  public deleteSecret(name: string): boolean {
    return this.secrets.delete(name);
  }
}

export interface SqliteSecretStoreOptions {
  readonly databasePath: string;
  readonly masterKey: string;
  readonly sqliteBin?: string | undefined;
}

export class SqliteSecretStore implements SecretStore {
  private readonly databasePath: string;
  private readonly sqliteBin: string;
  private readonly key: Buffer;

  public constructor(options: SqliteSecretStoreOptions) {
    if (!options.masterKey) {
      throw new Error("KELPCLAW_SECRET_MASTER_KEY is required for encrypted local secrets.");
    }
    this.databasePath = options.databasePath;
    this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
    this.key = createHash("sha256").update(options.masterKey, "utf8").digest();
    mkdirSync(dirname(this.databasePath), { recursive: true });
    this.runSql(sqliteSecretMigrations.join("\n"));
  }

  public async getSecretValue(name: string): Promise<string | null> {
    const [row] = this.querySql<SecretRow>(
      `SELECT name, nonce, ciphertext, tag, created_at AS createdAt, updated_at AS updatedAt FROM secrets WHERE name = ${sqlString(name)};`
    );
    if (!row) {
      return null;
    }

    return decryptSecret(row, this.key);
  }

  public listSecrets(): readonly SecretMetadata[] {
    return this.querySql<SecretMetadata>(
      "SELECT name, created_at AS createdAt, updated_at AS updatedAt FROM secrets ORDER BY name;"
    );
  }

  public putSecret(name: string, value: string): SecretMetadata {
    const now = new Date().toISOString();
    const encrypted = encryptSecret(value, this.key);
    this.runSql(`
      INSERT INTO secrets (name, nonce, ciphertext, tag, created_at, updated_at)
      VALUES (${sqlString(name)}, ${sqlString(encrypted.nonce)}, ${sqlString(encrypted.ciphertext)}, ${sqlString(encrypted.tag)}, ${sqlString(now)}, ${sqlString(now)})
      ON CONFLICT(name) DO UPDATE SET
        nonce = excluded.nonce,
        ciphertext = excluded.ciphertext,
        tag = excluded.tag,
        updated_at = excluded.updated_at;
    `);
    const [metadata] = this.querySql<SecretMetadata>(
      `SELECT name, created_at AS createdAt, updated_at AS updatedAt FROM secrets WHERE name = ${sqlString(name)};`
    );
    if (!metadata) {
      throw new Error(`Secret '${name}' was not persisted.`);
    }

    return metadata;
  }

  public deleteSecret(name: string): boolean {
    const before = this.listSecrets().length;
    this.runSql(`DELETE FROM secrets WHERE name = ${sqlString(name)};`);
    return this.listSecrets().length < before;
  }

  private runSql(sql: string): void {
    execFileSync(this.sqliteBin, [this.databasePath], {
      input: sql,
      encoding: "utf8"
    });
  }

  private querySql<T>(sql: string): T[] {
    const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
      encoding: "utf8"
    });
    if (output.trim().length === 0) {
      return [];
    }

    return JSON.parse(output) as T[];
  }
}

export function secretReadiness(secretStore: SecretStore): readonly {
  readonly id: string;
  readonly ready: boolean;
  readonly requiredSecrets: readonly string[];
}[] {
  const names = new Set(secretStore.listSecrets().map((secret) => secret.name));
  return [
    readiness("google", names, ["google.oauth.default"]),
    readiness("smtp", names, ["email.smtp.default"]),
    readiness("whatsapp", names, ["whatsapp.cloud.default"]),
    readiness("telegram", names, ["telegram.bot.default"])
  ];
}

export function createOAuthState(secretStore: SecretStore): string {
  const state = `oauth.${randomUUID()}`;
  secretStore.putSecret(
    `oauth.state.${state}`,
    JSON.stringify({ createdAt: new Date().toISOString() })
  );
  return state;
}

export async function consumeOAuthState(secretStore: SecretStore, state: string): Promise<boolean> {
  const value = await secretStore.getSecretValue(`oauth.state.${state}`);
  if (!value) {
    return false;
  }
  secretStore.deleteSecret(`oauth.state.${state}`);
  return true;
}

function encryptSecret(value: string, key: Buffer) {
  const nonce = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  const ciphertext = Buffer.concat([cipher.update(value, "utf8"), cipher.final()]);
  return {
    nonce: nonce.toString("base64"),
    ciphertext: ciphertext.toString("base64"),
    tag: cipher.getAuthTag().toString("base64")
  };
}

function decryptSecret(row: SecretRow, key: Buffer): string {
  const decipher = createDecipheriv("aes-256-gcm", key, Buffer.from(row.nonce, "base64"));
  decipher.setAuthTag(Buffer.from(row.tag, "base64"));
  return Buffer.concat([
    decipher.update(Buffer.from(row.ciphertext, "base64")),
    decipher.final()
  ]).toString("utf8");
}

function readiness(id: string, names: ReadonlySet<string>, requiredSecrets: readonly string[]) {
  return {
    id,
    ready: requiredSecrets.every((secret) => names.has(secret)),
    requiredSecrets
  };
}

function byName(left: { readonly name: string }, right: { readonly name: string }) {
  return left.name.localeCompare(right.name);
}

function sqlString(value: string): string {
  return `'${value.replace(/'/gu, "''")}'`;
}

interface SecretRow {
  readonly name: string;
  readonly nonce: string;
  readonly ciphertext: string;
  readonly tag: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

const sqliteSecretMigrations = [
  `CREATE TABLE IF NOT EXISTS secrets (
    name TEXT PRIMARY KEY,
    nonce TEXT NOT NULL,
    ciphertext TEXT NOT NULL,
    tag TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
  );`
] as const;
