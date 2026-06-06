import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { MainPane } from "./MainPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

vi.mock("./TerminalView", () => ({
  TerminalView: ({
    ptyId,
    visible,
    onUnavailable,
  }: {
    ptyId: string;
    visible?: boolean;
    onUnavailable?: () => void;
  }) => (
    <div data-testid="terminal-view" data-visible={String(visible)}>
      {ptyId}
      <button
        type="button"
        aria-label={`detach ${ptyId}`}
        onClick={() => onUnavailable?.()}
      />
    </div>
  ),
}));

vi.mock("./EditorBuffer", () => ({
  EditorBuffer: ({ path }: { path: string }) => (
    <div data-testid="editor-buffer">{path}</div>
  ),
}));

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
  vi.mocked(navigator.clipboard.writeText).mockClear();
  vi.mocked(window.confirm).mockReset();
  vi.mocked(window.confirm).mockReturnValue(true);
}

describe("MainPane", () => {
  beforeEach(() => {
    resetStore();
  });

  test("chooses editor when a file is selected", () => {
    const selection = {
      type: "file" as const,
      workspaceId: "workspace:/repo",
      workspaceRoot: "/repo",
      path: "/repo/a.txt",
      name: "a.txt",
      size: 1,
    };
    useSessionStore.setState({
      selectedFile: selection,
      openBuffers: [selection],
      activeBufferKey: `file:workspace:/repo:/repo/a.txt`,
    });

    render(<MainPane />);

    expect(screen.getByTestId("editor-buffer").textContent).toContain("/repo/a.txt");
  });

  test("chooses terminal when only a session is active", () => {
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
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      maximizedTerminalPaneId: null,
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);

    expect(screen.getByTestId("terminal-view").textContent).toContain("pty-1");
    expect(screen.getByTestId("terminal-view").getAttribute("data-visible")).toBe(
      "true",
    );
  });

  test("shows pane agent labels and a right-click context menu", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex work",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      maximizedTerminalPaneId: null,
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);

    expect(screen.getByTitle("Codex · Codex work")).toBeTruthy();
    const pane = screen.getByTestId("terminal-view").closest(".terminal-pane");
    expect(pane).toBeTruthy();
    fireEvent.contextMenu(pane!);

    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.click(screen.getByRole("menuitem", { name: "Copy Pane ID" }));

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("pane-1");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  test("renders an empty active workspace tab", () => {
    useSessionStore.setState({
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      workspaceTabs: [
        {
          id: "workspace-tab-1",
          workspaceId: "workspace:/repo",
          title: "Terminal",
          terminalLayout: null,
          activeTerminalPaneId: null,
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "workspace-tab-1",
    });

    render(<MainPane />);

    expect(screen.getByRole("tab", { name: /Terminal/ })).toBeTruthy();
    expect(screen.getByText("empty")).toBeTruthy();
    expect(screen.getByTestId("inline-session-launcher")).toBeTruthy();
  });

  test("switches workspace terminal tabs and mirrors the active layout", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell one",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-2",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex two",
          status: "running",
          createdAt: 2,
          pendingApprovals: [],
        },
      ],
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      workspaceTabs: [
        {
          id: "workspace-tab-1",
          workspaceId: "workspace:/repo",
          title: "One",
          terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
          activeTerminalPaneId: "pane-1",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
        {
          id: "workspace-tab-2",
          workspaceId: "workspace:/repo",
          title: "Two",
          terminalLayout: { type: "leaf", paneId: "pane-2", sessionId: "pty-2" },
          activeTerminalPaneId: "pane-2",
          maximizedTerminalPaneId: null,
          createdAt: 2,
          updatedAt: 2,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "workspace-tab-1",
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
    });

    render(<MainPane />);
    fireEvent.click(screen.getByRole("tab", { name: /Two/ }));

    expect(screen.getByTestId("terminal-view").textContent).toContain("pty-2");
    expect(useSessionStore.getState().activeWorkspaceTabId).toBe("workspace-tab-2");
    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-2",
      sessionId: "pty-2",
    });
  });

  test("auto-restarts stale saved sessions with restart metadata", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-restored";
      }
      return null;
    });
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-stale",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "stale",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
          restart: {
            command: "codex",
            args: [],
            cwd: "/repo",
            env: [],
          },
        },
      ],
      activeSessionId: "pty-stale",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-stale" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-restored");
    });
    expect(screen.getByTestId("terminal-view").textContent).toContain("pty-restored");
    expect(screen.queryByText("Session is stale")).toBeNull();
  });

  test("marks missing running ptys stale so they restart instead of blanking", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-restored";
      }
      return null;
    });
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-missing",
          agent: "opencode",
          workspaceId: "workspace:/repo",
          title: "OpenCode",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
          restart: {
            command: "opencode",
            args: [],
            cwd: "/repo",
            env: [],
          },
        },
      ],
      activeSessionId: "pty-missing",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-missing" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);
    fireEvent.click(screen.getByLabelText("detach pty-missing"));

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-restored");
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
      req: {
        command: "opencode",
        args: [],
        cwd: "/repo",
        env: [],
        rows: 30,
        cols: 100,
        agent: "opencode",
        workspaceId: "workspace:/repo",
        title: "OpenCode",
      },
    });
  });

  test("opens the new-session picker for a split shortcut", () => {
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
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      maximizedTerminalPaneId: null,
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);
    fireEvent.keyDown(window, { key: "d", metaKey: true });

    expect(screen.getByRole("dialog", { name: "New Session" })).toBeTruthy();
    expect(screen.getAllByText("/repo").length).toBeGreaterThan(0);
  });

  test("keeps the active terminal mounted behind a selected file", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "claude-code",
          workspaceId: "workspace:/repo",
          title: "Claude",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      selectedFile: {
        type: "file",
        workspaceId: "workspace:/repo",
        workspaceRoot: "/repo",
        path: "/repo/a.txt",
        name: "a.txt",
        size: 1,
      },
      openBuffers: [
        {
          type: "file",
          workspaceId: "workspace:/repo",
          workspaceRoot: "/repo",
          path: "/repo/a.txt",
          name: "a.txt",
          size: 1,
        },
      ],
      activeBufferKey: "file:workspace:/repo:/repo/a.txt",
    });

    render(<MainPane />);

    expect(screen.getByTestId("editor-buffer").textContent).toContain("/repo/a.txt");
    expect(screen.getByTestId("terminal-view").textContent).toContain("pty-1");
    expect(screen.getByTestId("terminal-view").getAttribute("data-visible")).toBe(
      "false",
    );
  });

  test("restarts the active pane from the toolbar", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-2";
      }
      return null;
    });
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
          cwd: "/repo",
          restart: {
            command: "",
            args: [],
            cwd: "/repo",
            env: [["ONIBI_SHELL_INTEGRATION", "1"]],
            shellMode: "login",
          },
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<MainPane />);
    fireEvent.click(screen.getByLabelText("Restart session"));

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
      req: expect.objectContaining({
        command: "",
        shellMode: "login",
      }),
    });
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-2",
      sessionIds: ["pty-2"],
    });
  });

  test("duplicates and closes sessions from the toolbar", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-copy";
      }
      return null;
    });
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
          cwd: "/repo",
          restart: {
            command: "",
            args: [],
            cwd: "/repo",
            env: [["ONIBI_SHELL_INTEGRATION", "1"]],
            shellMode: "login",
          },
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      settings: { ...DEFAULT_SETTINGS, terminalConfirmClose: false },
    });

    render(<MainPane />);
    fireEvent.click(screen.getByLabelText("Split right"));

    await waitFor(() => {
      expect(useSessionStore.getState().sessions.map((session) => session.id)).toContain(
        "pty-copy",
      );
    });
    fireEvent.click(screen.getAllByLabelText("Close session")[0]);

    await waitFor(() => {
      expect(useSessionStore.getState().sessions.map((session) => session.id)).not.toContain(
        "pty-1",
      );
    });
  });

  test("hands off the active workspace to another agent from the toolbar", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-codex";
      }
      return null;
    });
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "claude-code",
          workspaceId: "workspace:/repo",
          title: "Claude · repo",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo/packages/app",
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      sessionEvents: [
        {
          id: "event-1",
          timestamp: 1,
          type: "session-started",
          workspaceId: "workspace:/repo",
          sessionId: "pty-1",
          agent: "claude-code",
          summary: "Started Claude",
        },
      ],
    });

    render(<MainPane />);
    fireEvent.click(screen.getByLabelText("Handoff session to another agent"));
    const dialog = screen.getByRole("dialog", { name: "Handoff Session" });
    expect(dialog).toBeTruthy();
    expect(dialog.parentElement?.classList.contains("handoff-backdrop")).toBe(true);
    expect((screen.getByLabelText("Next agent") as HTMLSelectElement).value).toBe(
      "codex",
    );
    fireEvent.click(screen.getByText("Start Handoff"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
        req: {
          command: "codex",
          args: [
            expect.stringContaining(
              "You are taking over an Onibi workspace from Claude Code.",
            ),
          ],
          cwd: "/repo/packages/app",
          env: [],
          rows: 30,
          cols: 100,
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex · repo",
        },
      });
    });
    expect(useSessionStore.getState().activeSessionId).toBe("pty-codex");
    expect(useSessionStore.getState().activeTerminalPaneId).toBe("pane-1");
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-codex",
      sessionIds: ["pty-1", "pty-codex"],
    });
  });
});
