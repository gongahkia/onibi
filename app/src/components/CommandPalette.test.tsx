import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { CommandPalette } from "./CommandPalette";
import { DEFAULT_SETTINGS, type Session, useSessionStore } from "../lib/sessions";

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
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue([]);
  vi.mocked(window.confirm).mockReset();
  vi.mocked(window.confirm).mockReturnValue(true);
  vi.mocked(window.prompt).mockReset();
  vi.mocked(window.prompt).mockReturnValue(null);
}

describe("CommandPalette", () => {
  beforeEach(() => {
    resetStore();
    vi.clearAllMocks();
  });

  test("opens with ctrl+p and executes a settings command", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "theme light" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().settings.theme).toBe("vscode-light-plus");
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });

  test("switches to a matching session command", () => {
    const sessions: Session[] = [
      {
        id: "pty-1",
        agent: "shell",
        workspaceId: "workspace:/repo",
        title: "Shell",
        status: "running",
        createdAt: 1,
        pendingApprovals: [],
      },
      {
        id: "pty-2",
        agent: "gemini",
        workspaceId: "workspace:/repo",
        title: "Gemini",
        status: "running",
        createdAt: 2,
        pendingApprovals: [],
      },
    ];
    useSessionStore.setState({
      sessions,
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", metaKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "switch gemini" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
  });

  test("supports keyboard navigation and escape", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Escape" });

    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });

  test("focuses attention items and clears active attention", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-ok",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
        },
        {
          id: "pty-attention",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
          lastTrigger: {
            id: "tests-failed",
            label: "Tests failed",
            pattern: "failed",
            line: "tests failed",
            actions: ["badge"],
            timestamp: Date.now(),
          },
        },
      ],
      activeSessionId: "pty-ok",
      terminalLayout: {
        type: "split",
        paneId: "split-1",
        direction: "vertical",
        children: [
          { type: "leaf", paneId: "pane-ok", sessionId: "pty-ok" },
          { type: "leaf", paneId: "pane-attention", sessionId: "pty-attention" },
        ],
      },
      activeTerminalPaneId: "pane-ok",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    let input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "focus next attention" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSessionId).toBe("pty-attention");

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "clear active session attention" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().sessions[1].lastTrigger).toBeNull();
  });

  test("switches the sidebar to workspace search from the palette", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "workspace search" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSidebarView).toBe("search");
  });

  test("opens the quick workspace launcher", () => {
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
          cwd: "/repo/pkg",
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "quick workspace launcher" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(screen.getByRole("dialog", { name: "New Session" })).toBeTruthy();
  });

  test("saves and restores terminal arrangements from the palette", async () => {
    vi.mocked(window.prompt).mockReturnValue("Pairing layout");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-restored";
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

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    let input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "save arrangement" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().arrangements[0]?.name).toBe("Pairing layout");

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "restore pairing" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-restored");
    });
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-restored",
    });
  });
});
