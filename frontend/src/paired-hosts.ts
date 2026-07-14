export const pairedHostRegistryVersion = 1;

const databaseName = "onibi-paired-hosts";
const databaseVersion = 1;
const keysStore = "keys";
const recordsStore = "records";
const registryKeyID = "registry-key";
const registryAADPrefix = "onibi-paired-hosts/v1/";

export type PairedHostState = "pending" | "active" | "stale" | "revoked";
export type PairedHostEndpointKind = "mesh" | "ssh" | "relay";

export interface PairedHostEndpoint {
  kind: PairedHostEndpointKind;
  url: string;
}

export interface PairedHost {
  version: number;
  id: string;
  displayName: string;
  endpoint: PairedHostEndpoint;
  identityPublic: string;
  state: PairedHostState;
}

export interface EncryptedPairedHost {
  id: string;
  iv: ArrayBuffer;
  ciphertext: ArrayBuffer;
}

export interface PairedHostRegistryBackend {
  getKey(): Promise<CryptoKey | undefined>;
  addKey(key: CryptoKey): Promise<void>;
  getRecord(id: string): Promise<EncryptedPairedHost | undefined>;
  listRecords(): Promise<EncryptedPairedHost[]>;
  putRecord(record: EncryptedPairedHost): Promise<void>;
  deleteRecord(id: string): Promise<void>;
}

export class PairedHostRegistry {
  constructor(
    private readonly backend: PairedHostRegistryBackend = new IndexedDBPairedHostRegistryBackend()
  ) {}

  async put(host: PairedHost): Promise<void> {
    const normalized = normalizePairedHost(host);
    const key = await this.encryptionKey();
    const iv = randomBytes(12);
    const plaintext = new TextEncoder().encode(JSON.stringify(normalized));
    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: copyBuffer(iv), additionalData: registryAAD(normalized.id) },
      key,
      plaintext
    );
    await this.backend.putRecord({ id: normalized.id, iv: copyBuffer(iv), ciphertext });
  }

  async get(id: string): Promise<PairedHost | undefined> {
    if (!validHostID(id)) {
      throw new Error("invalid paired-host id");
    }
    const record = await this.backend.getRecord(id);
    if (record === undefined) {
      return undefined;
    }
    return this.decrypt(record);
  }

  async list(): Promise<PairedHost[]> {
    const records = await this.backend.listRecords();
    const hosts = await Promise.all(records.map((record) => this.decrypt(record)));
    return hosts.sort(
      (a, b) => a.displayName.localeCompare(b.displayName) || a.id.localeCompare(b.id)
    );
  }

  async remove(id: string): Promise<void> {
    if (!validHostID(id)) {
      throw new Error("invalid paired-host id");
    }
    await this.backend.deleteRecord(id);
  }

  private async encryptionKey(): Promise<CryptoKey> {
    const stored = await this.backend.getKey();
    if (stored !== undefined) {
      validateRegistryKey(stored);
      return stored;
    }
    const generated = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, [
      "encrypt",
      "decrypt"
    ]);
    try {
      await this.backend.addKey(generated);
      return generated;
    } catch (error) {
      if (!isConstraintError(error)) {
        throw error;
      }
      const concurrent = await this.backend.getKey();
      if (concurrent === undefined) {
        throw new Error("paired-host registry key was not persisted");
      }
      validateRegistryKey(concurrent);
      return concurrent;
    }
  }

  private async decrypt(record: EncryptedPairedHost): Promise<PairedHost> {
    validateEncryptedRecord(record);
    const key = await this.encryptionKey();
    let plaintext: ArrayBuffer;
    try {
      plaintext = await crypto.subtle.decrypt(
        { name: "AES-GCM", iv: record.iv, additionalData: registryAAD(record.id) },
        key,
        record.ciphertext
      );
    } catch {
      throw new Error("paired-host record failed authentication");
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(new TextDecoder().decode(plaintext));
    } catch {
      throw new Error("paired-host record has invalid payload");
    }
    const host = normalizePairedHost(parsed);
    if (host.id !== record.id) {
      throw new Error("paired-host record id mismatch");
    }
    return host;
  }
}

class IndexedDBPairedHostRegistryBackend implements PairedHostRegistryBackend {
  async getKey(): Promise<CryptoKey | undefined> {
    return this.run(keysStore, "readonly", async (store) => {
      const key = await requestResult<CryptoKey | undefined>(store.get(registryKeyID));
      return key;
    });
  }

  async addKey(key: CryptoKey): Promise<void> {
    await this.run(keysStore, "readwrite", async (store) => {
      await requestResult(store.add(key, registryKeyID));
    });
  }

  async getRecord(id: string): Promise<EncryptedPairedHost | undefined> {
    return this.run(recordsStore, "readonly", async (store) => {
      const record = await requestResult<EncryptedPairedHost | undefined>(store.get(id));
      return record;
    });
  }

  async listRecords(): Promise<EncryptedPairedHost[]> {
    return this.run(recordsStore, "readonly", async (store) =>
      requestResult<EncryptedPairedHost[]>(store.getAll())
    );
  }

  async putRecord(record: EncryptedPairedHost): Promise<void> {
    await this.run(recordsStore, "readwrite", async (store) => {
      await requestResult(store.put(record));
    });
  }

  async deleteRecord(id: string): Promise<void> {
    await this.run(recordsStore, "readwrite", async (store) => {
      await requestResult(store.delete(id));
    });
  }

  private async run<T>(
    storeName: string,
    mode: IDBTransactionMode,
    fn: (store: IDBObjectStore) => Promise<T>
  ): Promise<T> {
    const db = await openPairedHostDatabase();
    try {
      const tx = db.transaction(storeName, mode);
      const value = await fn(tx.objectStore(storeName));
      await transactionDone(tx);
      return value;
    } finally {
      db.close();
    }
  }
}

function normalizePairedHost(value: unknown): PairedHost {
  if (!isRecord(value)) {
    throw new Error("invalid paired-host record");
  }
  const id = stringValue(value.id);
  const displayName = stringValue(value.displayName);
  const identityPublic = stringValue(value.identityPublic);
  const state = stringValue(value.state);
  if (
    value.version !== pairedHostRegistryVersion ||
    !validHostID(id) ||
    displayName.length === 0 ||
    displayName.length > 128 ||
    identityPublic.length === 0 ||
    identityPublic.length > 4096 ||
    !validState(state)
  ) {
    throw new Error("invalid paired-host record");
  }
  if (!isRecord(value.endpoint)) {
    throw new Error("invalid paired-host endpoint");
  }
  const kind = stringValue(value.endpoint.kind);
  const endpointURL = stringValue(value.endpoint.url);
  if (!validEndpoint(kind, endpointURL)) {
    throw new Error("invalid paired-host endpoint");
  }
  return {
    version: pairedHostRegistryVersion,
    id,
    displayName,
    endpoint: { kind: kind as PairedHostEndpointKind, url: endpointURL },
    identityPublic,
    state: state as PairedHostState
  };
}

function validHostID(value: string): boolean {
  return /^[a-z0-9][a-z0-9_-]{2,63}$/.test(value);
}

function validState(value: string): value is PairedHostState {
  return value === "pending" || value === "active" || value === "stale" || value === "revoked";
}

function validEndpoint(kind: string, value: string): kind is PairedHostEndpointKind {
  if (kind === "ssh") {
    return value.length > 0 && !/[\r\n]/.test(value);
  }
  if (kind !== "mesh" && kind !== "relay") {
    return false;
  }
  try {
    const endpoint = new URL(value);
    return (
      endpoint.protocol === "https:" &&
      endpoint.host !== "" &&
      endpoint.username === "" &&
      endpoint.password === "" &&
      endpoint.search === "" &&
      endpoint.hash === ""
    );
  } catch {
    return false;
  }
}

function validateRegistryKey(key: CryptoKey): void {
  if (
    key.type !== "secret" ||
    key.extractable ||
    key.algorithm.name !== "AES-GCM" ||
    !key.usages.includes("encrypt") ||
    !key.usages.includes("decrypt")
  ) {
    throw new Error("invalid paired-host registry key");
  }
}

function validateEncryptedRecord(record: EncryptedPairedHost): void {
  if (
    !validHostID(record.id) ||
    !(record.iv instanceof ArrayBuffer) ||
    record.iv.byteLength !== 12 ||
    !(record.ciphertext instanceof ArrayBuffer) ||
    record.ciphertext.byteLength === 0
  ) {
    throw new Error("invalid encrypted paired-host record");
  }
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function registryAAD(id: string): ArrayBuffer {
  return copyBuffer(new TextEncoder().encode(registryAADPrefix + id));
}

function randomBytes(size: number): Uint8Array<ArrayBuffer> {
  const value = new Uint8Array(new ArrayBuffer(size));
  crypto.getRandomValues(value);
  return value;
}

function copyBuffer(value: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(value.byteLength);
  copy.set(value);
  return copy.buffer;
}

function isConstraintError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "ConstraintError";
}

function openPairedHostDatabase(): Promise<IDBDatabase> {
  if (typeof indexedDB === "undefined") {
    return Promise.reject(new Error("IndexedDB unavailable"));
  }
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, databaseVersion);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(keysStore)) {
        db.createObjectStore(keysStore);
      }
      if (!db.objectStoreNames.contains(recordsStore)) {
        db.createObjectStore(recordsStore, { keyPath: "id" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("open paired-host registry"));
    request.onblocked = () => reject(new Error("paired-host registry upgrade blocked"));
  });
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () =>
      reject(request.error ?? new Error("paired-host registry request failed"));
  });
}

function transactionDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onabort = () => reject(tx.error ?? new Error("paired-host registry transaction aborted"));
    tx.onerror = () => reject(tx.error ?? new Error("paired-host registry transaction failed"));
  });
}
