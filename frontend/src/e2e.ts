const version = "onibi.e2e.v1";
const salt = new TextEncoder().encode("onibi relay e2e salt v1");
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

  private constructor(private readonly baseKey: CryptoKey) {}

  static async fromFragment(): Promise<RelayE2E | undefined> {
    const hash = window.location.hash.startsWith("#") ? window.location.hash.slice(1) : "";
    const params = new URLSearchParams(hash);
    const encoded = params.get("k");
    if (encoded === null || encoded === "") {
      return undefined;
    }
    const raw = decodeBase64URL(encoded);
    const baseKey = await crypto.subtle.importKey("raw", arrayBuffer(raw), "HKDF", false, ["deriveKey"]);
    history.replaceState(null, document.title, window.location.pathname + window.location.search);
    return new RelayE2E(baseKey);
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
    const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: arrayBuffer(nonce) }, key, arrayBuffer(data));
    return { type: frame.t, data: new Uint8Array(plain) };
  }

  private async seal(type: "text" | "binary", data: Uint8Array, info: string): Promise<string> {
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const key = await this.key(info);
    const sealed = await crypto.subtle.encrypt({ name: "AES-GCM", iv: arrayBuffer(nonce) }, key, arrayBuffer(data));
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
      cached = crypto.subtle.deriveKey(
        { name: "HKDF", hash: "SHA-256", salt: arrayBuffer(salt), info: arrayBuffer(encoder.encode(info)) },
        this.baseKey,
        { name: "AES-GCM", length: 256 },
        false,
        ["encrypt", "decrypt"]
      );
      this.keys.set(info, cached);
    }
    return cached;
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
