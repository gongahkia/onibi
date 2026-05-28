import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { SettingsPane } from "./SettingsPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue(null);
}

describe("SettingsPane", () => {
  beforeEach(() => {
    resetStore();
  });

  test("updates tab orientation", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Layout"));
    fireEvent.change(screen.getByLabelText("Tab bar orientation"), {
      target: { value: "horizontal" },
    });

    expect(useSessionStore.getState().settings.tabBarOrientation).toBe("horizontal");
    expect(useSessionStore.getState().settings.tabBarPosition).toBe("top");
  });

  test("updates file tree icon visibility", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Layout"));
    fireEvent.click(screen.getByLabelText("Show file icons"));

    expect(useSessionStore.getState().settings.showFileIcons).toBe(false);
  });

  test("updates a custom color scheme", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Color scheme"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Terminal background color"), {
      target: { value: "#123456" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.theme).toBe("custom");
    expect(settings.customColorScheme.colors.terminalBackground).toBe("#123456");
  });

  test("selects locally available font families", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "list_font_families") {
        return ["Fira Code", "Monaco"];
      }
      return null;
    });

    render(<SettingsPane open onClose={vi.fn()} />);
    await within(screen.getByLabelText("Terminal font installed font")).findByRole(
      "option",
      { name: "Fira Code" },
    );

    fireEvent.change(screen.getByLabelText("Terminal font installed font"), {
      target: { value: "Fira Code" },
    });

    expect(useSessionStore.getState().settings.terminalFontFamily).toBe("Fira Code");
  });

  test("stores separate font families for ui terminal and file content", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "list_font_families") {
        return ["Avenir", "Fira Code", "Monaco"];
      }
      return null;
    });

    render(<SettingsPane open onClose={vi.fn()} />);
    await within(screen.getByLabelText("UI font installed font")).findByRole("option", {
      name: "Avenir",
    });

    fireEvent.change(screen.getByLabelText("UI font installed font"), {
      target: { value: "Avenir" },
    });
    fireEvent.change(screen.getByLabelText("Terminal font installed font"), {
      target: { value: "Monaco" },
    });
    fireEvent.change(screen.getByLabelText("File content font installed font"), {
      target: { value: "Fira Code" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.uiFontFamily).toBe("Avenir");
    expect(settings.terminalFontFamily).toBe("Monaco");
    expect(settings.editorFontFamily).toBe("Fira Code");
  });
});
