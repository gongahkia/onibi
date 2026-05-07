import { readFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createGhosttyTerminalEngine } from "../src/lib/ghosttyTerminal";

const ghosttyWasmBytes = readFileSync(join(process.cwd(), "public", "ghostty-vt.wasm"));

describe("createGhosttyTerminalEngine", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("promotes to the libghostty-vt WASM backend and replays pending bytes", async () => {
    globalThis.fetch = vi.fn(async () => {
      return new Response(ghosttyWasmBytes, {
        status: 200,
        headers: { "content-type": "application/wasm" }
      });
    }) as typeof fetch;

    let resolveReady: () => void = () => undefined;
    const ready = new Promise<void>((resolve) => {
      resolveReady = resolve;
    });
    const engine = createGhosttyTerminalEngine(20, 5, {
      onBackendReady: resolveReady
    });
    engine.ingest(new TextEncoder().encode("hello\r\n\u001b[32mgreen\u001b[0m"));

    await ready;

    try {
      const snapshot = engine.snapshot();
      expect(snapshot.rows.join("\n")).toContain("hello");
      expect(snapshot.rows.join("\n")).toContain("green");
      expect(snapshot.cursor).toMatchObject({ row: 1, col: 5, visible: true });
      expect(globalThis.fetch).toHaveBeenCalledWith("/ghostty-vt.wasm");
    } finally {
      engine.dispose?.();
    }
  }, 10_000);
});
