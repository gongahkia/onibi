import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AgentTabBar } from "./AgentTabBar";
import { DEFAULT_SETTINGS, type Session, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
}

describe("AgentTabBar", () => {
  beforeEach(() => {
    resetStore();
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
});
