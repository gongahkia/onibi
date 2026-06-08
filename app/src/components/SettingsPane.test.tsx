import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { SettingsPane } from "./SettingsPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
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
  globalThis.__TAURI_MOCKS__.updateCheck.mockReset();
  globalThis.__TAURI_MOCKS__.updateCheck.mockResolvedValue(null);
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

  test("updates pane agent label visibility", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Layout"));
    fireEvent.click(screen.getByLabelText("Show pane agent labels"));

    expect(useSessionStore.getState().settings.showTerminalPaneAgentLabels).toBe(false);
  });

  test("updates custom agent display labels", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Agents"));

    const input = screen.getByLabelText("Codex display label");
    fireEvent.change(input, { target: { value: "Local Codex" } });
    expect(useSessionStore.getState().settings.agentLabelOverrides.codex).toBe(
      "Local Codex",
    );

    fireEvent.change(input, { target: { value: "" } });
    expect(useSessionStore.getState().settings.agentLabelOverrides.codex).toBeUndefined();
  });

  test("updates terminal shell mode", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Shell mode"), {
      target: { value: "non_login" },
    });

    expect(useSessionStore.getState().settings.terminalShellMode).toBe("non_login");
  });

  test("updates remote session and pane history settings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Preserve pane history across restarts"));
    fireEvent.change(screen.getByLabelText("Remote keybindings policy"), {
      target: { value: "remote" },
    });
    fireEvent.change(screen.getByLabelText("Remote SSH command"), {
      target: { value: "/opt/homebrew/bin/ssh" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.paneHistoryEnabled).toBe(true);
    expect(settings.remoteKeybindingPolicy).toBe("remote");
    expect(settings.remoteSshCommand).toBe("/opt/homebrew/bin/ssh");
  });

  test("dispatches update checks from settings", () => {
    const listener = vi.fn();
    window.addEventListener("onibi:check-updates", listener);
    try {
      render(<SettingsPane open onClose={vi.fn()} />);
      fireEvent.click(screen.getByText("Updates"));
      expect(screen.getByText("1.5.0-dev")).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Check for Updates" }));

      expect(listener).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener("onibi:check-updates", listener);
    }
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

  test("updates system light and dark theme pair selections", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Light appearance"), {
      target: { value: "github-light" },
    });
    expect(useSessionStore.getState().settings.theme).toBe(
      "light:github-light,dark:vscode-dark-plus",
    );

    fireEvent.change(screen.getByLabelText("Dark appearance"), {
      target: { value: "tokyo-night" },
    });
    expect(useSessionStore.getState().settings.theme).toBe(
      "light:github-light,dark:tokyo-night",
    );
  });

  test("updates and resets resolved theme overrides", () => {
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, theme: "github-dark" },
    });
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Override Terminal background color"), {
      target: { value: "#010203" },
    });
    expect(
      useSessionStore.getState().settings.themeOverrides["github-dark"]
        ?.terminalBackground,
    ).toBe("#010203");

    fireEvent.click(screen.getByText("Reset"));
    expect(
      useSessionStore.getState().settings.themeOverrides["github-dark"],
    ).toBeUndefined();
  });

  test("updates terminal mouse capture and narrow layout threshold", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Send terminal mouse events"));
    fireEvent.change(screen.getByLabelText("Narrow layout threshold"), {
      target: { value: "960" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.terminalMouseCapture).toBe(false);
    expect(settings.mobileLayoutThresholdPx).toBe(960);
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

  test("stores separate font sizes, editor keybindings, and diff view mode", () => {
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
    fireEvent.change(screen.getByLabelText("Editor keybindings"), {
      target: { value: "vim" },
    });
    fireEvent.click(screen.getByLabelText("Vim relative line numbers"));
    fireEvent.click(screen.getByLabelText("Sync Vim register with system clipboard"));
    fireEvent.change(screen.getByLabelText("Diff view"), {
      target: { value: "unified" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.uiFontSize).toBe(16);
    expect(settings.terminalFontSize).toBe(18);
    expect(settings.editorFontSize).toBe(14);
    expect(settings.editorKeybindingMode).toBe("vim");
    expect(settings.editorVimRelativeLineNumbers).toBe(true);
    expect(settings.editorVimSystemClipboard).toBe(true);
    expect(settings.diffViewMode).toBe("unified");
  });

  test("stores emacs clipboard sync preference", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Editor keybindings"), {
      target: { value: "emacs" },
    });
    fireEvent.click(screen.getByLabelText("Use system clipboard for Emacs kill and yank"));

    const settings = useSessionStore.getState().settings;
    expect(settings.editorKeybindingMode).toBe("emacs");
    expect(settings.editorEmacsSystemClipboard).toBe(true);
  });

  test("stores terminal scrollback without exposing shell integration in general", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Terminal scrollback lines"), {
      target: { value: "50000" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.terminalScrollbackLines).toBe(50000);
    expect(screen.queryByLabelText("Enable shell integration")).toBeNull();
  });

  test("stores terminal screen reader mode", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Enable terminal screen reader mode"));

    expect(useSessionStore.getState().settings.terminalScreenReaderMode).toBe(true);
  });

  test("stores terminal copy and clipboard polish settings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Terminal copy format"), {
      target: { value: "html" },
    });
    fireEvent.click(screen.getByLabelText("Enable OSC 52 clipboard writes"));
    fireEvent.click(screen.getByLabelText("Use transparent terminal background"));
    fireEvent.change(screen.getByLabelText("Terminal inline images"), {
      target: { value: "auto" },
    });

    const settings = useSessionStore.getState().settings;
    expect(settings.terminalCopyFormat).toBe("html");
    expect(settings.terminalOsc52Clipboard).toBe(true);
    expect(settings.terminalTransparentBackground).toBe(true);
    expect(settings.terminalInlineImages).toBe("auto");
  });

  test("stores notification delivery and sound settings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("Notification delivery"), {
      target: { value: "terminal" },
    });
    fireEvent.click(screen.getByLabelText("Suppress foreground tab notifications"));
    fireEvent.click(screen.getByLabelText("Enable sound alerts"));
    fireEvent.change(screen.getByLabelText("Completion sound path"), {
      target: { value: "/tmp/done.mp3" },
    });
    fireEvent.change(screen.getByLabelText("Request sound path"), {
      target: { value: "/tmp/request.mp3" },
    });
    fireEvent.click(screen.getByLabelText("Codex sound alerts"));

    const settings = useSessionStore.getState().settings;
    expect(settings.notificationDelivery).toBe("terminal");
    expect(settings.suppressForegroundTabNotifications).toBe(false);
    expect(settings.soundAlertsEnabled).toBe(true);
    expect(settings.soundCompletionPath).toBe("/tmp/done.mp3");
    expect(settings.soundRequestPath).toBe("/tmp/request.mp3");
    expect(settings.soundAgents.codex).toBe(false);
  });

  test("adds custom command keybindings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Keybindings"));
    fireEvent.click(screen.getByRole("button", { name: "Add command" }));

    fireEvent.change(screen.getByLabelText("Custom command keybinding"), {
      target: { value: "prefix+t" },
    });
    fireEvent.change(screen.getByLabelText("Custom command description"), {
      target: { value: "Run tests" },
    });
    const command = screen.getByLabelText("Custom command shell command");
    fireEvent.change(command, { target: { value: "pnpm test" } });
    fireEvent.blur(command);

    expect(useSessionStore.getState().settings.customCommandKeybindings).toEqual([
      { keys: "prefix+t", description: "Run tests", command: "pnpm test" },
    ]);
  });

  test("shows terminal triggers as a top-level settings section", () => {
    render(<SettingsPane open onClose={vi.fn()} />);

    expect(screen.queryByRole("button", { name: "Advanced" })).toBeNull();
    expect(screen.queryByText("Approval needed")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Triggers" }));

    expect(screen.getByText("Approval needed")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Approval needed trigger"));
    expect(useSessionStore.getState().settings.terminalTriggers[0].enabled).toBe(true);
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

  test("sets the default agent from general settings", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Default agent"), {
      target: { value: "codex" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Shell integration" }));

    expect(useSessionStore.getState().settings.defaultAgent).toBe("codex");
    expect(screen.getByLabelText("Enable shell integration")).toBeTruthy();
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

  test("applies config.toml edits", () => {
    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "config.toml" }));
    fireEvent.change(screen.getByLabelText("Onibi config TOML"), {
      target: {
        value:
          'version = 1\n\n[settings]\ntheme = "github-light"\n\n[[workspaces]]\nid = "workspace:/repo"\npath = "/repo"\nname = "repo"\n',
      },
    });
    fireEvent.click(screen.getByText("Apply TOML"));

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
    fireEvent.click(screen.getByRole("button", { name: "Import terminal settings" }));
    expect((await screen.findAllByText("Ghostty")).length).toBeGreaterThan(0);
    expect(screen.getByText("Supported terminal imports")).toBeTruthy();
    expect(screen.getByText("Alacritty")).toBeTruthy();
    expect(screen.getAllByText("Manual import").length).toBeGreaterThan(4);
    fireEvent.click(await screen.findByRole("button", { name: "Apply selected" }));

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

  test("imports newer terminal config formats", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_detect_terminal_configs") {
        return [
          {
            source: "rio",
            label: "Rio",
            path: "/Users/test/.config/rio/config.toml",
            content:
              "font-family = \"JetBrains Mono\"\nfont-size = 15\nbackground = \"#010203\"\nforeground = \"#f1f2f3\"\n",
          },
        ];
      }
      if (command === "list_font_families") {
        return [];
      }
      return null;
    });

    render(<SettingsPane open onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Import terminal settings" }));
    expect((await screen.findAllByText("Rio")).length).toBeGreaterThan(0);
    fireEvent.click(await screen.findByRole("button", { name: "Apply selected" }));

    const settings = useSessionStore.getState().settings;
    expect(settings.terminalFontFamily).toBe("JetBrains Mono");
    expect(settings.terminalFontSize).toBe(15);
    expect(settings.customColorScheme.colors.terminalBackground).toBe("#010203");
  });
});
