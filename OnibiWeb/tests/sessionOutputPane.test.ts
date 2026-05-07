import { describe, expect, it, vi } from "vitest";
import {
  createTerminalRenderScheduler,
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

class MockCanvasContext {
  fillStyle = "";
  font = "";
  textBaseline = "";
  fillRects: Array<{ x: number; y: number; width: number; height: number }> = [];
  fillTexts: Array<{ text: string; x: number; y: number }> = [];

  setTransform = vi.fn();

  fillRect(x: number, y: number, width: number, height: number): void {
    this.fillRects.push({ x, y, width, height });
  }

  fillText(text: string, x: number, y: number): void {
    this.fillTexts.push({ text, x, y });
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

    expect(context.fillTexts[0]).toEqual({ text: "abc", x: 0, y: 3 });
    expect(context.fillRects).toContainEqual({ x: 20, y: 3, width: 2, height: 12 });
  });
});
