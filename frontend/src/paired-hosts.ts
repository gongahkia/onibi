export const pairedHostRegistryVersion = 1;

const databaseName = "onibi-paired-hosts";
const databaseVersion = 1;
const keysStore = "keys";
const recordsStore = "records";
const registryKeyID = "registry-key";
const registryAADPrefix = "onibi-paired-hosts/v1/";
const exportAAD = "onibi-paired-host-export/v1";
const exportKDFIterations = 600000;
const maxExportHosts = 128;
const maxExportPayloadBytes = 1 << 20;
const minExportPassphraseBytes = 12;

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

export interface PairedHostRegistryExport {
  version: number;
  kdf: {
    name: "PBKDF2";
    hash: "SHA-256";
    iterations: number;
    salt: string;
  };
  cipher: {
    name: "AES-GCM";
    iv: string;
    ciphertext: string;
  };
}

export interface PairedHostRegistryBackend {
  getKey(): Promise<CryptoKey | undefined>;
  addKey(key: CryptoKey): Promise<void>;
  getRecord(id: string): Promise<EncryptedPairedHost | undefined>;
  listRecords(): Promise<EncryptedPairedHost[]>;
  putRecord(record: EncryptedPairedHost): Promise<void>;
  putRecordsIfAbsent(records: EncryptedPairedHost[]): Promise<void>;
  deleteRecord(id: string): Promise<void>;
}

export class PairedHostRegistry {
  constructor(
    private readonly backend: PairedHostRegistryBackend = new IndexedDBPairedHostRegistryBackend()
  ) {}

  async put(host: PairedHost): Promise<void> {
    const normalized = normalizePairedHost(host);
    const previous = await this.get(normalized.id);
    if (previous?.state === "revoked" && normalized.state !== "revoked") {
      throw new Error("cannot reactivate revoked paired-host record");
    }
    const key = await this.encryptionKey();
    await this.backend.putRecord(await this.encrypt(normalized, key));
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

  async revoke(id: string): Promise<void> {
    const host = await this.get(id);
    if (host === undefined) {
      throw new Error("paired-host record not found");
    }
    if (host.state === "revoked") {
      return;
    }
    await this.put({ ...host, state: "revoked" });
  }

  async exportEncrypted(passphrase: string): Promise<string> {
    assertExportPassphrase(passphrase);
    const hosts = (await this.list()).filter(
      (host) => host.state === "pending" || host.state === "active"
    );
    if (hosts.length > maxExportHosts) {
      throw new Error("too many paired-host records to export");
    }
    const salt = randomBytes(16);
    const key = await exportKey(passphrase, salt);
    const plaintext = new TextEncoder().encode(
      JSON.stringify({ version: pairedHostRegistryVersion, hosts })
    );
    const iv = randomBytes(12);
    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: copyBuffer(iv), additionalData: exportAdditionalData() },
      key,
      plaintext
    );
    const envelope: PairedHostRegistryExport = {
      version: pairedHostRegistryVersion,
      kdf: {
        name: "PBKDF2",
        hash: "SHA-256",
        iterations: exportKDFIterations,
        salt: toBase64URL(salt)
      },
      cipher: {
        name: "AES-GCM",
        iv: toBase64URL(iv),
        ciphertext: toBase64URL(new Uint8Array(ciphertext))
      }
    };
    return JSON.stringify(envelope);
  }

  async importEncrypted(serialized: string, passphrase: string): Promise<number> {
    assertExportPassphrase(passphrase);
    const hosts = await decryptExport(serialized, passphrase);
    const existing = new Map((await this.list()).map((host) => [host.id, host]));
    const additions: PairedHost[] = [];
    for (const host of hosts) {
      const previous = existing.get(host.id);
      if (previous === undefined) {
        additions.push(host);
        continue;
      }
      if (JSON.stringify(previous) !== JSON.stringify(host)) {
        throw new Error("paired-host import conflicts with existing id " + host.id);
      }
    }
    if (additions.length === 0) {
      return 0;
    }
    const key = await this.encryptionKey();
    const records = await Promise.all(additions.map((host) => this.encrypt(host, key)));
    try {
      await this.backend.putRecordsIfAbsent(records);
    } catch (error) {
      if (isConstraintError(error)) {
        throw new Error("paired-host import conflicts with an existing record");
      }
      throw error;
    }
    return additions.length;
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

  private async encrypt(host: PairedHost, key: CryptoKey): Promise<EncryptedPairedHost> {
    const iv = randomBytes(12);
    const plaintext = new TextEncoder().encode(JSON.stringify(host));
    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: copyBuffer(iv), additionalData: registryAAD(host.id) },
      key,
      plaintext
    );
    return { id: host.id, iv: copyBuffer(iv), ciphertext };
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

  async putRecordsIfAbsent(records: EncryptedPairedHost[]): Promise<void> {
    await this.run(recordsStore, "readwrite", async (store) => {
      for (const record of records) {
        if (
          (await requestResult<EncryptedPairedHost | undefined>(store.get(record.id))) !== undefined
        ) {
          throw new DOMException("paired-host record exists", "ConstraintError");
        }
      }
      for (const record of records) {
        await requestResult(store.add(record));
      }
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
      try {
        const value = await fn(tx.objectStore(storeName));
        await transactionDone(tx);
        return value;
      } catch (error) {
        try {
          tx.abort();
        } catch {}
        throw error;
      }
    } finally {
      db.close();
    }
  }
}

async function decryptExport(serialized: string, passphrase: string): Promise<PairedHost[]> {
  if (new TextEncoder().encode(serialized).byteLength > maxExportPayloadBytes * 2) {
    throw new Error("paired-host export is too large");
  }
  let value: unknown;
  try {
    value = JSON.parse(serialized);
  } catch {
    throw new Error("invalid paired-host export");
  }
  const envelope = normalizeExport(value);
  const salt = fromBase64URL(envelope.kdf.salt);
  const iv = fromBase64URL(envelope.cipher.iv);
  const ciphertext = fromBase64URL(envelope.cipher.ciphertext);
  if (
    salt.byteLength !== 16 ||
    iv.byteLength !== 12 ||
    ciphertext.byteLength === 0 ||
    ciphertext.byteLength > maxExportPayloadBytes
  ) {
    throw new Error("invalid paired-host export");
  }
  const key = await exportKey(passphrase, salt);
  let plaintext: ArrayBuffer;
  try {
    plaintext = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: copyBuffer(iv), additionalData: exportAdditionalData() },
      key,
      copyBuffer(ciphertext)
    );
  } catch {
    throw new Error("paired-host export failed authentication");
  }
  let payload: unknown;
  try {
    payload = JSON.parse(new TextDecoder().decode(plaintext));
  } catch {
    throw new Error("invalid paired-host export payload");
  }
  if (
    !isRecord(payload) ||
    payload.version !== pairedHostRegistryVersion ||
    !Array.isArray(payload.hosts) ||
    payload.hosts.length > maxExportHosts
  ) {
    throw new Error("incompatible paired-host export");
  }
  const ids = new Set<string>();
  const hosts = payload.hosts.map((host) => normalizePairedHost(host));
  for (const host of hosts) {
    if (host.state === "stale" || host.state === "revoked") {
      throw new Error("stale or revoked paired-host imports are not allowed");
    }
    if (ids.has(host.id)) {
      throw new Error("duplicate paired-host id in export");
    }
    ids.add(host.id);
  }
  return hosts;
}

function normalizeExport(value: unknown): PairedHostRegistryExport {
  if (
    !isRecord(value) ||
    value.version !== pairedHostRegistryVersion ||
    !isRecord(value.kdf) ||
    !isRecord(value.cipher)
  ) {
    throw new Error("incompatible paired-host export");
  }
  if (
    value.kdf.name !== "PBKDF2" ||
    value.kdf.hash !== "SHA-256" ||
    value.kdf.iterations !== exportKDFIterations ||
    value.cipher.name !== "AES-GCM"
  ) {
    throw new Error("incompatible paired-host export");
  }
  const salt = stringValue(value.kdf.salt);
  const iv = stringValue(value.cipher.iv);
  const ciphertext = stringValue(value.cipher.ciphertext);
  if (salt === "" || iv === "" || ciphertext === "") {
    throw new Error("invalid paired-host export");
  }
  return {
    version: pairedHostRegistryVersion,
    kdf: { name: "PBKDF2", hash: "SHA-256", iterations: exportKDFIterations, salt },
    cipher: { name: "AES-GCM", iv, ciphertext }
  };
}

async function exportKey(passphrase: string, salt: Uint8Array): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(passphrase),
    "PBKDF2",
    false,
    ["deriveKey"]
  );
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt: copyBuffer(salt), iterations: exportKDFIterations, hash: "SHA-256" },
    base,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"]
  );
}

function assertExportPassphrase(passphrase: string): void {
  if (new TextEncoder().encode(passphrase).byteLength < minExportPassphraseBytes) {
    throw new Error("paired-host export passphrase must be at least 12 bytes");
  }
}

function exportAdditionalData(): ArrayBuffer {
  return copyBuffer(new TextEncoder().encode(exportAAD));
}

function toBase64URL(value: Uint8Array): string {
  let binary = "";
  for (const byte of value) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function fromBase64URL(value: string): Uint8Array {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) {
    throw new Error("invalid paired-host export encoding");
  }
  const padded =
    value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - (value.length % 4)) % 4);
  let binary: string;
  try {
    binary = atob(padded);
  } catch {
    throw new Error("invalid paired-host export encoding");
  }
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
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
    const match = /^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+|\[[0-9A-Fa-f:]+\])(?::([0-9]{1,5}))?$/.exec(
      value
    );
    if (match === null) {
      return false;
    }
    const port = match[3] === undefined ? 0 : Number(match[3]);
    return port === 0 || (port >= 1 && port <= 65535);
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
      endpoint.pathname === "/" &&
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
