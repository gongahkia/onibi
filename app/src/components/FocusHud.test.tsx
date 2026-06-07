import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { getGitStatus } from "../lib/git";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";
import { FocusHud } from "./FocusHud";

vi.mock("../lib/git", () => ({
  getGitStatus: vi.fn(async () => ({
    isRepo: false,
    branch: null,
    upstream: null,
    ahead: 0,
    behind: 0,
    entries: [],
  })),
}));

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
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
      },
    ],
    activeSessionId: "pty-1",
    workspaceTabs: [],
    activeWorkspaceId: "workspace:/repo",
    activeWorkspaceTabId: null,
    workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
}

describe("FocusHud", () => {
  beforeEach(() => {
    resetStore();
    vi.mocked(getGitStatus).mockClear();
  });

  test("stays quiet on initial context and flashes on active session changes", async () => {
    render(<FocusHud />);

    expect(screen.queryByRole("status")).toBeNull();
    await waitFor(() => expect(getGitStatus).toHaveBeenCalledWith("/repo"));

    act(() => {
      useSessionStore.setState((state) => ({
        sessions: state.sessions.map((session) =>
          session.id === "pty-1"
            ? { ...session, cwd: "/repo/packages/app", lastExitCode: 2 }
            : session,
        ),
      }));
    });

    const status = await screen.findByRole("status");
    expect(status.textContent).toContain("app");
    expect(status.textContent).toContain("exit 2");
  });
});
