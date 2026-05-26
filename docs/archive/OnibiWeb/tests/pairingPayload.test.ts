import { describe, expect, it } from "vitest";
import { parsePairingPayload } from "../src/lib/pairingPayload";

describe("parsePairingPayload", () => {
  it("parses the JSON payload shape", () => {
    const raw = JSON.stringify({
      type: "onibi_pairing",
      baseURL: "https://onibi.trycloudflare.com",
      token: "TOKEN123"
    });
    expect(parsePairingPayload(raw)).toEqual({
      baseURL: "https://onibi.trycloudflare.com",
      token: "TOKEN123"
    });
  });

  it("strips whitespace from JSON payload token", () => {
    const raw = JSON.stringify({
      baseURL: "  http://127.0.0.1:8787  ",
      token: "  AB\nCD  "
    });
    const result = parsePairingPayload(raw);
    expect(result?.baseURL).toBe("http://127.0.0.1:8787");
    expect(result?.token).toBe("ABCD");
  });

  it("parses onibi:// deep links", () => {
    const url = "onibi://pair?base=" + encodeURIComponent("http://192.168.1.20:8787") + "&token=abc";
    expect(parsePairingPayload(url)).toEqual({
      baseURL: "http://192.168.1.20:8787",
      token: "abc"
    });
  });

  it("rejects arbitrary strings", () => {
    expect(parsePairingPayload("not a payload")).toBeNull();
  });

  it("rejects empty input", () => {
    expect(parsePairingPayload("")).toBeNull();
    expect(parsePairingPayload("   ")).toBeNull();
  });
});
