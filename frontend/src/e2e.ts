const version = "onibi.e2e.v1";
const sessionInfo = "onibi-e2e-v1";
const verifyTokenInfo = "onibi-e2e-session-verifier-v1";
const streamInfoPrefix = "onibi-e2e-stream-v1:";
const nonceInfoPrefix = "onibi-e2e-nonce-v1:";
export const relayE2EContentType = "application/onibi-e2e+json";
const oldRelayE2EContentType = "application/vnd.onibi.e2e+json";
const encoder = new TextEncoder();

type Direction = "c2s" | "s2c";

type Frame = {
  v: string;
  sid: string;
  st: string;
  ch: string;
  dir: Direction;
  seq: number;
  iv: string;
  t: "text" | "binary";
  ct: string;
};

type StreamState = {
  streamID: string;
  sendSeq: bigint;
  recvSeq: bigint;
};

export type E2EFrame = {
  type: "text" | "binary";
  data: Uint8Array;
};

export type E2EHTTPRequest = {
  body: string;
  streamID: string;
};

export class RelayE2E {
  private readonly keys = new Map<string, Promise<CryptoKey>>();
  private readonly streams = new Map<string, StreamState>();
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
    const baseKey = await crypto.subtle.importKey("raw", arrayBuffer(raw), "HKDF", false, [
      "deriveBits"
    ]);
    history.replaceState(null, document.title, window.location.pathname + window.location.search);
    return new RelayE2E(baseKey);
  }

  async bindSession(sessionID: string): Promise<void> {
    if (sessionID === "") {
      throw new Error("missing e2e session");
    }
    this.sessionID = sessionID;
    this.keys.clear();
    this.streams.clear();
    const sessionBits = await this.derivePairBits(sessionID, sessionInfo);
    const verifyBits = await this.derivePairBits(sessionID, verifyTokenInfo);
    this.sessionKey = crypto.subtle.importKey("raw", sessionBits, "HKDF", false, [
      "deriveKey",
      "deriveBits"
    ]);
    this.verifyTokenValue = encodeBase64URL(new Uint8Array(verifyBits));
  }

  verifyToken(): string {
    if (this.verifyTokenValue === "") {
      throw new Error("e2e session unbound");
    }
    return this.verifyTokenValue;
  }

  startStream(channel: string): void {
    this.streams.set(channel, { streamID: newStreamID(), sendSeq: 0n, recvSeq: 0n });
  }

  async sealHTTPRequest(value: string, channel: string): Promise<E2EHTTPRequest> {
    const streamID = newStreamID();
    const body = await this.sealFrame(
      "text",
      new TextEncoder().encode(value),
      channel,
      streamID,
      "c2s",
      0n
    );
    return { body, streamID };
  }

  async openHTTPResponse(response: Response, channel: string, streamID: string): Promise<Response> {
    if (!isRelayE2EContentType(response.headers.get("Content-Type") ?? "")) {
      return response;
    }
    const raw = await response.text();
    if (raw === "") {
      return responseWith(response, new Uint8Array());
    }
    const frame = await this.openFrame(raw, channel, streamID, "s2c", 0n);
    return responseWith(response, frame.data);
  }

  async sealText(value: string, channel: string): Promise<string> {
    return this.sealBytes("text", new TextEncoder().encode(value), channel);
  }

  async sealBytes(type: "text" | "binary", data: Uint8Array, channel: string): Promise<string> {
    const state = this.stream(channel);
    const out = await this.sealFrame(type, data, channel, state.streamID, "c2s", state.sendSeq);
    state.sendSeq += 1n;
    return out;
  }

  async open(raw: string, channel: string): Promise<E2EFrame> {
    const state = this.stream(channel);
    const frame = await this.openFrame(raw, channel, state.streamID, "s2c", state.recvSeq);
    state.recvSeq += 1n;
    return frame;
  }

  private async sealFrame(
    type: "text" | "binary",
    data: Uint8Array,
    channel: string,
    streamID: string,
    dir: Direction,
    seq: bigint
  ): Promise<string> {
    const iv = await this.iv(streamID, channel, dir, seq);
    const frame: Frame = {
      v: version,
      sid: this.boundSessionID(),
      st: streamID,
      ch: channel,
      dir,
      seq: Number(seq),
      iv: encodeBase64URL(iv),
      t: type,
      ct: ""
    };
    const key = await this.streamKey(streamID, channel, dir);
    const sealed = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: arrayBuffer(iv), additionalData: this.aad(frame) },
      key,
      arrayBuffer(data)
    );
    frame.ct = encodeBase64URL(new Uint8Array(sealed));
    return JSON.stringify(frame);
  }

  private async openFrame(
    raw: string,
    channel: string,
    streamID: string,
    dir: Direction,
    seq: bigint
  ): Promise<E2EFrame> {
    const frame = JSON.parse(raw) as Frame;
    if (
      frame.v !== version ||
      frame.sid !== this.boundSessionID() ||
      frame.st !== streamID ||
      frame.ch !== channel ||
      frame.dir !== dir ||
      BigInt(frame.seq) !== seq
    ) {
      throw new Error("bad e2e frame");
    }
    if (frame.t !== "text" && frame.t !== "binary") {
      throw new Error("bad e2e frame type");
    }
    const iv = decodeBase64URL(frame.iv);
    const wantIV = await this.iv(streamID, channel, dir, seq);
    if (iv.byteLength !== 12 || !equalBytes(iv, wantIV)) {
      throw new Error("bad e2e iv");
    }
    const data = decodeBase64URL(frame.ct);
    const key = await this.streamKey(streamID, channel, dir);
    const plain = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: arrayBuffer(iv), additionalData: this.aad(frame) },
      key,
      arrayBuffer(data)
    );
    return { type: frame.t, data: new Uint8Array(plain) };
  }

  private stream(channel: string): StreamState {
    let state = this.streams.get(channel);
    if (state === undefined) {
      state = { streamID: newStreamID(), sendSeq: 0n, recvSeq: 0n };
      this.streams.set(channel, state);
    }
    return state;
  }

  private streamKey(streamID: string, channel: string, dir: Direction): Promise<CryptoKey> {
    const cacheKey = `${streamID}:${channel}:${dir}`;
    let cached = this.keys.get(cacheKey);
    if (cached === undefined) {
      cached = this.sessionBaseKey().then((baseKey) =>
        crypto.subtle.deriveKey(
          {
            name: "HKDF",
            hash: "SHA-256",
            salt: arrayBuffer(decodeBase64URL(streamID)),
            info: arrayBuffer(encoder.encode(streamInfoPrefix + channel + ":" + dir))
          },
          baseKey,
          { name: "AES-GCM", length: 256 },
          false,
          ["encrypt", "decrypt"]
        )
      );
      this.keys.set(cacheKey, cached);
    }
    return cached;
  }

  private async iv(
    streamID: string,
    channel: string,
    dir: Direction,
    seq: bigint
  ): Promise<Uint8Array> {
    const prefix = new Uint8Array(
      await crypto.subtle.deriveBits(
        {
          name: "HKDF",
          hash: "SHA-256",
          salt: arrayBuffer(decodeBase64URL(streamID)),
          info: arrayBuffer(encoder.encode(nonceInfoPrefix + channel + ":" + dir))
        },
        await this.sessionBaseKey(),
        32
      )
    );
    return concatBytes(prefix, uint64BE(seq));
  }

  private async sessionBaseKey(): Promise<CryptoKey> {
    if (this.sessionKey === undefined) {
      throw new Error("e2e session unbound");
    }
    return this.sessionKey;
  }

  private boundSessionID(): string {
    if (this.sessionID === "") {
      throw new Error("e2e session unbound");
    }
    return this.sessionID;
  }

  private derivePairBits(sessionID: string, info: string): Promise<ArrayBuffer> {
    return crypto.subtle.deriveBits(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: arrayBuffer(encoder.encode(sessionID)),
        info: arrayBuffer(encoder.encode(info))
      },
      this.pairKey,
      256
    );
  }

  private aad(frame: Frame): ArrayBuffer {
    return arrayBuffer(
      encoder.encode(
        [
          frame.v,
          frame.sid,
          frame.st,
          frame.ch,
          frame.dir,
          String(frame.seq),
          frame.iv,
          frame.t
        ].join("\n")
      )
    );
  }
}

export function decodeText(data: Uint8Array): string {
  return new TextDecoder().decode(data);
}

export function isRelayE2EContentType(value: string): boolean {
  const base = value.split(";")[0].trim().toLowerCase();
  return base === relayE2EContentType || base === oldRelayE2EContentType;
}

function newStreamID(): string {
  return encodeBase64URL(crypto.getRandomValues(new Uint8Array(16)));
}

function encodeBase64URL(bytes: Uint8Array): string {
  let binary = "";
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return window.btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function decodeBase64URL(value: string): Uint8Array {
  const padded = value
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .padEnd(Math.ceil(value.length / 4) * 4, "=");
  const binary = window.atob(padded);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

function responseWith(response: Response, data: Uint8Array): Response {
  const headers = new Headers(response.headers);
  headers.set("Content-Type", "application/json");
  headers.delete("Content-Length");
  return new Response(arrayBuffer(data), {
    status: response.status,
    statusText: response.statusText,
    headers
  });
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

function equalBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.byteLength !== b.byteLength) {
    return false;
  }
  let diff = 0;
  for (let i = 0; i < a.byteLength; i += 1) {
    diff |= a[i] ^ b[i];
  }
  return diff === 0;
}
