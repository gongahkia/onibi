import { load } from "@tauri-apps/plugin-store";
import { beforeEach, describe, expect, test } from "vitest";
import {
  DEFAULT_SETTINGS,
  hydrateSessionStore,
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
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
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
});
