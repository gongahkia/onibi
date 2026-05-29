import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { TerminalView } from "./TerminalView";
import { DEFAULT_SETTINGS } from "../lib/sessions";

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
    dispose = vi.fn();
    clearTextureAtlas = vi.fn();
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

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: vi.fn().mockImplementation(function WebglAddonConstructor() {
    return {
    dispose: vi.fn(),
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
      fontFamily: '"Fira Code", Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      letterSpacing: 0,
      lineHeight: 1,
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
        fontFamily:
          '"Fira Code", Menlo, Monaco, Consolas, "Liberation Mono", monospace',
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
});
