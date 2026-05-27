import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createGhosttyTerminalEngine,
  type GhosttyTerminalEngine
} from "../src/lib/ghosttyTerminal";

const ghosttyWasmBytes = readFileSync(join(process.cwd(), "public", "ghostty-vt.wasm"));
const encoder = new TextEncoder();

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

  it("returns only the changed row after an initial full render", async () => {
    const engine = await createReadyWasmEngine(12, 4);

    try {
      engine.ingest(encoder.encode("alpha\r\nbeta"));
      expect(engine.snapshot().rows.join("\n")).toContain("beta");
      expect(engine.takeDirtyRows()).toEqual([0, 1, 2, 3]);

      engine.ingest(encoder.encode("\u001b[2;1HZ"));
      const snapshot = engine.snapshot();

      expect(snapshot.rows[1].startsWith("Zeta")).toBe(true);
      expect(engine.takeDirtyRows()).toEqual([1]);
    } finally {
      engine.dispose?.();
    }
  }, 10_000);

  it("tracks cursor movement and row overwrite dirtiness", async () => {
    const engine = await createReadyWasmEngine(12, 4);

    try {
      engine.ingest(encoder.encode("row0\r\nrow1\r\nrow2"));
      engine.snapshot();
      engine.takeDirtyRows();

      engine.ingest(encoder.encode("\u001b[1;1HAA"));
      const snapshot = engine.snapshot();

      expect(snapshot.rows[0].startsWith("AAw0")).toBe(true);
      expect(snapshot.cursor).toMatchObject({ row: 0, col: 2, visible: true });
      expect(engine.takeDirtyRows()).toEqual([0, 2]);
    } finally {
      engine.dispose?.();
    }
  }, 10_000);

  it("marks every row dirty after resize and keeps snapshot shape usable", async () => {
    const engine = await createReadyWasmEngine(8, 2);

    try {
      engine.ingest(encoder.encode("hello"));
      engine.snapshot();
      engine.takeDirtyRows();

      engine.resize(10, 3);
      const snapshot = engine.snapshot();

      expect(snapshot.rows).toHaveLength(3);
      expect(snapshot.rows.every((row) => row.length >= 10)).toBe(true);
      expect(engine.takeDirtyRows()).toEqual([0, 1, 2]);
    } finally {
      engine.dispose?.();
    }
  }, 10_000);

  it("exposes styled render-state cells for ANSI colors and text attributes", async () => {
    const engine = await createReadyWasmEngine(12, 3);

    try {
      engine.ingest(encoder.encode("\u001b[31;1;3mA\u001b[0m\u001b[7mB\u001b[0m"));
      const snapshot = engine.snapshot();
      const firstCell = snapshot.styledRows?.[0]?.[0];
      const secondCell = snapshot.styledRows?.[0]?.[1];

      expect(snapshot.rows[0].startsWith("AB")).toBe(true);
      expect(firstCell).toEqual(
        expect.objectContaining({
          text: "A",
          foreground: expect.stringMatching(/^rgb\(/),
          bold: true,
          italic: true
        })
      );
      expect(secondCell).toEqual(
        expect.objectContaining({
          text: "B",
          inverse: true
        })
      );
      expect(snapshot.colors).toEqual(
        expect.objectContaining({
          background: expect.stringMatching(/^rgb\(/),
          foreground: expect.stringMatching(/^rgb\(/)
        })
      );
    } finally {
      engine.dispose?.();
    }
  }, 10_000);

  it("clears cached rows and marks every row dirty on reset", async () => {
    const engine = await createReadyWasmEngine(8, 3);

    try {
      engine.ingest(encoder.encode("hello\r\nworld"));
      engine.snapshot();
      engine.takeDirtyRows();

      engine.reset();
      const snapshot = engine.snapshot();

      expect(snapshot.rows).toHaveLength(3);
      expect(snapshot.rows.every((row) => row.trim() === "")).toBe(true);
      expect(engine.takeDirtyRows()).toEqual([0, 1, 2]);
    } finally {
      engine.dispose?.();
    }
  }, 10_000);

  it("does not import xterm from the web source tree", () => {
    const sourceRoot = join(process.cwd(), "src");
    const sourceText = readSourceFiles(sourceRoot).join("\n");

    expect(sourceText).not.toMatch(/@xterm|xterm/);
  });
});

async function createReadyWasmEngine(cols: number, rows: number): Promise<GhosttyTerminalEngine> {
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
  const engine = createGhosttyTerminalEngine(cols, rows, {
    onBackendReady: resolveReady
  });

  await ready;
  return engine;
}

function readSourceFiles(directory: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      files.push(...readSourceFiles(path));
    } else if (/\.(css|tsx?|jsx?)$/.test(entry)) {
      files.push(readFileSync(path, "utf8"));
    }
  }
  return files;
}
