import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { TerminalView } from "./TerminalView";
import { DEFAULT_SETTINGS } from "../lib/sessions";

const EXPECTED_TERMINAL_FONT_FALLBACK =
  '"Symbols Nerd Font Mono", "Symbols Nerd Font", "MesloLGS NF", ' +
  '"MesloLGS Nerd Font Mono", "JetBrainsMono Nerd Font Mono", ' +
  '"JetBrainsMono Nerd Font", "FiraCode Nerd Font Mono", "FiraCode Nerd Font", ' +
  '"Hack Nerd Font Mono", "Hack Nerd Font", "CaskaydiaCove Nerd Font Mono", ' +
  '"CaskaydiaCove Nerd Font", "CaskaydiaMono Nerd Font Mono", ' +
  '"CaskaydiaMono Nerd Font", Menlo, Monaco, Consolas, "Liberation Mono", monospace';

const terminalMocks = vi.hoisted(() => {
  class MockTerminal {
    options: unknown;
    rows = 30;
    cols = 100;
    onDataHandler: ((data: string) => void) | undefined;
    loadAddon = vi.fn();
    open = vi.fn();
    focus = vi.fn();
    write = vi.fn();
    refresh = vi.fn();
    scrollToBottom = vi.fn();
    clear = vi.fn();
    selectAll = vi.fn();
    dispose = vi.fn();
    clearTextureAtlas = vi.fn();
    getSelection = vi.fn(() => "");
    attachCustomKeyEventHandler = vi.fn();
    unicode = { activeVersion: "6" };
    parser = { registerOscHandler: vi.fn() };
    onData = vi.fn((handler: (data: string) => void) => {
      this.onDataHandler = handler;
      return { dispose: vi.fn() };
    });
    emitData(data: string) {
      this.onDataHandler?.(data);
    }
    constructor(options: unknown) {
      this.options = options;
    }
  }
  return {
    instances: [] as MockTerminal[],
    fitAddons: [] as Array<{
      fit: ReturnType<typeof vi.fn>;
      dispose: ReturnType<typeof vi.fn>;
    }>,
    MockTerminal,
  };
});

vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation(function TerminalConstructor(options) {
    const terminal = new terminalMocks.MockTerminal(options);
    terminalMocks.instances.push(terminal);
    return terminal;
  }),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(function FitAddonConstructor() {
    const fitAddon = {
      fit: vi.fn(),
      dispose: vi.fn(),
    };
    terminalMocks.fitAddons.push(fitAddon);
    return fitAddon;
  }),
}));

vi.mock("@xterm/addon-search", () => ({
  SearchAddon: vi.fn().mockImplementation(function SearchAddonConstructor() {
    return {
      findNext: vi.fn(),
      findPrevious: vi.fn(),
      dispose: vi.fn(),
    };
  }),
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: vi.fn().mockImplementation(function WebglAddonConstructor() {
    return {
      dispose: vi.fn(),
      onContextLoss: vi.fn(),
    };
  }),
}));

vi.mock("@xterm/addon-web-links", () => ({
  WebLinksAddon: vi.fn().mockImplementation(function WebLinksAddonConstructor() {
    return {
    dispose: vi.fn(),
    };
  }),
}));

vi.mock("@xterm/addon-unicode11", () => ({
  Unicode11Addon: vi.fn().mockImplementation(function Unicode11AddonConstructor() {
    return {
      dispose: vi.fn(),
    };
  }),
}));

vi.mock("@xterm/addon-serialize", () => ({
  SerializeAddon: vi.fn().mockImplementation(function SerializeAddonConstructor() {
    return {
      dispose: vi.fn(),
      serialize: vi.fn(() => ""),
    };
  }),
}));

describe("TerminalView", () => {
  beforeEach(() => {
    terminalMocks.instances.length = 0;
    terminalMocks.fitAddons.length = 0;
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue(undefined);
    globalThis.__TAURI_MOCKS__.listen.mockResolvedValue(
      globalThis.__TAURI_MOCKS__.unlisten,
    );
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("mounts without throwing", () => {
    render(<TerminalView ptyId="pty-1" />);
    expect(terminalMocks.instances).toHaveLength(1);
  });

  test("uses the selected color scheme for xterm", () => {
    render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, theme: "night-owl" }}
      />,
    );

    expect(terminalMocks.instances[0].options).toMatchObject({
      theme: {
        background: "#011627",
        foreground: "#d6deeb",
        cursor: "#80a4c2",
        selectionBackground: "#1d3b53",
      },
    });
  });

  test("passes selected terminal font to xterm with monospace fallbacks", () => {
    render(<TerminalView ptyId="pty-1" fontFamily="Fira Code" />);

    expect(terminalMocks.instances[0].options).toMatchObject({
      fontFamily: `"Fira Code", ${EXPECTED_TERMINAL_FONT_FALLBACK}`,
      letterSpacing: 0,
      lineHeight: 1,
    });
  });

  test("uses configured terminal scrollback", () => {
    render(
      <TerminalView
        ptyId="pty-1"
        settings={{ ...DEFAULT_SETTINGS, terminalScrollbackLines: 50000 }}
      />,
    );

    expect(terminalMocks.instances[0].options).toMatchObject({
      scrollback: 50000,
    });
  });

  test("adds Nerd Font fallbacks to comma-separated terminal stacks", () => {
    render(<TerminalView ptyId="pty-1" fontFamily="Menlo, Monaco, monospace" />);

    expect(terminalMocks.instances[0].options).toMatchObject({
      fontFamily: `Menlo, ${EXPECTED_TERMINAL_FONT_FALLBACK.replace(
        "Menlo, ",
        "",
      )}`,
    });
  });

  test("updates terminal font without remounting xterm", async () => {
    const { rerender } = render(
      <TerminalView ptyId="pty-1" fontFamily="Menlo" />,
    );
    const terminal = terminalMocks.instances[0];

    rerender(<TerminalView ptyId="pty-1" fontFamily="Fira Code" />);

    await waitFor(() => {
      expect(terminalMocks.instances).toHaveLength(1);
      expect(terminal.options).toMatchObject({
        fontFamily: `"Fira Code", ${EXPECTED_TERMINAL_FONT_FALLBACK}`,
      });
      expect(terminal.clearTextureAtlas).toHaveBeenCalled();
    });
  });

  test("refits and refreshes when revealed after being hidden", async () => {
    const { rerender } = render(<TerminalView ptyId="pty-1" visible={false} />);
    const terminal = terminalMocks.instances[0];
    const fitAddon = terminalMocks.fitAddons[0];

    await waitFor(() => expect(terminal.refresh).toHaveBeenCalled());
    terminal.refresh.mockClear();
    terminal.focus.mockClear();
    fitAddon.fit.mockClear();

    rerender(<TerminalView ptyId="pty-1" visible />);

    await waitFor(() => {
      expect(fitAddon.fit).toHaveBeenCalled();
      expect(terminal.refresh).toHaveBeenCalledWith(0, 29);
      expect(terminal.focus).toHaveBeenCalled();
    });
  });

  test("subscribes on mount and unsubscribes on unmount", async () => {
    const { unmount } = render(<TerminalView ptyId="pty-1" />);
    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.listen).toHaveBeenCalledWith(
        "pty:pty-1",
        expect.any(Function),
      );
    });
    unmount();
    expect(globalThis.__TAURI_MOCKS__.unlisten).toHaveBeenCalled();
  });

  test("writes encoded terminal input to the pty", async () => {
    render(<TerminalView ptyId="pty-1" />);
    await waitFor(() => expect(terminalMocks.instances[0]).toBeDefined());
    terminalMocks.instances[0].emitData("a");
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_write", {
      id: "pty-1",
      data: [97],
    });
  });

  test("emits shell command metadata and detected previews", async () => {
    const onShellUpdate = vi.fn();
    render(<TerminalView ptyId="pty-1" onShellUpdate={onShellUpdate} />);
    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.listen).toHaveBeenCalledWith(
        "pty:pty-1",
        expect.any(Function),
      );
    });

    const listener = globalThis.__TAURI_MOCKS__.listen.mock.calls[0][1] as (
      event: { payload: unknown },
    ) => void;
    const text =
      "\x1b]133;C;pnpm dev\x07Local: http://localhost:1420/\nready\n\x1b]133;D;0\x07";
    listener({ payload: { type: "data", data: btoa(text) } });

    await waitFor(() => {
      expect(onShellUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          preview: expect.objectContaining({ url: "http://localhost:1420/" }),
          lastCommand: expect.objectContaining({
            command: "pnpm dev",
            exitCode: 0,
          }),
        }),
      );
    });
  });
});
