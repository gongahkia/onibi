import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { SourceControlView } from "./SourceControlView";
import type { GitStatus } from "../lib/git";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

const workspace = { id: "workspace:/repo", path: "/repo", name: "repo" };

const status: GitStatus = {
  isRepo: true,
  repoRoot: "/repo",
  branch: "main",
  upstream: "origin/main",
  ahead: 1,
  behind: 2,
  entries: [
    {
      path: "README.md",
      fullPath: "/repo/README.md",
      indexStatus: null,
      worktreeStatus: "M",
    },
    {
      path: "src/index.ts",
      fullPath: "/repo/src/index.ts",
      indexStatus: "A",
      worktreeStatus: null,
    },
  ],
};

describe("SourceControlView", () => {
  beforeEach(() => {
    globalThis.__TAURI_MOCKS__.invoke.mockReset();
    vi.mocked(window.confirm).mockReturnValue(true);
    vi.mocked(window.prompt).mockReset();
    vi.mocked(window.prompt).mockReturnValue(null);
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
  });

  test("renders repo state and stages changed paths", async () => {
    const onRefresh = vi.fn(async () => undefined);
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue("");

    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByText("main")).toBeTruthy();
    expect(screen.queryByText(/Sync/)).toBeNull();
    expect(screen.getByText("README.md")).toBeTruthy();

    fireEvent.click(screen.getByLabelText("Stage README.md"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "git_stage_paths",
        {
          root: "/repo",
          paths: ["README.md"],
        },
      );
    });
    expect(onRefresh).toHaveBeenCalled();
  });

  test("commits staged changes with a message", async () => {
    const onRefresh = vi.fn(async () => undefined);
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue("");

    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={onRefresh}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("Message (main)"), {
      target: { value: "Add source view" },
    });
    fireEvent.click(screen.getByText("Commit"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("git_commit", {
        root: "/repo",
        message: "Add source view",
      });
    });
  });

  test("opens clicked changed files as git diffs", () => {
    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={vi.fn(async () => undefined)}
      />,
    );

    fireEvent.click(screen.getByText("README.md"));

    expect(useSessionStore.getState().selectedFile).toMatchObject({
      type: "git-diff",
      workspaceRoot: "/repo",
      path: "README.md",
      stage: "working",
    });
  });

  test("creates a worktree and opens it as a workspace", async () => {
    vi.mocked(window.prompt)
      .mockReturnValueOnce("feature/mobile")
      .mockReturnValueOnce("/repo-feature-mobile");
    const onRefresh = vi.fn(async () => undefined);
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "git_worktrees") {
        return [
          {
            path: "/repo",
            branch: "main",
            head: "abc",
            detached: false,
            bare: false,
            prunable: false,
          },
        ];
      }
      if (command === "git_create_worktree") {
        return {
          path: "/repo-feature-mobile",
          branch: "feature/mobile",
          head: "def",
          detached: false,
          bare: false,
          prunable: false,
        };
      }
      if (command === "fs_workspace_info") {
        return { path: "/repo-feature-mobile", name: "repo-feature-mobile" };
      }
      return "";
    });

    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={onRefresh}
      />,
    );

    const createButton = await screen.findByLabelText("Create worktree");
    await waitFor(() =>
      expect((createButton as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(createButton);

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "git_create_worktree",
        {
          root: "/repo",
          branch: "feature/mobile",
          path: "/repo-feature-mobile",
          base: "main",
        },
      );
    });
    expect(useSessionStore.getState().workspaces).toContainEqual({
      id: "workspace:/repo-feature-mobile",
      path: "/repo-feature-mobile",
      name: "repo-feature-mobile",
    });
  });
});
