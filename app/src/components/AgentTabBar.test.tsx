import {
  createEvent,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AgentTabBar } from "./AgentTabBar";
import { DEFAULT_SETTINGS, type Session, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    activeTerminalPaneId: null,
    terminalLayout: null,
    workspaces: [],
    selectedFile: null,
    sessionEvents: [],
    settings: DEFAULT_SETTINGS,
  });
}

describe("AgentTabBar", () => {
  beforeEach(() => {
    resetStore();
    vi.mocked(navigator.clipboard.writeText).mockClear();
    globalThis.__TAURI_MOCKS__.invoke.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("renders sessions and flashes pending approvals", () => {
    const sessions: Session[] = [
      {
        id: "pty-1",
        agent: "codex",
        workspaceId: "workspace:/repo",
        title: "Codex",
        status: "awaiting-approval",
        createdAt: 1,
        pendingApprovals: ["approval-1"],
      },
    ];
    useSessionStore.setState({
      sessions,
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<AgentTabBar orientation="vertical" />);

    expect(screen.getByLabelText("Codex session").className).toContain("flash");
  });

  test("activates a tab on click", () => {
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
        {
          id: "pty-2",
          agent: "gemini",
          workspaceId: "workspace:/repo",
          title: "Gemini",
          status: "idle",
          createdAt: 2,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<AgentTabBar orientation="horizontal" />);
    fireEvent.click(screen.getByLabelText("Gemini session"));

    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
  });

  test("opens the activity center from the rail action", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<AgentTabBar orientation="vertical" />);
    fireEvent.click(screen.getByLabelText("Activity"));

    expect(screen.getByRole("dialog", { name: "Activity" })).toBeTruthy();
    expect(screen.getByText("Codex · Running")).toBeTruthy();
  });

  test("opens a workflow context menu on right click", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<AgentTabBar orientation="vertical" />);
    const tab = screen.getByLabelText("Codex session");
    const event = createEvent.contextMenu(tab, {
      clientX: 120,
      clientY: 80,
    });
    fireEvent(tab, event);

    expect(event.defaultPrevented).toBe(true);
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(screen.getByText("Open Workspace Diff")).toBeTruthy();
    expect(screen.getByText("Use Unified Diff")).toBeTruthy();
  });

  test("opens the first workflow workspace change as a git diff", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce({
      isRepo: true,
      branch: "main",
      upstream: "origin/main",
      ahead: 0,
      behind: 0,
      entries: [
        {
          path: "README.md",
          fullPath: "/repo/README.md",
          indexStatus: null,
          worktreeStatus: "M",
        },
      ],
    });

    render(<AgentTabBar orientation="horizontal" />);
    fireEvent.contextMenu(screen.getByLabelText("Codex session"));
    fireEvent.click(screen.getByText("Open Workspace Diff"));

    await waitFor(() => {
      expect(useSessionStore.getState().selectedFile).toMatchObject({
        type: "git-diff",
        workspaceId: "workspace:/repo",
        workspaceRoot: "/repo",
        path: "README.md",
        stage: "working",
      });
    });
  });
});
