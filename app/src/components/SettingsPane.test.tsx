import { fireEvent, render, screen } from "@testing-library/react";
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
    await screen.findByRole("option", { name: "Fira Code" });

    fireEvent.change(screen.getByLabelText("Installed font family"), {
      target: { value: "Fira Code" },
    });

    expect(useSessionStore.getState().settings.fontFamily).toBe("Fira Code");
  });
});
