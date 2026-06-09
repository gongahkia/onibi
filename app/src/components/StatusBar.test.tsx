import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
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

  test("dispatches focus-approval-banner when pending approvals need attention", () => {
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
    const listener = vi.fn();
    window.addEventListener("onibi:focus-approval-banner", listener);

    try {
      render(<StatusBar />);
      fireEvent.click(screen.getByRole("button", { name: /1 pending/i }));
      expect(listener).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener("onibi:focus-approval-banner", listener);
    }
  });
});
