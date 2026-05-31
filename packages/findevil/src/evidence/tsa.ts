import { spawn } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const sha256Oid = [2, 16, 840, 1, 101, 3, 4, 2, 1] as const;
const statusGranted = new Set([0, 1]);

interface DerElement {
  readonly tag: number;
  readonly start: number;
  readonly valueStart: number;
  readonly end: number;
}

export async function requestTimestampToken(sha256hex: string, tsaUrl: string): Promise<Buffer> {
  return timestampTokenFromResponse(await requestTimestampResponse(sha256hex, tsaUrl));
}

export async function requestTimestampResponse(sha256hex: string, tsaUrl: string): Promise<Buffer> {
  const digest = sha256Digest(sha256hex);
  const request = buildTimestampRequest(digest);
  const response = await fetch(tsaUrl, {
    method: "POST",
    headers: {
      accept: "application/timestamp-reply",
      "content-type": "application/timestamp-query"
    },
    body: new Uint8Array(request)
  });
  if (!response.ok) {
    throw new Error(`TSA request failed with HTTP ${response.status}.`);
  }
  const responseBody = Buffer.from(await response.arrayBuffer());
  timestampTokenFromResponse(responseBody);
  return responseBody;
}

export async function verifyTimestampToken(
  token: Buffer,
  expectedSha256hex: string
): Promise<boolean> {
  const expectedSha256 = sha256Digest(expectedSha256hex).toString("hex");
  const tempDir = await mkdtemp(join(tmpdir(), "findevil-tsa-"));
  const tokenPath = join(tempDir, "timestamp-token.der");
  try {
    await writeFile(tokenPath, token);
    const output = await opensslTs(["ts", "-reply", "-in", tokenPath, "-token_in", "-text"]);
    const actualSha256 = messageDigestFromTimestampText(output);
    return actualSha256 === expectedSha256;
  } catch {
    return false;
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
}

function buildTimestampRequest(digest: Buffer): Buffer {
  return derSequence([
    derInteger(1),
    derSequence([derSequence([derObjectIdentifier(sha256Oid), derNull()]), derOctetString(digest)]),
    derBoolean(true)
  ]);
}

function timestampTokenFromResponse(response: Buffer): Buffer {
  const top = readDerElement(response, 0);
  if (top.tag !== 0x30 || top.end !== response.length) {
    throw new Error("TSA response is not a DER TimeStampResp sequence.");
  }
  const children = readDerChildren(response, top);
  const statusInfo = children[0];
  if (!statusInfo || statusInfo.tag !== 0x30) {
    throw new Error("TSA response is missing PKIStatusInfo.");
  }
  const status = readPkiStatus(response, statusInfo);
  if (!statusGranted.has(status)) {
    throw new Error(`TSA rejected timestamp request with PKIStatus ${status}.`);
  }
  const token = children[1];
  if (!token) {
    throw new Error("TSA granted timestamp request without a TimeStampToken.");
  }
  return response.subarray(token.start, token.end);
}

function readPkiStatus(buffer: Buffer, statusInfo: DerElement): number {
  const statusChildren = readDerChildren(buffer, statusInfo);
  const status = statusChildren[0];
  if (!status || status.tag !== 0x02) {
    throw new Error("TSA response PKIStatusInfo is missing status.");
  }
  return derIntegerValue(buffer.subarray(status.valueStart, status.end));
}

function readDerChildren(buffer: Buffer, parent: DerElement): DerElement[] {
  const children: DerElement[] = [];
  let offset = parent.valueStart;
  while (offset < parent.end) {
    const child = readDerElement(buffer, offset);
    children.push(child);
    offset = child.end;
  }
  if (offset !== parent.end) {
    throw new Error("Malformed DER sequence length.");
  }
  return children;
}

function readDerElement(buffer: Buffer, offset: number): DerElement {
  const tag = buffer[offset];
  if (tag === undefined) {
    throw new Error("Truncated DER element.");
  }
  const lengthInfo = readDerLength(buffer, offset + 1);
  const valueStart = lengthInfo.offset;
  const end = valueStart + lengthInfo.length;
  if (end > buffer.length) {
    throw new Error("DER element length exceeds input.");
  }
  return { tag, start: offset, valueStart, end };
}

function readDerLength(
  buffer: Buffer,
  offset: number
): { readonly length: number; readonly offset: number } {
  const first = buffer[offset];
  if (first === undefined) {
    throw new Error("Truncated DER length.");
  }
  if (first < 0x80) {
    return { length: first, offset: offset + 1 };
  }
  const bytes = first & 0x7f;
  if (bytes === 0) {
    throw new Error("Indefinite DER lengths are unsupported.");
  }
  if (bytes > 4 || offset + bytes >= buffer.length) {
    throw new Error("Invalid DER length.");
  }
  let length = 0;
  for (let index = 0; index < bytes; index += 1) {
    const byte = buffer[offset + 1 + index];
    if (byte === undefined) {
      throw new Error("Truncated DER length.");
    }
    length = (length << 8) | byte;
  }
  return { length, offset: offset + 1 + bytes };
}

function derIntegerValue(bytes: Buffer): number {
  if (bytes.length === 0) {
    throw new Error("Empty DER integer.");
  }
  if ((bytes[0] ?? 0) & 0x80) {
    throw new Error("Negative DER integer is unsupported.");
  }
  let value = 0;
  for (const byte of bytes) {
    value = (value << 8) | byte;
  }
  return value;
}

function derSequence(children: readonly Buffer[]): Buffer {
  return derElement(0x30, Buffer.concat(children));
}

function derInteger(value: number): Buffer {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error("DER integer must be a non-negative safe integer.");
  }
  const bytes: number[] = [];
  let remaining = value;
  do {
    bytes.unshift(remaining & 0xff);
    remaining >>= 8;
  } while (remaining > 0);
  if ((bytes[0] ?? 0) & 0x80) {
    bytes.unshift(0);
  }
  return derElement(0x02, Buffer.from(bytes));
}

function derBoolean(value: boolean): Buffer {
  return derElement(0x01, Buffer.from([value ? 0xff : 0x00]));
}

function derNull(): Buffer {
  return derElement(0x05, Buffer.alloc(0));
}

function derOctetString(value: Buffer): Buffer {
  return derElement(0x04, value);
}

function derObjectIdentifier(oid: readonly number[]): Buffer {
  if (oid.length < 2 || oid[0] === undefined || oid[1] === undefined) {
    throw new Error("OID must contain at least two arcs.");
  }
  const first = oid[0];
  const second = oid[1];
  if (first > 2 || second > 39) {
    throw new Error("Invalid OID root arcs.");
  }
  return derElement(
    0x06,
    Buffer.from([first * 40 + second, ...oid.slice(2).flatMap((arc) => base128(arc))])
  );
}

function derElement(tag: number, value: Buffer): Buffer {
  return Buffer.concat([Buffer.from([tag]), derLength(value.length), value]);
}

function derLength(length: number): Buffer {
  if (length < 0x80) {
    return Buffer.from([length]);
  }
  const bytes: number[] = [];
  let remaining = length;
  while (remaining > 0) {
    bytes.unshift(remaining & 0xff);
    remaining >>= 8;
  }
  return Buffer.from([0x80 | bytes.length, ...bytes]);
}

function base128(value: number): number[] {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error("OID arc must be a non-negative safe integer.");
  }
  const bytes = [value & 0x7f];
  let remaining = value >> 7;
  while (remaining > 0) {
    bytes.unshift((remaining & 0x7f) | 0x80);
    remaining >>= 7;
  }
  return bytes;
}

function sha256Digest(value: string): Buffer {
  const normalized = value.trim().toLowerCase();
  if (!/^[0-9a-f]{64}$/u.test(normalized)) {
    throw new Error("sha256hex must be 64 lowercase or uppercase hex characters.");
  }
  return Buffer.from(normalized, "hex");
}

function messageDigestFromTimestampText(output: string): string | undefined {
  const match = /Message data:\s*([\s\S]*?)(?:\n[A-Z][^\n]*:|\n\S|$)/u.exec(output);
  if (!match?.[1]) {
    return undefined;
  }
  return [...match[1].matchAll(/\b[0-9a-f]{2}\b/giu)].map(([byte]) => byte.toLowerCase()).join("");
}

async function opensslTs(args: readonly string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn("openssl", args, { stdio: ["ignore", "pipe", "pipe"] });
    const stdout: Buffer[] = [];
    const stderr: Buffer[] = [];
    child.stdout.on("data", (chunk: Buffer) => stdout.push(chunk));
    child.stderr.on("data", (chunk: Buffer) => stderr.push(chunk));
    child.on("error", reject);
    child.on("close", (code) => {
      const output = Buffer.concat(stdout).toString("utf8");
      const errorOutput = Buffer.concat(stderr).toString("utf8");
      if (code === 0) {
        resolve(`${output}${errorOutput}`);
        return;
      }
      reject(new Error(errorOutput || `openssl exited with code ${code ?? "unknown"}.`));
    });
  });
}
