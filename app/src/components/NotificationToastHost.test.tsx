import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { NotificationToastHost } from "./NotificationToastHost";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

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
}

describe("NotificationToastHost", () => {
  beforeEach(() => {
    resetStore();
  });

  test("routes pty notifications into in-app toasts", async () => {
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
    });
    render(<NotificationToastHost />);

    window.dispatchEvent(
      new CustomEvent("onibi:pty-notification", {
        detail: {
          ptyId: "pty-1",
          title: "Build ready",
          body: "localhost:5173",
        },
      }),
    );

    await waitFor(() => expect(screen.getByText("Build ready")).toBeTruthy());
    expect(screen.getByText("localhost:5173")).toBeTruthy();
  });
});
