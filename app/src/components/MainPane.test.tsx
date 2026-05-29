import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { MainPane } from "./MainPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

vi.mock("./TerminalView", () => ({
  TerminalView: ({ ptyId, visible }: { ptyId: string; visible?: boolean }) => (
    <div data-testid="terminal-view" data-visible={String(visible)}>
      {ptyId}
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
  vi.mocked(window.confirm).mockReset();
  vi.mocked(window.confirm).mockReturnValue(true);
}

describe("MainPane", () => {
  beforeEach(() => {
    resetStore();
  });

  test("chooses editor when a file is selected", () => {
    useSessionStore.setState({
      selectedFile: {
        workspaceId: "workspace:/repo",
        workspaceRoot: "/repo",
        path: "/repo/a.txt",
        name: "a.txt",
        size: 1,
      },
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
    expect(screen.getByText("/repo")).toBeTruthy();
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
        workspaceId: "workspace:/repo",
        workspaceRoot: "/repo",
        path: "/repo/a.txt",
        name: "a.txt",
        size: 1,
      },
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
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-2",
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
    fireEvent.click(screen.getByLabelText("Duplicate session"));

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
});
