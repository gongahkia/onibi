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
});
