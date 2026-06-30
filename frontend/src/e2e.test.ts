import vectors from "../../internal/e2e/testdata/vectors.json";

const encoder = new TextEncoder();

export async function verifyE2EVectors(): Promise<void> {
  if (vectors.info !== "onibi-e2e-v1") {
    throw new Error("bad e2e vector info");
  }
  for (const vector of vectors.vectors) {
    const base = await crypto.subtle.importKey("raw", arrayBuffer(fromHex(vector.masterKeyHex)), "HKDF", false, ["deriveBits"]);
    const bits = await crypto.subtle.deriveBits(
      {
        name: "HKDF",
        hash: "SHA-256",
        salt: arrayBuffer(encoder.encode(vector.sessionID)),
        info: arrayBuffer(encoder.encode(vectors.info))
      },
      base,
      256
    );
    if (toHex(new Uint8Array(bits)) !== vector.sessionKeyHex) {
      throw new Error(`${vector.name}: session key mismatch`);
    }
    const key = await crypto.subtle.importKey("raw", bits, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
    const sealed = await crypto.subtle.encrypt(
      {
        name: "AES-GCM",
        iv: arrayBuffer(fromHex(vector.ivHex)),
        additionalData: arrayBuffer(fromHex(vector.aadHex)),
        tagLength: 128
      },
      key,
      arrayBuffer(fromHex(vector.plaintextHex))
    );
    if (toHex(new Uint8Array(sealed)) !== vector.ciphertextHex) {
      throw new Error(`${vector.name}: ciphertext mismatch`);
    }
  }
}

function fromHex(value: string): Uint8Array {
  if (value.length % 2 !== 0) {
    throw new Error("bad hex length");
  }
  const out = new Uint8Array(value.length / 2);
  for (let i = 0; i < out.length; i += 1) {
    out[i] = Number.parseInt(value.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function arrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const out = new Uint8Array(bytes.byteLength);
  out.set(bytes);
  return out.buffer;
}
