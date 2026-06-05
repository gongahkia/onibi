import { load } from "@tauri-apps/plugin-store";
import { beforeEach, describe, expect, test } from "vitest";
import {
  APP_KEYBINDING_ACTION_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_SETTINGS,
  appKeybindingConflicts,
  hydrateSessionStore,
  keyChordFromKeyboardEvent,
  normalizeNewPaneCwdMode,
  normalizeTerminalShellMode,
  parseOnibiConfigToml,
  resolveNewPaneCwd,
  serializeOnibiConfigToml,
  terminalThemeForSettings,
  type TerminalPaneNode,
  useSessionStore,
} from "./sessions";

async function resetStore() {
  const store = await load("settings.json");
  await store.clear();
  useSessionStore.setState({
    hydrated: false,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    activeTerminalPaneId: null,
    maximizedTerminalPaneId: null,
    terminalLayout: null,
    workspaces: [],
    selectedFile: null,
    sessionEvents: [],
    openBuffers: [],
    activeBufferKey: null,
    closedBufferStack: [],
    bufferAccessOrder: [],
    dirtyBufferKeys: [],
    commandBlocks: [],
    activeCommandBlocks: {},
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
}

function leafPaneIds(node: TerminalPaneNode | null): string[] {
  if (!node) {
    return [];
  }
  if (node.type === "leaf") {
    return [node.paneId];
  }
  return node.children.flatMap(leafPaneIds);
}

describe("session hydration", () => {
  beforeEach(async () => {
    await resetStore();
  });

  test("keeps daemon-detected heuristic agent labels", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "onibi_read_config_toml") {
        return null;
      }
      if (command === "pty_sessions") {
        return [
          {
            id: "pty-1",
            paneId: "pty-1",
            agent: "grok",
            workspaceId: "workspace:/repo",
            cwd: "/repo",
            title: "Grok · repo",
            status: "working",
            lifecycle: "running",
            rows: 30,
            cols: 100,
            createdAt: 1,
            updatedAt: 1,
            stoppedAt: null,
            exitCode: null,
            exitSignal: null,
            restart: null,
          },
        ];
      }
      if (command === "fs_read_ghostty_config") {
        return null;
      }
      return null;
    });

    await hydrateSessionStore();

    const state = useSessionStore.getState();
    expect(state.sessions).toHaveLength(1);
    expect(state.sessions[0]).toMatchObject({
      id: "pty-1",
      agent: "grok",
      status: "running",
      workspaceId: "workspace:/repo",
    });
    expect(state.workspaces[0]).toMatchObject({
      id: "workspace:/repo",
      path: "/repo",
      name: "repo",
    });
  });

  test("moves terminal panes while preserving split geometry", async () => {
    const layout: TerminalPaneNode = {
      type: "split",
      paneId: "split-root",
      direction: "vertical",
      sizes: [35, 65],
      children: [
        { type: "leaf", paneId: "pane-a", sessionId: "pty-a" },
        {
          type: "split",
          paneId: "split-inner",
          direction: "horizontal",
          sizes: [40, 60],
          children: [
            { type: "leaf", paneId: "pane-b", sessionId: "pty-b" },
            { type: "leaf", paneId: "pane-c", sessionId: "pty-c" },
          ],
        },
      ],
    };
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-a",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "A",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-b",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "B",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-c",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "C",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      workspaceTabs: [
        {
          id: "tab-1",
          workspaceId: "workspace:/repo",
          title: "Terminal",
          terminalLayout: layout,
          activeTerminalPaneId: "pane-c",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "tab-1",
      terminalLayout: layout,
      activeTerminalPaneId: "pane-c",
      activeSessionId: "pty-c",
    });

    useSessionStore.getState().moveTerminalPane("pane-c", "pane-a", "before");

    const state = useSessionStore.getState();
    expect(leafPaneIds(state.terminalLayout)).toEqual(["pane-c", "pane-a", "pane-b"]);
    expect(state.terminalLayout).toMatchObject({
      type: "split",
      sizes: [35, 65],
      children: [
        { type: "leaf", paneId: "pane-c" },
        {
          type: "split",
          sizes: [40, 60],
        },
      ],
    });
    expect(state.activeTerminalPaneId).toBe("pane-c");
    expect(state.activeSessionId).toBe("pty-c");
  });

  test("applies terminal layout presets to the active workspace tab", async () => {
    const layout: TerminalPaneNode = {
      type: "split",
      paneId: "split-root",
      direction: "horizontal",
      sizes: [50, 50],
      children: [
        { type: "leaf", paneId: "pane-a", sessionId: "pty-a" },
        { type: "leaf", paneId: "pane-b", sessionId: "pty-b" },
        { type: "leaf", paneId: "pane-c", sessionId: "pty-c" },
      ],
    };
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-a",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "A",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-b",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "B",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-c",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "C",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      workspaceTabs: [
        {
          id: "tab-1",
          workspaceId: "workspace:/repo",
          title: "Terminal",
          terminalLayout: layout,
          activeTerminalPaneId: "pane-b",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "tab-1",
      terminalLayout: layout,
      activeTerminalPaneId: "pane-b",
      activeSessionId: "pty-b",
    });

    useSessionStore.getState().applyTerminalLayoutPreset("tiled");

    const state = useSessionStore.getState();
    expect(leafPaneIds(state.terminalLayout)).toEqual(["pane-a", "pane-b", "pane-c"]);
    expect(state.terminalLayout).toMatchObject({
      type: "split",
      direction: "vertical",
      sizes: [50, 50],
    });
    expect(state.activeTerminalPaneId).toBe("pane-b");
    expect(state.activeSessionId).toBe("pty-b");
    expect(state.workspaceTabs[0]?.terminalLayout).toEqual(state.terminalLayout);
  });

  test("resolves follow and fixed new pane cwd modes", async () => {
    const workspace = { id: "workspace:/repo", path: "/repo", name: "repo" };
    const session = {
      id: "pty-1",
      agent: "shell" as const,
      workspaceId: workspace.id,
      title: "Shell",
      status: "running" as const,
      createdAt: 1,
      pendingApprovals: [],
      cwd: "/repo/packages/app",
    };

    expect(
      resolveNewPaneCwd(
        { ...DEFAULT_SETTINGS, newPaneCwd: "follow" },
        session,
        workspace,
      ),
    ).toBe("/repo/packages/app");
    expect(
      resolveNewPaneCwd(
        { ...DEFAULT_SETTINGS, newPaneCwd: "fixed:/tmp/onibi" },
        session,
        workspace,
      ),
    ).toBe("/tmp/onibi");
    expect(normalizeNewPaneCwdMode("fixed:relative")).toBe(
      DEFAULT_SETTINGS.newPaneCwd,
    );
  });

  test("normalizes terminal shell mode", async () => {
    expect(DEFAULT_SETTINGS.terminalShellMode).toBe("auto");
    expect(normalizeTerminalShellMode("login")).toBe("login");
    expect(normalizeTerminalShellMode("non_login")).toBe("non_login");
    expect(normalizeTerminalShellMode("interactive")).toBe("auto");
  });

  test("defaults bind prefix number keys to workspace tabs", async () => {
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+1",
      action: "workspace.tab.focusIndex1",
    });
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+9",
      action: "workspace.tab.focusIndex9",
    });
  });

  test("defaults include navigators and remap close away from prefix w", async () => {
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+w",
      action: "workspace.navigator.open",
    });
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+g",
      action: "session.navigator.open",
    });
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+?",
      action: "keybindings.help.open",
    });
    expect(DEFAULT_SETTINGS.appKeybindings).toContainEqual({
      keys: "prefix+x",
      action: "session.closeActive",
    });
    expect(DEFAULT_SETTINGS.appKeybindings).not.toContainEqual({
      keys: "prefix+w",
      action: "session.closeActive",
    });
  });

  test("normalizes shifted question mark events for prefix help", async () => {
    const event = new KeyboardEvent("keydown", { key: "?", shiftKey: true });

    expect(keyChordFromKeyboardEvent(event)).toBe("?");
  });

  test("focuses workspaces tabs and terminal panes by one-based index", async () => {
    const layout: TerminalPaneNode = {
      type: "split",
      paneId: "split-root",
      direction: "vertical",
      children: [
        { type: "leaf", paneId: "pane-a", sessionId: "pty-a" },
        { type: "leaf", paneId: "pane-b", sessionId: "pty-b" },
      ],
    };
    const secondLayout: TerminalPaneNode = {
      type: "leaf",
      paneId: "pane-c",
      sessionId: "pty-c",
    };
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-a",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "A",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-b",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "B",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-c",
          agent: "shell",
          workspaceId: "workspace:/other",
          title: "C",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      workspaces: [
        { id: "workspace:/repo", path: "/repo", name: "repo" },
        { id: "workspace:/other", path: "/other", name: "other" },
      ],
      workspaceTabs: [
        {
          id: "tab-1",
          workspaceId: "workspace:/repo",
          title: "One",
          terminalLayout: layout,
          activeTerminalPaneId: "pane-a",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
        {
          id: "tab-2",
          workspaceId: "workspace:/repo",
          title: "Two",
          terminalLayout: secondLayout,
          activeTerminalPaneId: "pane-c",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
        {
          id: "tab-3",
          workspaceId: "workspace:/other",
          title: "Other",
          terminalLayout: secondLayout,
          activeTerminalPaneId: "pane-c",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "tab-1",
      terminalLayout: layout,
      activeTerminalPaneId: "pane-a",
      activeSessionId: "pty-a",
    });

    useSessionStore.getState().focusTerminalPaneIndex(2);
    expect(useSessionStore.getState().activeTerminalPaneId).toBe("pane-b");
    expect(useSessionStore.getState().activeSessionId).toBe("pty-b");

    useSessionStore.getState().focusWorkspaceTabIndex(2);
    expect(useSessionStore.getState().activeWorkspaceTabId).toBe("tab-2");
    expect(useSessionStore.getState().activeTerminalPaneId).toBe("pane-c");

    useSessionStore.getState().focusWorkspaceIndex(2);
    expect(useSessionStore.getState().activeWorkspaceId).toBe("workspace:/other");
    expect(useSessionStore.getState().activeWorkspaceTabId).toBe("tab-3");
  });

  test("round-trips custom command keybindings through TOML", async () => {
    const parsed = parseOnibiConfigToml(`
version = 1

[settings]
show_terminal_pane_agent_labels = false
terminal_shell_mode = "login"

[keybindings]
prefix = "ctrl+a"

[[keybindings.app]]
keys = "prefix+1"
action = "workspace.focusIndex1"

[[keybindings.app]]
keys = "prefix+g"
action = "session.navigator.open"

[[keybindings.command]]
keys = "prefix+t"
description = "Run tests"
command = "pnpm test"
`);

    expect(parsed.settings.keybindingPrefix).toBe("ctrl+a");
    expect(parsed.settings.showTerminalPaneAgentLabels).toBe(false);
    expect(parsed.settings.terminalShellMode).toBe("login");
    expect(parsed.settings.appKeybindings).toContainEqual({
      keys: "prefix+1",
      action: "workspace.focusIndex1",
    });
    expect(parsed.settings.appKeybindings).toContainEqual({
      keys: "prefix+g",
      action: "session.navigator.open",
    });
    expect(parsed.settings.customCommandKeybindings).toEqual([
      { keys: "prefix+t", description: "Run tests", command: "pnpm test" },
    ]);

    const serialized = serializeOnibiConfigToml({
      version: 1,
      settings: parsed.settings,
      workspaces: [],
    });
    expect(serialized).toContain("[[keybindings.command]]");
    expect(serialized).toContain('terminal_shell_mode = "login"');
    expect(serialized).toContain("show_terminal_pane_agent_labels = false");
    expect(serialized).toContain('description = "Run tests"');
    expect(serialized).toContain('command = "pnpm test"');
  });

  test("supports terminal theme mode in options and TOML", async () => {
    expect(COLOR_SCHEME_OPTIONS).toContainEqual({
      id: "terminal",
      label: "Terminal",
    });

    const parsed = parseOnibiConfigToml(`
version = 1

[settings]
theme = "terminal"
`);
    expect(parsed.settings.theme).toBe("terminal");

    const serialized = serializeOnibiConfigToml({
      version: 1,
      settings: parsed.settings,
      workspaces: [],
    });
    expect(serialized).toContain('theme = "terminal"');
  });

  test("terminal theme uses imported terminal colors when available", async () => {
    const theme = terminalThemeForSettings({
      ...DEFAULT_SETTINGS,
      theme: "terminal",
      ghosttyTheme: {
        background: "#101010",
        foreground: "#eeeeee",
        palette: { 4: "#335577" },
      },
    });

    expect(theme).toMatchObject({
      background: "#101010",
      foreground: "#eeeeee",
      cursor: "#eeeeee",
      selectionBackground: "#335577",
    });
  });

  test("reports conflicts between app and custom command keybindings", async () => {
    const conflicts = appKeybindingConflicts(
      [{ keys: "prefix+t", action: "workspace.tab.focusIndex1" }],
      [{ keys: "prefix+t", command: "pnpm test", description: "Run tests" }],
    );

    expect(conflicts).toEqual([
      {
        keys: "prefix+t",
        actions: ["workspace.tab.focusIndex1"],
        commands: ["Run tests"],
      },
    ]);
  });

  test("default app keybindings include terminal copy mode", () => {
    expect(DEFAULT_SETTINGS.appKeybindings).toEqual(
      expect.arrayContaining([
        { keys: "prefix+[", action: "terminal.copyMode.enter" },
      ]),
    );
    expect(APP_KEYBINDING_ACTION_LABELS["terminal.copyMode.enter"]).toBe(
      "Enter terminal copy mode",
    );
  });
});
