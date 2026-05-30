import { createCipheriv, createDecipheriv, createHash, randomBytes, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
export class InMemorySecretStore {
    secrets = new Map();
    async getSecretValue(name) {
        return this.secrets.get(name)?.value ?? null;
    }
    listSecrets() {
        return [...this.secrets.values()].map((entry) => entry.metadata).sort(byName);
    }
    putSecret(name, value) {
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
    deleteSecret(name) {
        return this.secrets.delete(name);
    }
}
export class SqliteSecretStore {
    databasePath;
    sqliteBin;
    key;
    constructor(options) {
        if (!options.masterKey) {
            throw new Error("KELPCLAW_SECRET_MASTER_KEY is required for encrypted local secrets.");
        }
        this.databasePath = options.databasePath;
        this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
        this.key = createHash("sha256").update(options.masterKey, "utf8").digest();
        mkdirSync(dirname(this.databasePath), { recursive: true });
        this.runSql(sqliteSecretMigrations.join("\n"));
    }
    async getSecretValue(name) {
        const [row] = this.querySql(`SELECT name, nonce, ciphertext, tag, created_at AS createdAt, updated_at AS updatedAt FROM secrets WHERE name = ${sqlString(name)};`);
        if (!row) {
            return null;
        }
        return decryptSecret(row, this.key);
    }
    listSecrets() {
        return this.querySql("SELECT name, created_at AS createdAt, updated_at AS updatedAt FROM secrets ORDER BY name;");
    }
    putSecret(name, value) {
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
        const [metadata] = this.querySql(`SELECT name, created_at AS createdAt, updated_at AS updatedAt FROM secrets WHERE name = ${sqlString(name)};`);
        if (!metadata) {
            throw new Error(`Secret '${name}' was not persisted.`);
        }
        return metadata;
    }
    deleteSecret(name) {
        const before = this.listSecrets().length;
        this.runSql(`DELETE FROM secrets WHERE name = ${sqlString(name)};`);
        return this.listSecrets().length < before;
    }
    runSql(sql) {
        execFileSync(this.sqliteBin, [this.databasePath], {
            input: sql,
            encoding: "utf8"
        });
    }
    querySql(sql) {
        const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
            encoding: "utf8"
        });
        if (output.trim().length === 0) {
            return [];
        }
        return JSON.parse(output);
    }
}
export function secretReadiness(secretStore) {
    const names = new Set(secretStore.listSecrets().map((secret) => secret.name));
    return [
        readiness("google", names, ["google.oauth.default"]),
        readiness("smtp", names, ["email.smtp.default"]),
        readiness("whatsapp", names, ["whatsapp.cloud.default"]),
        readiness("telegram", names, ["telegram.bot.default"]),
        readiness("github", names, ["github.token.default"]),
        readiness("slack", names, ["slack.bot.default"]),
        readiness("discord", names, ["discord.bot.default"]),
        readiness("notion", names, ["notion.api.default"]),
        readiness("linear", names, ["linear.api.default"]),
        readiness("jira", names, ["jira.basic.default"]),
        readiness("airtable", names, ["airtable.api.default"]),
        readiness("webhook", names, ["webhook.token.default"]),
        readiness("database", names, ["database.connection.default"])
    ];
}
export function createOAuthState(secretStore) {
    const state = `oauth.${randomUUID()}`;
    secretStore.putSecret(`oauth.state.${state}`, JSON.stringify({ createdAt: new Date().toISOString() }));
    return state;
}
export async function consumeOAuthState(secretStore, state) {
    const value = await secretStore.getSecretValue(`oauth.state.${state}`);
    if (!value) {
        return false;
    }
    secretStore.deleteSecret(`oauth.state.${state}`);
    return true;
}
function encryptSecret(value, key) {
    const nonce = randomBytes(12);
    const cipher = createCipheriv("aes-256-gcm", key, nonce);
    const ciphertext = Buffer.concat([cipher.update(value, "utf8"), cipher.final()]);
    return {
        nonce: nonce.toString("base64"),
        ciphertext: ciphertext.toString("base64"),
        tag: cipher.getAuthTag().toString("base64")
    };
}
function decryptSecret(row, key) {
    const decipher = createDecipheriv("aes-256-gcm", key, Buffer.from(row.nonce, "base64"));
    decipher.setAuthTag(Buffer.from(row.tag, "base64"));
    return Buffer.concat([
        decipher.update(Buffer.from(row.ciphertext, "base64")),
        decipher.final()
    ]).toString("utf8");
}
function readiness(id, names, requiredSecrets) {
    return {
        id,
        ready: requiredSecrets.every((secret) => names.has(secret)),
        requiredSecrets
    };
}
function byName(left, right) {
    return left.name.localeCompare(right.name);
}
function sqlString(value) {
    return `'${value.replace(/'/gu, "''")}'`;
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
];
//# sourceMappingURL=secrets.js.map