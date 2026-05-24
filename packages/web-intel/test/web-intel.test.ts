import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import {
  createWebIntelClient,
  readWebEvidenceBundle,
  toolNameForWebRequest,
  writeWebEvidenceFiles
} from "../src/index.js";

describe("web-intel", () => {
  it("normalizes Exa search evidence with redaction and hashes", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({
        results: [
          {
            title: "MAS AI guidance",
            url: "https://example.test/mas-ai",
            text: "Contact ai@example.test before rollout.",
            score: 0.9
          }
        ]
      })
    );
    const client = createWebIntelClient({
      fetch: fetchImpl as unknown as typeof fetch,
      exaApiKey: "exa-test",
      now: () => new Date("2026-05-24T00:00:00.000Z")
    });

    const bundle = await client.run({
      operation: "web.search",
      provider: "exa",
      query: "Singapore agentic AI governance",
      domains: ["mas.gov.sg"]
    });

    expect(fetchImpl).toHaveBeenCalledWith(
      "https://api.exa.ai/search",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ "x-api-key": "exa-test" })
      })
    );
    expect(bundle).toMatchObject({
      schemaVersion: "1.0.0",
      selectedProvider: "exa",
      escalationLevel: "search",
      summary: { sourceCount: 1, redacted: true, errorCount: 0 }
    });
    expect(bundle.sources[0]).toMatchObject({
      title: "MAS AI guidance",
      url: "https://example.test/mas-ai",
      excerpt: "Contact <redacted-email> before rollout.",
      contentHash: expect.stringMatching(/^sha256:/u)
    });
    expect(bundle.events[0]).toMatchObject({
      toolName: "exa.search",
      resultHash: expect.stringMatching(/^sha256:/u),
      sourceUrls: ["https://example.test/mas-ai"]
    });
  });

  it("normalizes TinyFish fetch evidence and writes portable files", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-web-intel-"));
    const client = createWebIntelClient({
      fetch: vi.fn(async () =>
        jsonResponse({
          title: "Fetched page",
          url: "https://example.test/page",
          markdown: "Page TOKEN=abc123 should be redacted."
        })
      ) as unknown as typeof fetch,
      tinyfishApiKey: "tinyfish-test",
      now: () => new Date("2026-05-24T00:00:00.000Z")
    });

    try {
      const bundle = await client.run({
        operation: "web.fetch",
        provider: "tinyfish",
        url: "https://example.test/page",
        storeFullContent: true
      });
      const files = await writeWebEvidenceFiles(tempDir, bundle);

      expect(files).toEqual([
        "web-evidence.json",
        "web-events.jsonl",
        "web-bom.json",
        "web-evidence.html"
      ]);
      expect(await readWebEvidenceBundle(tempDir)).toMatchObject({
        selectedProvider: "tinyfish",
        summary: { storedFullContent: true, redacted: true }
      });
      await expect(readFile(join(tempDir, "web-evidence.html"), "utf8")).resolves.toContain(
        "KelpClaw Web Evidence"
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("requires provider credentials before live calls", async () => {
    const client = createWebIntelClient({
      fetch: vi.fn() as unknown as typeof fetch,
      exaApiKey: ""
    });

    await expect(
      client.run({ operation: "web.search", provider: "exa", query: "agent governance" })
    ).rejects.toThrow(/EXA_API_KEY/u);
  });

  it("maps provider operations to stable policy tool names", () => {
    expect(
      toolNameForWebRequest({ operation: "web.fetch", provider: "exa", url: "https://x.test" })
    ).toBe("exa.contents");
    expect(
      toolNameForWebRequest({ operation: "web.browser.session", provider: "tinyfish", query: "x" })
    ).toBe("tinyfish.browser.session");
  });
});

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" }
  });
}
