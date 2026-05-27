import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { MainPane } from "./MainPane";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

vi.mock("./TerminalView", () => ({
  TerminalView: ({ ptyId }: { ptyId: string }) => (
    <div data-testid="terminal-view">{ptyId}</div>
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
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
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
    });

    render(<MainPane />);

    expect(screen.getByTestId("terminal-view").textContent).toContain("pty-1");
  });
});
