import { describe, expect, it, vi } from "vitest";
import {
  createTerminalRenderScheduler,
  createTerminalBacklogProcessor,
  cellFromPoint,
  calculateTerminalSize,
  copyTextForSelection,
  FALLBACK_TERMINAL_METRICS,
  keyEventToTerminalInput,
  measureTerminalMetrics,
  normalizedSelection,
  renderTerminal,
  routeTerminalPaste,
  type TerminalSelection,
  type TerminalRenderState
} from "../src/components/SessionOutputPane";
import type { GhosttySnapshot, GhosttyTerminalEngine } from "../src/lib/ghosttyTerminal";
import type { OutputEntry } from "../src/store/sessionStore";

class MockCanvasContext {
  fillStyle = "";
  font = "";
  textBaseline = "";
  fillRects: Array<{ x: number; y: number; width: number; height: number; fillStyle: string }> = [];
  fillTexts: Array<{ text: string; x: number; y: number; fillStyle: string; font: string }> = [];

  setTransform = vi.fn();

  fillRect(x: number, y: number, width: number, height: number): void {
    this.fillRects.push({ x, y, width, height, fillStyle: this.fillStyle });
  }

  fillText(text: string, x: number, y: number): void {
    this.fillTexts.push({ text, x, y, fillStyle: this.fillStyle, font: this.font });
  }
}

function createMockCanvas(context: MockCanvasContext): HTMLCanvasElement {
  return {
    clientWidth: 120,
    clientHeight: 80,
    getContext: (type: string) => (type === "2d" ? context : null)
  } as unknown as HTMLCanvasElement;
}

function createMockEngine(
  snapshots: GhosttySnapshot[],
  dirtyRows: number[][]
): GhosttyTerminalEngine {
  return {
    resize: vi.fn(),
    ingest: vi.fn(),
    snapshot: vi.fn(() => snapshots.shift() ?? snapshots[snapshots.length - 1]),
    takeDirtyRows: vi.fn(() => dirtyRows.shift() ?? []),
    reset: vi.fn()
  };
}

describe("renderTerminal", () => {
  it("redraws the previous and current cursor rows for cursor-only movement", () => {
    const context = new MockCanvasContext();
    const renderState: TerminalRenderState = {};
    const engine = createMockEngine(
      [
        {
          rows: ["one", "two", "three"],
          cursor: { row: 0, col: 1, visible: true },
          bell: false
        },
        {
          rows: ["one", "two", "three"],
          cursor: { row: 2, col: 3, visible: true },
          bell: false
        }
      ],
      [[0, 1, 2], [], []]
    );

    renderTerminal(createMockCanvas(context), engine, renderState, true);
    context.fillRects = [];
    context.fillTexts = [];

    renderTerminal(createMockCanvas(context), engine, renderState);

    expect(context.fillTexts.map((call) => call.text)).toEqual(["one", "three"]);
    expect(context.fillRects).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ y: 0, height: 20 }),
        expect.objectContaining({ y: 40, height: 20 }),
        expect.objectContaining({ x: 25.200000000000003, y: 42, width: 2, height: 16 })
      ])
    );
  });

  it("redraws dirty text rows together with old and new cursor rows", () => {
    const context = new MockCanvasContext();
    const renderState: TerminalRenderState = {
      lastCursor: { row: 1, col: 0, visible: true }
    };
    const engine = createMockEngine(
      [
        {
          rows: ["alpha", "beta", "gamma", "delta"],
          cursor: { row: 3, col: 2, visible: true },
          bell: false
        }
      ],
      [[0]]
    );

    renderTerminal(createMockCanvas(context), engine, renderState);

    expect(context.fillTexts.map((call) => call.text)).toEqual(["alpha", "beta", "delta"]);
    expect(renderState.lastCursor).toEqual({ row: 3, col: 2, visible: true });
  });
});

describe("createTerminalRenderScheduler", () => {
  it("coalesces repeated triggers into one animation-frame render", () => {
    const context = new MockCanvasContext();
    const canvas = createMockCanvas(context);
    const engine = createMockEngine(
      [
        {
          rows: ["alpha"],
          cursor: { row: 0, col: 0, visible: true },
          bell: false
        }
      ],
      [[0]]
    );
    const callbacks: FrameRequestCallback[] = [];
    const scheduler = createTerminalRenderScheduler(
      () => canvas,
      () => engine,
      () => FALLBACK_TERMINAL_METRICS,
      {},
      (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      vi.fn()
    );

    scheduler.schedule();
    scheduler.schedule();
    scheduler.schedule();

    expect(callbacks).toHaveLength(1);
    callbacks[0](0);
    expect(engine.snapshot).toHaveBeenCalledTimes(1);
  });

  it("lets a full render win over a pending partial render", () => {
    const context = new MockCanvasContext();
    const canvas = createMockCanvas(context);
    const engine = createMockEngine(
      [
        {
          rows: ["alpha", "beta"],
          cursor: { row: 1, col: 0, visible: true },
          bell: false
        }
      ],
      [[1], []]
    );
    const callbacks: FrameRequestCallback[] = [];
    const scheduler = createTerminalRenderScheduler(
      () => canvas,
      () => engine,
      () => FALLBACK_TERMINAL_METRICS,
      {},
      (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      vi.fn()
    );

    scheduler.schedule();
    scheduler.schedule(true);

    callbacks[0](0);
    expect(context.fillTexts.map((call) => call.text)).toEqual(["alpha", "beta"]);
  });

  it("cancels pending frames and skips renders after disposal", () => {
    const context = new MockCanvasContext();
    const canvas = createMockCanvas(context);
    const engine = createMockEngine(
      [
        {
          rows: ["alpha"],
          cursor: { row: 0, col: 0, visible: true },
          bell: false
        }
      ],
      [[0]]
    );
    const callbacks: FrameRequestCallback[] = [];
    const cancelFrame = vi.fn();
    const scheduler = createTerminalRenderScheduler(
      () => canvas,
      () => engine,
      () => FALLBACK_TERMINAL_METRICS,
      {},
      (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      cancelFrame
    );

    scheduler.schedule();
    scheduler.dispose();
    callbacks[0](0);
    scheduler.schedule(true);

    expect(cancelFrame).toHaveBeenCalledWith(1);
    expect(engine.snapshot).not.toHaveBeenCalled();
    expect(callbacks).toHaveLength(1);
  });
});

describe("routeTerminalPaste", () => {
  it("routes multiline clipboard text through terminal paste", () => {
    const input = vi.fn();
    const paste = vi.fn();

    expect(routeTerminalPaste("one\ntwo", input, paste)).toBe(true);

    expect(input).not.toHaveBeenCalled();
    expect(paste).toHaveBeenCalledWith("one\ntwo");
  });

  it("lets single-character paste continue through terminal text input", () => {
    const input = vi.fn();
    const paste = vi.fn();

    expect(routeTerminalPaste("x", input, paste)).toBe(true);

    expect(input).toHaveBeenCalledWith("x");
    expect(paste).not.toHaveBeenCalled();
  });

  it("ignores empty clipboard text", () => {
    const input = vi.fn();
    const paste = vi.fn();

    expect(routeTerminalPaste("", input, paste)).toBe(false);

    expect(input).not.toHaveBeenCalled();
    expect(paste).not.toHaveBeenCalled();
  });
});

describe("terminal selection helpers", () => {
  it("maps pointer coordinates to clamped terminal cells", () => {
    const bounds = { left: 10, top: 20, width: 84, height: 60 };

    expect(cellFromPoint(19, 41, bounds, 3, 10)).toEqual({ row: 1, col: 1 });
    expect(cellFromPoint(-100, -100, bounds, 3, 10)).toEqual({ row: 0, col: 0 });
    expect(cellFromPoint(500, 500, bounds, 3, 10)).toEqual({ row: 2, col: 9 });
  });

  it("normalizes reversed selections", () => {
    const selection: TerminalSelection = {
      anchor: { row: 2, col: 4 },
      head: { row: 0, col: 1 }
    };

    expect(normalizedSelection(selection)).toEqual({
      start: { row: 0, col: 1 },
      end: { row: 2, col: 4 }
    });
  });

  it("copies selected text while trimming row padding only at selected row ends", () => {
    const selection: TerminalSelection = {
      anchor: { row: 0, col: 1 },
      head: { row: 2, col: 3 }
    };

    expect(copyTextForSelection([" ab c   ", "  keep  mid  ", "tail   "], selection)).toBe(
      "ab c\n  keep  mid\ntail"
    );
  });

  it("keeps Ctrl+C as terminal input when there is no selection", () => {
    const event = new KeyboardEvent("keydown", {
      key: "c",
      ctrlKey: true
    });

    expect(keyEventToTerminalInput(event)).toBe("\u0003");
  });

  it("draws selection highlights on selected rows", () => {
    const context = new MockCanvasContext();
    const renderState: TerminalRenderState = {
      selection: {
        anchor: { row: 0, col: 1 },
        head: { row: 1, col: 2 }
      },
      extraDirtyRows: new Set([0, 1])
    };
    const engine = createMockEngine(
      [
        {
          rows: ["abcd", "efgh"],
          cursor: { row: 0, col: 0, visible: false },
          bell: false
        }
      ],
      [[]]
    );

    renderTerminal(createMockCanvas(context), engine, renderState);

    expect(context.fillRects).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ x: 8.4, y: 0, width: 25.200000000000003, height: 20 }),
        expect.objectContaining({ x: 0, y: 20, width: 25.200000000000003, height: 20 })
      ])
    );
  });
});

describe("terminal metrics", () => {
  it("calculates terminal size from injected metrics", () => {
    expect(
      calculateTerminalSize(101, 91, {
        cellWidth: 10,
        lineHeight: 18,
        fontSize: 14,
        textTopOffset: 2
      })
    ).toEqual({ cols: 10, rows: 5 });
  });

  it("measures monospace cell width and line height with fallbacks", () => {
    const metrics = measureTerminalMetrics({
      measureText: (sample: string) => ({
        width: sample.length * 9,
        actualBoundingBoxAscent: 12,
        actualBoundingBoxDescent: 4
      })
    } as unknown as CanvasRenderingContext2D);

    expect(metrics.cellWidth).toBe(9);
    expect(metrics.lineHeight).toBe(22);
  });

  it("uses injected metrics for cursor and text placement", () => {
    const context = new MockCanvasContext();
    const renderState: TerminalRenderState = {};
    const engine = createMockEngine(
      [
        {
          rows: ["abc"],
          cursor: { row: 0, col: 2, visible: true },
          bell: false
        }
      ],
      [[0]]
    );

    renderTerminal(createMockCanvas(context), engine, renderState, false, {
      cellWidth: 10,
      lineHeight: 18,
      fontSize: 14,
      textTopOffset: 3
    });

    expect(context.fillTexts[0]).toEqual(expect.objectContaining({ text: "abc", x: 0, y: 3 }));
    expect(context.fillRects).toContainEqual(
      expect.objectContaining({ x: 20, y: 3, width: 2, height: 12 })
    );
  });
});

describe("styled terminal rendering", () => {
  it("draws styled cell foreground, background, and bold italic font", () => {
    const context = new MockCanvasContext();
    const engine = createMockEngine(
      [
        {
          rows: ["A"],
          styledRows: [
            [
              {
                text: "A",
                foreground: "rgb(204, 102, 102)",
                background: "rgb(29, 31, 33)",
                bold: true,
                italic: true
              }
            ]
          ],
          colors: {
            background: "rgb(0, 0, 0)",
            foreground: "rgb(255, 255, 255)",
            cursor: "rgb(255, 255, 255)",
            palette: []
          },
          cursor: { row: 0, col: 0, visible: false },
          bell: false
        }
      ],
      [[0]]
    );

    renderTerminal(createMockCanvas(context), engine, {});

    expect(context.fillRects).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ x: 0, y: 0, width: 8.4, height: 20, fillStyle: "rgb(29, 31, 33)" })
      ])
    );
    expect(context.fillTexts).toEqual([
      expect.objectContaining({
        text: "A",
        fillStyle: "rgb(204, 102, 102)"
      })
    ]);
    expect(context.fillTexts[0].font).toContain("italic 700");
  });

  it("applies inverse colors and keeps selection overlay over styled backgrounds", () => {
    const context = new MockCanvasContext();
    const engine = createMockEngine(
      [
        {
          rows: ["AB"],
          styledRows: [
            [
              {
                text: "A",
                foreground: "rgb(1, 2, 3)",
                background: "rgb(4, 5, 6)",
                inverse: true
              },
              { text: "B" }
            ]
          ],
          colors: {
            background: "rgb(0, 0, 0)",
            foreground: "rgb(255, 255, 255)",
            palette: []
          },
          cursor: { row: 0, col: 0, visible: false },
          bell: false
        }
      ],
      [[0]]
    );

    renderTerminal(
      createMockCanvas(context),
      engine,
      {
        selection: {
          anchor: { row: 0, col: 0 },
          head: { row: 0, col: 1 }
        }
      },
      false
    );

    expect(context.fillRects).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ x: 0, y: 0, width: 8.4, height: 20, fillStyle: "rgb(1, 2, 3)" }),
        expect.objectContaining({ x: 0, y: 0, width: 16.8, height: 20, fillStyle: "rgba(56, 139, 253, 0.35)" })
      ])
    );
    expect(context.fillTexts[0]).toEqual(
      expect.objectContaining({ text: "A", fillStyle: "rgb(4, 5, 6)" })
    );
  });

  it("keeps plain row drawing when styled data is unavailable", () => {
    const context = new MockCanvasContext();
    const engine = createMockEngine(
      [
        {
          rows: ["plain"],
          cursor: { row: 0, col: 0, visible: false },
          bell: false
        }
      ],
      [[0]]
    );

    renderTerminal(createMockCanvas(context), engine, {});

    expect(context.fillTexts).toEqual([
      expect.objectContaining({ text: "plain", x: 0, y: 2, fillStyle: "#f3f4f6" })
    ]);
  });
});

describe("createTerminalBacklogProcessor", () => {
  it("ingests catch-up entries in bounded animation-frame batches", () => {
    const entries = [entry("1", "ab"), entry("2", "cd"), entry("3", "ef")];
    const ingested: string[] = [];
    const advanced: string[] = [];
    const callbacks: FrameRequestCallback[] = [];
    const scheduleRender = vi.fn();
    const processor = createTerminalBacklogProcessor({
      entries,
      startIndex: 0,
      ingestEntry: (item) => ingested.push(item.text),
      onEntryIngested: (item) => advanced.push(item.id),
      scheduleRender,
      maxBatchEntries: 2,
      maxBatchBytes: 100,
      requestFrame: (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      cancelFrame: vi.fn()
    });

    processor.start();
    expect(callbacks).toHaveLength(1);

    callbacks[0](0);
    expect(ingested).toEqual(["ab", "cd"]);
    expect(advanced).toEqual(["1", "2"]);
    expect(scheduleRender).toHaveBeenCalledTimes(1);
    expect(callbacks).toHaveLength(2);

    callbacks[1](0);
    expect(ingested).toEqual(["ab", "cd", "ef"]);
    expect(advanced).toEqual(["1", "2", "3"]);
    expect(scheduleRender).toHaveBeenCalledTimes(2);
  });

  it("cancels stale backlog work before the next batch runs", () => {
    const entries = [entry("1", "ab"), entry("2", "cd")];
    const ingested: string[] = [];
    const callbacks: FrameRequestCallback[] = [];
    const cancelFrame = vi.fn();
    const processor = createTerminalBacklogProcessor({
      entries,
      startIndex: 0,
      ingestEntry: (item) => ingested.push(item.id),
      onEntryIngested: vi.fn(),
      scheduleRender: vi.fn(),
      maxBatchEntries: 1,
      requestFrame: (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      cancelFrame
    });

    processor.start();
    processor.cancel();
    callbacks[0](0);

    expect(cancelFrame).toHaveBeenCalledWith(1);
    expect(ingested).toEqual([]);
  });

  it("renders between backlog batches and completes after the last ingested chunk", () => {
    const entries = [entry("1", "a"), entry("2", "b")];
    const callbacks: FrameRequestCallback[] = [];
    const onComplete = vi.fn();
    const scheduleRender = vi.fn();
    const advanced: string[] = [];
    const processor = createTerminalBacklogProcessor({
      entries,
      startIndex: 0,
      ingestEntry: vi.fn(),
      onEntryIngested: (item) => advanced.push(item.id),
      scheduleRender,
      onComplete,
      maxBatchEntries: 1,
      requestFrame: (callback) => {
        callbacks.push(callback);
        return callbacks.length;
      },
      cancelFrame: vi.fn()
    });

    processor.start();
    callbacks[0](0);
    callbacks[1](0);

    expect(advanced).toEqual(["1", "2"]);
    expect(scheduleRender).toHaveBeenCalledTimes(2);
    expect(onComplete).toHaveBeenCalledTimes(1);
  });
});

function entry(id: string, text: string): OutputEntry {
  return {
    id,
    text,
    bytes: new TextEncoder().encode(text),
    sessionId: "s1",
    stream: "stdout",
    timestamp: new Date(0).toISOString()
  };
}
