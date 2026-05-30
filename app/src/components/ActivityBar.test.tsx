import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { ActivityBar } from "./ActivityBar";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [
      {
        id: "pty-1",
        agent: "codex",
        workspaceId: "workspace:/repo",
        title: "Codex",
        status: "running",
        createdAt: Date.now(),
        pendingApprovals: [],
        cwd: "/repo",
      },
      {
        id: "pty-2",
        agent: "gemini",
        workspaceId: "workspace:/other",
        title: "Gemini",
        status: "running",
        createdAt: Date.now(),
        pendingApprovals: [],
        cwd: "/other",
      },
    ],
    activeSessionId: "pty-1",
    activeTerminalPaneId: null,
    terminalLayout: null,
    workspaces: [
      { id: "workspace:/repo", path: "/repo", name: "repo" },
      { id: "workspace:/other", path: "/other", name: "other" },
    ],
    selectedFile: null,
    sessionEvents: [],
    commandBlocks: [],
    activeCommandBlocks: {},
    settings: DEFAULT_SETTINGS,
  });
}

describe("ActivityBar", () => {
  beforeEach(() => {
    resetStore();
  });

  test("opens workspace-scoped activity and no longer owns settings", () => {
    render(<ActivityBar sidebarCollapsed={false} onToggleSidebar={vi.fn()} />);

    expect(screen.queryByLabelText("Settings")).toBeNull();
    fireEvent.click(screen.getByLabelText("Activity center"));

    expect(screen.getByRole("dialog", { name: "Activity" })).toBeTruthy();
    expect(screen.getByText("Codex · Running")).toBeTruthy();
    expect(screen.queryByText("Gemini · Running")).toBeNull();
  });

  test("terminal button returns focus to the active session", () => {
    useSessionStore.setState({
      selectedFile: {
        type: "file",
        workspaceId: "workspace:/repo",
        workspaceRoot: "/repo",
        path: "/repo/a.ts",
        name: "a.ts",
        size: 1,
      },
    });

    render(<ActivityBar sidebarCollapsed={false} onToggleSidebar={vi.fn()} />);
    fireEvent.click(screen.getByLabelText("Terminal"));

    expect(useSessionStore.getState().selectedFile).toBeNull();
    expect(useSessionStore.getState().activeSessionId).toBe("pty-1");
  });
});
