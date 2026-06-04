import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { DEFAULT_SETTINGS, useSessionStore, type CommandBlock } from "../lib/sessions";
import { SessionHistoryView } from "./SessionHistoryView";

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
        lastExitCode: null,
        lastTrigger: null,
      },
    ],
    activeSessionId: "pty-1",
    workspaceTabs: [],
    activeWorkspaceId: "workspace:/repo",
    activeWorkspaceTabId: null,
    terminalLayout: null,
    activeTerminalPaneId: null,
    maximizedTerminalPaneId: null,
    arrangements: [],
    activeSidebarView: "files",
    workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    selectedFile: null,
    sessionEvents: [],
    commandBlocks: [],
    activeCommandBlocks: {},
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue(undefined);
}

function block(patch: Partial<CommandBlock> = {}): CommandBlock {
  return {
    id: "cmd-1",
    sessionId: "pty-1",
    workspaceId: "workspace:/repo",
    agent: "codex",
    command: "pnpm test",
    cwd: "/repo",
    startedAt: 1_700_000_000_000,
    endedAt: 1_700_000_001_000,
    exitCode: 1,
    status: "failed",
    outputPreview: "1 test failed",
    previewUrl: "http://localhost:1420/",
    changedFiles: ["src/App.tsx"],
    attention: "failed",
    source: "shell-integration",
    ...patch,
  };
}

describe("SessionHistoryView", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders searchable command blocks with actions", () => {
    useSessionStore.setState({ commandBlocks: [block()] });

    render(<SessionHistoryView />);

    expect(screen.getByText("pnpm test")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Search command timeline"), {
      target: { value: "failed" },
    });
    expect(screen.getByText("1 test failed")).toBeTruthy();

    fireEvent.click(screen.getByText("Rerun"));
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_write", {
      id: "pty-1",
      data: Array.from(new TextEncoder().encode("pnpm test\n")),
    });
  });

  test("combines current sessions with command history", () => {
    useSessionStore.setState({ commandBlocks: [block()] });

    render(<SessionHistoryView />);

    expect(screen.getByText("Codex · Running")).toBeTruthy();
    expect(screen.getByText("pnpm test")).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Current" }));
    expect(screen.getByText("Codex · Running")).toBeTruthy();
    expect(screen.queryByText("pnpm test")).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: "Commands" }));
    expect(screen.queryByText("Codex · Running")).toBeNull();
    expect(screen.getByText("pnpm test")).toBeTruthy();
  });

  test("opens changed files from command blocks", () => {
    useSessionStore.setState({ commandBlocks: [block()] });

    render(<SessionHistoryView />);
    fireEvent.click(screen.getByText("src/App.tsx"));

    expect(useSessionStore.getState().selectedFile?.path).toBe("/repo/src/App.tsx");
  });

  test("shows retained session chat logs", () => {
    useSessionStore.setState((state) => ({
      sessions: state.sessions.map((session) => ({
        ...session,
        transcript: {
          text: "Agent: inspected the repository\nUser: run the tests\n",
          updatedAt: 1_700_000_002_000,
        },
      })),
    }));

    render(<SessionHistoryView />);

    expect(screen.getByText("Codex · Chat Log")).toBeTruthy();
    expect(screen.getByText(/inspected the repository/)).toBeTruthy();
  });

  test("sanitizes terminal control noise from retained chat logs", () => {
    useSessionStore.setState((state) => ({
      sessions: state.sessions.map((session) => ({
        ...session,
        transcript: {
          text: "\u001b[31mClaude\u001b[0m\r\n\ue0b6████\ue0b4\n──────\nTry `fix`\n",
          updatedAt: 1_700_000_002_000,
        },
      })),
    }));

    render(<SessionHistoryView />);

    const output = screen.getByText(/Claude/);
    expect(output.textContent).toContain("Try `fix`");
    expect(output.textContent).not.toContain("\u001b");
    expect(output.textContent).not.toContain("\ue0b6");
    const copyLogButtons = screen.getAllByText("Copy Log");
    fireEvent.click(copyLogButtons[copyLogButtons.length - 1]);
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("Claude\nTry `fix`");
  });

  test("can scope activity to one workspace", () => {
    useSessionStore.setState((state) => ({
      workspaces: [
        ...state.workspaces,
        { id: "workspace:/other", path: "/other", name: "other" },
      ],
      sessions: [
        ...state.sessions,
        {
          id: "pty-2",
          agent: "gemini",
          workspaceId: "workspace:/other",
          title: "Gemini",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
          cwd: "/other",
          lastExitCode: null,
          lastTrigger: null,
        },
      ],
      commandBlocks: [
        block(),
        block({
          id: "cmd-2",
          sessionId: "pty-2",
          workspaceId: "workspace:/other",
          agent: "gemini",
          command: "npm build",
          cwd: "/other",
          outputPreview: "other output",
        }),
      ],
      sessionEvents: [
        {
          id: "event-1",
          timestamp: 1_700_000_003_000,
          type: "file-opened",
          workspaceId: "workspace:/other",
          sessionId: "pty-2",
          agent: "gemini",
          summary: "opened other file",
        },
      ],
    }));

    render(<SessionHistoryView workspaceId="workspace:/repo" />);

    expect(screen.getByText("Codex · Running")).toBeTruthy();
    expect(screen.getByText("pnpm test")).toBeTruthy();
    expect(screen.queryByText("Gemini · Running")).toBeNull();
    expect(screen.queryByText("npm build")).toBeNull();
    expect(screen.queryByText("opened other file")).toBeNull();
  });
});
