import { describe, expect, it, vi } from "vitest";
import {
  createTerminalRenderScheduler,
  renderTerminal,
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
