import { describe, expect, it } from "vitest";
import { normalizeConnectionConfig } from "../src/api/httpClient";

describe("normalizeConnectionConfig", () => {
  it("strips all whitespace from the token", () => {
    const result = normalizeConnectionConfig({
      baseURL: "http://127.0.0.1:8787",
      token: "  abc\n def\tghi  "
    });
    expect(result.token).toBe("abcdefghi");
  });

  it("trims and strips trailing slashes from the base URL", () => {
    const result = normalizeConnectionConfig({
      baseURL: "  http://127.0.0.1:8787///  ",
      token: "x"
    });
    expect(result.baseURL).toBe("http://127.0.0.1:8787");
  });

  it("passes through an already-clean config", () => {
    const result = normalizeConnectionConfig({
      baseURL: "https://onibi.trycloudflare.com",
      token: "TOKEN123"
    });
    expect(result).toEqual({
      baseURL: "https://onibi.trycloudflare.com",
      token: "TOKEN123"
    });
  });
});
