import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";
import { StatusBar } from "./StatusBar";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    activeSidebarView: "files",
    rightDockView: "files",
    rightDockMode: "expanded",
    sidebarCollapsed: true,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
}

describe("StatusBar", () => {
  beforeEach(() => {
    resetStore();
  });

  test("collapses to an idle edge when there are no approvals", () => {
    render(<StatusBar />);

    const status = screen.getByRole("contentinfo", { name: "Status" });
    expect(status.classList.contains("idle")).toBe(true);
    expect(screen.queryByText(/pending/i)).toBeNull();
  });

  test("opens approvals when pending approvals need attention", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "awaiting-approval",
          createdAt: 1,
          pendingApprovals: ["approval-1"],
        },
      ],
    });

    render(<StatusBar />);
    fireEvent.click(screen.getByRole("button", { name: /1 pending/i }));

    expect(useSessionStore.getState().activeSidebarView).toBe("approvals");
    expect(useSessionStore.getState().rightDockMode).toBe("compressed");
    expect(useSessionStore.getState().sidebarCollapsed).toBe(false);
  });
});
