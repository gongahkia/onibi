import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { TerminalView } from "./TerminalView";
import { DEFAULT_SETTINGS } from "../lib/sessions";
import { clearTerminalRenderProfiles } from "../lib/terminal-render-profile";
import { notificationEvents } from "../lib/notifications";

const terminalConstructor = vi.hoisted(() => vi.fn());
const webglConstructor = vi.hoisted(() => vi.fn());
const imageConstructor = vi.hoisted(() => vi.fn());

vi.mock("@xterm/xterm", () => ({
  Terminal: terminalConstructor,
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-image", () => ({
  ImageAddon: imageConstructor,
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
    serialize = vi.fn(() => "\u001b[31mserialized\u001b[0m");
    serializeAsHTML = vi.fn(() => "<span style=\"color:red\">serialized</span>");
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
  WebglAddon: webglConstructor,
}));

function createTerminalMock() {
  const lines = ["alpha beta", "gamma delta", "", "omega"];
  const activeBuffer = {
    type: "normal",
    cursorY: 1,
    cursorX: 0,
    viewportY: 0,
    baseY: 0,
    length: lines.length,
    getLine: vi.fn((row: number) => {
      const text = lines[row];
      if (text === undefined) {
        return undefined;
      }
      return {
        isWrapped: false,
        length: text.length,
        getCell: vi.fn(),
        translateToString: vi.fn(
          (trimRight = false, startColumn = 0, endColumn = text.length) => {
            const value = text.slice(startColumn, endColumn);
            return trimRight ? value.replace(/\s+$/g, "") : value;
          },
        ),
      };
    }),
    getNullCell: vi.fn(),
  };
  return {
    rows: 24,
    cols: 80,
    buffer: {
      active: activeBuffer,
      normal: activeBuffer,
      alternate: activeBuffer,
      onBufferChange: vi.fn(),
    },
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
    select: vi.fn(),
    selectAll: vi.fn(),
    getSelection: vi.fn(() => ""),
    clearSelection: vi.fn(),
    clearTextureAtlas: vi.fn(),
    scrollToLine: vi.fn(),
    scrollToBottom: vi.fn(),
    dispose: vi.fn(),
    options: {},
  };
}

function terminalWriteText(value: string | Uint8Array): string {
  return typeof value === "string" ? value : new TextDecoder().decode(value);
}

describe("TerminalView", () => {
  beforeEach(() => {
    terminalConstructor.mockReset();
    terminalConstructor.mockImplementation(createTerminalMock);
    webglConstructor.mockReset();
    webglConstructor.mockImplementation(function () {
      return {
      dispose: vi.fn(),
      onContextLoss: vi.fn(),
      };
    });
    imageConstructor.mockReset();
    imageConstructor.mockImplementation(function () {
      return {
        dispose: vi.fn(),
      };
    });
    clearTerminalRenderProfiles();
    localStorage.removeItem("onibiTerminalDebug");
    globalThis.__TAURI_MOCKS__.invoke.mockReset();
    globalThis.__TAURI_MOCKS__.listen.mockReset();
    globalThis.__TAURI_MOCKS__.listen.mockResolvedValue(
      globalThis.__TAURI_MOCKS__.unlisten,
    );
    vi.mocked(navigator.clipboard.writeText).mockClear();
    vi.mocked(navigator.clipboard.write).mockClear();
  });

  test("enables xterm proposed APIs for the unicode addon", async () => {
    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    expect(terminalConstructor.mock.calls[0][0]).toMatchObject({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      screenReaderMode: false,
    });
  });

  test("wires screen reader mode and attempts the webgl renderer", async () => {
    render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, terminalScreenReaderMode: true }}
        visible={false}
      />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    expect(terminalConstructor.mock.calls[0][0]).toMatchObject({
      screenReaderMode: true,
    });
    expect(webglConstructor).toHaveBeenCalled();
  });

  test("loads inline image support only when enabled", async () => {
    const first = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    expect(imageConstructor).not.toHaveBeenCalled();
    first.unmount();

    terminalConstructor.mockClear();
    imageConstructor.mockClear();
    render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, terminalInlineImages: "auto" }}
        visible={false}
      />,
    );

    await waitFor(() => expect(imageConstructor).toHaveBeenCalled());
    expect(imageConstructor.mock.calls[0][0]).toMatchObject({
      sixelSupport: true,
      iipSupport: true,
      storageLimit: 64,
    });
  });

  test("lets IME composition key events reach xterm", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalKeybindings: [{ keys: "cmd+c", action: "copy" as const }],
    };
    render(<TerminalView ptyId="pty-1" settings={settings} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    const event = new KeyboardEvent("keydown", { key: "c", metaKey: true });
    Object.defineProperty(event, "isComposing", { value: true });
    const preventDefault = vi.spyOn(event, "preventDefault");

    expect(handler(event)).toBe(true);
    expect(preventDefault).not.toHaveBeenCalled();
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
  });

  test("copies the current selection from terminal keybindings", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalKeybindings: [{ keys: "cmd+c", action: "copy" as const }],
    };
    render(<TerminalView ptyId="pty-1" settings={settings} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    term.getSelection.mockReturnValue("selected text");
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    const event = new KeyboardEvent("keydown", { key: "c", metaKey: true });
    const preventDefault = vi.spyOn(event, "preventDefault");

    expect(handler(event)).toBe(false);
    expect(preventDefault).toHaveBeenCalled();
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("selected text");
  });

  test("lets remote-owned keybindings pass through to xterm", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalKeybindings: [{ keys: "cmd+c", action: "copy" as const }],
    };
    const { getByTestId } = render(
      <TerminalView
        ptyId="pty-1"
        settings={settings}
        remoteKeybindingPolicy="remote"
        visible={false}
      />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    term.getSelection.mockReturnValue("selected text");
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    const event = new KeyboardEvent("keydown", { key: "c", metaKey: true });
    const preventDefault = vi.spyOn(event, "preventDefault");

    expect(handler(event)).toBe(true);
    expect(preventDefault).not.toHaveBeenCalled();
    expect(navigator.clipboard.writeText).not.toHaveBeenCalledWith("selected text");
    expect(getByTestId("terminal-view").dataset.remoteKeybindings).toBe("remote");
  });

  test("restores pane history when no live replay is available", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_replay") {
        return null;
      }
      return null;
    });
    render(
      <TerminalView
        ptyId="pty-1"
        settings={DEFAULT_SETTINGS}
        initialTranscript={"hello\nworld"}
        visible={false}
      />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() =>
      expect(term.write).toHaveBeenCalledWith(
        "[onibi: restored pane history]\r\nhello\r\nworld",
      ),
    );
  });

  test("copies the current selection as rich html when configured", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalCopyFormat: "html" as const,
      terminalKeybindings: [{ keys: "cmd+c", action: "copy" as const }],
    };
    render(<TerminalView ptyId="pty-1" settings={settings} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    term.getSelection.mockReturnValue("selected text");
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;

    handler(new KeyboardEvent("keydown", { key: "c", metaKey: true }));

    await waitFor(() => expect(navigator.clipboard.write).toHaveBeenCalled());
    expect(navigator.clipboard.writeText).not.toHaveBeenCalledWith("selected text");
  });

  test("does not write clipboard text for empty terminal selections", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalKeybindings: [{ keys: "cmd+c", action: "copy" as const }],
    };
    render(<TerminalView ptyId="pty-1" settings={settings} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    term.getSelection.mockReturnValue("");
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;

    handler(new KeyboardEvent("keydown", { key: "c", metaKey: true }));
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
  });

  test("copies the xterm word selection after double click", async () => {
    const { getByTestId } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    term.getSelection.mockReturnValue("word");
    fireEvent.doubleClick(getByTestId("terminal-view"));

    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith("word"),
    );
  });

  test("enters keyboard copy mode from the terminal copy-mode event", async () => {
    const { getByTestId } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );

    await waitFor(() =>
      expect(
        getByTestId("terminal-view").parentElement?.getAttribute("data-copy-mode"),
      ).toBe("true"),
    );
    expect(term.select).toHaveBeenLastCalledWith(0, 1, 1);
  });

  test("copy mode movement intercepts vim keys without writing to the pty", async () => {
    render(<TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );
    globalThis.__TAURI_MOCKS__.invoke.mockClear();
    const event = new KeyboardEvent("keydown", { key: "j" });
    const preventDefault = vi.spyOn(event, "preventDefault");

    expect(handler(event)).toBe(false);
    expect(preventDefault).toHaveBeenCalled();
    expect(term.select).toHaveBeenLastCalledWith(0, 2, 1);
    expect(
      globalThis.__TAURI_MOCKS__.invoke.mock.calls.some(
        ([command]) => command === "pty_write",
      ),
    ).toBe(false);
  });

  test("copy mode visual selection updates xterm selection", async () => {
    render(<TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );

    handler(new KeyboardEvent("keydown", { key: "v" }));
    handler(new KeyboardEvent("keydown", { key: "l" }));

    expect(term.select).toHaveBeenLastCalledWith(0, 1, 2);
  });

  test("copy mode y copies selected text and exits", async () => {
    const { getByTestId } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );
    handler(new KeyboardEvent("keydown", { key: "v" }));
    handler(new KeyboardEvent("keydown", { key: "l" }));
    term.getSelection.mockReturnValue("ga");

    handler(new KeyboardEvent("keydown", { key: "y" }));

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("ga");
    expect(term.clearSelection).toHaveBeenCalled();
    expect(getByTestId("terminal-view").parentElement?.getAttribute("data-copy-mode")).toBe(
      "false",
    );
  });

  test("copy mode can copy ANSI-preserving row data", async () => {
    const settings = {
      ...DEFAULT_SETTINGS,
      terminalCopyFormat: "ansi" as const,
    };
    render(<TerminalView ptyId="pty-1" settings={settings} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );

    handler(new KeyboardEvent("keydown", { key: "y" }));

    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
        "\u001b[31mserialized\u001b[0m",
      ),
    );
  });

  test("handles OSC 52 clipboard writes only when enabled", async () => {
    render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, terminalOsc52Clipboard: true }}
        visible={false}
      />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const osc52 = term.parser.registerOscHandler.mock.calls.find(
      ([code]) => code === 52,
    )?.[1] as ((data: string) => boolean) | undefined;

    expect(osc52?.("c;aGVsbG8=")).toBe(true);
    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith("hello"),
    );
    vi.mocked(navigator.clipboard.writeText).mockClear();
    expect(osc52?.("c;?")).toBe(true);
    expect(osc52?.("c;%%%")).toBe(true);
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
  });

  test("ignores OSC 52 clipboard writes by default", async () => {
    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const osc52 = term.parser.registerOscHandler.mock.calls.find(
      ([code]) => code === 52,
    )?.[1] as ((data: string) => boolean) | undefined;

    expect(osc52?.("c;aGVsbG8=")).toBe(true);
    expect(navigator.clipboard.writeText).not.toHaveBeenCalledWith("hello");
  });

  test("marks transparent terminal backgrounds in the DOM and xterm theme", async () => {
    const { getByTestId } = render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, terminalTransparentBackground: true }}
        visible={false}
      />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    expect(terminalConstructor.mock.calls[0][0].theme).toMatchObject({
      background: "transparent",
    });
    expect(getByTestId("terminal-view").parentElement?.getAttribute("data-transparent")).toBe(
      "true",
    );
  });

  test("emits render profile diagnostics only when terminal debug is enabled", async () => {
    const debug = vi.spyOn(console, "debug").mockImplementation(() => undefined);
    localStorage.setItem("onibiTerminalDebug", "1");
    const profiles: unknown[] = [];
    const handleProfile = (event: Event) => {
      profiles.push((event as CustomEvent).detail);
    };
    window.addEventListener("onibi:terminal-render-profile", handleProfile);
    let emitPty: ((payload: unknown) => void) | undefined;
    globalThis.__TAURI_MOCKS__.listen.mockImplementation(
      async (_eventName: string, callback: (event: { payload: unknown }) => void) => {
        emitPty = (payload) => callback({ payload });
        return globalThis.__TAURI_MOCKS__.unlisten;
      },
    );
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          return null;
        }
        return null;
      },
    );
    const { unmount } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(emitPty).toBeDefined());
    emitPty?.({ type: "data", data: "aGk=", offset: 0 });
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    await waitFor(() => expect(debug).toHaveBeenCalled());
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-render-profile-request", {
        detail: { ptyId: "pty-1" },
      }),
    );
    expect(profiles).toContainEqual(
      expect.objectContaining({
        ptyId: "pty-1",
        reason: "request",
        renderer: "webgl",
        inlineImageMode: "off",
        bytes: 2,
      }),
    );
    unmount();

    expect(
      debug.mock.calls.some(([message]) =>
        String(message).includes("[onibi:terminal] render profile"),
      ),
    ).toBe(true);
    expect(profiles).toContainEqual(
      expect.objectContaining({
        ptyId: "pty-1",
        reason: "dispose",
      }),
    );
    window.removeEventListener("onibi:terminal-render-profile", handleProfile);
    localStorage.removeItem("onibiTerminalDebug");
    debug.mockRestore();
  });

  test("does not emit render profile events when terminal debug is disabled", async () => {
    const profiles: unknown[] = [];
    const handleProfile = (event: Event) => {
      profiles.push((event as CustomEvent).detail);
    };
    window.addEventListener("onibi:terminal-render-profile", handleProfile);
    const { unmount } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-render-profile-request", {
        detail: { ptyId: "pty-1" },
      }),
    );
    unmount();

    expect(profiles).toEqual([]);
    window.removeEventListener("onibi:terminal-render-profile", handleProfile);
  });

  test("writes terminal notice events for the matching pty", async () => {
    render(<TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    window.dispatchEvent(
      new CustomEvent(notificationEvents.terminalNotice, {
        detail: {
          title: "Build ready",
          body: "localhost:5173",
          kind: "info",
          source: "trigger",
          sessionId: "pty-1",
          agent: null,
        },
      }),
    );

    expect(term.write).toHaveBeenCalledWith(
      "\r\n[onibi: Build ready - localhost:5173]\r\n",
    );
  });

  test("copy mode enter copies the current line without visual selection", async () => {
    render(<TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />);

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );

    handler(new KeyboardEvent("keydown", { key: "Enter" }));

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("gamma delta");
    expect(term.clearSelection).toHaveBeenCalled();
  });

  test("copy mode escape exits and clears synthetic selection", async () => {
    const { getByTestId } = render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={false} />,
    );

    await waitFor(() => expect(terminalConstructor).toHaveBeenCalled());
    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    const handler = term.attachCustomKeyEventHandler.mock.calls[0][0] as (
      event: KeyboardEvent,
    ) => boolean;
    window.dispatchEvent(
      new CustomEvent("onibi:terminal-copy-mode", { detail: { ptyId: "pty-1" } }),
    );

    handler(new KeyboardEvent("keydown", { key: "Escape" }));

    expect(term.clearSelection).toHaveBeenCalled();
    expect(getByTestId("terminal-view").parentElement?.getAttribute("data-copy-mode")).toBe(
      "false",
    );
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
    expect(terminalWriteText(term.write.mock.calls[0][0])).toBe("hello");
  });

  test("does not resend duplicate pty resizes for repeated layout passes", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string) => {
        if (command === "pty_replay") {
          return null;
        }
        return null;
      },
    );

    render(
      <TerminalView ptyId="pty-1" settings={DEFAULT_SETTINGS} visible={true} />,
    );

    await waitFor(() =>
      expect(
        globalThis.__TAURI_MOCKS__.invoke.mock.calls.filter(
          ([command]) => command === "pty_resize",
        ),
      ).toHaveLength(1),
    );
    await new Promise((resolve) => window.setTimeout(resolve, 20));
    expect(
      globalThis.__TAURI_MOCKS__.invoke.mock.calls.filter(
        ([command]) => command === "pty_resize",
      ),
    ).toHaveLength(1);
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
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    const output = term.write.mock.calls
      .map(([value]) => terminalWriteText(value))
      .join("");
    expect(output).toBe("hello world");
  });

  test("queues live pty output until replay resolves", async () => {
    let emitPty: ((payload: unknown) => void) | undefined;
    let resolveReplay:
      | ((snapshot: { data: string; startOffset: number; endOffset: number } | null) => void)
      | undefined;
    const replay = new Promise<{
      data: string;
      startOffset: number;
      endOffset: number;
    } | null>((resolve) => {
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
    emitPty?.({ type: "data", data: "aGk=", offset: 0 });

    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    expect(term.write).not.toHaveBeenCalled();
    resolveReplay?.(null);
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    expect(terminalWriteText(term.write.mock.calls[0][0])).toBe("hi");
  });

  test("flushes queued exit events after replay output", async () => {
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
    const onExit = vi.fn();
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
      <TerminalView
        ptyId="pty-1"
        settings={DEFAULT_SETTINGS}
        visible={false}
        onExit={onExit}
      />,
    );

    await waitFor(() => expect(emitPty).toBeDefined());
    emitPty?.({ type: "exit", code: 7, signal: null });
    resolveReplay?.({ data: "ZG9uZQ==", startOffset: 0, endOffset: 4 });

    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(onExit).toHaveBeenCalledWith({ code: 7, signal: null }));
    const output = term.write.mock.calls
      .map(([value]) => terminalWriteText(value))
      .join("");
    expect(output).toContain("done");
    expect(output).toContain("[process exited: 7]");
  });

  test("keeps live output when replay fails after data arrives", async () => {
    let emitPty: ((payload: unknown) => void) | undefined;
    let rejectReplay: ((error: Error) => void) | undefined;
    const replay = new Promise<never>((_resolve, reject) => {
      rejectReplay = reject;
    });
    const onUnavailable = vi.fn();
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
      <TerminalView
        ptyId="pty-1"
        settings={DEFAULT_SETTINGS}
        visible={false}
        onUnavailable={onUnavailable}
      />,
    );

    await waitFor(() => expect(emitPty).toBeDefined());
    emitPty?.({ type: "data", data: "bGl2ZQ==", offset: 0 });
    rejectReplay?.(new Error("replay failed"));

    const term = terminalConstructor.mock.results[0]
      .value as ReturnType<typeof createTerminalMock>;
    await waitFor(() => expect(term.write).toHaveBeenCalled());
    expect(terminalWriteText(term.write.mock.calls[0][0])).toBe("live");
    expect(onUnavailable).not.toHaveBeenCalled();
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
