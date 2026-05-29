import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { SettingsPane } from "./SettingsPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    terminalLayout: null,
    activeTerminalPaneId: null,
    maximizedTerminalPaneId: null,
    arrangements: [],
    activeSidebarView: "files",
    workspaces: [],
    selectedFile: null,
    sessionEvents: [],
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

  test("stores separate font sizes and diff view mode", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("UI font size"), {
      target: { value: "16" },
    });
    fireEvent.change(screen.getByLabelText("Terminal font size"), {
      target: { value: "18" },
    });
    fireEvent.change(screen.getByLabelText("File content font size"), {
      target: { value: "14" },
    });
    fireEvent.change(screen.getByLabelText("Diff view"), {
      target: { value: "unified" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.uiFontSize).toBe(16);
    expect(settings.terminalFontSize).toBe(18);
    expect(settings.editorFontSize).toBe(14);
    expect(settings.diffViewMode).toBe("unified");
  });

  test("stores terminal scrollback and shell integration settings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Terminal scrollback lines"), {
      target: { value: "50000" },
    });
    fireEvent.click(screen.getByLabelText("Enable shell completions and autosuggestions"));

    const settings = useSessionStore.getState().settings;
    expect(settings.terminalScrollbackLines).toBe(50000);
    expect(settings.terminalShellIntegration).toBe(false);
  });

  test("lists newly supported agent commands", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Agents"));

    expect(screen.getByLabelText("Copilot CLI launch command")).toBeTruthy();
    expect(screen.getByLabelText("Amp launch command")).toBeTruthy();
    expect(screen.getByLabelText("Pi launch command")).toBeTruthy();
    expect(screen.getByLabelText("Cline launch command")).toBeTruthy();
    expect(screen.getByLabelText("Crush launch command")).toBeTruthy();
  });

  test("edits terminal profiles", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Profiles"));

    fireEvent.change(screen.getByLabelText("Profile name"), {
      target: { value: "Project shell" },
    });
    fireEvent.change(screen.getByLabelText("Profile environment"), {
      target: { value: "FOO=bar\nEMPTY=" },
    });
    fireEvent.change(screen.getByLabelText("Profile cwd policy"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Profile custom cwd"), {
      target: { value: "/tmp/onibi" },
    });

    const profile = useSessionStore.getState().settings.terminalProfiles[0];
    expect(profile.name).toBe("Project shell");
    expect(profile.env).toEqual([
      ["FOO", "bar"],
      ["EMPTY", ""],
    ]);
    expect(profile.cwdPolicy).toBe("custom");
    expect(profile.customCwd).toBe("/tmp/onibi");
  });

  test("shows shell integration runtime status", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo/packages/app",
          lastExitCode: 2,
        },
      ],
    });

    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Shell integration" }));

    expect(screen.getByText("cwd: /repo/packages/app")).toBeTruthy();
    expect(screen.getByText("last exit: 2")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Enable shell integration"));
    expect(useSessionStore.getState().settings.terminalShellIntegration).toBe(false);
  });

  test("applies config.json edits", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("config.json"));
    fireEvent.change(screen.getByLabelText("Onibi config JSON"), {
      target: {
        value: JSON.stringify({
          version: 1,
          settings: { ...DEFAULT_SETTINGS, theme: "github-light" },
          workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
        }),
      },
    });
    fireEvent.click(screen.getByText("Apply JSON"));

    expect(useSessionStore.getState().settings.theme).toBe("github-light");
    expect(useSessionStore.getState().workspaces[0].path).toBe("/repo");
  });

  test("imports terminal config colors and fonts", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_detect_terminal_configs") {
        return [
          {
            source: "ghostty",
            label: "Ghostty",
            path: "/Users/test/.config/ghostty/config",
            content:
              "font-family = Fira Code\nfont-size = 16\nbackground = #101010\nforeground = #eeeeee\npalette = 4=#88aaff\nkeybind = cmd+c=copy_to_clipboard\ncustom-shader = /tmp/ghostty.glsl\n",
          },
        ];
      }
      if (command === "list_font_families") {
        return [];
      }
      return null;
    });

    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Import config from ..."));
    expect(await screen.findByText("Ghostty")).toBeTruthy();
    fireEvent.click(screen.getByText("Apply selected"));

    const settings = useSessionStore.getState().settings;
    expect(settings.theme).toBe("custom");
    expect(settings.terminalFontFamily).toBe("Fira Code");
    expect(settings.terminalFontSize).toBe(16);
    expect(settings.customColorScheme.colors.terminalBackground).toBe("#101010");
    expect(settings.terminalKeybindings).toEqual([
      { keys: "cmd+c", action: "copy", source: "ghostty" },
    ]);
    expect(settings.terminalShaderPaths).toEqual(["/tmp/ghostty.glsl"]);
  });
});
