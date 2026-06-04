import { load } from "@tauri-apps/plugin-store";
import { beforeEach, describe, expect, test } from "vitest";
import {
  DEFAULT_SETTINGS,
  hydrateSessionStore,
  normalizeNewPaneCwdMode,
  resolveNewPaneCwd,
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
});
