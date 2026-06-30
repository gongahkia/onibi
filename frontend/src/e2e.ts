const version = "onibi.e2e.v1";
const channelSalt = new TextEncoder().encode("onibi relay e2e salt v1");
const sessionInfo = "onibi-e2e-v1";
const verifyTokenInfo = "onibi-verify-token-v1";
const encoder = new TextEncoder();

type Frame = {
  v: string;
  t: "text" | "binary";
  n: string;
  ct: string;
};

export type E2EFrame = {
  type: "text" | "binary";
  data: Uint8Array;
};

export class RelayE2E {
  private readonly keys = new Map<string, Promise<CryptoKey>>();
  private readonly sendSeq = new Map<string, bigint>();
  private readonly recvSeq = new Map<string, bigint>();
  private sessionID = "";
  private sessionKey: Promise<CryptoKey> | undefined;
  private verifyTokenValue = "";

  private constructor(private readonly pairKey: CryptoKey) {}

  static async fromFragment(): Promise<RelayE2E | undefined> {
    const hash = window.location.hash.startsWith("#") ? window.location.hash.slice(1) : "";
    const match = hash.match(/(?:^|&)k=([A-Za-z0-9_-]+)/);
    if (match === null) {
      return undefined;
    }
    const encoded = match[1];
    const raw = decodeBase64URL(encoded);
    if (raw.byteLength !== 32) {
      throw new Error("bad e2e key length");
    }
    const baseKey = await crypto.subtle.importKey("raw", arrayBuffer(raw), "HKDF", false, ["deriveBits"]);
    history.replaceState(null, document.title, window.location.pathname + window.location.search);
    return new RelayE2E(baseKey);
  }

  async bindSession(sessionID: string): Promise<void> {
    if (sessionID === "") {
      throw new Error("missing e2e session");
    }
    this.sessionID = sessionID;
    this.keys.clear();
    this.sendSeq.clear();
    this.recvSeq.clear();
    const sessionBits = await this.deriveBits(sessionID, sessionInfo);
    const verifyBits = await this.deriveBits(sessionID, verifyTokenInfo);
    this.sessionKey = crypto.subtle.importKey("raw", sessionBits, "HKDF", false, ["deriveKey"]);
    this.verifyTokenValue = encodeBase64URL(new Uint8Array(verifyBits));
  }

  verifyToken(): string {
    if (this.verifyTokenValue === "") {
      throw new Error("e2e session unbound");
    }
    return this.verifyTokenValue;
  }

  async sealText(value: string, info: string): Promise<string> {
    return this.seal("text", new TextEncoder().encode(value), info);
  }

  async sealBytes(type: "text" | "binary", data: Uint8Array, info: string): Promise<string> {
    return this.seal(type, data, info);
  }

  async open(raw: string, info: string): Promise<E2EFrame> {
    const frame = JSON.parse(raw) as Frame;
    if (frame.v !== version) {
      throw new Error("bad e2e frame");
    }
    const nonce = decodeBase64URL(frame.n);
    const data = decodeBase64URL(frame.ct);
    const key = await this.key(info);
    const seq = this.sequence(info, this.recvSeq);
    const params: AesGcmParams = { name: "AES-GCM", iv: arrayBuffer(nonce) };
    if (seq !== undefined) {
      params.additionalData = this.aad(info, "s2c", seq, frame.t);
    }
    const plain = await crypto.subtle.decrypt(params, key, arrayBuffer(data));
    if (seq !== undefined) {
      this.recvSeq.set(info, seq + 1n);
    }
    return { type: frame.t, data: new Uint8Array(plain) };
  }

  private async seal(type: "text" | "binary", data: Uint8Array, info: string): Promise<string> {
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const key = await this.key(info);
    const seq = this.sequence(info, this.sendSeq);
    const params: AesGcmParams = { name: "AES-GCM", iv: arrayBuffer(nonce) };
    if (seq !== undefined) {
      params.additionalData = this.aad(info, "c2s", seq, type);
    }
    const sealed = await crypto.subtle.encrypt(params, key, arrayBuffer(data));
    if (seq !== undefined) {
      this.sendSeq.set(info, seq + 1n);
    }
    const frame: Frame = {
      v: version,
      t: type,
      n: encodeBase64URL(nonce),
      ct: encodeBase64URL(new Uint8Array(sealed))
    };
    return JSON.stringify(frame);
  }

  private key(info: string): Promise<CryptoKey> {
    let cached = this.keys.get(info);
    if (cached === undefined) {
      cached = this.sessionBaseKey().then((baseKey) =>
        crypto.subtle.deriveKey(
          { name: "HKDF", hash: "SHA-256", salt: arrayBuffer(channelSalt), info: arrayBuffer(encoder.encode(info)) },
          baseKey,
          { name: "AES-GCM", length: 256 },
          false,
          ["encrypt", "decrypt"]
        )
      );
      this.keys.set(info, cached);
    }
    return cached;
  }

  private async sessionBaseKey(): Promise<CryptoKey> {
    if (this.sessionKey === undefined) {
      throw new Error("e2e session unbound");
    }
    return this.sessionKey;
  }

  private deriveBits(sessionID: string, info: string): Promise<ArrayBuffer> {
    return crypto.subtle.deriveBits(
      { name: "HKDF", hash: "SHA-256", salt: arrayBuffer(encoder.encode(sessionID)), info: arrayBuffer(encoder.encode(info)) },
      this.pairKey,
      256
    );
  }

  private sequence(info: string, store: Map<string, bigint>): bigint | undefined {
    if (info !== "ws:pty" && info !== "ws:events") {
      return undefined;
    }
    return store.get(info) ?? 0n;
  }

  private aad(info: string, dir: "c2s" | "s2c", seq: bigint, type: "text" | "binary"): ArrayBuffer {
    return arrayBuffer(
      concatBytes(
        encoder.encode(this.sessionID),
        new Uint8Array([0]),
        encoder.encode(info),
        new Uint8Array([0]),
        encoder.encode(dir),
        new Uint8Array([0]),
        uint64BE(seq),
        new Uint8Array([0]),
        encoder.encode(type)
      )
    );
  }
}

export function decodeText(data: Uint8Array): string {
  return new TextDecoder().decode(data);
}

function encodeBase64URL(bytes: Uint8Array): string {
  let binary = "";
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return window.btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function decodeBase64URL(value: string): Uint8Array {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  const binary = window.atob(padded);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

function arrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const out = new Uint8Array(bytes.byteLength);
  out.set(bytes);
  return out.buffer;
}

function uint64BE(value: bigint): Uint8Array {
  const out = new Uint8Array(8);
  for (let i = 7; i >= 0; i -= 1) {
    out[i] = Number(value & 0xffn);
    value >>= 8n;
  }
  return out;
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, part) => n + part.byteLength, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.byteLength;
  }
  return out;
}
