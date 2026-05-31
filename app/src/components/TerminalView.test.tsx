import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { TerminalView } from "./TerminalView";
import { DEFAULT_SETTINGS } from "../lib/sessions";

const terminalConstructor = vi.hoisted(() => vi.fn());

vi.mock("@xterm/xterm", () => ({
  Terminal: terminalConstructor,
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-search", () => ({
  SearchAddon: class {
    findNext = vi.fn();
    findPrevious = vi.fn();
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-serialize", () => ({
  SerializeAddon: class {
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-unicode11", () => ({
  Unicode11Addon: class {
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-web-links", () => ({
  WebLinksAddon: class {
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = vi.fn();
    onContextLoss = vi.fn();
  },
}));

function createTerminalMock() {
  return {
    rows: 24,
    cols: 80,
    unicode: { activeVersion: "6" },
    parser: { registerOscHandler: vi.fn() },
    loadAddon: vi.fn(),
    open: vi.fn(),
    attachCustomKeyEventHandler: vi.fn(),
    onData: vi.fn(() => ({ dispose: vi.fn() })),
    write: vi.fn(),
    refresh: vi.fn(),
    focus: vi.fn(),
    clear: vi.fn(),
    selectAll: vi.fn(),
    getSelection: vi.fn(() => ""),
    clearTextureAtlas: vi.fn(),
    dispose: vi.fn(),
    options: {},
  };
}

describe("TerminalView", () => {
  beforeEach(() => {
    terminalConstructor.mockReset();
    terminalConstructor.mockImplementation(createTerminalMock);
    globalThis.__TAURI_MOCKS__.invoke.mockReset();
    globalThis.__TAURI_MOCKS__.listen.mockReset();
    globalThis.__TAURI_MOCKS__.listen.mockResolvedValue(
      globalThis.__TAURI_MOCKS__.unlisten,
    );
  });

  test("enables xterm proposed APIs for the unicode addon", async () => {
    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    expect(terminalConstructor.mock.calls[0][0]).toMatchObject({
      allowProposedApi: true,
    });
  });

  test("replays buffered pty output after the event listener attaches", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          return { data: "aGVsbG8=", startOffset: 0, endOffset: 5 };
        }
        return null;
      },
    );

    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "pty_replay",
        { id: "pty-1" },
      );
    });
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    const bytes = term.write.mock.calls[0][0] as Uint8Array;
    expect(new TextDecoder().decode(bytes)).toBe("hello");
  });

  test("merges replay and queued live output without duplication", async () => {
    let emitPty: ((payload: unknown) => void) | undefined;
    let resolveReplay:
      | ((snapshot: { data: string; startOffset: number; endOffset: number }) => void)
      | undefined;
    const replay = new Promise<{
      data: string;
      startOffset: number;
      endOffset: number;
    }>((resolve) => {
      resolveReplay = resolve;
    });
    globalThis.__TAURI_MOCKS__.listen.mockImplementation(
      async (_eventName: string, callback: (event: { payload: unknown }) => void) => {
        emitPty = (payload) => callback({ payload });
        return globalThis.__TAURI_MOCKS__.unlisten;
      },
    );
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          return replay;
        }
        return null;
      },
    );

    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(emitPty).toBeDefined());
    emitPty?.({ type: "data", data: "bGxvIHdvcmxk", offset: 2 });
    resolveReplay?.({ data: "aGU=", startOffset: 0, endOffset: 2 });

    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(term.write).toHaveBeenCalledTimes(2));
    const output = term.write.mock.calls
      .map(([bytes]) => new TextDecoder().decode(bytes as Uint8Array))
      .join("");
    expect(output).toBe("hello world");
  });

  test("flushes live pty output when replay stalls", async () => {
    let emitPty: ((payload: unknown) => void) | undefined;
    const stalledReplay = new Promise<{
      data: string;
      startOffset: number;
      endOffset: number;
    }>(() => undefined);
    globalThis.__TAURI_MOCKS__.listen.mockImplementation(
      async (_eventName: string, callback: (event: { payload: unknown }) => void) => {
        emitPty = (payload) => callback({ payload });
        return globalThis.__TAURI_MOCKS__.unlisten;
      },
    );
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          return stalledReplay;
        }
        return null;
      },
    );

    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(emitPty).toBeDefined());
    emitPty?.({ type: "data", data: "aGk=", offset: 0 });

    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    const bytes = term.write.mock.calls[0][0] as Uint8Array;
    expect(new TextDecoder().decode(bytes)).toBe("hi");
  });

  test("reports unavailable ptys when replay attach fails", async () => {
    const onUnavailable = vi.fn();
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          throw new Error("pty session missing was not found");
        }
        return null;
      },
    );

    render(
      <TerminalView
        ptyId="missing"
        settings={DEFAULT_SETTINGS}
        visible={false}
        onUnavailable={onUnavailable}
      />,
    );

    await waitFor(() => expect(onUnavailable).toHaveBeenCalled());
    expect(String(onUnavailable.mock.calls[0][0])).toContain("not found");
  });
});
