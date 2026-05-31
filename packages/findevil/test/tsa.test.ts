import { readdir } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { requestTimestampToken } from "../src/evidence/tsa.js";

interface UndiciMockModule {
  readonly MockAgent: new () => {
    disableNetConnect(): void;
    close(): Promise<void>;
    get(origin: string): {
      intercept(options: { readonly path: string; readonly method: string }): {
        reply(
          callback: (options: {
            readonly body?: Uint8Array | null;
            readonly headers?: Record<string, string>;
          }) => {
            readonly statusCode: number;
            readonly data: Buffer;
            readonly responseOptions?: { readonly headers: Record<string, string> };
          }
        ): unknown;
      };
    };
  };
  readonly getGlobalDispatcher: () => unknown;
  readonly setGlobalDispatcher: (dispatcher: unknown) => void;
}

const repoRoot = new URL("../../..", import.meta.url);
const digestHex = "a".repeat(64);
const token = derSequence([derObjectIdentifier([1, 2, 840, 113549, 1, 7, 2])]);
const grantedResponse = derSequence([derSequence([derInteger(0)]), token]);

let undici: UndiciMockModule;
let originalDispatcher: unknown;
let mockAgent: InstanceType<UndiciMockModule["MockAgent"]>;

describe("RFC3161 TSA client", () => {
  beforeEach(async () => {
    undici = await importUndiciMock();
    originalDispatcher = undici.getGlobalDispatcher();
    mockAgent = new undici.MockAgent();
    mockAgent.disableNetConnect();
    undici.setGlobalDispatcher(mockAgent);
  });

  afterEach(async () => {
    undici.setGlobalDispatcher(originalDispatcher);
    await mockAgent.close();
  });

  it("posts an RFC3161 TimeStampReq and returns the signed token bytes", async () => {
    let requestBody: Buffer | undefined;
    let requestHeaders: Record<string, string> | undefined;
    mockAgent
      .get("https://tsa.test")
      .intercept({ path: "/tsr", method: "POST" })
      .reply((options) => {
        requestBody = Buffer.from(options.body ?? []);
        requestHeaders = options.headers;
        return {
          statusCode: 200,
          data: grantedResponse,
          responseOptions: { headers: { "content-type": "application/timestamp-reply" } }
        };
      });

    await expect(requestTimestampToken(digestHex, "https://tsa.test/tsr")).resolves.toEqual(token);
    expect(requestHeaders?.["content-type"]).toContain("application/timestamp-query");
    expect(requestBody).toEqual(expectedTimestampRequest(Buffer.from(digestHex, "hex")));
  });

  it("rejects a granted TimeStampResp without a token", async () => {
    mockAgent
      .get("https://tsa.test")
      .intercept({ path: "/tsr", method: "POST" })
      .reply(() => ({
        statusCode: 200,
        data: derSequence([derSequence([derInteger(0)])])
      }));

    await expect(requestTimestampToken(digestHex, "https://tsa.test/tsr")).rejects.toThrow(
      /without a TimeStampToken/u
    );
  });
});

async function importUndiciMock(): Promise<UndiciMockModule> {
  const pnpmDir = join(fileURLToPath(new URL("node_modules/.pnpm", repoRoot)));
  const entries = await readdir(pnpmDir);
  const undiciDir = entries
    .filter((entry) => /^undici@\d/u.test(entry))
    .sort()
    .at(-1);
  if (!undiciDir) {
    throw new Error("undici package is required for TSA MockAgent tests.");
  }
  return import(
    pathToFileURL(join(pnpmDir, undiciDir, "node_modules/undici/index.js")).href
  ) as Promise<UndiciMockModule>;
}

function expectedTimestampRequest(digest: Buffer): Buffer {
  return derSequence([
    derInteger(1),
    derSequence([
      derSequence([derObjectIdentifier([2, 16, 840, 1, 101, 3, 4, 2, 1]), derNull()]),
      derOctetString(digest)
    ]),
    derBoolean(true)
  ]);
}

function derSequence(children: readonly Buffer[]): Buffer {
  return derElement(0x30, Buffer.concat(children));
}

function derInteger(value: number): Buffer {
  return derElement(0x02, Buffer.from([value]));
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
  const [first, second, ...rest] = oid;
  if (first === undefined || second === undefined) {
    throw new Error("OID requires at least two arcs.");
  }
  return derElement(0x06, Buffer.from([first * 40 + second, ...rest.flatMap(base128)]));
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
  const bytes = [value & 0x7f];
  let remaining = value >> 7;
  while (remaining > 0) {
    bytes.unshift((remaining & 0x7f) | 0x80);
    remaining >>= 7;
  }
  return bytes;
}
