import { beforeEach, describe, expect, it } from "vitest";
import { forgetHost, loadRecentHosts, rememberHost } from "../src/store/recentHosts";

beforeEach(() => {
  window.localStorage.clear();
});

describe("recent hosts store", () => {
  it("remembers hosts in most-recent-first order", () => {
    rememberHost("http://127.0.0.1:8787", 1);
    rememberHost("http://192.168.1.20:8787", 2);
    rememberHost("https://onibi.trycloudflare.com", 3);
    const result = loadRecentHosts();
    expect(result.map((entry) => entry.baseURL)).toEqual([
      "https://onibi.trycloudflare.com",
      "http://192.168.1.20:8787",
      "http://127.0.0.1:8787"
    ]);
  });

  it("de-duplicates when the same host is remembered again", () => {
    rememberHost("http://127.0.0.1:8787", 1);
    rememberHost("http://192.168.1.20:8787", 2);
    rememberHost("http://127.0.0.1:8787", 3);
    const result = loadRecentHosts();
    expect(result).toHaveLength(2);
    expect(result[0].baseURL).toBe("http://127.0.0.1:8787");
    expect(result[0].lastUsedAt).toBe(3);
  });

  it("caps the list at 5 entries", () => {
    for (let i = 0; i < 8; i++) {
      rememberHost(`http://host-${i}:8787`, i);
    }
    expect(loadRecentHosts()).toHaveLength(5);
  });

  it("forgetHost removes only the requested entry", () => {
    rememberHost("http://127.0.0.1:8787", 1);
    rememberHost("http://192.168.1.20:8787", 2);
    const after = forgetHost("http://127.0.0.1:8787");
    expect(after.map((entry) => entry.baseURL)).toEqual(["http://192.168.1.20:8787"]);
  });
});
