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
  });

  it("handles concurrent registry key initialization", async () => {
    const backend = new MemoryRegistryBackend();
    const first = new PairedHostRegistry(backend);
    const second = new PairedHostRegistry(backend);
    const mini = { ...host, id: "host-mini", displayName: "Mac mini" };

    await Promise.all([first.put(host), second.put(mini)]);
    await expect(first.list()).resolves.toEqual([mini, host]);
  });
});

function copyBuffer(value: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(value.byteLength);
  copy.set(value);
  return copy.buffer;
}
