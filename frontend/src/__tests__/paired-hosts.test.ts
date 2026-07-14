// @vitest-environment node

import { webcrypto } from "node:crypto";

import { beforeEach, describe, expect, it } from "vitest";

import {
  type EncryptedPairedHost,
  type PairedHost,
  type PairedHostRegistryBackend,
  PairedHostRegistry,
  pairedHostRegistryVersion
} from "../paired-hosts";

class MemoryRegistryBackend implements PairedHostRegistryBackend {
  key: CryptoKey | undefined;
  readonly records = new Map<string, EncryptedPairedHost>();

  async getKey(): Promise<CryptoKey | undefined> {
    return this.key;
  }

  async addKey(key: CryptoKey): Promise<void> {
    if (this.key !== undefined) {
      throw new DOMException("key exists", "ConstraintError");
    }
    this.key = key;
  }

  async getRecord(id: string): Promise<EncryptedPairedHost | undefined> {
    return this.records.get(id);
  }

  async listRecords(): Promise<EncryptedPairedHost[]> {
    return [...this.records.values()];
  }

  async putRecord(record: EncryptedPairedHost): Promise<void> {
    this.records.set(record.id, record);
  }

  async putRecordsIfAbsent(records: EncryptedPairedHost[]): Promise<void> {
    if (records.some((record) => this.records.has(record.id))) {
      throw new DOMException("record exists", "ConstraintError");
    }
    for (const record of records) {
      this.records.set(record.id, record);
    }
  }

  async deleteRecord(id: string): Promise<void> {
    this.records.delete(id);
  }
}

const host: PairedHost = {
  version: pairedHostRegistryVersion,
  id: "host-macbook",
  displayName: "MacBook Pro",
  endpoint: { kind: "mesh", url: "https://macbook.tailnet.ts.net" },
  identityPublic: "ed25519-public-key",
  state: "active"
};
const slowCryptoTimeout = 15000;

beforeEach(() => {
  Object.defineProperty(globalThis, "crypto", { configurable: true, value: webcrypto });
});

describe("PairedHostRegistry", () => {
  it("persists only authenticated ciphertext and excludes enrollment secrets", async () => {
    const backend = new MemoryRegistryBackend();
    const registry = new PairedHostRegistry(backend);
    await registry.put({ ...host, enrollmentSecret: "must-not-persist" } as PairedHost);

    const stored = backend.records.get(host.id);
    expect(stored).toMatchObject({ id: host.id });
    expect(stored?.iv.byteLength).toBe(12);
    expect(stored?.ciphertext).toBeInstanceOf(ArrayBuffer);
    expect(backend.key?.extractable).toBe(false);
    expect(await registry.get(host.id)).toEqual(host);

    const plaintext = await webcrypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: stored?.iv ?? new ArrayBuffer(0),
        additionalData: copyBuffer(new TextEncoder().encode(`onibi-paired-hosts/v1/${host.id}`))
      },
      backend.key as CryptoKey,
      stored?.ciphertext as ArrayBuffer
    );
    expect(new TextDecoder().decode(plaintext)).not.toContain("must-not-persist");
  });

  it("rejects tampering and ciphertext swapping", async () => {
    const backend = new MemoryRegistryBackend();
    const registry = new PairedHostRegistry(backend);
    await registry.put(host);
    const stored = backend.records.get(host.id) as EncryptedPairedHost;
    backend.records.set("host-mini", { ...stored, id: "host-mini" });

    await expect(registry.get("host-mini")).rejects.toThrow("failed authentication");
    backend.records.set(host.id, { ...stored, ciphertext: new Uint8Array([1, 2, 3]).buffer });
    await expect(registry.get(host.id)).rejects.toThrow("failed authentication");
  });

  it("persists records across registry instances and validates input", async () => {
    const backend = new MemoryRegistryBackend();
    const first = new PairedHostRegistry(backend);
    await first.put(host);
    const second = new PairedHostRegistry(backend);

    await expect(second.list()).resolves.toEqual([host]);
    await second.remove(host.id);
    await expect(second.get(host.id)).resolves.toBeUndefined();
    await expect(
      first.put({ ...host, endpoint: { kind: "relay", url: "http://unsafe.example.test" } })
    ).rejects.toThrow("invalid paired-host endpoint");
    await expect(
      first.put({
        ...host,
        endpoint: { kind: "relay", url: "https://relay.example.test/pair/secret" }
      })
    ).rejects.toThrow("invalid paired-host endpoint");
    await expect(
      first.put({
        ...host,
        id: "host-ssh",
        endpoint: { kind: "ssh", url: "owner:secret@host.example.test" }
      })
    ).rejects.toThrow("invalid paired-host endpoint");
  });

  it("does not reactivate revoked local records", async () => {
    const registry = new PairedHostRegistry(new MemoryRegistryBackend());
    await registry.put({ ...host, state: "revoked" });

    await expect(registry.put(host)).rejects.toThrow("cannot reactivate revoked");
    await expect(registry.get(host.id)).resolves.toEqual({ ...host, state: "revoked" });
  });

  it("handles concurrent registry key initialization", async () => {
    const backend = new MemoryRegistryBackend();
    const first = new PairedHostRegistry(backend);
    const second = new PairedHostRegistry(backend);
    const mini = { ...host, id: "host-mini", displayName: "Mac mini" };

    await Promise.all([first.put(host), second.put(mini)]);
    await expect(first.list()).resolves.toEqual([mini, host]);
  });

  it(
    "exports only transferable metadata with authenticated encryption",
    async () => {
      const source = new PairedHostRegistry(new MemoryRegistryBackend());
      await source.put(host);
      await source.put({ ...host, id: "host-old", displayName: "Old host", state: "revoked" });

      const exported = await source.exportEncrypted("transfer passphrase");
      expect(exported).not.toContain(host.displayName);
      expect(exported).not.toContain(host.identityPublic);

      const target = new PairedHostRegistry(new MemoryRegistryBackend());
      await expect(target.importEncrypted(exported, "transfer passphrase")).resolves.toBe(1);
      await expect(target.list()).resolves.toEqual([host]);
      await expect(target.importEncrypted(exported, "transfer passphrase")).resolves.toBe(0);
      await expect(target.importEncrypted(exported, "wrong passphrase")).rejects.toThrow(
        "failed authentication"
      );
    },
    slowCryptoTimeout
  );

  it(
    "rejects malformed, incompatible, stale, and revoked imports",
    async () => {
      const source = new PairedHostRegistry(new MemoryRegistryBackend());
      await source.put(host);
      const exported = await source.exportEncrypted("transfer passphrase");
      const target = new PairedHostRegistry(new MemoryRegistryBackend());

      await expect(target.importEncrypted("not json", "transfer passphrase")).rejects.toThrow(
        "invalid paired-host export"
      );
      const incompatible = JSON.parse(exported) as { version: number };
      incompatible.version = 2;
      await expect(
        target.importEncrypted(JSON.stringify(incompatible), "transfer passphrase")
      ).rejects.toThrow("incompatible paired-host export");
      const stale = await reseal(exported, "transfer passphrase", "stale");
      await expect(target.importEncrypted(stale, "transfer passphrase")).rejects.toThrow(
        "stale or revoked"
      );
      const revoked = await reseal(exported, "transfer passphrase", "revoked");
      await expect(target.importEncrypted(revoked, "transfer passphrase")).rejects.toThrow(
        "stale or revoked"
      );
    },
    slowCryptoTimeout
  );

  it(
    "keeps concurrent imports atomic",
    async () => {
      const source = new PairedHostRegistry(new MemoryRegistryBackend());
      await source.put(host);
      const exported = await source.exportEncrypted("transfer passphrase");
      const conflictingSource = new PairedHostRegistry(new MemoryRegistryBackend());
      await conflictingSource.put({ ...host, displayName: "Other Mac" });
      const conflicting = await conflictingSource.exportEncrypted("transfer passphrase");
      const backend = new MemoryRegistryBackend();
      const first = new PairedHostRegistry(backend);
      const second = new PairedHostRegistry(backend);

      const results = await Promise.allSettled([
        first.importEncrypted(exported, "transfer passphrase"),
        second.importEncrypted(conflicting, "transfer passphrase")
      ]);
      expect(results.filter((result) => result.status === "fulfilled")).toHaveLength(1);
      expect(results.filter((result) => result.status === "rejected")).toHaveLength(1);
      const hosts = await first.list();
      expect(hosts).toHaveLength(1);
      expect([host.displayName, "Other Mac"]).toContain(hosts[0].displayName);
    },
    slowCryptoTimeout
  );
});

async function reseal(
  serialized: string,
  passphrase: string,
  state: "stale" | "revoked"
): Promise<string> {
  const envelope = JSON.parse(serialized) as {
    version: number;
    kdf: { salt: string };
    cipher: { iv: string; ciphertext: string };
  };
  const salt = fromBase64URL(envelope.kdf.salt);
  const key = await webcrypto.subtle.deriveKey(
    {
      name: "PBKDF2",
      salt: copyBuffer(salt),
      iterations: 600000,
      hash: "SHA-256"
    },
    await webcrypto.subtle.importKey("raw", new TextEncoder().encode(passphrase), "PBKDF2", false, [
      "deriveKey"
    ]),
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"]
  );
  const plaintext = await webcrypto.subtle.decrypt(
    {
      name: "AES-GCM",
      iv: copyBuffer(fromBase64URL(envelope.cipher.iv)),
      additionalData: new TextEncoder().encode("onibi-paired-host-export/v1")
    },
    key,
    copyBuffer(fromBase64URL(envelope.cipher.ciphertext))
  );
  const payload = JSON.parse(new TextDecoder().decode(plaintext)) as {
    hosts: Array<{ state: string }>;
  };
  payload.hosts[0].state = state;
  const iv = webcrypto.getRandomValues(new Uint8Array(12));
  envelope.cipher.iv = toBase64URL(iv);
  envelope.cipher.ciphertext = toBase64URL(
    new Uint8Array(
      await webcrypto.subtle.encrypt(
        {
          name: "AES-GCM",
          iv: copyBuffer(iv),
          additionalData: new TextEncoder().encode("onibi-paired-host-export/v1")
        },
        key,
        new TextEncoder().encode(JSON.stringify(payload))
      )
    )
  );
  return JSON.stringify(envelope);
}

function toBase64URL(value: Uint8Array): string {
  return Buffer.from(value).toString("base64url");
}

function fromBase64URL(value: string): Uint8Array {
  return new Uint8Array(Buffer.from(value, "base64url"));
}

function copyBuffer(value: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(value.byteLength);
  copy.set(value);
  return copy.buffer;
}
